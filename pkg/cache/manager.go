package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Manager handles local disk caching of file chunks.
// Chunks are stored in a directory structure organized by SHA256 hash:
//
//	<cache-dir>/<sha256>/chunk_N
//	<cache-dir>/<sha256>/_verified
//
// The _verified marker file indicates that the full file has been
// downloaded and passed SHA256 verification.
//
// TODO(production): Consider hash prefix sharding for >100 models:
//
//	<cache-dir>/ab/c123.../chunk_N
//
// See context/bundles/M3-implementation-notes.md for details.
type Manager struct {
	dir string
}

// sha256Pattern validates SHA256 hash format (64 lowercase hex characters)
var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// NewManager creates a new cache manager for the given directory.
// The directory will be created if it doesn't exist.
// Returns an error if the directory cannot be created or accessed.
func NewManager(dir string) (*Manager, error) {
	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Manager{dir: dir}, nil
}

// validateSHA256 checks if the SHA256 hash is valid (64 lowercase hex chars).
// Returns an error if the hash is invalid.
func validateSHA256(sha256 string) error {
	if !sha256Pattern.MatchString(sha256) {
		return fmt.Errorf("invalid SHA256 hash: must be 64 lowercase hex characters, got %q", sha256)
	}
	return nil
}

// ChunkPath returns the filesystem path for a specific chunk.
// This is a public method to support testing and debugging.
func (m *Manager) ChunkPath(sha256 string, chunkIndex int) string {
	return filepath.Join(m.dir, sha256, fmt.Sprintf("chunk_%d", chunkIndex))
}

// Has checks if a chunk exists in the cache.
// Returns false if the chunk doesn't exist or if the SHA256 is invalid.
func (m *Manager) Has(sha256 string, chunkIndex int) bool {
	if err := validateSHA256(sha256); err != nil {
		return false
	}

	if chunkIndex < 0 {
		return false
	}

	chunkPath := m.ChunkPath(sha256, chunkIndex)
	_, err := os.Stat(chunkPath)
	return err == nil
}

// Read reads a chunk from the cache.
// Returns an error if the chunk doesn't exist, the SHA256 is invalid,
// or if there's a filesystem error.
func (m *Manager) Read(sha256 string, chunkIndex int) ([]byte, error) {
	if err := validateSHA256(sha256); err != nil {
		return nil, err
	}

	if chunkIndex < 0 {
		return nil, fmt.Errorf("invalid chunk index: %d (must be >= 0)", chunkIndex)
	}

	chunkPath := m.ChunkPath(sha256, chunkIndex)
	data, err := os.ReadFile(chunkPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("chunk not found: %s (index %d)", sha256[:16], chunkIndex)
		}
		return nil, fmt.Errorf("failed to read chunk: %w", err)
	}

	return data, nil
}

// Write writes a chunk to the cache using an atomic write pattern.
// The chunk is written to a temporary file first, then renamed to the
// final chunk path. This prevents partial writes from corrupting the cache.
//
// The method is idempotent: writing the same chunk multiple times is safe.
//
// Returns an error if:
//   - The SHA256 is invalid
//   - The chunk index is negative
//   - The cache directory cannot be created
//   - Disk is full (ENOSPC)
//   - Permission denied (EACCES)
func (m *Manager) Write(sha256 string, chunkIndex int, data []byte) error {
	if err := validateSHA256(sha256); err != nil {
		return err
	}

	if chunkIndex < 0 {
		return fmt.Errorf("invalid chunk index: %d (must be >= 0)", chunkIndex)
	}

	// Create chunk directory (e.g., <cache-dir>/<sha256>/)
	chunkDir := filepath.Join(m.dir, sha256)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunk directory: %w", err)
	}

	// Write to temporary file first (atomic write pattern)
	chunkPath := m.ChunkPath(sha256, chunkIndex)
	tempPath := chunkPath + ".tmp"

	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("cache write failed (disk full?): %w", err)
	}

	// Atomically rename temp file to final chunk path
	if err := os.Rename(tempPath, chunkPath); err != nil {
		// Clean up temp file on rename failure
		os.Remove(tempPath)
		return fmt.Errorf("failed to finalize chunk write: %w", err)
	}

	return nil
}

// IsVerified checks if a file has been fully downloaded and verified.
// Returns true if the _verified marker file exists for the given SHA256.
func (m *Manager) IsVerified(sha256 string) bool {
	if err := validateSHA256(sha256); err != nil {
		return false
	}

	markerPath := filepath.Join(m.dir, sha256, "_verified")
	_, err := os.Stat(markerPath)
	return err == nil
}

// MarkVerified creates a verification marker file to indicate that
// a file has been fully downloaded and passed SHA256 verification.
//
// This should only be called after:
//  1. All chunks have been downloaded
//  2. The complete file's SHA256 has been verified
//
// Returns an error if the marker file cannot be created.
func (m *Manager) MarkVerified(sha256 string) error {
	if err := validateSHA256(sha256); err != nil {
		return err
	}

	// Ensure chunk directory exists
	chunkDir := filepath.Join(m.dir, sha256)
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("failed to create chunk directory: %w", err)
	}

	// Create empty marker file
	markerPath := filepath.Join(m.dir, sha256, "_verified")
	if err := os.WriteFile(markerPath, []byte{}, 0644); err != nil {
		return fmt.Errorf("failed to create verification marker: %w", err)
	}

	return nil
}
