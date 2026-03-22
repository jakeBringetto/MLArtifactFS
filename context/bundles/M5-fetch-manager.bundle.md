# M5: Fetch Manager — Context Bundle

**Target:** Implement chunk-aligned S3 fetch with local cache integration and prefetch+verification
**Estimated Reading Time:** 3 minutes

---

## Objective

Build the `fetch.Manager` that bridges the S3 client (M4) and cache manager (M3). It translates arbitrary byte-range reads into 16 MB chunk-aligned S3 fetches, caches chunks locally, and provides blocking prefetch with SHA256 integrity verification.

---

## Deliverable Definition

### Acceptance Criteria

- [ ] `fetch.Manager` struct with constructor `NewManager(s3Client, cacheManager, chunkSize)`
- [ ] `Read(ctx, url, sha256, offset, size) ([]byte, error)`:
  - Calculate chunk index: `chunkIndex = offset / chunkSize`
  - Cache hit: read from cache, return requested slice
  - Cache miss: fetch 16 MB chunk from S3, write to cache, return requested slice
  - Handle reads that span two chunks (offset+size crosses chunk boundary)
- [ ] `Prefetch(ctx, url, sha256, size) error`:
  - Download entire file sequentially in 16 MB chunks
  - Write each chunk to cache
  - Compute SHA256 of full reassembled file
  - If mismatch: return error (do not mark verified)
  - If match: `cacheManager.MarkVerified(sha256)`
- [ ] Unit tests with mock S3 client and real temp-dir cache
- [ ] `go test ./pkg/fetch` passes

---

## Current State

### What Exists
- [pkg/s3/client.go](../pkg/s3/client.go) — `GetRange(ctx, url, start, end int64) ([]byte, error)` (M4 complete)
- [pkg/cache/manager.go](../pkg/cache/manager.go) — `Has`, `Read`, `Write`, `IsVerified`, `MarkVerified` (M3 complete)
- `pkg/fetch/` — directory exists, empty

### What's Missing
- `pkg/fetch/manager.go` — fetch manager implementation

### Interfaces to Depend On

**S3 client (inject as interface for testability):**
```go
type S3Client interface {
    GetRange(ctx context.Context, url string, start, end int64) ([]byte, error)
}
```

**Cache manager (inject as interface or concrete type):**
```go
// Relevant methods on *cache.Manager:
Has(sha256 string, chunkIndex int) bool
Read(sha256 string, chunkIndex int) ([]byte, error)
Write(sha256 string, chunkIndex int, data []byte) error
IsVerified(sha256 string) bool
MarkVerified(sha256 string) error
```

---

## Constraints & Invariants

1. **16 MB chunk size** — `chunkSize = 16 * 1024 * 1024` (ADR-001). Chunk index = `offset / chunkSize`.
2. **Range header is inclusive** — `GetRange(ctx, url, start, end)` where `end = start + chunkSize - 1`. Last chunk may be smaller.
3. **Return only requested bytes** — `Read` receives arbitrary `offset`/`size`; must slice correctly from the fetched chunk.
4. **Cross-chunk reads** — if `offset + size` spans a chunk boundary, fetch both chunks and concatenate before slicing.
5. **SHA256 verification is per-file, not per-chunk** — hash the full concatenated file bytes after all chunks are downloaded (ADR-003).
6. **Prefetch is sequential** — download chunks 0, 1, 2… in order. No parallelism in MVP.
7. **Read-only** — never write to S3; cache writes only.

### Known Concurrency Risk
FUSE will issue concurrent `Read` calls for the same file. Two goroutines may simultaneously detect a cache miss for the same chunk and both issue S3 fetches. For MVP: accept the duplicate fetch (idempotent, write is atomic in cache). If this is unacceptable, add per-chunk `sync.Mutex` — document in ADR-007.

---

## Key Decisions

- **[ADR-001: 16 MB Chunk Size](../decisions/ADR-001-chunk-size.md)** — chunk alignment math
- **[ADR-002: Blocking Prefetch](../decisions/ADR-002-blocking-prefetch.md)** — `Prefetch` blocks mount until complete
- **[ADR-003: SHA256 Verification](../decisions/ADR-003-sha256-verification.md)** — verify full file after all chunks downloaded
- **[ADR-004: Unbounded Cache](../decisions/ADR-004-unbounded-cache.md)** — no eviction, write all chunks
- **[ADR-006: Retry Policy](../decisions/ADR-006-retry-policy.md)** — retry is handled in S3 client; fetch manager does not retry

**TODO:** If concurrent duplicate fetches are a problem during implementation, create ADR-007 for per-chunk locking strategy.

---

## Ordered Reading List

1. **[current.md](../current.md)** — Project status
2. **[milestones/M3-cache-manager.md](../milestones/M3-cache-manager.md)** — Cache API and behavior
3. **[milestones/M4-s3-client.md](../milestones/M4-s3-client.md)** — S3 client API and error behavior
4. **[ADR-001: Chunk Size](../decisions/ADR-001-chunk-size.md)** — 16 MB alignment
5. **[ADR-003: SHA256 Verification](../decisions/ADR-003-sha256-verification.md)** — verification strategy
6. **[ADR-002: Blocking Prefetch](../decisions/ADR-002-blocking-prefetch.md)** — prefetch semantics
7. **[planning/04-implementation-plan.md § M5](../planning/04-implementation-plan.md#milestone-5-fetch-manager)** — task breakdown

---

## Open Questions / Risks

| Question | Recommendation |
|---|---|
| Cross-chunk reads: fetch both chunks always, or check cache per-chunk? | Check cache per-chunk; only fetch missing ones |
| Last chunk smaller than 16 MB — how to handle 416? | Calculate actual end byte from file size; don't request past EOF |
| Concurrent reads to same chunk (duplicate S3 fetches) | Accept for MVP (cache write is atomic); add locking if observed in testing |
| SHA256 streaming vs full load | Load all chunk bytes into memory for hashing in MVP; stream post-M8 if memory is an issue |

---

## Implementation Sketch

```go
package fetch

import (
    "context"
    "crypto/sha256"
    "fmt"
    "io"

    "github.com/user/mlartifactfs/pkg/cache"
)

const DefaultChunkSize = 16 * 1024 * 1024

type S3Client interface {
    GetRange(ctx context.Context, url string, start, end int64) ([]byte, error)
}

type Manager struct {
    s3        S3Client
    cache     *cache.Manager
    chunkSize int64
}

func NewManager(s3 S3Client, cache *cache.Manager, chunkSize int64) *Manager

// Read returns bytes [offset, offset+size) from the file identified by url/sha256.
func (m *Manager) Read(ctx context.Context, url, sha256 string, offset, size int64) ([]byte, error)

// Prefetch downloads the entire file, verifies SHA256, and marks it as verified in cache.
func (m *Manager) Prefetch(ctx context.Context, url, fileSHA256 string, fileSize int64) error
```

**Chunk math:**
```go
chunkIndex := offset / m.chunkSize
chunkStart := chunkIndex * m.chunkSize
chunkEnd   := chunkStart + m.chunkSize - 1  // clamp to fileSize-1 for last chunk
offsetInChunk := offset - chunkStart
```

---

## Success Criteria

1. `go test ./pkg/fetch -v` passes
2. `Read` returns correct bytes for arbitrary offset/size combinations
3. `Read` populates cache on miss (verified by subsequent cache hit)
4. `Prefetch` downloads all chunks, verifies SHA256, calls `MarkVerified`
5. `Prefetch` returns error on SHA256 mismatch (does not mark verified)
6. M6 (FUSE) can call `fetchManager.Read(ctx, url, sha256, offset, size)` for file reads
