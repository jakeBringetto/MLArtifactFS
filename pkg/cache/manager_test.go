package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestNewManager verifies that NewManager creates the cache directory
// and returns a valid Manager instance.
func TestNewManager(t *testing.T) {
	tempDir := t.TempDir()
	cacheDir := filepath.Join(tempDir, "cache")

	manager, err := NewManager(cacheDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("NewManager returned nil manager")
	}

	if manager.dir != cacheDir {
		t.Errorf("Manager.dir = %q, want %q", manager.dir, cacheDir)
	}

	// Verify cache directory was created
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("Cache directory not created: %v", err)
	}

	if !info.IsDir() {
		t.Errorf("Cache path is not a directory")
	}
}

// TestNewManagerExistingDir verifies that NewManager works with
// an existing directory.
func TestNewManagerExistingDir(t *testing.T) {
	tempDir := t.TempDir()

	manager, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("NewManager with existing dir failed: %v", err)
	}

	if manager.dir != tempDir {
		t.Errorf("Manager.dir = %q, want %q", manager.dir, tempDir)
	}
}

// TestWriteAndRead verifies the basic write and read flow.
func TestWriteAndRead(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "a" + string(bytes.Repeat([]byte("0"), 63)) // Valid 64-char hash
	chunkIndex := 0
	data := []byte("test chunk data")

	// Write chunk
	if err := manager.Write(sha256, chunkIndex, data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read chunk back
	readData, err := manager.Read(sha256, chunkIndex)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	// Verify data matches
	if !bytes.Equal(readData, data) {
		t.Errorf("Read data = %q, want %q", readData, data)
	}
}

// TestWriteMultipleChunks verifies writing multiple chunks for the same file.
func TestWriteMultipleChunks(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "b" + string(bytes.Repeat([]byte("0"), 63))
	chunks := [][]byte{
		[]byte("chunk 0 data"),
		[]byte("chunk 1 data"),
		[]byte("chunk 2 data"),
	}

	// Write all chunks
	for i, data := range chunks {
		if err := manager.Write(sha256, i, data); err != nil {
			t.Fatalf("Write chunk %d failed: %v", i, err)
		}
	}

	// Read all chunks back and verify
	for i, expected := range chunks {
		readData, err := manager.Read(sha256, i)
		if err != nil {
			t.Fatalf("Read chunk %d failed: %v", i, err)
		}

		if !bytes.Equal(readData, expected) {
			t.Errorf("Chunk %d: got %q, want %q", i, readData, expected)
		}
	}
}

// TestHas verifies chunk existence checking.
func TestHas(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "c" + string(bytes.Repeat([]byte("0"), 63))
	data := []byte("test data")

	// Chunk should not exist initially
	if manager.Has(sha256, 0) {
		t.Error("Has returned true for non-existent chunk")
	}

	// Write chunk
	if err := manager.Write(sha256, 0, data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Chunk should exist now
	if !manager.Has(sha256, 0) {
		t.Error("Has returned false for existing chunk")
	}

	// Different chunk should not exist
	if manager.Has(sha256, 1) {
		t.Error("Has returned true for non-existent chunk 1")
	}
}

// TestMarkVerified verifies the verification marker functionality.
func TestMarkVerified(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "d" + string(bytes.Repeat([]byte("0"), 63))

	// Should not be verified initially
	if manager.IsVerified(sha256) {
		t.Error("IsVerified returned true before marking")
	}

	// Mark as verified
	if err := manager.MarkVerified(sha256); err != nil {
		t.Fatalf("MarkVerified failed: %v", err)
	}

	// Should be verified now
	if !manager.IsVerified(sha256) {
		t.Error("IsVerified returned false after marking")
	}

	// Verify marker file exists
	markerPath := filepath.Join(manager.dir, sha256, "_verified")
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("Marker file not found: %v", err)
	}
}

// TestIsVerified verifies checking verification status.
func TestIsVerified(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256a := "e" + string(bytes.Repeat([]byte("0"), 63))
	sha256b := "f" + string(bytes.Repeat([]byte("0"), 63))

	// Mark first file as verified
	if err := manager.MarkVerified(sha256a); err != nil {
		t.Fatalf("MarkVerified failed: %v", err)
	}

	// First should be verified
	if !manager.IsVerified(sha256a) {
		t.Error("IsVerified returned false for verified file")
	}

	// Second should not be verified
	if manager.IsVerified(sha256b) {
		t.Error("IsVerified returned true for unverified file")
	}
}

// TestInvalidSHA256 verifies that invalid SHA256 hashes are rejected.
func TestInvalidSHA256(t *testing.T) {
	manager, _ := NewManager(t.TempDir())
	data := []byte("test data")

	testCases := []struct {
		name   string
		sha256 string
	}{
		{"empty", ""},
		{"too short", "abc123"},
		{"too long", string(bytes.Repeat([]byte("a"), 65))},
		{"uppercase", "A" + string(bytes.Repeat([]byte("0"), 63))},
		{"invalid chars", "g" + string(bytes.Repeat([]byte("0"), 63))},
		{"special chars", "../etc/passwd" + string(bytes.Repeat([]byte("0"), 51))},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Write should fail
			if err := manager.Write(tc.sha256, 0, data); err == nil {
				t.Error("Write should fail with invalid SHA256")
			}

			// Read should fail
			if _, err := manager.Read(tc.sha256, 0); err == nil {
				t.Error("Read should fail with invalid SHA256")
			}

			// Has should return false
			if manager.Has(tc.sha256, 0) {
				t.Error("Has should return false for invalid SHA256")
			}

			// MarkVerified should fail
			if err := manager.MarkVerified(tc.sha256); err == nil {
				t.Error("MarkVerified should fail with invalid SHA256")
			}

			// IsVerified should return false
			if manager.IsVerified(tc.sha256) {
				t.Error("IsVerified should return false for invalid SHA256")
			}
		})
	}
}

// TestNegativeChunkIndex verifies that negative chunk indices are rejected.
func TestNegativeChunkIndex(t *testing.T) {
	manager, _ := NewManager(t.TempDir())
	sha256 := "1" + string(bytes.Repeat([]byte("0"), 63))
	data := []byte("test data")

	// Write with negative index should fail
	if err := manager.Write(sha256, -1, data); err == nil {
		t.Error("Write should fail with negative chunk index")
	}

	// Read with negative index should fail
	if _, err := manager.Read(sha256, -1); err == nil {
		t.Error("Read should fail with negative chunk index")
	}

	// Has with negative index should return false
	if manager.Has(sha256, -1) {
		t.Error("Has should return false for negative chunk index")
	}
}

// TestMissingChunk verifies that reading a non-existent chunk returns an error.
func TestMissingChunk(t *testing.T) {
	manager, _ := NewManager(t.TempDir())
	sha256 := "2" + string(bytes.Repeat([]byte("0"), 63))

	// Read non-existent chunk
	data, err := manager.Read(sha256, 0)
	if err == nil {
		t.Error("Read should fail for missing chunk")
	}

	if data != nil {
		t.Errorf("Read should return nil data for missing chunk, got %v", data)
	}

	// Error should mention "not found"
	if err != nil && err.Error() == "" {
		t.Error("Error message should not be empty")
	}
}

// TestChunkPath verifies the chunk path computation.
func TestChunkPath(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "3" + string(bytes.Repeat([]byte("0"), 63))
	chunkIndex := 5

	path := manager.ChunkPath(sha256, chunkIndex)
	expected := filepath.Join(manager.dir, sha256, "chunk_5")

	if path != expected {
		t.Errorf("ChunkPath = %q, want %q", path, expected)
	}
}

// TestAtomicWrite verifies that the atomic write pattern works correctly.
func TestAtomicWrite(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "4" + string(bytes.Repeat([]byte("0"), 63))
	data := []byte("atomic write test")

	// Write chunk
	if err := manager.Write(sha256, 0, data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify temp file was cleaned up
	tempPath := manager.ChunkPath(sha256, 0) + ".tmp"
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Error("Temporary file should be removed after successful write")
	}

	// Verify final chunk file exists
	chunkPath := manager.ChunkPath(sha256, 0)
	if _, err := os.Stat(chunkPath); err != nil {
		t.Errorf("Chunk file should exist: %v", err)
	}
}

// TestIdempotentWrite verifies that writing the same chunk multiple times is safe.
func TestIdempotentWrite(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "5" + string(bytes.Repeat([]byte("0"), 63))
	data1 := []byte("first write")
	data2 := []byte("second write")

	// First write
	if err := manager.Write(sha256, 0, data1); err != nil {
		t.Fatalf("First write failed: %v", err)
	}

	// Read and verify
	readData, err := manager.Read(sha256, 0)
	if err != nil {
		t.Fatalf("Read after first write failed: %v", err)
	}
	if !bytes.Equal(readData, data1) {
		t.Errorf("After first write: got %q, want %q", readData, data1)
	}

	// Second write (overwrite)
	if err := manager.Write(sha256, 0, data2); err != nil {
		t.Fatalf("Second write failed: %v", err)
	}

	// Read and verify (should have new data)
	readData, err = manager.Read(sha256, 0)
	if err != nil {
		t.Fatalf("Read after second write failed: %v", err)
	}
	if !bytes.Equal(readData, data2) {
		t.Errorf("After second write: got %q, want %q", readData, data2)
	}
}

// TestEmptyChunk verifies that empty chunks can be written and read.
func TestEmptyChunk(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "6" + string(bytes.Repeat([]byte("0"), 63))
	data := []byte{}

	// Write empty chunk
	if err := manager.Write(sha256, 0, data); err != nil {
		t.Fatalf("Write empty chunk failed: %v", err)
	}

	// Read empty chunk
	readData, err := manager.Read(sha256, 0)
	if err != nil {
		t.Fatalf("Read empty chunk failed: %v", err)
	}

	if len(readData) != 0 {
		t.Errorf("Empty chunk should have length 0, got %d", len(readData))
	}
}

// TestLargeChunk verifies that large chunks (16 MB) can be written and read.
func TestLargeChunk(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	sha256 := "7" + string(bytes.Repeat([]byte("0"), 63))

	// Create 16 MB chunk
	chunkSize := 16 * 1024 * 1024
	data := make([]byte, chunkSize)
	for i := 0; i < chunkSize; i++ {
		data[i] = byte(i % 256)
	}

	// Write large chunk
	if err := manager.Write(sha256, 0, data); err != nil {
		t.Fatalf("Write large chunk failed: %v", err)
	}

	// Read large chunk
	readData, err := manager.Read(sha256, 0)
	if err != nil {
		t.Fatalf("Read large chunk failed: %v", err)
	}

	// Verify size
	if len(readData) != chunkSize {
		t.Errorf("Chunk size = %d, want %d", len(readData), chunkSize)
	}

	// Verify contents
	if !bytes.Equal(readData, data) {
		t.Error("Large chunk data mismatch")
	}
}

// TestMultipleFiles verifies that multiple files can be cached simultaneously.
func TestMultipleFiles(t *testing.T) {
	manager, _ := NewManager(t.TempDir())

	files := []struct {
		sha256 string
		data   []byte
	}{
		{"8" + string(bytes.Repeat([]byte("0"), 63)), []byte("file 1 data")},
		{"9" + string(bytes.Repeat([]byte("0"), 63)), []byte("file 2 data")},
		{"a" + string(bytes.Repeat([]byte("1"), 63)), []byte("file 3 data")},
	}

	// Write chunks for all files
	for _, f := range files {
		if err := manager.Write(f.sha256, 0, f.data); err != nil {
			t.Fatalf("Write failed for %s: %v", f.sha256[:8], err)
		}
	}

	// Verify all files are cached correctly
	for _, f := range files {
		readData, err := manager.Read(f.sha256, 0)
		if err != nil {
			t.Fatalf("Read failed for %s: %v", f.sha256[:8], err)
		}

		if !bytes.Equal(readData, f.data) {
			t.Errorf("Data mismatch for %s", f.sha256[:8])
		}
	}
}

// TestCacheDirectoryStructure verifies the correct directory structure is created.
func TestCacheDirectoryStructure(t *testing.T) {
	tempDir := t.TempDir()
	manager, _ := NewManager(tempDir)

	sha256 := "b" + string(bytes.Repeat([]byte("1"), 63))
	data := []byte("test data")

	// Write chunk and mark verified
	if err := manager.Write(sha256, 0, data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := manager.MarkVerified(sha256); err != nil {
		t.Fatalf("MarkVerified failed: %v", err)
	}

	// Verify directory structure
	expectedFiles := []string{
		filepath.Join(tempDir, sha256, "chunk_0"),
		filepath.Join(tempDir, sha256, "_verified"),
	}

	for _, path := range expectedFiles {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("Expected file not found: %s (%v)", path, err)
		}
	}
}
