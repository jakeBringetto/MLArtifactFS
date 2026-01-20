package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// hashFile computes the SHA256 hash of a file.
// It streams the file through the hasher to avoid loading large files into memory.
// Returns the hash as a lowercase hexadecimal string.
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()

	// Stream file contents through hasher (memory-efficient)
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	// Return hex-encoded hash (lowercase)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
