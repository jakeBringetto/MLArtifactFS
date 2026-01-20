package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Generate creates a manifest by walking a directory tree and computing
// metadata for each file (size, SHA256 hash, URL).
//
// Parameters:
//   - dir: Local directory to scan
//   - id: Artifact identifier (e.g., "llama-7b")
//   - version: Version string (e.g., "v1.0")
//   - urlPrefix: Base URL for S3 storage (e.g., "https://bucket.s3.amazonaws.com/models/v1")
//   - prefetchPaths: List of file paths to prefetch at mount time
//
// Returns a Manifest struct or an error if the directory cannot be read.
func Generate(dir string, id string, version string, urlPrefix string, prefetchPaths []string) (*Manifest, error) {
	// Validate inputs
	if id == "" || version == "" {
		return nil, fmt.Errorf("id and version are required")
	}

	// Check that directory exists
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", dir)
	}

	// Validate URL prefix (basic check)
	if !strings.HasPrefix(urlPrefix, "http://") && !strings.HasPrefix(urlPrefix, "https://") {
		return nil, fmt.Errorf("url-prefix must start with http:// or https://")
	}

	// Strip trailing slash from URL prefix
	urlPrefix = strings.TrimSuffix(urlPrefix, "/")

	// Initialize manifest
	manifest := &Manifest{
		ArtifactID: id,
		Version:    version,
		MountPath:  "/mnt/mlmodel", // Default mount path
		Prefetch:   normalizePrefetchPaths(prefetchPaths),
		Files:      []File{},
	}

	// Walk directory tree
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip hidden files (starting with .)
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Skip symlinks (for MVP simplicity)
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		// Compute relative path from base directory
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}

		// Normalize path to Unix-style (forward slashes)
		relPath = filepath.ToSlash(relPath)

		// Compute SHA256 hash
		hash, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("failed to hash file %s: %w", relPath, err)
		}

		// Construct S3 URL
		fileURL := urlPrefix + "/" + relPath

		// Add file to manifest
		manifest.Files = append(manifest.Files, File{
			Path:        relPath,
			URL:         fileURL,
			Size:        info.Size(),
			SHA256:      hash,
			Compression: "none", // MVP: no compression support
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return manifest, nil
}

// normalizePrefetchPaths processes the prefetch paths list:
// - Trims whitespace
// - Converts to forward slashes
// - Removes empty strings
func normalizePrefetchPaths(paths []string) []string {
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Convert to forward slashes (Unix-style)
		p = filepath.ToSlash(p)
		normalized = append(normalized, p)
	}
	return normalized
}
