package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	maxRetries = 3
	baseDelay  = time.Second
)

// s3API is the subset of the AWS S3 SDK used by Client.
// Defined as an interface to enable mock injection in tests.
type s3API interface {
	GetObject(ctx context.Context, input *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
}

// Client wraps the AWS S3 SDK and provides range-request fetching with retry logic.
// Supports s3://, https:// S3 URLs, and presigned URLs.
// Use NewClient for production; use newClientForTest in unit tests.
type Client struct {
	s3Client    s3API
	httpClient  *http.Client
	backoffFunc func(ctx context.Context, attempt int) error // injectable for testing
}

// parsedS3URL holds bucket and key extracted from an S3 URL.
type parsedS3URL struct {
	bucket string
	key    string
}

// httpStatusError represents an HTTP error from a presigned URL request.
// The AWS SDK path produces smithyhttp.ResponseError; the plain HTTP path produces this.
type httpStatusError struct {
	statusCode int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d %s", e.statusCode, http.StatusText(e.statusCode))
}

// NewClient creates a Client using the default AWS credential chain:
//  1. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//  2. IAM role (EC2/ECS instance profile)
//  3. ~/.aws/credentials file
func NewClient(ctx context.Context) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Client{
		s3Client:    awss3.NewFromConfig(cfg),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		backoffFunc: defaultBackoff,
	}, nil
}

// newClientForTest constructs a Client with injected dependencies. Test use only.
func newClientForTest(s3 s3API, httpCl *http.Client, backoff func(ctx context.Context, attempt int) error) *Client {
	return &Client{
		s3Client:    s3,
		httpClient:  httpCl,
		backoffFunc: backoff,
	}
}

// GetRange fetches bytes [start, end] (inclusive) from the given S3 URL.
// Supports s3://, virtual-hosted HTTPS, path-style HTTPS, and presigned URLs.
// Transient errors (503, 429, timeouts) are retried up to maxRetries times with
// exponential backoff. Permanent errors (403, 404, 416) are returned immediately.
func (c *Client) GetRange(ctx context.Context, rawURL string, start, end int64) ([]byte, error) {
	rangeHeader := fmt.Sprintf("bytes=%d-%d", start, end)

	if isPresignedURL(rawURL) {
		return c.withRetry(ctx, func() ([]byte, error) {
			return c.fetchPresigned(ctx, rawURL, rangeHeader)
		})
	}

	parsed, err := parseS3URL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid S3 URL: %w", err)
	}

	return c.withRetry(ctx, func() ([]byte, error) {
		return c.fetchS3(ctx, parsed, rangeHeader)
	})
}

// fetchS3 performs a single GetObject call via the AWS SDK.
func (c *Client) fetchS3(ctx context.Context, parsed *parsedS3URL, rangeHeader string) ([]byte, error) {
	out, err := c.s3Client.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: &parsed.bucket,
		Key:    &parsed.key,
		Range:  &rangeHeader,
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("reading S3 response body: %w", err)
	}
	return data, nil
}

// fetchPresigned performs a single HTTP GET on a presigned URL with a Range header.
func (c *Client) fetchPresigned(ctx context.Context, rawURL, rangeHeader string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Range", rangeHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err // net/http errors are checked by isTransientError
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		// Drain body to allow connection reuse before returning the error.
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return nil, &httpStatusError{statusCode: resp.StatusCode}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return data, nil
}

// withRetry executes fetch, retrying transient errors with exponential backoff.
// Permanent errors and unknown errors are returned immediately without retrying.
func (c *Client) withRetry(ctx context.Context, fetch func() ([]byte, error)) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		data, err := fetch()
		if err == nil {
			return data, nil
		}
		lastErr = err

		if isPermanentError(err) {
			return nil, err
		}

		if attempt == maxRetries {
			break
		}

		if !isTransientError(err) {
			return nil, err
		}

		if err := c.backoffFunc(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// defaultBackoff sleeps for an exponentially increasing duration with ±25% jitter,
// or returns early if the context is cancelled.
func defaultBackoff(ctx context.Context, attempt int) error {
	delay := calculateBackoff(attempt)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// calculateBackoff returns a jittered exponential backoff duration.
// attempt=0 → ~1s, attempt=1 → ~2s, attempt=2 → ~4s (each ±25%)
func calculateBackoff(attempt int) time.Duration {
	base := baseDelay * (1 << uint(attempt))
	// ±25% jitter: shift down 25%, then add a random 0-50% of base.
	jitter := time.Duration(rand.Float64() * 0.5 * float64(base))
	return base - time.Duration(0.25*float64(base)) + jitter
}

// isPermanentError returns true for HTTP errors that should never be retried.
// 403 Forbidden, 404 Not Found, 416 Range Not Satisfiable.
func isPermanentError(err error) bool {
	code := httpStatusCode(err)
	return code == http.StatusForbidden ||
		code == http.StatusNotFound ||
		code == http.StatusRequestedRangeNotSatisfiable
}

// isTransientError returns true for errors that are safe to retry.
// 503 Service Unavailable, 429 Too Many Requests, network timeouts.
func isTransientError(err error) bool {
	code := httpStatusCode(err)
	if code == http.StatusServiceUnavailable || code == http.StatusTooManyRequests {
		return true
	}
	// Network-level errors from net/http (used by both presigned path and SDK transport).
	var netErr *url.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// httpStatusCode extracts the HTTP status code from either an AWS SDK error
// (smithyhttp.ResponseError) or a plain httpStatusError. Returns 0 if not found.
func httpStatusCode(err error) int {
	var smithyErr *smithyhttp.ResponseError
	if errors.As(err, &smithyErr) {
		return smithyErr.HTTPStatusCode()
	}
	var plainErr *httpStatusError
	if errors.As(err, &plainErr) {
		return plainErr.statusCode
	}
	return 0
}

// isPresignedURL detects presigned S3 URLs by the presence of the AWS signature
// query parameter (X-Amz-Signature), which is unique to pre-authenticated URLs.
func isPresignedURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "x-amz-signature")
}

// parseS3URL extracts bucket and key from an S3 URL.
//
// Supported formats:
//
//	s3://bucket/key
//	https://bucket.s3.amazonaws.com/key                  (virtual-hosted)
//	https://bucket.s3.us-east-1.amazonaws.com/key        (virtual-hosted with region)
//	https://s3.amazonaws.com/bucket/key                  (path-style, deprecated)
//	https://s3.us-east-1.amazonaws.com/bucket/key        (path-style with region)
func parseS3URL(rawURL string) (*parsedS3URL, error) {
	switch {
	case strings.HasPrefix(rawURL, "s3://"):
		return parseS3Scheme(rawURL)
	case strings.HasPrefix(rawURL, "https://"), strings.HasPrefix(rawURL, "http://"):
		return parseHTTPSScheme(rawURL)
	default:
		return nil, fmt.Errorf("unsupported scheme in %q (expected s3://, https://, or http://)", rawURL)
	}
}

// parseS3Scheme handles s3://bucket/key URLs.
func parseS3Scheme(rawURL string) (*parsedS3URL, error) {
	rest := strings.TrimPrefix(rawURL, "s3://")
	slashIdx := strings.IndexByte(rest, '/')
	if slashIdx < 0 {
		return nil, fmt.Errorf("missing key in %q (expected s3://bucket/key)", rawURL)
	}
	bucket := rest[:slashIdx]
	key := rest[slashIdx+1:]
	if bucket == "" {
		return nil, fmt.Errorf("missing bucket in %q", rawURL)
	}
	if key == "" {
		return nil, fmt.Errorf("missing key in %q", rawURL)
	}
	return &parsedS3URL{bucket: bucket, key: key}, nil
}

// parseHTTPSScheme handles both virtual-hosted and path-style S3 HTTPS URLs.
func parseHTTPSScheme(rawURL string) (*parsedS3URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	host := u.Hostname()
	if !strings.HasSuffix(host, ".amazonaws.com") {
		return nil, fmt.Errorf("unrecognized S3 host %q (expected *.amazonaws.com)", host)
	}

	// Split host into dot-separated parts.
	// Virtual-hosted: ["bucket", "s3", "amazonaws", "com"]
	//             or: ["bucket", "s3", "us-east-1", "amazonaws", "com"]
	// Path-style:     ["s3", "amazonaws", "com"]
	//             or: ["s3", "us-east-1", "amazonaws", "com"]
	parts := strings.Split(host, ".")

	// Find the "s3" segment.
	s3Idx := -1
	for i, p := range parts {
		if p == "s3" {
			s3Idx = i
			break
		}
	}
	if s3Idx < 0 {
		return nil, fmt.Errorf("unrecognized S3 URL format %q", rawURL)
	}

	pathKey := strings.TrimPrefix(u.Path, "/")

	if s3Idx == 0 {
		// Path-style: bucket and key are in the URL path.
		slashIdx := strings.IndexByte(pathKey, '/')
		if slashIdx < 0 {
			return nil, fmt.Errorf("missing key in path-style URL %q", rawURL)
		}
		bucket := pathKey[:slashIdx]
		key := pathKey[slashIdx+1:]
		if bucket == "" {
			return nil, fmt.Errorf("missing bucket in path-style URL %q", rawURL)
		}
		if key == "" {
			return nil, fmt.Errorf("missing key in path-style URL %q", rawURL)
		}
		return &parsedS3URL{bucket: bucket, key: key}, nil
	}

	// Virtual-hosted style: everything before "s3" is the bucket name.
	bucket := strings.Join(parts[:s3Idx], ".")
	if pathKey == "" {
		return nil, fmt.Errorf("missing key in virtual-hosted URL %q", rawURL)
	}
	return &parsedS3URL{bucket: bucket, key: pathKey}, nil
}
