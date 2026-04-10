# ADR-007: Per-Chunk Singleflight for Concurrent Read Coalescing

**Status:** Accepted
**Date:** 2026-04-09
**Context:** Milestone 5 (Fetch Manager)

---

## Decision

Use `golang.org/x/sync/singleflight` keyed on `(sha256, chunkIndex)` to coalesce concurrent S3 fetches for the same chunk. Each fetch runs with a detached `context.Background()` + 1-minute timeout; individual callers wait via `DoChan` + `select` so they can cancel without killing the shared in-flight request.

---

## Context

FUSE issues concurrent `Read` calls for the same file. When the same 16 MB chunk is uncached, multiple goroutines may simultaneously detect a cache miss and each independently issue an S3 `GetRange` call for identical byte ranges. This wastes:
- Bandwidth (same 16 MB fetched N times)
- S3 request quota
- Local disk write pressure (N concurrent atomic writes to the same chunk path)

The M5 bundle acknowledged this risk and listed two resolutions:
1. Accept duplicate fetches (cache write is atomic/idempotent) — originally proposed as MVP
2. Add per-chunk locking (document in ADR-007)

---

## Alternatives Considered

| Approach | Pros | Cons |
|---|---|---|
| Accept duplicates (no lock) | Zero added complexity | Wastes bandwidth; N×16 MB fetches under concurrency |
| Per-chunk `sync.Mutex` | Simple, no new dep | Goroutine holds lock during entire S3 fetch (seconds); other readers starve |
| Per-chunk `sync.Mutex` + condition variable | Avoids starvation | Complex, error-prone |
| **`singleflight` + `DoChan`** | **One fetch, all waiters share result, context-safe** | **New dependency; context design requires care** |

---

## Rationale

`singleflight` is the correct primitive here: it de-duplicates in-flight calls to the same key, allowing all concurrent waiters to share a single result without any goroutine holding a lock across network I/O. Key properties:

1. **Only one S3 fetch per chunk per time window** — regardless of how many FUSE goroutines hit the same cache miss simultaneously
2. **No lock held during network I/O** — waiters block on a channel, not a mutex
3. **Error isolation** — if the fetch fails, all waiters receive the error and can retry independently; a new `DoChan` call starts a fresh fetch
4. **Context-safe cancellation** — individual callers exit via `DoChan` + `select` without killing the shared fetch

### Context Design

The singleflight closure runs with `context.Background()` + a 1-minute per-chunk timeout rather than the first caller's context. This is intentional:

- If a caller's context is cancelled (e.g., FUSE read timeout, process shutdown signal), the in-flight fetch continues to completion, landing the chunk in cache
- Subsequent reads for the same chunk are cache hits — no wasted work
- The 1-minute timeout acts as a safety guardrail against hung S3 connections that bypass the 30s HTTP timeout (e.g., connection established but stalled during body transfer)

```go
ch := m.group.DoChan(key, func() (interface{}, error) {
    fetchCtx, cancel := context.WithTimeout(context.Background(), chunkFetchTimeout)
    defer cancel()
    data, err := m.s3.GetRange(fetchCtx, url, start, end)
    if err != nil {
        return nil, err
    }
    if err := m.cache.Write(fileSHA256, chunkIndex, data); err != nil {
        return nil, fmt.Errorf("cache write: %w", err)
    }
    return data, nil
})

select {
case res := <-ch:
    return res.Val.([]byte), res.Err
case <-ctx.Done():
    return nil, ctx.Err()
}
```

---

## Consequences

### Positive
- Reduces duplicate S3 requests to exactly one per chunk under any concurrency level
- No mutex held across network I/O — no goroutine starvation
- Cache hit after any single successful fetch — abandoned waiters (cancelled callers) benefit from the completed fetch
- Directly addresses the concurrent-read race identified in M5 bundle

### Negative
- New dependency: `golang.org/x/sync`
- Fetch continues after all callers cancel — minor resource use for abandoned fetches (acceptable; chunk lands in cache for future reads)
- If the fetch goroutine's context (Background + 1min) is the limiting factor rather than the caller's context, a hung connection could delay error reporting by up to 1 minute

### Scope
- Applies only to `Read` → `fetchChunk` path
- `Prefetch` is called once per file at mount time (sequential, no concurrency); singleflight is not used there

---

## Implementation Notes

```go
const chunkFetchTimeout = 1 * time.Minute

type Manager struct {
    s3        S3Client
    cache     *cache.Manager
    chunkSize int64
    group     singleflight.Group  // zero value is valid, no init needed
}

// singleflight key format: "<sha256>/<chunkIndex>"
key := fmt.Sprintf("%s/%d", fileSHA256, chunkIndex)
```

The `singleflight.Group` zero value is ready to use — no constructor call required.

---

## Related

- **ADR-001:** 16 MB Chunk Size — chunk granularity that makes this coalescing valuable
- **M5 Bundle:** Identified concurrent read risk; deferred locking strategy to this ADR
- **future-optimizations.md § Fetch Concurrency & Reliability** — original proposal

---

## References

- `golang.org/x/sync/singleflight` documentation
- [future-optimizations.md](../references/future-optimizations.md#1-per-chunk-single-flight-request-coalescing)
