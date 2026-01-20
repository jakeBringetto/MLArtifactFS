package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerate_SimpleDirectory(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()

	// Create test files
	if err := os.WriteFile(filepath.Join(tmpDir, "message.txt"), []byte("Hello S3!\n"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(`{"name": "test"}`), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Generate manifest
	m, err := Generate(tmpDir, "test-id", "v1.0", "https://example.com/test", []string{"message.txt"})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify manifest fields
	if m.ArtifactID != "test-id" {
		t.Errorf("Expected artifact_id 'test-id', got '%s'", m.ArtifactID)
	}
	if m.Version != "v1.0" {
		t.Errorf("Expected version 'v1.0', got '%s'", m.Version)
	}
	if m.MountPath != "/mnt/mlmodel" {
		t.Errorf("Expected mount_path '/mnt/mlmodel', got '%s'", m.MountPath)
	}

	// Verify prefetch
	if len(m.Prefetch) != 1 || m.Prefetch[0] != "message.txt" {
		t.Errorf("Expected prefetch ['message.txt'], got %v", m.Prefetch)
	}

	// Verify files
	if len(m.Files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(m.Files))
	}

	// Check that files have expected properties
	for _, f := range m.Files {
		if f.Path == "" {
			t.Error("File path is empty")
		}
		if f.URL == "" {
			t.Error("File URL is empty")
		}
		if f.Size == 0 {
			t.Error("File size is 0")
		}
		if f.SHA256 == "" {
			t.Error("File SHA256 is empty")
		}
		if f.Compression != "none" {
			t.Errorf("Expected compression 'none', got '%s'", f.Compression)
		}
	}
}

func TestGenerate_NestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested structure
	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root"), 0644); err != nil {
		t.Fatalf("Failed to create root file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatalf("Failed to create nested file: %v", err)
	}

	// Generate manifest
	m, err := Generate(tmpDir, "test", "v1", "https://example.com", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should have 2 files
	if len(m.Files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(m.Files))
	}

	// Check that nested file has correct path (Unix-style)
	found := false
	for _, f := range m.Files {
		if f.Path == "subdir/nested.txt" {
			found = true
			if f.URL != "https://example.com/subdir/nested.txt" {
				t.Errorf("Expected URL 'https://example.com/subdir/nested.txt', got '%s'", f.URL)
			}
		}
	}
	if !found {
		t.Error("Did not find nested file with path 'subdir/nested.txt'")
	}
}

func TestGenerate_EmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	m, err := Generate(tmpDir, "empty", "v1", "https://example.com", nil)
	if err != nil {
		t.Fatalf("Generate failed for empty directory: %v", err)
	}

	if len(m.Files) != 0 {
		t.Errorf("Expected 0 files for empty directory, got %d", len(m.Files))
	}
}

func TestGenerate_HiddenFilesSkipped(t *testing.T) {
	tmpDir := t.TempDir()

	// Create regular file
	if err := os.WriteFile(filepath.Join(tmpDir, "visible.txt"), []byte("visible"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Create hidden files
	if err := os.WriteFile(filepath.Join(tmpDir, ".hidden"), []byte("hidden"), 0644); err != nil {
		t.Fatalf("Failed to create hidden file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".DS_Store"), []byte("ds_store"), 0644); err != nil {
		t.Fatalf("Failed to create .DS_Store: %v", err)
	}

	m, err := Generate(tmpDir, "test", "v1", "https://example.com", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Should only have visible.txt
	if len(m.Files) != 1 {
		t.Errorf("Expected 1 file (hidden files skipped), got %d", len(m.Files))
	}

	if m.Files[0].Path != "visible.txt" {
		t.Errorf("Expected only 'visible.txt', got '%s'", m.Files[0].Path)
	}
}

func TestGenerate_URLConstruction(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	tests := []struct {
		name      string
		urlPrefix string
		expected  string
	}{
		{
			name:      "URL without trailing slash",
			urlPrefix: "https://bucket.s3.amazonaws.com/path",
			expected:  "https://bucket.s3.amazonaws.com/path/test.txt",
		},
		{
			name:      "URL with trailing slash",
			urlPrefix: "https://bucket.s3.amazonaws.com/path/",
			expected:  "https://bucket.s3.amazonaws.com/path/test.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Generate(tmpDir, "test", "v1", tt.urlPrefix, nil)
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			if m.Files[0].URL != tt.expected {
				t.Errorf("Expected URL '%s', got '%s'", tt.expected, m.Files[0].URL)
			}
		})
	}
}

func TestGenerate_SHA256Correctness(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with known content
	content := "test\n"
	expectedHash := "f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2"

	if err := os.WriteFile(filepath.Join(tmpDir, "test.txt"), []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	m, err := Generate(tmpDir, "test", "v1", "https://example.com", nil)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if m.Files[0].SHA256 != expectedHash {
		t.Errorf("Expected SHA256 '%s', got '%s'", expectedHash, m.Files[0].SHA256)
	}
}

func TestGenerate_InvalidInputs(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		id        string
		version   string
		urlPrefix string
		wantErr   bool
	}{
		{
			name:      "Empty ID",
			id:        "",
			version:   "v1",
			urlPrefix: "https://example.com",
			wantErr:   true,
		},
		{
			name:      "Empty version",
			id:        "test",
			version:   "",
			urlPrefix: "https://example.com",
			wantErr:   true,
		},
		{
			name:      "Invalid URL prefix (no http/https)",
			id:        "test",
			version:   "v1",
			urlPrefix: "s3://bucket/path",
			wantErr:   true,
		},
		{
			name:      "Valid inputs",
			id:        "test",
			version:   "v1",
			urlPrefix: "https://example.com",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Generate(tmpDir, tt.id, tt.version, tt.urlPrefix, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Expected error=%v, got error=%v", tt.wantErr, err)
			}
		})
	}
}

func TestGenerate_NonexistentDirectory(t *testing.T) {
	_, err := Generate("/nonexistent/directory", "test", "v1", "https://example.com", nil)
	if err == nil {
		t.Error("Expected error for nonexistent directory, got nil")
	}
}

func TestNormalizePrefetchPaths(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "Trim whitespace",
			input:    []string{" config.json ", "  tokenizer.json"},
			expected: []string{"config.json", "tokenizer.json"},
		},
		{
			name:     "Remove empty strings",
			input:    []string{"file.txt", "", "  ", "other.txt"},
			expected: []string{"file.txt", "other.txt"},
		},
		{
			name:     "Empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "Nil input",
			input:    nil,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePrefetchPaths(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d items, got %d", len(tt.expected), len(result))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("At index %d: expected '%s', got '%s'", i, tt.expected[i], v)
				}
			}
		})
	}
}
