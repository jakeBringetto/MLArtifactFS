package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jakeBringetto/MLArtifactFS/pkg/cache"
)

// ---------------------------------------------------------------------------
// Mock S3 client
// ---------------------------------------------------------------------------

// mockS3 is a controllable S3Client for tests.
type mockS3 struct {
	mu        sync.Mutex
	callCount int // total GetRange calls
	// fetchFunc, if set, is called for every GetRange. Otherwise returns data from chunks.
	fetchFunc func(url string, start, end int64) ([]byte, error)
	// chunks maps "start-end" to data for simple fixed-response tests.
	chunks map[string][]byte
}

func (m *mockS3) GetRange(_ context.Context, url string, start, end int64) ([]byte, error) {
	m.mu.Lock()
	m.callCount++
	m.mu.Unlock()

	if m.fetchFunc != nil {
		return m.fetchFunc(url, start, end)
	}

	key := fmt.Sprintf("%d-%d", start, end)
	if data, ok := m.chunks[key]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("mockS3: no data for range %s", key)
}

func (m *mockS3) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const testChunkSize = 1024 // 1 KB for fast tests

// newTestManager creates a Manager with a real temp-dir cache and the given mock S3.
func newTestManager(t *testing.T, s3 S3Client) (*Manager, *cache.Manager) {
	t.Helper()
	cm, err := cache.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return NewManager(s3, cm, testChunkSize), cm
}

// sha256hex computes the hex-encoded SHA256 of data.
func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// makeChunk returns a byte slice of length n filled with the byte val.
func makeChunk(val byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = val
	}
	return b
}

// ---------------------------------------------------------------------------
// Read tests
// ---------------------------------------------------------------------------

func TestRead_ZeroSize(t *testing.T) {
	m, _ := newTestManager(t, &mockS3{})
	got, err := m.Read(context.Background(), "http://example.com/f", strings.Repeat("a", 64), 0, 0, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d bytes", len(got))
	}
}

func TestRead_CacheHit(t *testing.T) {
	s3 := &mockS3{}
	m, cm := newTestManager(t, s3)

	data := makeChunk(0xAB, testChunkSize)
	fileSHA := sha256hex(data)

	// Pre-populate cache for chunk 0.
	if err := cm.Write(fileSHA, 0, data); err != nil {
		t.Fatalf("cache Write: %v", err)
	}

	got, err := m.Read(context.Background(), "http://example.com/f", fileSHA, 10, 5, int64(testChunkSize))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(data[10:15]) {
		t.Fatalf("wrong bytes: got %v want %v", got, data[10:15])
	}
	if s3.calls() != 0 {
		t.Fatalf("expected 0 S3 calls on cache hit, got %d", s3.calls())
	}
}

func TestRead_CacheMiss_FetchesAndCaches(t *testing.T) {
	chunk0 := makeChunk(0x01, testChunkSize)
	fileSHA := sha256hex(chunk0) // single-chunk file for simplicity

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("0-%d", testChunkSize-1): chunk0,
		},
	}
	m, cm := newTestManager(t, s3)

	got, err := m.Read(context.Background(), "http://example.com/f", fileSHA, 0, 10, int64(testChunkSize))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(chunk0[:10]) {
		t.Fatalf("wrong bytes")
	}
	if s3.calls() != 1 {
		t.Fatalf("expected 1 S3 call, got %d", s3.calls())
	}

	// Second read should be a cache hit.
	_, err = m.Read(context.Background(), "http://example.com/f", fileSHA, 0, 10, int64(testChunkSize))
	if err != nil {
		t.Fatalf("second Read: %v", err)
	}
	if s3.calls() != 1 {
		t.Fatalf("expected still 1 S3 call after cache hit, got %d", s3.calls())
	}

	// Chunk should be written to cache.
	if !cm.Has(fileSHA, 0) {
		t.Fatal("expected chunk 0 in cache after miss")
	}
}

func TestRead_CorrectSlice_MiddleOfChunk(t *testing.T) {
	chunk0 := make([]byte, testChunkSize)
	for i := range chunk0 {
		chunk0[i] = byte(i % 256)
	}
	fileSHA := sha256hex(chunk0)

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("0-%d", testChunkSize-1): chunk0,
		},
	}
	m, _ := newTestManager(t, s3)

	offset, size := int64(100), int64(50)
	got, err := m.Read(context.Background(), "http://x", fileSHA, offset, size, int64(testChunkSize))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(chunk0[100:150]) {
		t.Fatal("wrong slice returned")
	}
}

func TestRead_CrossChunk(t *testing.T) {
	// Two chunks; read straddles the boundary.
	chunk0 := makeChunk(0xAA, testChunkSize)
	chunk1 := makeChunk(0xBB, testChunkSize)
	fileSize := int64(2 * testChunkSize)
	// SHA is irrelevant for routing; use something valid.
	fileSHA := strings.Repeat("a", 64)

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("0-%d", testChunkSize-1):              chunk0,
			fmt.Sprintf("%d-%d", testChunkSize, fileSize-1):   chunk1,
		},
	}
	m, _ := newTestManager(t, s3)

	// Read last 4 bytes of chunk0 + first 4 bytes of chunk1 = 8 bytes.
	offset := int64(testChunkSize - 4)
	size := int64(8)
	got, err := m.Read(context.Background(), "http://x", fileSHA, offset, size, fileSize)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	expected := append(chunk0[testChunkSize-4:], chunk1[:4]...)
	if string(got) != string(expected) {
		t.Fatalf("cross-chunk read wrong: got %v want %v", got, expected)
	}
	if s3.calls() != 2 {
		t.Fatalf("expected 2 S3 calls for cross-chunk miss, got %d", s3.calls())
	}
}

func TestRead_CrossChunk_PartialCacheHit(t *testing.T) {
	// chunk0 already cached; chunk1 is a miss.
	chunk0 := makeChunk(0xAA, testChunkSize)
	chunk1 := makeChunk(0xBB, testChunkSize)
	fileSize := int64(2 * testChunkSize)
	fileSHA := strings.Repeat("b", 64)

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("%d-%d", testChunkSize, fileSize-1): chunk1,
		},
	}
	m, cm := newTestManager(t, s3)

	// Pre-populate chunk0.
	if err := cm.Write(fileSHA, 0, chunk0); err != nil {
		t.Fatalf("cache Write: %v", err)
	}

	offset := int64(testChunkSize - 4)
	size := int64(8)
	got, err := m.Read(context.Background(), "http://x", fileSHA, offset, size, fileSize)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	expected := append(chunk0[testChunkSize-4:], chunk1[:4]...)
	if string(got) != string(expected) {
		t.Fatalf("wrong bytes in partial-cache cross-chunk read")
	}
	// Only chunk1 should have been fetched from S3.
	if s3.calls() != 1 {
		t.Fatalf("expected 1 S3 call (chunk1 miss only), got %d", s3.calls())
	}
}

func TestRead_LastChunk_Smaller(t *testing.T) {
	// File size is 1.5 chunks; last chunk is half-size.
	halfChunk := testChunkSize / 2
	chunk0 := makeChunk(0x01, testChunkSize)
	chunk1 := makeChunk(0x02, halfChunk)
	fileSize := int64(testChunkSize + halfChunk)
	fileSHA := strings.Repeat("c", 64)

	var capturedEnd int64
	s3 := &mockS3{
		fetchFunc: func(url string, start, end int64) ([]byte, error) {
			capturedEnd = end
			if start == 0 {
				return chunk0, nil
			}
			return chunk1, nil
		},
	}
	m, _ := newTestManager(t, s3)

	// Read from the last chunk.
	offset := int64(testChunkSize)
	size := int64(halfChunk)
	got, err := m.Read(context.Background(), "http://x", fileSHA, offset, size, fileSize)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(chunk1) {
		t.Fatal("wrong last-chunk data")
	}
	// End byte should be clamped to fileSize-1, not chunkSize*2-1.
	wantEnd := fileSize - 1
	if capturedEnd != wantEnd {
		t.Fatalf("range end not clamped: got %d want %d", capturedEnd, wantEnd)
	}
}

func TestRead_S3Error_Propagated(t *testing.T) {
	sentinel := errors.New("S3 unavailable")
	s3 := &mockS3{fetchFunc: func(url string, start, end int64) ([]byte, error) {
		return nil, sentinel
	}}
	m, _ := newTestManager(t, s3)

	_, err := m.Read(context.Background(), "http://x", strings.Repeat("d", 64), 0, 10, 1024)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestRead_Concurrent_SameChunk_SingleFlight(t *testing.T) {
	// Many goroutines read the same chunk simultaneously. With singleflight,
	// exactly one S3 request should be issued.
	//
	// Design: block the S3 fetch until we know it's in-flight, give other
	// goroutines time to queue up as DoChan waiters, then unblock. This avoids
	// the race where a fast fetch completes before other goroutines reach DoChan.
	chunk0 := makeChunk(0xFF, testChunkSize)
	fileSHA := strings.Repeat("e", 64)

	var s3CallCount atomic.Int32
	fetchStarted := make(chan struct{}, 1) // buffered: doesn't block the fetch goroutine
	unblock := make(chan struct{})

	s3 := &mockS3{fetchFunc: func(url string, start, end int64) ([]byte, error) {
		s3CallCount.Add(1)
		select {
		case fetchStarted <- struct{}{}: // signal once that a fetch is in flight
		default:
		}
		<-unblock
		return chunk0, nil
	}}
	m, _ := newTestManager(t, s3)

	const n = 20
	results := make([][]byte, n)
	errs := make([]error, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = m.Read(context.Background(), "http://x", fileSHA, 0, 10, int64(testChunkSize))
		}(i)
	}

	// Wait for the first S3 fetch to actually start (it's now blocked on <-unblock).
	// Then sleep briefly so other goroutines can reach DoChan and become waiters
	// while the fetch is still in-flight — that's when singleflight coalesces them.
	<-fetchStarted
	time.Sleep(5 * time.Millisecond)
	close(unblock)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d error: %v", i, err)
		}
	}
	for i, res := range results {
		if string(res) != string(chunk0[:10]) {
			t.Fatalf("goroutine %d: wrong result", i)
		}
	}
	if got := s3CallCount.Load(); got != 1 {
		t.Fatalf("expected 1 S3 call with singleflight, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Prefetch tests
// ---------------------------------------------------------------------------

func TestPrefetch_Success_SingleChunk(t *testing.T) {
	content := makeChunk(0xAB, testChunkSize)
	fileSHA := sha256hex(content)

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("0-%d", testChunkSize-1): content,
		},
	}
	m, cm := newTestManager(t, s3)

	if err := m.Prefetch(context.Background(), "http://x", fileSHA, int64(testChunkSize)); err != nil {
		t.Fatalf("Prefetch: %v", err)
	}

	if !cm.Has(fileSHA, 0) {
		t.Fatal("chunk 0 not in cache after prefetch")
	}
	if !cm.IsVerified(fileSHA) {
		t.Fatal("file not marked verified after prefetch")
	}
}

func TestPrefetch_Success_MultiChunk(t *testing.T) {
	chunk0 := makeChunk(0x01, testChunkSize)
	chunk1 := makeChunk(0x02, testChunkSize)
	chunk2 := makeChunk(0x03, testChunkSize/2) // partial last chunk

	allData := append(append(chunk0, chunk1...), chunk2...)
	fileSHA := sha256hex(allData)
	fileSize := int64(len(allData))

	s3 := &mockS3{
		fetchFunc: func(url string, start, end int64) ([]byte, error) {
			switch start {
			case 0:
				return chunk0, nil
			case int64(testChunkSize):
				return chunk1, nil
			case int64(2 * testChunkSize):
				return chunk2, nil
			}
			return nil, fmt.Errorf("unexpected range %d-%d", start, end)
		},
	}
	m, cm := newTestManager(t, s3)

	if err := m.Prefetch(context.Background(), "http://x", fileSHA, fileSize); err != nil {
		t.Fatalf("Prefetch: %v", err)
	}

	for i := 0; i < 3; i++ {
		if !cm.Has(fileSHA, i) {
			t.Fatalf("chunk %d not in cache after prefetch", i)
		}
	}
	if !cm.IsVerified(fileSHA) {
		t.Fatal("file not marked verified after multi-chunk prefetch")
	}
	if s3.calls() != 3 {
		t.Fatalf("expected 3 S3 calls, got %d", s3.calls())
	}
}

func TestPrefetch_SHA256Mismatch(t *testing.T) {
	content := makeChunk(0xAB, testChunkSize)
	wrongSHA := strings.Repeat("f", 64)

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("0-%d", testChunkSize-1): content,
		},
	}
	m, cm := newTestManager(t, s3)

	err := m.Prefetch(context.Background(), "http://x", wrongSHA, int64(testChunkSize))
	if err == nil {
		t.Fatal("expected error on SHA256 mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("expected SHA256 mismatch error, got: %v", err)
	}
	if cm.IsVerified(wrongSHA) {
		t.Fatal("file must not be marked verified on SHA256 mismatch")
	}
}

func TestPrefetch_S3Error(t *testing.T) {
	sentinel := errors.New("network failure")
	s3 := &mockS3{fetchFunc: func(url string, start, end int64) ([]byte, error) {
		return nil, sentinel
	}}
	fileSHA := strings.Repeat("0", 64)
	m, cm := newTestManager(t, s3)

	err := m.Prefetch(context.Background(), "http://x", fileSHA, int64(testChunkSize))
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if cm.IsVerified(fileSHA) {
		t.Fatal("file must not be marked verified after S3 error")
	}
}

func TestPrefetch_Idempotent(t *testing.T) {
	content := makeChunk(0x55, testChunkSize)
	fileSHA := sha256hex(content)

	s3 := &mockS3{
		chunks: map[string][]byte{
			fmt.Sprintf("0-%d", testChunkSize-1): content,
		},
	}
	m, cm := newTestManager(t, s3)

	for i := 0; i < 2; i++ {
		if err := m.Prefetch(context.Background(), "http://x", fileSHA, int64(testChunkSize)); err != nil {
			t.Fatalf("Prefetch attempt %d: %v", i+1, err)
		}
	}
	if !cm.IsVerified(fileSHA) {
		t.Fatal("file not verified after idempotent prefetch")
	}
}

func TestPrefetch_S3ErrorPropagatedFromContext(t *testing.T) {
	// Prefetch passes the caller's context to s3.GetRange. Verify that if
	// GetRange returns context.Canceled (as the real S3 client would on
	// cancellation), Prefetch surfaces that error and does not mark verified.
	s3 := &mockS3{fetchFunc: func(url string, start, end int64) ([]byte, error) {
		return nil, context.Canceled
	}}
	fileSHA := strings.Repeat("1", 64)
	m, cm := newTestManager(t, s3)

	err := m.Prefetch(context.Background(), "http://x", fileSHA, int64(testChunkSize))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if cm.IsVerified(fileSHA) {
		t.Fatal("must not be marked verified after context cancellation")
	}
}
