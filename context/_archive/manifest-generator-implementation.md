# Manifest Generator Implementation Plan

## Overview
Implement Milestone 2 from the implementation plan: Create the manifest generator that scans a local directory and produces a JSON manifest file. This is a critical foundation component that enables the entire MLArtifactFS system.

**Goal:** Enable users to run `mlfs generate` to create a manifest from any local directory.

---

## Background Context

### The Core Problem: Reproducibility & Sharing

MLArtifactFS applies **container principles** to ML model artifacts:

| Container Principle | MLArtifactFS Equivalent |
|---------------------|-------------------------|
| **Reproducible builds** | Content-addressable manifests (SHA256) |
| **Shared registries** | S3 as centralized model storage |
| **Version control** | Git-tracked manifests (100 KB, not 25 GB) |
| **Lazy pull** | Lazy-load chunks on-demand from S3 |
| **Cached per node** | Local disk cache with SHA256 verification |

### The Problem: Models Baked Into Images

**Current workflow (slow iteration):**
```bash
# Download model
huggingface-cli download llama-7b --local-dir ./llama-7b

# Bake into image
FROM python:3.11
COPY ./llama-7b /app/model  # ← 25 GB in image layer
COPY inference.py /app/
CMD python /app/inference.py --model /app/model

# Build & push (30+ minutes, 25 GB upload)
docker build -t my-app:llama-7b .
docker push my-app:llama-7b

# Want to A/B test different model? Rebuild everything.
```

**Pain points:**
- Image builds take 10-30 minutes (uploading GBs)
- A/B testing requires separate images per model
- Image registry fills with near-identical images
- No separation of code vs. data

### The Solution: Decoupled, Versioned Artifacts

**MLArtifactFS workflow (fast iteration):**
```bash
# 1. Generate manifest (one-time per model version)
mlfs generate \
  --id llama-7b \
  --version v1.0 \
  --url-prefix https://s3.../models/llama-7b/v1.0 \
  --prefetch config.json,tokenizer.json \
  ./llama-7b > llama-7b-v1.0-manifest.json

# 2. Upload model to S3 (one-time, shared across all users/nodes)
aws s3 sync ./llama-7b s3://my-models/llama-7b/v1.0/

# 3. Build tiny image (NO model files, just code)
FROM python:3.11
COPY inference.py /app/
CMD mlfs mount --manifest /manifest.json /mnt/model && \
    python /app/inference.py --model /mnt/model

# Image is now 500 MB instead of 25 GB

# 4. A/B test by swapping manifests (instant)
docker run -v llama-7b-v1.0.json:/manifest.json my-app
docker run -v llama-7b-v2.0.json:/manifest.json my-app  # Different model, same image!
docker run -v llama-13b-v1.0.json:/manifest.json my-app # Different model size, same image!
```

### Why S3? (Shared, Reproducible Storage)

**S3 acts like a container registry, but for models:**

```
Docker Hub/ECR              S3 Model Storage
     ↓                            ↓
Container images     ←→    Model artifacts
     ↓                            ↓
Pulled on-demand     ←→    Lazy-loaded on-demand
     ↓                            ↓
Cached per node      ←→    Cached per node
     ↓                            ↓
docker pull image:tag ←→   mlfs mount manifest.json
```

**Benefits:**
- ✅ **Reproducible:** SHA256 hashes verify exact file contents
- ✅ **Shared:** Multiple nodes/users fetch from same S3 location
- ✅ **Versioned:** Manifests in Git track exact model versions
- ✅ **Efficient:** Lazy-load only needed chunks (not entire 25 GB)

### What is the Manifest?

The manifest is a **version-controlled, content-addressable** JSON file that defines:
- Artifact metadata (ID, version)
- List of files with S3 URLs, sizes, and SHA256 hashes
- Prefetch hints for critical files

**Key property:** Manifest is tiny (100 KB) and can be version-controlled in Git, unlike 25 GB model files.

### How It Fits Into Later Stages
1. **Cache Manager (M3)**: Uses SHA256 hashes to organize cache directory
2. **S3 Client (M4)**: Fetches from URLs defined in manifest
3. **Fetch Manager (M5)**: Uses file sizes for chunk alignment, SHA256 for verification
4. **FUSE FS (M6)**: Builds in-memory directory tree from manifest.Files[]
5. **Mount Command (M7)**: Loads manifest and prefetches files in manifest.Prefetch[]

---

## Component Breakdown

### 1. Manifest Data Structures (`pkg/manifest/manifest.go`)

Define Go structs that match the JSON schema from 03-design.md:

```go
type Manifest struct {
    ArtifactID string   `json:"artifact_id"`
    Version    string   `json:"version"`
    MountPath  string   `json:"mount_path"`
    Prefetch   []string `json:"prefetch"`
    Files      []File   `json:"files"`
}

type File struct {
    Path        string `json:"path"`
    URL         string `json:"url"`
    Size        int64  `json:"size"`
    SHA256      string `json:"sha256"`
    Compression string `json:"compression"`
}
```

**Key Design Decisions:**
- Use standard Go JSON tags for marshaling
- `MountPath` is informational (suggested mount point)
- `Compression` hardcoded to "none" for MVP
- `Path` uses forward slashes (Unix-style) regardless of OS

---

### 2. Directory Walking (`pkg/manifest/generator.go`)

Implement `Generate()` function that walks a directory tree:

```go
func Generate(
    dir string,
    id string,
    version string,
    urlPrefix string,
    prefetchPaths []string,
) (*Manifest, error)
```

**Algorithm:**
1. Validate inputs:
   - `dir` must be an existing directory
   - `id` and `version` are non-empty
   - `urlPrefix` is a valid URL (basic check)
2. Walk directory using `filepath.Walk`:
   - Skip directories (only process regular files)
   - Skip hidden files (starting with `.`)
   - Skip symlinks (for MVP simplicity)
3. For each file:
   - Compute relative path from base directory
   - Normalize path separators to `/` (Unix-style)
   - Calculate file size using `os.Stat()`
   - Compute SHA256 hash by reading file
   - Construct S3 URL: `urlPrefix + "/" + relativePath`
4. Return `Manifest` struct with all files

**Error Handling:**
- Return error if directory doesn't exist or isn't readable
- Return error if any file cannot be read or hashed
- Log warnings for skipped files (symlinks, hidden files)

---

### 3. SHA256 Hashing (`pkg/manifest/hash.go`)

Implement helper function for hashing files:

```go
func hashFile(path string) (string, error)
```

**Implementation:**
- Open file for reading
- Create `sha256.New()` hasher
- Use `io.Copy()` to stream file through hasher (memory-efficient)
- Return hex-encoded hash string

**Performance:**
- For large models (25 GB), hashing takes ~30-60 seconds
- This is acceptable for one-time manifest generation
- Future optimization: parallel hashing with worker pool (not MVP)

---

### 4. Prefetch Path Matching (`pkg/manifest/generator.go`)

**Input:** Comma-separated string from CLI flag (e.g., "config.json,tokenizer.json")

**Processing:**
1. Split by comma
2. Trim whitespace from each path
3. Normalize to forward slashes
4. Store in `manifest.Prefetch[]`

**Validation:**
- Warn if prefetch path doesn't match any file in directory
- This is non-fatal (user might add files later to S3)

---

### 5. JSON Marshaling (`pkg/manifest/manifest.go`)

Implement function to serialize manifest:

```go
func Marshal(m *Manifest) ([]byte, error)
```

**Implementation:**
- Use `json.MarshalIndent(m, "", "  ")` for pretty-printing
- Return formatted JSON bytes

**Why Pretty-Print?**
- Manifests are human-readable and version-controlled
- 2-space indentation matches industry standard

---

### 6. CLI Integration (`cmd/mlfs/main.go`)

Update `generateCmd()` function (currently a TODO stub):

**Steps:**
1. Parse flags (already done in skeleton)
2. Validate required flags (`--id`, `--version`, `--url-prefix`)
3. Parse prefetch flag (comma-separated string → []string)
4. Call `manifest.Generate(dir, id, version, urlPrefix, prefetchPaths)`
5. Marshal manifest to JSON
6. Write JSON to stdout
7. Handle errors gracefully with clear messages

**Example Usage:**
```bash
mlfs generate \
  --id distilbert \
  --version v1 \
  --url-prefix https://my-bucket.s3.amazonaws.com/distilbert/v1 \
  --prefetch config.json,tokenizer.json \
  ./distilbert > manifest.json
```

**Expected Output (stdout):**
```json
{
  "artifact_id": "distilbert",
  "version": "v1",
  "mount_path": "/mnt/mlmodel",
  "prefetch": ["config.json", "tokenizer.json"],
  "files": [
    {
      "path": "config.json",
      "url": "https://my-bucket.s3.amazonaws.com/distilbert/v1/config.json",
      "size": 1234,
      "sha256": "abc123...",
      "compression": "none"
    },
    ...
  ]
}
```

**Expected Output (stderr):**
Progress logs, warnings, errors (does NOT interfere with JSON on stdout)

---

## Edge Cases and Error Handling

### Input Validation
| Case | Behavior |
|------|----------|
| Directory doesn't exist | Exit with error: "Error: directory not found: \<path\>" |
| Directory is empty | Generate manifest with empty `files[]` array |
| `--url-prefix` has trailing slash | Strip trailing slash before concatenation |
| Prefetch file doesn't exist in dir | Log warning: "Warning: prefetch file not found: \<path\>" |
| File permissions prevent reading | Exit with error: "Error: cannot read file: \<path\>" |

### File Type Handling
| File Type | Behavior |
|-----------|----------|
| Regular file | Include in manifest |
| Directory | Skip (directories are implicit from file paths) |
| Symlink | Skip with warning (MVP limitation) |
| Hidden file (`.git`, `.DS_Store`) | Skip silently |
| Empty file (0 bytes) | Include with size=0, SHA256 of empty string |

### Path Normalization
- Convert Windows backslashes to forward slashes
- Remove leading `./` from relative paths
- Preserve subdirectories (e.g., `weights/pytorch_model.bin`)

---

## Testing Strategy

### Philosophy: Simple Files First, Models Later

**Development testing:** Use simple text and binary files to verify correctness
**Integration testing:** Use real ML models to validate at scale (optional)

**Why this approach:**
- ML models are just "large files" to the filesystem
- Faster iteration with small test files
- No dependency on HuggingFace or specific model formats
- Same code works for any file type

### Unit Tests (`pkg/manifest/generator_test.go`)

**Test Cases (using simple files):**
1. `TestGenerate_SimpleDirectory`: 3 text files, no subdirectories
   ```
   test-data/
     message.txt      (10 bytes: "Hello S3!\n")
     config.json      (20 bytes: '{"name": "test"}')
     data.bin         (1 KB random bytes)
   ```

2. `TestGenerate_NestedDirectories`: Files in subdirectories
   ```
   test-data/
     readme.txt
     subdir/
       file1.txt
       file2.txt
   ```

3. `TestGenerate_EmptyDirectory`: Should succeed with empty `files[]` array

4. `TestGenerate_PrefetchMatching`: Verify prefetch paths populate correctly

5. `TestGenerate_URLConstruction`: Verify URL concatenation handles trailing slashes

6. `TestGenerate_SHA256Correctness`: Hash a known file, verify output
   - Use test file with content "test\n"
   - Expected SHA256: `f2ca1bb6c7e907d06dafe4687e579fce76b37e4e93b7605022da52e6ccc26fd2`

7. `TestGenerate_HiddenFilesSkipped`: Ensure `.git`, `.DS_Store` files ignored

8. `TestGenerate_SymlinksSkipped`: Verify symlinks are skipped

**Test Helpers:**
- Create temporary directories with test files
- Use `t.TempDir()` for automatic cleanup
- Use `echo "content" > file.txt` for predictable hashes

### Integration Test (Simple CLI Test)

**Phase 1: Basic CLI Validation**
```bash
# Create simple test data
mkdir test-data
echo "Hello from S3!" > test-data/message.txt
echo '{"test": true}' > test-data/config.json
dd if=/dev/urandom of=test-data/large.bin bs=1M count=10  # 10 MB file

# Run generator
./mlfs generate \
  --id test-artifact \
  --version v1.0 \
  --url-prefix https://test-bucket.s3.amazonaws.com/test/v1.0 \
  --prefetch message.txt,config.json \
  ./test-data > manifest.json

# Validate output
jq . manifest.json  # Check valid JSON
jq '.files | length' manifest.json  # Should be 3
jq '.prefetch' manifest.json  # Should be ["message.txt", "config.json"]

# Verify SHA256 (manual check)
sha256sum test-data/message.txt
jq -r '.files[] | select(.path == "message.txt") | .sha256' manifest.json
# ↑ These should match
```

**Phase 2: S3 Integration (Optional, for later milestones)**
```bash
# Upload test files to S3
aws s3 sync ./test-data s3://test-bucket/test/v1.0/

# Later: test mlfs mount with this manifest (Milestone 7)
```

### Real Model Testing (Optional Validation)

**Only after basic implementation works:**
```bash
# Download small model (~500 MB, not 25 GB)
huggingface-cli download distilbert-base-uncased --local-dir ./distilbert

# Generate manifest
./mlfs generate \
  --id distilbert \
  --version test \
  --url-prefix https://s3.../distilbert/test \
  --prefetch config.json,tokenizer.json \
  ./distilbert > distilbert-manifest.json

# Verify structure (just check it works, no inference needed)
jq '.files | length' distilbert-manifest.json
ls -lh ./distilbert  # Compare file count
```

**Note:** Real model testing is NOT required for Milestone 2 completion. It's only for confidence before deploying to production.

---

## Dependencies and Imports

**Standard Library:**
- `crypto/sha256` - hashing
- `encoding/hex` - hash encoding
- `encoding/json` - JSON marshaling
- `fmt` - error formatting
- `io` - file streaming
- `os` - file operations
- `path/filepath` - directory walking, path manipulation
- `strings` - string processing (split, trim)

**External Libraries:**
- None (generator is stdlib-only)

---

## File Structure

```
pkg/manifest/
  manifest.go       # Manifest and File structs, Marshal()
  generator.go      # Generate() function, directory walking
  hash.go           # hashFile() helper
  generator_test.go # Unit tests

cmd/mlfs/
  main.go           # Update generateCmd() to call manifest.Generate()
```

---

## Implementation Order

### Phase 1: Core Data Structures
1. Define `Manifest` and `File` structs in `manifest.go`
2. Implement `Marshal()` function
3. Write unit test for marshaling

### Phase 2: File Hashing
1. Implement `hashFile()` in `hash.go`
2. Write unit tests with known test vectors

### Phase 3: Directory Walking
1. Implement `Generate()` in `generator.go`
2. Handle file walking, path normalization
3. Integrate hashing
4. Build file list

### Phase 4: Prefetch Logic
1. Parse prefetch string to slice
2. Populate `manifest.Prefetch[]`
3. Add warning for non-matching paths

### Phase 5: CLI Integration
1. Update `generateCmd()` in `main.go`
2. Wire up flag parsing to `Generate()` call
3. Handle stdout/stderr correctly

### Phase 6: Testing
1. Write unit tests for all functions
2. Create simple test data (text files, small binaries)
3. Run integration test with simple files
4. (Optional) Test with real model directory for validation

---

## Integration Points with Future Milestones

### M3 (Cache Manager)
- Cache manager will use SHA256 from manifest to organize cache:
  ```
  <cache-dir>/<sha256>/chunk_0
  ```
- Generator must produce consistent SHA256 format (lowercase hex)

### M5 (Fetch Manager)
- Fetch manager will use:
  - `File.Size` to calculate chunk boundaries
  - `File.SHA256` to verify downloaded files
  - `File.URL` to fetch from S3
- Generator must ensure URLs are valid and size matches actual file

### M6 (FUSE FS)
- FUSE will build directory tree from `manifest.Files[].Path`
- Generator must ensure paths use Unix-style separators
- Generator must ensure paths don't have leading slashes

### M7 (Mount Command)
- Mount command will read manifest with:
  ```go
  manifest.Load(manifestPath)
  ```
- Need to implement `Load()` function (reverse of `Marshal()`)

**Action Item:** Add `Load(path string) (*Manifest, error)` function to manifest package:
```go
func Load(path string) (*Manifest, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var m Manifest
    err = json.Unmarshal(data, &m)
    return &m, err
}
```

---

## Performance Considerations

### Hashing Large Files
- A 25 GB model file takes ~30-60 seconds to hash (depends on disk I/O)
- For MVP, this is acceptable (one-time operation)
- Progress indicator on stderr would improve UX (future enhancement)

### Memory Usage
- Directory walking is streaming (low memory)
- Hashing uses `io.Copy()` (constant memory, ~32 KB buffer)
- Final manifest JSON held in memory (typically <1 MB)

**Estimated Memory:** <50 MB for directory with 1000 files

---

## Non-Functional Requirements

### Usability
- Clear error messages with file paths
- Progress indication on stderr (future: "Hashing file 5/10...")
- Warnings for skipped files

### Correctness
- SHA256 hashes must be deterministic and correct
- Path normalization must be consistent across platforms
- URLs must be properly escaped (no spaces, special chars)

### Portability
- Works on Linux, macOS, Windows
- Handles different path separators
- No platform-specific dependencies

---

## Validation Checklist

Before marking Milestone 2 complete:
- [ ] `go build ./cmd/mlfs` succeeds
- [ ] `mlfs generate --help` shows usage
- [ ] Can generate manifest from test directory
- [ ] Output JSON is valid and well-formatted
- [ ] SHA256 hashes are correct (verified with `sha256sum`)
- [ ] URLs are properly constructed
- [ ] Prefetch list populated correctly
- [ ] Hidden files are skipped
- [ ] Subdirectories are handled correctly
- [ ] All unit tests pass
- [ ] Integration test passes

---

## Example Workflow

### Simple Test (Development)

```bash
# Create simple test directory
mkdir test-data
echo "Hello from S3!" > test-data/message.txt
echo '{"name": "test"}' > test-data/config.json
dd if=/dev/urandom of=test-data/data.bin bs=1K count=100  # 100 KB file

# Generate manifest
./mlfs generate \
  --id test-artifact \
  --version v1.0 \
  --url-prefix https://test-bucket.s3.amazonaws.com/test/v1.0 \
  --prefetch message.txt,config.json \
  ./test-data > manifest.json
```

### Expected Output (stdout → manifest.json)
```json
{
  "artifact_id": "test-artifact",
  "version": "v1.0",
  "mount_path": "/mnt/mlmodel",
  "prefetch": ["message.txt", "config.json"],
  "files": [
    {
      "path": "config.json",
      "url": "https://test-bucket.s3.amazonaws.com/test/v1.0/config.json",
      "size": 17,
      "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "compression": "none"
    },
    {
      "path": "data.bin",
      "url": "https://test-bucket.s3.amazonaws.com/test/v1.0/data.bin",
      "size": 102400,
      "sha256": "<actual hash computed>",
      "compression": "none"
    },
    {
      "path": "message.txt",
      "url": "https://test-bucket.s3.amazonaws.com/test/v1.0/message.txt",
      "size": 15,
      "sha256": "8663bab6d124806b9727f89bb4ab9db4cbcc3862f6bbf22024dfa7212aa4ab7d",
      "compression": "none"
    }
  ]
}
```

### Verify
```bash
# Check JSON is valid
jq . manifest.json

# Verify SHA256 hash matches
sha256sum test-data/message.txt
jq -r '.files[] | select(.path == "message.txt") | .sha256' manifest.json
# ↑ Should match

# Check file count
ls test-data/ | wc -l
jq '.files | length' manifest.json
# ↑ Should match
```

### Real Model Example (Optional)
```bash
# Download small model for validation
huggingface-cli download distilbert-base-uncased --local-dir ./distilbert

# Upload to S3
aws s3 sync ./distilbert s3://my-bucket/distilbert/v1/

# Generate manifest
./mlfs generate \
  --id distilbert \
  --version v1 \
  --url-prefix https://my-bucket.s3.amazonaws.com/distilbert/v1 \
  --prefetch config.json,tokenizer.json \
  ./distilbert > distilbert-manifest.json

# Verify
jq '.files | length' distilbert-manifest.json
```

---

## Open Questions / Decisions Needed

### 1. MountPath Field Value
**Question:** What should `manifest.MountPath` default to?

**Options:**
- A) Use a fixed default like "/mnt/mlmodel"
- B) Use `--mount-path` flag (optional)
- C) Leave empty (user specifies at mount time)

**Recommendation:** Option A (fixed default). Field is informational only; actual mount point is specified in `mlfs mount` command.

**Decision:** Use "/mnt/mlmodel" as default.

### 2. URL Prefix Validation
**Question:** Should we validate that `--url-prefix` is a valid URL?

**Options:**
- A) Basic validation (starts with http:// or https://)
- B) No validation (trust user input)
- C) Full URL parsing and validation

**Recommendation:** Option A. Prevents obvious typos without over-engineering.

**Decision:** Check prefix starts with "http://" or "https://", error otherwise.

### 3. Large File Hashing Feedback
**Question:** Should we show progress for hashing large files?

**Options:**
- A) Silent (just hash and complete)
- B) Progress bar per file
- C) Simple log message per file on stderr

**Recommendation:** Option C for MVP. Progress bars add complexity.

**Decision:** Log "Hashing: <filename> (<size>)" on stderr for files >100 MB.

### 4. Handling Spaces in Filenames
**Question:** How to handle files with spaces in names?

**Options:**
- A) URL-encode spaces to %20
- B) Warn user and skip files with spaces
- C) Accept as-is (S3 keys can have spaces)

**Recommendation:** Option A. S3 keys work with spaces, but URLs need encoding.

**Decision:** Use `url.PathEscape()` for S3 URL construction.

---

## Success Criteria

**Milestone 2 is complete when:**
1. User can run `mlfs generate` on any directory and get valid JSON
2. SHA256 hashes match actual file hashes (verified manually)
3. Generated manifest can be loaded by future components
4. All tests pass
5. Documentation is clear for end users

**Estimated Effort:** 4-6 hours (includes testing and documentation)

---

## Next Steps After Completion

After Milestone 2 is validated:
1. **Immediately useful:** Users can generate manifests for their models
2. **Unblocks M7:** Mount command needs `manifest.Load()` to read manifests
3. **Parallel work:** M3 (Cache) and M4 (S3) can start independently
4. **Integration:** M5 (Fetch) will consume manifest data structure

**Recommended Next Milestone:** M3 (Cache Manager) or M4 (S3 Client) — both are independent and can run in parallel.

---

## Implementation Checklist

- [ ] Create `pkg/manifest/manifest.go` with structs
- [ ] Implement `Marshal()` function
- [ ] Create `pkg/manifest/hash.go` with `hashFile()`
- [ ] Write hash unit tests
- [ ] Create `pkg/manifest/generator.go` with `Generate()`
- [ ] Implement directory walking logic
- [ ] Implement path normalization
- [ ] Implement prefetch parsing
- [ ] Add URL validation (http/https check)
- [ ] Add URL encoding for special characters
- [ ] Update `cmd/mlfs/main.go` `generateCmd()` function
- [ ] Wire up all flags to `Generate()` call
- [ ] Add error handling and user-friendly messages
- [ ] Write unit tests for `Generate()`
- [ ] Create integration test script
- [ ] Test with real model directory
- [ ] Implement `Load()` function for M7
- [ ] Document usage in code comments
- [ ] Update README with examples (if needed)

**Total Tasks:** 18
**Estimated Time:** 4-6 hours
