# Step 4 — Implementation Plan

## Overview

This plan breaks the MLArtifactFS POC into ordered milestones. Each milestone is independently testable and builds toward a working end-to-end system.

**Total estimated effort:** 1-2 weeks (POC scope)

---

## Milestone 1: Project Scaffolding

**Goal:** Set up Go project structure and dependencies.

### Tasks
1. Initialize Go module: `go mod init github.com/<user>/mlartifactfs`
2. Create directory structure:
   ```
   mlartifactfs/
     cmd/mlfs/main.go
     pkg/
       manifest/
       cache/
       s3/
       fetch/
       fuse/
     go.mod
     go.sum
     README.md
   ```
3. Add dependencies:
   - `github.com/hanwen/go-fuse/v2` (FUSE)
   - `github.com/aws/aws-sdk-go-v2` (S3 client)
   - Standard library: `crypto/sha256`, `encoding/json`, `flag`
4. Create minimal `main.go` with CLI skeleton (cobra or stdlib `flag`)

**Validation:**
- `go build ./cmd/mlfs` succeeds
- `./mlfs --help` shows usage

**Dependencies:** None

---

## Milestone 2: Manifest Format and Generator

**Goal:** Define manifest schema and implement `mlfs generate` command.

### Tasks
1. Define `manifest.Manifest` struct in `pkg/manifest/manifest.go`
   - Fields: `ArtifactID`, `Version`, `MountPath`, `Prefetch`, `Files[]`
   - `File` struct: `Path`, `URL`, `Size`, `SHA256`, `Compression`
2. Implement `manifest.Generate(dir, id, version, urlPrefix, prefetch)`:
   - Walk directory recursively using `filepath.Walk`
   - For each file:
     - Compute SHA256 hash
     - Get file size
     - Construct URL: `urlPrefix + "/" + relativePath`
   - Return `Manifest` struct
3. Implement `manifest.Marshal(m Manifest) -> JSON`
4. Wire up `mlfs generate` command in `main.go`:
   - Parse flags: `--id`, `--version`, `--url-prefix`, `--prefetch`
   - Call `manifest.Generate()`
   - Print JSON to stdout

**Validation:**
- Create test directory with sample files
- Run: `mlfs generate --id test --version v1 --url-prefix https://s3.example.com/test/v1 ./test-dir`
- Verify output JSON has correct structure and SHA256 hashes

**Dependencies:** Milestone 1

---

## Milestone 3: Cache Manager

**Goal:** Implement local disk cache with chunk storage.

### Tasks
1. Implement `cache.Manager` in `pkg/cache/manager.go`:
   - `NewManager(dir string) *Manager` — create cache directory
   - `ChunkPath(sha256 string, chunkIndex int) string` — compute chunk file path
   - `Has(sha256, chunkIndex) bool` — check if chunk exists
   - `Read(sha256, chunkIndex) ([]byte, error)` — read chunk from disk
   - `Write(sha256, chunkIndex, data []byte) error` — write chunk to disk
   - `IsVerified(sha256) bool` — check if `_verified` marker exists
   - `MarkVerified(sha256) error` — create `_verified` marker file
2. Create cache directory structure:
   ```
   <cache-dir>/
     <sha256>/
       chunk_0
       chunk_1
       ...
       _verified
   ```

**Validation:**
- Unit test: write chunk, verify it exists, read back
- Unit test: verify marker logic

**Dependencies:** Milestone 1

---

## Milestone 4: S3 Client

**Goal:** Implement S3 range request client.

### Tasks
1. Implement `s3.Client` in `pkg/s3/client.go`:
   - `NewClient() *Client` — initialize AWS SDK v2 client with default credentials
   - `GetRange(ctx, url, start, end) ([]byte, error)`:
     - Parse S3 URL (bucket + key)
     - Issue GetObject with `Range: bytes=start-end` header
     - Handle errors: 403, 404, 416, 503
     - Return byte slice
2. Add retry logic for transient errors (503, connection reset):
   - Exponential backoff: 1s, 2s, 4s
   - Max 3 retries

**Validation:**
- Integration test: upload test file to S3, fetch range
- Unit test: verify range header construction

**Dependencies:** Milestone 1

---

## Milestone 5: Fetch Manager

**Goal:** Coordinate S3 fetches and caching.

### Tasks
1. Implement `fetch.Manager` in `pkg/fetch/manager.go`:
   - `NewManager(s3Client, cacheManager, chunkSize) *Manager`
   - `Read(ctx, url, sha256, offset, size) ([]byte, error)`:
     - Calculate chunk index: `chunkIndex = offset / chunkSize`
     - Check cache: `cacheManager.Has(sha256, chunkIndex)`
     - If cache hit: read from cache
     - If cache miss:
       - Calculate aligned range: `[chunkIndex*chunkSize, (chunkIndex+1)*chunkSize)`
       - Fetch from S3: `s3Client.GetRange(url, start, end)`
       - Write to cache: `cacheManager.Write(sha256, chunkIndex, data)`
     - Return requested byte range from chunk
   - `Prefetch(ctx, url, sha256, size) error`:
     - Download entire file in 16 MB chunks sequentially
     - Write each chunk to cache
     - Compute SHA256 of full file
     - If mismatch: return error
     - If match: `cacheManager.MarkVerified(sha256)`
2. Set chunk size: 16 MB (16 * 1024 * 1024)

**Validation:**
- Unit test: mock S3 client, verify chunk alignment
- Integration test: fetch file ranges, verify cache population

**Dependencies:** Milestone 3, Milestone 4

---

## Milestone 6: FUSE Filesystem

**Goal:** Implement FUSE operations using hanwen/go-fuse.

### Tasks
1. Implement `fuse.FS` in `pkg/fuse/fs.go`:
   - `NewFS(manifest Manifest, fetchManager *fetch.Manager) *FS`
   - Build in-memory directory tree from `manifest.Files[]`:
     - Use map: `path -> FileEntry{size, sha256, url, mode}`
     - Support directory traversal (split paths by `/`)
   - Implement hanwen/go-fuse nodefs interfaces:
     - `Lookup(name) -> Node` — find file/dir in tree
     - `Getattr() -> Attr` — return size, mode, mtime from manifest
     - `Open() -> FileHandle` — return handle
     - `Read(offset, size) -> []byte` — call `fetchManager.Read()`
     - `Readdir() -> []DirEntry` — list directory contents
   - Return `EROFS` for write operations
2. Implement `Mount(fs *FS, mountPoint string) error`:
   - Call `fuse.NewServer()` with FS
   - Call `server.Serve()`

**Validation:**
- Unit test: build directory tree, verify lookups
- Integration test: mount with mock fetch manager, read file

**Dependencies:** Milestone 2, Milestone 5

---

## Milestone 7: CLI Mount Command

**Goal:** Wire up `mlfs mount` command.

### Tasks
1. Implement `mlfs mount` in `main.go`:
   - Parse flags: `--manifest`, `--cache-dir`, `--log-level`
   - Parse positional arg: `mountPoint`
2. Mount flow:
   - Load manifest from file: `manifest.Load(manifestPath)`
   - Initialize S3 client: `s3.NewClient()`
   - Initialize cache manager: `cache.NewManager(cacheDir)`
   - Initialize fetch manager: `fetch.NewManager(s3Client, cacheManager, 16*1024*1024)`
   - Prefetch files from `manifest.Prefetch[]`:
     - For each file: `fetchManager.Prefetch(ctx, file.URL, file.SHA256, file.Size)`
     - Log progress
     - If error: exit with error
   - Initialize FUSE FS: `fuse.NewFS(manifest, fetchManager)`
   - Mount: `fuse.Mount(fs, mountPoint)`
   - Block until SIGINT/SIGTERM
3. Add logging (stdout):
   - Info: "Prefetching <file>..."
   - Info: "Mount ready at <mountPoint>"
   - Debug: "S3 GetRange <url> <start>-<end>"

**Validation:**
- Create manifest with 2 files (1 prefetch, 1 lazy)
- Run: `mlfs mount --manifest manifest.json /mnt/test`
- Verify prefetch file downloads
- Verify mount succeeds
- Read files from `/mnt/test`, verify contents

**Dependencies:** Milestone 6

---

## Milestone 8: End-to-End Testing

**Goal:** Validate full workflow with real S3 and container.

### Tasks
1. Create test model:
   - Download small model from HuggingFace (e.g., distilbert-base-uncased)
   - Upload to test S3 bucket
2. Generate manifest:
   - Run: `mlfs generate --id distilbert --version test --url-prefix <s3-url> --prefetch config.json,tokenizer.json ./distilbert`
3. Test in Docker:
   - Build Docker image with `mlfs` binary
   - Run: `docker run --cap-add SYS_ADMIN --device /dev/fuse -v ./manifest.json:/manifest.json <image> mlfs mount --manifest /manifest.json /mnt/model`
   - Inside container: `cat /mnt/model/config.json` (should print config)
   - Inside container: `ls -lh /mnt/model` (should list all files)
4. Test lazy loading:
   - Read large file (pytorch_model.bin) in chunks
   - Verify chunks are fetched on demand (check logs)
5. Test error handling:
   - Invalid manifest (malformed JSON)
   - Missing S3 object (404)
   - SHA256 mismatch (corrupt prefetch file)

**Validation:**
- All tests pass
- Logs show expected behavior (prefetch, lazy fetch, cache hits)

**Dependencies:** Milestone 7

---

## Milestone 9: Documentation

**Goal:** Document usage and requirements.

### Tasks
1. Write `README.md`:
   - Overview of MLArtifactFS
   - Installation: `go install ./cmd/mlfs`
   - Usage examples:
     - Generate manifest
     - Mount filesystem
     - Use in Docker
   - Docker requirements: `--cap-add SYS_ADMIN --device /dev/fuse`
   - Manifest format reference
2. Write inline code comments for public APIs
3. Add example manifest file to repo: `examples/manifest.json`
4. Add example Dockerfile: `examples/Dockerfile`

**Validation:**
- README has all essential information
- Examples are copy-pasteable

**Dependencies:** Milestone 8

---

## Task Dependency Graph

```
M1 (Scaffolding)
  ↓
  ├─→ M2 (Manifest) ─┐
  ├─→ M3 (Cache) ────┤
  └─→ M4 (S3) ───────┤
                     ↓
                   M5 (Fetch Manager)
                     ↓
                   M6 (FUSE)
                     ↓
                   M7 (CLI Mount)
                     ↓
                   M8 (E2E Testing)
                     ↓
                   M9 (Documentation)
```

**Critical path:** M1 → M4 → M5 → M6 → M7 → M8

**Parallelizable:** M2, M3, M4 can be built concurrently after M1

---

## Implementation Notes

### Order of Development
1. **Start with M2 (Manifest)** — enables early testing of `generate` command
2. **Build M3, M4 in parallel** — independent components
3. **Integrate with M5** — fetch manager brings S3 + cache together
4. **Implement M6** — FUSE is the core complexity
5. **Wire up M7** — CLI glue
6. **Validate with M8** — catch integration issues
7. **Polish with M9** — make it usable

### Testing Strategy
- **Unit tests:** Each milestone has self-contained tests
- **Integration tests:** M8 validates full stack with real S3
- **Manual testing:** Use Docker to simulate production environment

### Iteration Points
After M7, the system is end-to-end functional. Iterations can focus on:
- Performance tuning (parallel prefetch, async chunk fetching)
- Error handling improvements (better error messages)
- Observability (metrics, structured logging)

---

## Risk Mitigation

| Risk | Mitigation |
|------|-----------|
| FUSE complexity | Start with simple read-only ops, use hanwen examples |
| S3 credential issues | Test with public bucket first, then add auth |
| Cache corruption | Write atomic (tmp file + rename), verify SHA256 |
| Mount hangs | Add timeouts to S3 calls, log all operations |
| Performance issues | Profile after M8, optimize hot paths |

---

## Exit Condition

✅ **COMPLETE** — Plan is concrete enough to begin coding immediately.

**Next Steps:**
1. Begin with Milestone 1 (scaffolding)
2. Implement milestones sequentially (or M2/M3/M4 in parallel)
3. Validate each milestone before proceeding
4. Iterate based on testing findings

**Deliverable:** Working MLArtifactFS POC that can mount ML models from S3 in Docker containers.
