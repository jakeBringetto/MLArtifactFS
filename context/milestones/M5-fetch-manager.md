# M5: Fetch Manager — Completion Doc

**Completed:** 2026-04-09
**Status:** ✅ Complete

---

## What Was Built

`pkg/fetch/manager.go` — chunk-aligned S3 fetch manager with local cache integration, singleflight request coalescing, and blocking prefetch with SHA256 verification.

### Public API

```go
func NewManager(s3 S3Client, cache *cache.Manager, chunkSize int64) *Manager

// Read returns bytes [offset, offset+size) from the file at url.
// fileSize is required to clamp the range on the last chunk.
func (m *Manager) Read(ctx context.Context, url, fileSHA256 string, offset, size, fileSize int64) ([]byte, error)

// Prefetch downloads the complete file sequentially, verifies SHA256,
// and marks it as verified in cache.
func (m *Manager) Prefetch(ctx context.Context, url, fileSHA256 string, fileSize int64) error
```

**Note:** `Read` takes `fileSize int64` — not in original spec, added to clamp last-chunk S3 range and for cross-chunk boundary calculation.

### Key Implementation Details

- **singleflight:** `singleflight.Group` keyed on `"sha256/chunkIndex"` — concurrent reads for the same uncached chunk issue exactly one S3 request; all others wait via `DoChan` + `select` (ADR-007)
- **Per-waiter cancellation:** `DoChan` + `select { case res := <-ch: ... case <-ctx.Done(): ... }` — a cancelled caller unblocks immediately without aborting the in-flight fetch
- **Detached fetch context:** S3 fetch runs with `context.WithTimeout(context.Background(), 1*time.Minute)` — abandoning callers don't cancel the fetch; result lands in cache for subsequent readers
- **Cross-chunk reads:** `startChunk = offset / chunkSize`, `endChunk = (offset+size-1) / chunkSize`; each chunk checked in cache independently, only missing chunks fetched
- **Last-chunk clamping:** `end = min(chunkStart + chunkSize - 1, fileSize - 1)` — prevents 416 Range Not Satisfiable
- **Prefetch:** uses direct `s3.GetRange` (no singleflight), sequential chunks 0..N-1, SHA256 streamed via `crypto/sha256`; error on mismatch, never calls `MarkVerified` on failure

---

## Tests

**15 tests passing** (`go test ./pkg/fetch`):

**Read (9):**
- `TestRead_ZeroSize` — size=0 returns empty slice, no S3 call
- `TestRead_CacheHit` — pre-populated cache, 0 S3 calls
- `TestRead_CacheMiss_FetchesAndCaches` — miss fetches, second read is cache hit
- `TestRead_CorrectSlice_MiddleOfChunk` — arbitrary offset/size sliced correctly
- `TestRead_CrossChunk` — straddles boundary, 2 S3 calls
- `TestRead_CrossChunk_PartialCacheHit` — one chunk cached, one fetched; 1 S3 call
- `TestRead_LastChunk_Smaller` — end byte clamped to fileSize-1, not chunkSize*2-1
- `TestRead_S3Error_Propagated` — sentinel error wraps correctly
- `TestRead_Concurrent_SameChunk_SingleFlight` — 20 goroutines, exactly 1 S3 call; uses blocked fetchFunc + fetchStarted channel to ensure goroutines queue as DoChan waiters while fetch is in-flight

**Prefetch (6):**
- `TestPrefetch_Success_SingleChunk` — chunk cached, file verified
- `TestPrefetch_Success_MultiChunk` — 2.5 chunks, partial last chunk, all cached + verified
- `TestPrefetch_SHA256Mismatch` — error returned, IsVerified remains false
- `TestPrefetch_S3Error` — sentinel error propagated, not verified
- `TestPrefetch_Idempotent` — two calls succeed, file verified
- `TestPrefetch_S3ErrorPropagatedFromContext` — context.Canceled surfaced, not verified

---

## Decisions Made

| Decision | Choice | Rationale |
|---|---|---|
| singleflight in M5 | Implemented now (not post-M8) | Concurrent FUSE reads expected; duplicate S3 fetches observable in unit tests — ADR-007 created |
| fileSize in Read signature | Added vs. original spec | Required for last-chunk clamping and cross-chunk boundary math |
| Detached fetch context | `context.Background()` + 1min timeout | Cancelled callers shouldn't abort a fetch shared by other goroutines |
| Prefetch bypasses singleflight | Direct GetRange calls | Prefetch is sequential and single-caller; singleflight adds no value |

**ADR-007 created:** [decisions/ADR-007-per-chunk-singleflight.md](../decisions/ADR-007-per-chunk-singleflight.md)

---

## Integration Readiness

**M6 (FUSE Filesystem) can:**
- Call `fetchManager.Read(ctx, url, sha256, offset, size, fileSize)` for arbitrary byte-range reads
- Call `fetchManager.Prefetch(ctx, url, sha256, fileSize)` at mount time for prefetch files
- Pass any fileSize from manifest; last-chunk clamping handled internally
- Rely on concurrent Read calls being safe (singleflight-protected)

---

## Related

- [ADR-007: Per-Chunk Singleflight](../decisions/ADR-007-per-chunk-singleflight.md)
- [ADR-001: 16 MB Chunk Size](../decisions/ADR-001-chunk-size.md)
- [ADR-003: SHA256 Verification](../decisions/ADR-003-sha256-verification.md)
- [ADR-002: Blocking Prefetch](../decisions/ADR-002-blocking-prefetch.md)
- [bundles/M5-fetch-manager.bundle.md](../bundles/M5-fetch-manager.bundle.md) — original spec
