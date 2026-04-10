package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/jakeBringetto/MLArtifactFS/pkg/cache"
)

const (
	DefaultChunkSize  = 16 * 1024 * 1024
	chunkFetchTimeout = 1 * time.Minute
)

// S3Client fetches byte ranges from remote storage.
type S3Client interface {
	GetRange(ctx context.Context, url string, start, end int64) ([]byte, error)
}

// Manager translates arbitrary byte-range reads into chunk-aligned S3 fetches,
// caching chunks on disk. Concurrent reads for the same chunk are coalesced via
// singleflight so only one S3 request is issued per chunk at a time.
type Manager struct {
	s3        S3Client
	cache     *cache.Manager
	chunkSize int64
	group     singleflight.Group
}

func NewManager(s3 S3Client, cache *cache.Manager, chunkSize int64) *Manager {
	return &Manager{s3: s3, cache: cache, chunkSize: chunkSize}
}

// Read returns bytes [offset, offset+size) from the file at url.
// fileSize is required to clamp the range on the last chunk.
// Reads spanning a chunk boundary fetch each chunk independently.
func (m *Manager) Read(ctx context.Context, url, fileSHA256 string, offset, size, fileSize int64) ([]byte, error) {
	if size == 0 {
		return []byte{}, nil
	}

	startChunk := offset / m.chunkSize
	endChunk := (offset + size - 1) / m.chunkSize

	if startChunk == endChunk {
		chunk, err := m.fetchChunk(ctx, url, fileSHA256, startChunk, fileSize)
		if err != nil {
			return nil, err
		}
		offsetInChunk := offset - startChunk*m.chunkSize
		return chunk[offsetInChunk : offsetInChunk+size], nil
	}

	// Cross-chunk read: concatenate all needed chunks, then slice.
	var combined []byte
	for i := startChunk; i <= endChunk; i++ {
		chunk, err := m.fetchChunk(ctx, url, fileSHA256, i, fileSize)
		if err != nil {
			return nil, err
		}
		combined = append(combined, chunk...)
	}

	offsetInCombined := offset - startChunk*m.chunkSize
	return combined[offsetInCombined : offsetInCombined+size], nil
}

// Prefetch downloads the complete file sequentially, verifies its SHA256, and
// marks it as verified in cache. Returns an error on hash mismatch or S3 failure.
func (m *Manager) Prefetch(ctx context.Context, url, fileSHA256 string, fileSize int64) error {
	numChunks := (fileSize + m.chunkSize - 1) / m.chunkSize
	hasher := sha256.New()

	for i := int64(0); i < numChunks; i++ {
		start := i * m.chunkSize
		end := start + m.chunkSize - 1
		if end >= fileSize {
			end = fileSize - 1
		}

		data, err := m.s3.GetRange(ctx, url, start, end)
		if err != nil {
			return fmt.Errorf("prefetch chunk %d: %w", i, err)
		}
		if err := m.cache.Write(fileSHA256, int(i), data); err != nil {
			return fmt.Errorf("prefetch cache write chunk %d: %w", i, err)
		}
		hasher.Write(data)
	}

	actual := hex.EncodeToString(hasher.Sum(nil))
	if actual != fileSHA256 {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", fileSHA256, actual)
	}

	if err := m.cache.MarkVerified(fileSHA256); err != nil {
		return fmt.Errorf("mark verified: %w", err)
	}

	return nil
}

// fetchChunk returns chunk data from cache if present, otherwise fetches from S3.
// The S3 fetch runs under a detached context so a single cancelled caller doesn't
// abort an in-flight fetch shared by other goroutines.
func (m *Manager) fetchChunk(ctx context.Context, url, fileSHA256 string, chunkIndex, fileSize int64) ([]byte, error) {
	if m.cache.Has(fileSHA256, int(chunkIndex)) {
		return m.cache.Read(fileSHA256, int(chunkIndex))
	}

	key := fmt.Sprintf("%s/%d", fileSHA256, chunkIndex)

	ch := m.group.DoChan(key, func() (interface{}, error) {
		fetchCtx, cancel := context.WithTimeout(context.Background(), chunkFetchTimeout)
		defer cancel()

		start := chunkIndex * m.chunkSize
		end := start + m.chunkSize - 1
		if fileSize > 0 && end >= fileSize {
			end = fileSize - 1
		}

		data, err := m.s3.GetRange(fetchCtx, url, start, end)
		if err != nil {
			return nil, fmt.Errorf("S3 fetch chunk %d: %w", chunkIndex, err)
		}
		if err := m.cache.Write(fileSHA256, int(chunkIndex), data); err != nil {
			return nil, fmt.Errorf("cache write chunk %d: %w", chunkIndex, err)
		}
		return data, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]byte), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
