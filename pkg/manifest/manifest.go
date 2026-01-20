package manifest

import (
	"encoding/json"
	"os"
)

// Manifest defines the structure of a manifest file that describes
// a virtual filesystem for lazy-loading files from remote storage.
type Manifest struct {
	ArtifactID string   `json:"artifact_id"`
	Version    string   `json:"version"`
	MountPath  string   `json:"mount_path"`
	Prefetch   []string `json:"prefetch"`
	Files      []File   `json:"files"`
}

// File represents a single file in the manifest with its metadata.
type File struct {
	Path        string `json:"path"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Compression string `json:"compression"`
}

// Marshal serializes a Manifest to pretty-printed JSON.
// The output is formatted with 2-space indentation for readability
// and version control (manifests are meant to be committed to Git).
func Marshal(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// Load reads a manifest from a JSON file.
// This function will be used by the mount command (Milestone 7).
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	return &m, nil
}
