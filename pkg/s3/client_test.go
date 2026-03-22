package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// ---- helpers ----------------------------------------------------------------

// noopBackoff replaces defaultBackoff so retry tests don't actually sleep.
func noopBackoff(_ context.Context, _ int) error { return nil }

// makeSDKError builds a smithyhttp.ResponseError with the given HTTP status code,
// mimicking what the AWS SDK returns for HTTP-level failures.
func makeSDKError(statusCode int) error {
	return &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: statusCode},
		},
		Err: fmt.Errorf("HTTP %d", statusCode),
	}
}

// mockS3 is a fake s3API that returns pre-programmed responses in order.
type mockS3 struct {
	responses []mockS3Response
	callCount int32
}

type mockS3Response struct {
	data []byte
	err  error
}

func (m *mockS3) GetObject(_ context.Context, _ *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	idx := int(atomic.AddInt32(&m.callCount, 1)) - 1
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("mockS3: unexpected call #%d", idx+1)
	}
	r := m.responses[idx]
	if r.err != nil {
		return nil, r.err
	}
	return &awss3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(r.data))}, nil
}

// newTestClient returns a Client wired with a mock S3, a plain http.Client,
// and a no-op backoff so retry tests don't sleep.
func newTestClient(mock *mockS3) *Client {
	return newClientForTest(mock, &http.Client{Timeout: 5 * time.Second}, noopBackoff)
}

// ---- URL parsing ------------------------------------------------------------

func TestParseS3URL(t *testing.T) {
	tests := []struct {
		name       string
		rawURL     string
		wantBucket string
		wantKey    string
		wantErr    bool
	}{
		// s3:// scheme
		{
			name:       "s3 scheme simple",
			rawURL:     "s3://my-bucket/my-key",
			wantBucket: "my-bucket",
			wantKey:    "my-key",
		},
		{
			name:       "s3 scheme nested key",
			rawURL:     "s3://my-bucket/path/to/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "path/to/model.bin",
		},
		{
			name:    "s3 scheme missing key",
			rawURL:  "s3://my-bucket",
			wantErr: true,
		},
		{
			name:    "s3 scheme missing bucket",
			rawURL:  "s3:///key",
			wantErr: true,
		},
		{
			name:    "s3 scheme empty key after slash",
			rawURL:  "s3://my-bucket/",
			wantErr: true,
		},

		// virtual-hosted style (no region)
		{
			name:       "virtual-hosted no region",
			rawURL:     "https://my-bucket.s3.amazonaws.com/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "model.bin",
		},
		{
			name:       "virtual-hosted no region nested key",
			rawURL:     "https://my-bucket.s3.amazonaws.com/path/to/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "path/to/model.bin",
		},

		// virtual-hosted style (with region)
		{
			name:       "virtual-hosted with region",
			rawURL:     "https://my-bucket.s3.us-east-1.amazonaws.com/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "model.bin",
		},
		{
			name:       "virtual-hosted us-west-2",
			rawURL:     "https://my-bucket.s3.us-west-2.amazonaws.com/path/to/weights.pt",
			wantBucket: "my-bucket",
			wantKey:    "path/to/weights.pt",
		},

		// path-style (no region)
		{
			name:       "path-style no region",
			rawURL:     "https://s3.amazonaws.com/my-bucket/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "model.bin",
		},
		{
			name:       "path-style nested key",
			rawURL:     "https://s3.amazonaws.com/my-bucket/path/to/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "path/to/model.bin",
		},

		// path-style (with region)
		{
			name:       "path-style with region",
			rawURL:     "https://s3.us-east-1.amazonaws.com/my-bucket/model.bin",
			wantBucket: "my-bucket",
			wantKey:    "model.bin",
		},

		// error cases
		{
			name:    "unsupported scheme",
			rawURL:  "ftp://bucket/key",
			wantErr: true,
		},
		{
			name:    "non-amazonaws host",
			rawURL:  "https://example.com/bucket/key",
			wantErr: true,
		},
		{
			name:    "virtual-hosted missing key",
			rawURL:  "https://my-bucket.s3.amazonaws.com/",
			wantErr: true,
		},
		{
			name:    "path-style missing key",
			rawURL:  "https://s3.amazonaws.com/my-bucket",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseS3URL(tc.rawURL)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseS3URL(%q) expected error, got bucket=%q key=%q", tc.rawURL, got.bucket, got.key)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseS3URL(%q) unexpected error: %v", tc.rawURL, err)
			}
			if got.bucket != tc.wantBucket {
				t.Errorf("bucket = %q, want %q", got.bucket, tc.wantBucket)
			}
			if got.key != tc.wantKey {
				t.Errorf("key = %q, want %q", got.key, tc.wantKey)
			}
		})
	}
}

func TestIsPresignedURL(t *testing.T) {
	tests := []struct {
		rawURL string
		want   bool
	}{
		{
			rawURL: "https://bucket.s3.amazonaws.com/key?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Signature=abc123",
			want:   true,
		},
		{
			rawURL: "https://bucket.s3.amazonaws.com/key?x-amz-signature=abc123", // lowercase
			want:   true,
		},
		{
			rawURL: "https://bucket.s3.amazonaws.com/key",
			want:   false,
		},
		{
			rawURL: "s3://bucket/key",
			want:   false,
		},
		{
			rawURL: "https://bucket.s3.amazonaws.com/key?X-Amz-Algorithm=AWS4-HMAC-SHA256",
			want:   false, // Algorithm without Signature is not a complete presigned URL
		},
	}

	for _, tc := range tests {
		if got := isPresignedURL(tc.rawURL); got != tc.want {
			t.Errorf("isPresignedURL(%q) = %v, want %v", tc.rawURL, got, tc.want)
		}
	}
}

// ---- backoff calculation ----------------------------------------------------

func TestCalculateBackoff(t *testing.T) {
	// Expected base delays: attempt 0 → 1s, 1 → 2s, 2 → 4s
	bases := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

	for attempt, base := range bases {
		low := time.Duration(0.75 * float64(base))
		high := time.Duration(1.25 * float64(base))

		// Run several times to exercise the random jitter.
		for i := 0; i < 100; i++ {
			d := calculateBackoff(attempt)
			if d < low || d > high {
				t.Errorf("attempt %d: calculateBackoff() = %v, want [%v, %v]", attempt, d, low, high)
			}
		}
	}
}

// ---- error classification ---------------------------------------------------

func TestIsPermanentError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"403 SDK", makeSDKError(http.StatusForbidden), true},
		{"404 SDK", makeSDKError(http.StatusNotFound), true},
		{"416 SDK", makeSDKError(http.StatusRequestedRangeNotSatisfiable), true},
		{"503 SDK", makeSDKError(http.StatusServiceUnavailable), false},
		{"429 SDK", makeSDKError(http.StatusTooManyRequests), false},
		{"403 plain", &httpStatusError{http.StatusForbidden}, true},
		{"404 plain", &httpStatusError{http.StatusNotFound}, true},
		{"416 plain", &httpStatusError{http.StatusRequestedRangeNotSatisfiable}, true},
		{"503 plain", &httpStatusError{http.StatusServiceUnavailable}, false},
		{"other error", fmt.Errorf("some other error"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPermanentError(tc.err); got != tc.want {
				t.Errorf("isPermanentError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"503 SDK", makeSDKError(http.StatusServiceUnavailable), true},
		{"429 SDK", makeSDKError(http.StatusTooManyRequests), true},
		{"503 plain", &httpStatusError{http.StatusServiceUnavailable}, true},
		{"429 plain", &httpStatusError{http.StatusTooManyRequests}, true},
		{"403 SDK", makeSDKError(http.StatusForbidden), false},
		{"404 SDK", makeSDKError(http.StatusNotFound), false},
		{"context deadline", context.DeadlineExceeded, true},
		{"other error", fmt.Errorf("random error"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientError(tc.err); got != tc.want {
				t.Errorf("isTransientError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ---- GetRange via mock S3 ---------------------------------------------------

func TestGetRange_Success(t *testing.T) {
	wantData := []byte("hello chunk data")
	mock := &mockS3{responses: []mockS3Response{{data: wantData}}}
	client := newTestClient(mock)

	got, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 15)
	if err != nil {
		t.Fatalf("GetRange returned error: %v", err)
	}
	if !bytes.Equal(got, wantData) {
		t.Errorf("GetRange = %q, want %q", got, wantData)
	}
	if mock.callCount != 1 {
		t.Errorf("S3 called %d times, want 1", mock.callCount)
	}
}

func TestGetRange_RangeHeaderFormat(t *testing.T) {
	// Verify the Range header is formatted correctly: bytes=start-end (inclusive)
	var capturedRange string
	captured := &captureRangeMock{onCall: func(r string) { capturedRange = r }}
	client := newClientForTest(captured, &http.Client{}, noopBackoff)

	_, _ = client.GetRange(context.Background(), "s3://bucket/key", 0, 16777215)
	if capturedRange != "bytes=0-16777215" {
		t.Errorf("Range header = %q, want %q", capturedRange, "bytes=0-16777215")
	}
}

// captureRangeMock records the Range value passed to GetObject.
type captureRangeMock struct {
	onCall func(rangeVal string)
}

func (m *captureRangeMock) GetObject(_ context.Context, input *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	if m.onCall != nil && input.Range != nil {
		m.onCall(*input.Range)
	}
	return &awss3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func TestGetRange_RetryTransient(t *testing.T) {
	// Return 503 twice, then succeed on the third attempt.
	wantData := []byte("success after retries")
	mock := &mockS3{responses: []mockS3Response{
		{err: makeSDKError(http.StatusServiceUnavailable)},
		{err: makeSDKError(http.StatusServiceUnavailable)},
		{data: wantData},
	}}
	client := newTestClient(mock)

	got, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 20)
	if err != nil {
		t.Fatalf("GetRange returned error: %v", err)
	}
	if !bytes.Equal(got, wantData) {
		t.Errorf("GetRange = %q, want %q", got, wantData)
	}
	if mock.callCount != 3 {
		t.Errorf("S3 called %d times, want 3", mock.callCount)
	}
}

func TestGetRange_RetryThrottle(t *testing.T) {
	// Return 429 twice, then succeed.
	wantData := []byte("throttle recovery")
	mock := &mockS3{responses: []mockS3Response{
		{err: makeSDKError(http.StatusTooManyRequests)},
		{err: makeSDKError(http.StatusTooManyRequests)},
		{data: wantData},
	}}
	client := newTestClient(mock)

	got, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 16)
	if err != nil {
		t.Fatalf("GetRange returned error: %v", err)
	}
	if !bytes.Equal(got, wantData) {
		t.Errorf("GetRange = %q, want %q", got, wantData)
	}
}

func TestGetRange_MaxRetriesExceeded(t *testing.T) {
	// Return 503 on every attempt (maxRetries+1 = 4 total calls).
	responses := make([]mockS3Response, maxRetries+1)
	for i := range responses {
		responses[i] = mockS3Response{err: makeSDKError(http.StatusServiceUnavailable)}
	}
	mock := &mockS3{responses: responses}
	client := newTestClient(mock)

	_, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 0)
	if err == nil {
		t.Fatal("GetRange should fail after max retries")
	}
	if int(mock.callCount) != maxRetries+1 {
		t.Errorf("S3 called %d times, want %d", mock.callCount, maxRetries+1)
	}
}

func TestGetRange_PermanentError403(t *testing.T) {
	// 403 must not be retried.
	mock := &mockS3{responses: []mockS3Response{
		{err: makeSDKError(http.StatusForbidden)},
	}}
	client := newTestClient(mock)

	_, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 0)
	if err == nil {
		t.Fatal("GetRange should fail on 403")
	}
	if mock.callCount != 1 {
		t.Errorf("S3 called %d times on 403, want exactly 1 (no retries)", mock.callCount)
	}
}

func TestGetRange_PermanentError404(t *testing.T) {
	mock := &mockS3{responses: []mockS3Response{
		{err: makeSDKError(http.StatusNotFound)},
	}}
	client := newTestClient(mock)

	_, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 0)
	if err == nil {
		t.Fatal("GetRange should fail on 404")
	}
	if mock.callCount != 1 {
		t.Errorf("S3 called %d times on 404, want exactly 1 (no retries)", mock.callCount)
	}
}

func TestGetRange_PermanentError416(t *testing.T) {
	mock := &mockS3{responses: []mockS3Response{
		{err: makeSDKError(http.StatusRequestedRangeNotSatisfiable)},
	}}
	client := newTestClient(mock)

	_, err := client.GetRange(context.Background(), "s3://bucket/key", 100, 200)
	if err == nil {
		t.Fatal("GetRange should fail on 416")
	}
	if mock.callCount != 1 {
		t.Errorf("S3 called %d times on 416, want exactly 1 (no retries)", mock.callCount)
	}
}

func TestGetRange_UnknownErrorNoRetry(t *testing.T) {
	// Unknown (non-HTTP) errors should fail immediately without retry.
	mock := &mockS3{responses: []mockS3Response{
		{err: fmt.Errorf("unexpected EOF")},
	}}
	client := newTestClient(mock)

	_, err := client.GetRange(context.Background(), "s3://bucket/key", 0, 0)
	if err == nil {
		t.Fatal("GetRange should fail on unknown error")
	}
	if mock.callCount != 1 {
		t.Errorf("S3 called %d times, want 1 (no retry on unknown error)", mock.callCount)
	}
}

func TestGetRange_ContextCancelled(t *testing.T) {
	// Context cancelled during backoff should abort immediately.
	callCount := int32(0)
	cancelBackoff := func(ctx context.Context, _ int) error {
		return ctx.Err()
	}
	mock := &mockS3{responses: []mockS3Response{
		{err: makeSDKError(http.StatusServiceUnavailable)},
		{err: makeSDKError(http.StatusServiceUnavailable)},
	}}
	client := newClientForTest(mock, &http.Client{}, cancelBackoff)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.GetRange(ctx, "s3://bucket/key", 0, 0)
	if err == nil {
		t.Fatal("GetRange should fail when context is cancelled")
	}
	_ = callCount
}

func TestGetRange_InvalidURL(t *testing.T) {
	mock := &mockS3{}
	client := newTestClient(mock)

	_, err := client.GetRange(context.Background(), "ftp://bucket/key", 0, 0)
	if err == nil {
		t.Fatal("GetRange should fail on unsupported URL scheme")
	}
	if mock.callCount != 0 {
		t.Errorf("S3 called %d times for invalid URL, want 0", mock.callCount)
	}
}

// ---- Presigned URL via httptest.Server --------------------------------------

func TestGetRange_PresignedURLSuccess(t *testing.T) {
	wantData := []byte("presigned chunk data")
	var capturedRange string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRange = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(wantData) //nolint:errcheck
	}))
	defer server.Close()

	// Build a fake presigned URL pointing at our test server.
	presignedURL := server.URL + "/bucket/key?X-Amz-Signature=fakesig&X-Amz-Algorithm=AWS4-HMAC-SHA256"
	client := newClientForTest(nil, server.Client(), noopBackoff)

	got, err := client.GetRange(context.Background(), presignedURL, 0, 19)
	if err != nil {
		t.Fatalf("GetRange presigned returned error: %v", err)
	}
	if !bytes.Equal(got, wantData) {
		t.Errorf("GetRange presigned = %q, want %q", got, wantData)
	}
	if capturedRange != "bytes=0-19" {
		t.Errorf("Range header = %q, want %q", capturedRange, "bytes=0-19")
	}
}

func TestGetRange_PresignedURL403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	callCount := 0
	countingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusForbidden)
	})
	server2 := httptest.NewServer(countingHandler)
	defer server2.Close()

	presignedURL := server2.URL + "/key?X-Amz-Signature=fakesig"
	client := newClientForTest(nil, server2.Client(), noopBackoff)

	_, err := client.GetRange(context.Background(), presignedURL, 0, 0)
	if err == nil {
		t.Fatal("GetRange presigned 403 should return error")
	}
	if callCount != 1 {
		t.Errorf("server called %d times on 403, want 1 (no retries)", callCount)
	}
}

func TestGetRange_PresignedURL503Retry(t *testing.T) {
	callCount := 0
	wantData := []byte("ok after 503")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		w.Write(wantData) //nolint:errcheck
	}))
	defer server.Close()

	presignedURL := server.URL + "/key?X-Amz-Signature=fakesig"
	client := newClientForTest(nil, server.Client(), noopBackoff)

	got, err := client.GetRange(context.Background(), presignedURL, 0, 11)
	if err != nil {
		t.Fatalf("GetRange presigned retry returned error: %v", err)
	}
	if !bytes.Equal(got, wantData) {
		t.Errorf("GetRange = %q, want %q", got, wantData)
	}
	if callCount != 3 {
		t.Errorf("server called %d times, want 3", callCount)
	}
}

// ---- Integration tests (require real AWS credentials + S3_TEST_BUCKET) ------

func skipIfNoIntegrationCreds(t *testing.T) (bucket string) {
	t.Helper()
	bucket = os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		t.Skip("Skipping integration test: set S3_TEST_BUCKET and AWS credentials to run")
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" && os.Getenv("AWS_PROFILE") == "" {
		t.Skip("Skipping integration test: AWS credentials not configured")
	}
	return bucket
}

func TestGetRange_Integration(t *testing.T) {
	bucket := skipIfNoIntegrationCreds(t)

	ctx := context.Background()
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// Expects test-small.txt to exist in the bucket (created during infra setup).
	url := fmt.Sprintf("s3://%s/test-small.txt", bucket)
	data, err := client.GetRange(ctx, url, 0, 8)
	if err != nil {
		t.Fatalf("GetRange integration failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("GetRange returned empty data")
	}
	t.Logf("fetched %d bytes: %q", len(data), data)
}

func TestGetRange_IntegrationVirtualHostedURL(t *testing.T) {
	bucket := skipIfNoIntegrationCreds(t)
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	ctx := context.Background()
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/test-small.txt", bucket, region)
	data, err := client.GetRange(ctx, url, 0, 8)
	if err != nil {
		t.Fatalf("GetRange virtual-hosted integration failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("GetRange returned empty data")
	}
	t.Logf("fetched %d bytes: %q", len(data), data)
}

func TestGetRange_IntegrationMultiChunk(t *testing.T) {
	bucket := skipIfNoIntegrationCreds(t)

	ctx := context.Background()
	client, err := NewClient(ctx)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	const chunkSize = 16 * 1024 * 1024 // 16 MB

	// Expects test-32mb.bin in the bucket (created during infra setup).
	url := fmt.Sprintf("s3://%s/test-32mb.bin", bucket)

	// Fetch chunk 0 (bytes 0–16MB-1)
	chunk0, err := client.GetRange(ctx, url, 0, chunkSize-1)
	if err != nil {
		t.Fatalf("GetRange chunk 0 failed: %v", err)
	}
	if len(chunk0) != chunkSize {
		t.Errorf("chunk 0 size = %d, want %d", len(chunk0), chunkSize)
	}

	// Fetch chunk 1 (bytes 16MB–32MB-1)
	chunk1, err := client.GetRange(ctx, url, chunkSize, 2*chunkSize-1)
	if err != nil {
		t.Fatalf("GetRange chunk 1 failed: %v", err)
	}
	if len(chunk1) != chunkSize {
		t.Errorf("chunk 1 size = %d, want %d", len(chunk1), chunkSize)
	}

	// Chunks must be different (random data).
	if bytes.Equal(chunk0, chunk1) {
		t.Error("chunk 0 and chunk 1 are identical — random test data should differ")
	}

	t.Logf("fetched 2 x 16 MB chunks successfully")
}
