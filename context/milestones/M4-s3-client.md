# M4: S3 Client — Completion Doc

**Completed:** 2026-03-22
**Status:** ✅ Complete

---

## What Was Built

`pkg/s3/client.go` — S3 range-request client wrapping AWS SDK v2.

### Public API

```go
func NewClient(ctx context.Context) (*Client, error)
func (c *Client) GetRange(ctx context.Context, rawURL string, start, end int64) ([]byte, error)
```

### Key Implementation Details

- **Dual fetch paths:** AWS SDK path for `s3://` and `https://` S3 URLs; plain `net/http` path for presigned URLs (detected via `X-Amz-Signature` query param)
- **URL formats supported:** `s3://bucket/key`, virtual-hosted HTTPS (with/without region), path-style HTTPS (deprecated), presigned URLs
- **Retry logic:** 3 retries (4 total attempts), exponential backoff 1s/2s/4s ±25% jitter — matches ADR-006
- **Permanent errors (no retry):** 403, 404, 416 — returned immediately
- **Transient errors (retry):** 503, 429, `url.Error` timeouts/temporary, `context.DeadlineExceeded`
- **Unknown errors:** returned immediately without retry
- **HTTP timeout:** 30s per request
- **`s3API` interface:** mock-injectable for tests (no real S3 needed in unit tests)
- **`backoffFunc` injectable:** no-op in tests, avoids sleep during test runs

---

## Tests

**19 unit tests passing** (`go test ./pkg/s3`):
- URL parsing: `s3://`, virtual-hosted (with/without region), path-style, presigned detection, error cases
- Backoff calculation: jitter bounds verified over 100 iterations per attempt
- Error classification: permanent vs transient for both SDK errors and plain HTTP errors
- `GetRange` via mock S3: success, range header format, retry-on-503, retry-on-429, max-retries exceeded, permanent 403/404/416, unknown error no-retry, context cancellation, invalid URL
- Presigned URL via `httptest.Server`: success (206), 403 no-retry, 503 retry

**3 integration tests** (skipped unless `S3_TEST_BUCKET` + AWS credentials set):
- `TestGetRange_Integration` — s3:// URL, small file
- `TestGetRange_IntegrationVirtualHostedURL` — virtual-hosted HTTPS URL
- `TestGetRange_IntegrationMultiChunk` — two 16 MB chunks from 32 MB file

---

## Decisions Made

No new ADRs required. All decisions follow existing ADRs or are implementation-level choices:

| Decision | Choice | Rationale |
|---|---|---|
| HTTP timeout | 30s per request | Resolves M4 open question; sufficient for 16 MB on typical network |
| Unknown errors | Fail immediately, no retry | Not safe to retry unknown failure modes |
| Mock injection | `s3API` interface + `backoffFunc` field | Standard Go testability pattern; no new ADR needed |
| Presigned detection | `X-Amz-Signature` query param | Only param present in all presigned URL formats |

---

## Integration Readiness

**M5 (Fetch Manager) can:**
- Call `client.GetRange(ctx, url, start, end)` for 16 MB chunk fetches
- Pass `s3://`, `https://`, or presigned URLs — all handled transparently
- Rely on retry behavior conforming to ADR-006
- Rely on permanent errors propagating immediately (403 = bad creds, 404 = missing object, 416 = range past EOF)

---

## Related

- [ADR-006: Retry Policy](../decisions/ADR-006-retry-policy.md)
- [ADR-001: 16 MB Chunk Size](../decisions/ADR-001-chunk-size.md)
- [bundles/M4-s3-client.bundle.md](../bundles/M4-s3-client.bundle.md) — original spec
