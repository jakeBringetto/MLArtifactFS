# MLArtifactFS — Current State

**Last Updated:** 2026-02-16

---

## What We're Building

A FUSE-based filesystem for lazy-loading ML models from S3 storage. Enables rapid experimentation and A/B testing of ML models in containerized environments without rebuilding Docker images.

**Core Value Proposition:** Decouple ML model artifacts from Docker images, allowing instant model version switching.

---

## Current Milestone

**Milestone 4: S3 Client** (Next to implement)

**Status:** M1 (Scaffolding) ✅, M2 (Manifest Generator) ✅, and M3 (Cache Manager) ✅ are complete.

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

---

## What's Next

### Immediate: M4 - S3 Client
**Objective:** Implement S3 range request client with retry logic and error handling.

**Deliverables:**
- `pkg/s3/client.go` with:
  - `NewClient()` - Initialize AWS SDK v2 client
  - `GetRange(ctx, url, start, end)` - Fetch byte range from S3
  - Retry logic for transient errors (503, connection reset)
  - Error handling for 403/404/416
- Support for presigned URLs
- Unit and integration tests

### Subsequent Milestones:
- **M5:** Fetch Manager (orchestrate S3 + cache, chunk alignment)
- **M6:** FUSE Filesystem (read-only virtual FS)
- **M7:** CLI Mount Command (wire everything together)
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
│   ├── s3/             # 🔲 S3 client with range requests (M4 - next)
│   ├── fetch/          # 🔲 Fetch manager (M5)
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

For the current milestone (M4), see:
- [bundles/M4-s3-client.bundle.md](./bundles/M4-s3-client.bundle.md)

---

## Open Questions / Known Risks

### For M4:
- S3 rate limiting strategy - how aggressive should retry backoff be?
- Presigned URL support - test with both IAM and presigned URLs
- Connection pooling - use default AWS SDK settings or customize?
- Timeout values - what's appropriate for 16 MB chunk downloads?

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

---

## Navigation

- **Full design:** [planning/03-design.md](./planning/03-design.md)
- **Implementation plan:** [planning/04-implementation-plan.md](./planning/04-implementation-plan.md)
- **Completed milestones:** [milestones/](./milestones/) (M2, M3)
- **Next milestone bundle:** [bundles/M4-s3-client.bundle.md](./bundles/M4-s3-client.bundle.md)

---

**Status:** Ready to begin M4 (S3 Client)
