# MLArtifactFS — Current State

**Last Updated:** 2026-04-09

---

## What We're Building

A FUSE-based filesystem for lazy-loading ML models from S3 storage. Enables rapid experimentation and A/B testing of ML models in containerized environments without rebuilding Docker images.

**Core Value Proposition:** Decouple ML model artifacts from Docker images, allowing instant model version switching.

---

## Current Milestone

**Milestone 6: FUSE Filesystem** (Next to implement)

**Status:** M1 (Scaffolding) ✅, M2 (Manifest Generator) ✅, M3 (Cache Manager) ✅, M4 (S3 Client) ✅, and M5 (Fetch Manager) ✅ are complete.

---

## What's Done

### M1: Project Scaffolding ✅
- Go module initialized
- Directory structure created (`cmd/mlfs`, `pkg/manifest`, `pkg/cache`, `pkg/s3`, `pkg/fetch`, `pkg/fuse`)
- Dependencies added: `hanwen/go-fuse`, `aws-sdk-go-v2`
- CLI skeleton with `flag` package

### M2: Manifest Generator ✅ (Completed 2026-01-19)
- `mlfs generate` command fully functional
- Manifest format defined and implemented
- Core packages:
  - [pkg/manifest/manifest.go](../pkg/manifest/manifest.go) - Manifest/File structs, Marshal(), Load()
  - [pkg/manifest/hash.go](../pkg/manifest/hash.go) - SHA256 computation
  - [pkg/manifest/generator.go](../pkg/manifest/generator.go) - Directory walk, metadata extraction
- All 10 unit tests passing
- Validation: Can generate manifests from local directories with correct SHA256 hashes and S3 URLs

**Key Deliverable:** Working `mlfs generate` command that produces JSON manifests

### M3: Cache Manager ✅ (Completed 2026-02-16)
- Content-addressable storage with SHA256-based paths
- Atomic writes (temp file + rename pattern)
- Verification marker system (`_verified` file)
- Core packages:
  - [pkg/cache/manager.go](../pkg/cache/manager.go) - Cache operations (Has, Read, Write, IsVerified, MarkVerified)
- All 11 unit tests passing (100% coverage)
- Implementation follows industry patterns (Git objects, Docker layers)

**Key Deliverable:** Production-ready cache manager for POC scale

### M5: Fetch Manager ✅ (Completed 2026-04-09)
- Chunk-aligned S3 fetch bridging S3 client (M4) and cache manager (M3)
- singleflight request coalescing per chunk (ADR-007) — concurrent FUSE reads issue exactly one S3 request per chunk
- Per-waiter cancellation via `DoChan` + `select` — cancelled callers unblock without aborting in-flight fetch
- Cross-chunk reads handled; last-chunk range clamped to `fileSize-1`
- Core packages:
  - [pkg/fetch/manager.go](../pkg/fetch/manager.go) - `NewManager()`, `Read(ctx, url, sha256, offset, size, fileSize)`, `Prefetch(...)`
- 15 unit tests passing (9 Read + 6 Prefetch)
- Note: `Read` takes `fileSize int64` (added vs. original spec — required for last-chunk clamping)

**Key Deliverable:** `fetchManager.Read()` and `fetchManager.Prefetch()` ready for M6 consumption

### M4: S3 Client ✅ (Completed 2026-03-22)
- S3 range-request client wrapping AWS SDK v2
- Dual fetch paths: AWS SDK for `s3://`/HTTPS URLs, `net/http` for presigned URLs
- All S3 URL formats: `s3://`, virtual-hosted HTTPS (with/without region), path-style HTTPS, presigned
- Retry: 3 retries, exponential backoff 1s/2s/4s ±25% jitter (ADR-006)
- Permanent errors (403, 404, 416) fail immediately; transient (503, 429, timeouts) retry
- Core packages:
  - [pkg/s3/client.go](../pkg/s3/client.go) - `NewClient()`, `GetRange(ctx, url, start, end)`
- 19 unit tests passing, 3 integration tests (skipped without AWS creds)

**Key Deliverable:** `client.GetRange(ctx, url, start, end)` ready for M5 consumption

---

## What's Next

### Immediate: M6 - FUSE Filesystem
**Objective:** Implement read-only FUSE virtual filesystem using hanwen/go-fuse v2 (`fs` package). Maps manifest file tree to FUSE operations; delegates reads to fetch manager.

**Deliverables:**
- `pkg/fuse/fs.go` with:
  - `NewFS(manifest, fetchManager)` — build in-memory directory tree from manifest
  - `Mount(mountPoint, fs)` — wire up go-fuse server
  - FUSE ops: `Lookup`, `Getattr`, `Open`, `Read`, `Readdir`
  - Write ops return `EROFS`

### Subsequent Milestones:
- **M7:** CLI Mount Command (wire everything together)
- **M8:** End-to-End Testing
- **M8:** End-to-End Testing
- **M9:** Documentation

---

## Constraints & Invariants

### Technical Constraints:
1. **Read-only filesystem** - No write operations supported
2. **16 MB chunk size** - S3 range requests aligned to 16 MB boundaries
3. **SHA256 verification** - Required for all prefetched files
4. **Single binary deployment** - All functionality in one `mlfs` executable
5. **Linux + FUSE required** - Depends on `/dev/fuse` kernel module
6. **Docker capabilities** - Requires `SYS_ADMIN` and `/dev/fuse` device

### Design Invariants:
1. Files identified by SHA256 hash (content-addressable)
2. Manifest defines complete virtual filesystem structure
3. Prefetch files block mount operation until verified
4. Cache is unbounded (MVP - no eviction policy)
5. All paths normalized to Unix-style forward slashes
6. Hidden files (`.DS_Store`, `.git`) skipped during manifest generation

### Platform Constraints:
- Go 1.23+
- Linux with FUSE support
- S3 or S3-compatible storage
- AWS credentials (env vars, IAM role, or presigned URLs)

---

## Key Decisions

Critical architectural decisions are documented in [decisions/](./decisions/):
- **ADR-001:** 16 MB chunk size for S3 range requests
- **ADR-002:** Blocking prefetch at mount time
- **ADR-003:** SHA256 verification after full download
- **ADR-004:** Unbounded cache (no eviction in MVP)
- **ADR-005:** Read-only filesystem
- **ADR-006:** Retry policy with exponential backoff and jitter (1s, 2s, 4s ±25%)

**Other design decisions:**
- **Single mount per process** - Separate processes for multiple models
- **Default mount path: `/mnt/mlmodel`** - Informational only, user can override

---

## Project Structure

```
mlartifactfs/
├── cmd/mlfs/           # CLI entry point (generate, mount commands)
├── pkg/
│   ├── manifest/       # ✅ Manifest format, generator, hash computation
│   ├── cache/          # ✅ Cache manager (M3 complete)
│   ├── s3/             # ✅ S3 client with range requests (M4 complete)
│   ├── fetch/          # ✅ Fetch manager (M5 complete)
│   └── fuse/           # 🔲 FUSE filesystem implementation (M6)
├── context/            # Project knowledge base
│   ├── current.md      # ⬅️ This file
│   ├── index.md        # Navigation guide
│   ├── planning/       # Design docs, implementation plan
│   ├── decisions/      # ADRs
│   ├── milestones/     # Completed milestone docs
│   ├── bundles/        # Milestone-specific context bundles
│   └── references/     # Future optimizations, research notes
└── test-data/          # Test fixtures for validation
```

---

## Active Context Bundle

For the current milestone (M6), see:
- [bundles/M6-fuse-filesystem.bundle.md](./bundles/M6-fuse-filesystem.bundle.md)

---

## Open Questions / Known Risks

### For M6:
- FUSE deadlock prevention: don't call back into FUSE from FUSE callbacks; fetchManager.Read only touches disk/S3
- Testing without /dev/fuse: test node struct methods directly without mounting
- go-fuse inode caching: set EntryTimeout to max (manifest is immutable)
- Short read handling: if fetchManager.Read returns fewer bytes than requested (near EOF), go-fuse handles gracefully

### General:
- FUSE deadlock prevention (M6)
- Performance profiling and optimization deferred to post-M8

---

## Quick Reference

**Run tests:**
```bash
go test ./pkg/...
```

**Build binary:**
```bash
go build -o mlfs ./cmd/mlfs
```

**Generate manifest (example):**
```bash
./mlfs generate \
  --id test-model \
  --version v1.0 \
  --url-prefix https://bucket.s3.amazonaws.com/models/test/v1.0 \
  --prefetch config.json,tokenizer.json \
  ./local-model-dir > manifest.json
```

**Current build status:**
- Builds successfully: ✅
- All tests passing: ✅
  - pkg/manifest: 10/10 tests
  - pkg/cache: 11/11 tests (100% coverage)
  - pkg/s3: 19/19 unit tests (+ 3 integration tests skipped without AWS creds)
  - pkg/fetch: 15/15 tests (9 Read + 6 Prefetch)

---

## Navigation

- **Full design:** [planning/03-design.md](./planning/03-design.md)
- **Implementation plan:** [planning/04-implementation-plan.md](./planning/04-implementation-plan.md)
- **Completed milestones:** [milestones/](./milestones/) (M2, M3, M4)
- **Next milestone bundle:** [bundles/M6-fuse-filesystem.bundle.md](./bundles/M6-fuse-filesystem.bundle.md)

---

**Status:** Ready to begin M5 (Fetch Manager)
