# M4: S3 Client — Context Bundle

**Target:** Implement S3 range request client with retry logic and error handling
**Estimated Reading Time:** 3 minutes

---

## Objective

Build an S3 client that fetches byte ranges from S3 using the AWS SDK for Go v2, with exponential backoff retry logic for transient errors and proper handling of S3-specific error codes.

---

## Deliverable Definition

### Acceptance Criteria

- [ ] `Client` struct with constructor `NewClient()`
- [ ] `GetRange(ctx, url, start, end) ([]byte, error)` — fetch byte range from S3
- [ ] Parse S3 URLs (extract bucket and key from `s3://bucket/key` or `https://bucket.s3.amazonaws.com/key`)
- [ ] Support presigned URLs (no authentication required)
- [ ] Retry logic for transient errors (503, 429, connection timeouts)
  - Exponential backoff with jitter: 1s, 2s, 4s (±25% random variation)
  - Max 3 retries (4 total attempts)
  - See [ADR-006](../decisions/ADR-006-retry-policy.md) for implementation details
- [ ] Error handling for permanent failures:
  - 403 Forbidden → return error immediately (no retry)
  - 404 Not Found → return error immediately (no retry)
  - 416 Range Not Satisfiable → return error (read past EOF)
- [ ] Unit tests for URL parsing
- [ ] Integration tests with real S3 bucket (or LocalStack/Minio)
- [ ] `go test ./pkg/s3` passes

---

## Current State

### What Exists
- Project scaffolding (M1 complete)
- Manifest generator (M2 complete) that produces S3 URLs in manifest
- Cache manager (M3 complete) ready to store downloaded chunks
- Directory structure: `pkg/s3/` exists but is empty
- Go module has AWS SDK dependency in `go.mod`

### What's Missing
- S3 client implementation
- URL parsing logic (extract bucket + key from URLs)
- Retry logic with exponential backoff
- Error classification (transient vs permanent)

---

## Constraints & Invariants

### Non-Negotiables
1. **16 MB chunk size** — Range requests will typically be `bytes=0-16777215` (16 MB aligned, see ADR-001)
2. **AWS SDK for Go v2** — Use `github.com/aws/aws-sdk-go-v2`
3. **Range header format** — `Range: bytes=start-end` (inclusive, e.g., `bytes=0-15` fetches 16 bytes)
4. **Retry only transient errors** — 503, connection timeouts, throttling
5. **Fail fast on permanent errors** — 403, 404 (don't waste time retrying)

### Authentication Strategy
- **Default credential chain:**
  1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
  2. IAM role (if running in EC2/ECS)
  3. `~/.aws/credentials` file
- **Presigned URLs:** No credentials needed, URL contains temporary auth

### S3 URL Formats to Support
```
s3://bucket-name/path/to/object
https://bucket-name.s3.amazonaws.com/path/to/object
https://bucket-name.s3.region.amazonaws.com/path/to/object
https://s3.amazonaws.com/bucket-name/path/to/object (path-style, deprecated)
```

**Presigned URL example:**
```
https://bucket.s3.amazonaws.com/object?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=...
```

### Error Handling Requirements
| Error Code | Meaning | Action |
|------------|---------|--------|
| 200/206    | Success (206 = partial content) | Return data |
| 403        | Forbidden (no permissions) | Fail immediately, clear error |
| 404        | Not Found | Fail immediately, clear error |
| 416        | Range Not Satisfiable (past EOF) | Fail immediately |
| 503        | Service Unavailable | Retry with backoff |
| Connection timeout | Network issue | Retry with backoff |
| Connection reset | Network issue | Retry with backoff |

---

## Key Decisions

### ADRs to Respect
- **[ADR-001: 16 MB Chunk Size](../decisions/ADR-001-chunk-size.md)** — S3 client will be called with 16 MB ranges
- **[ADR-006: Retry Policy](../decisions/ADR-006-retry-policy.md)** — Exponential backoff with jitter (1s, 2s, 4s ±25%)

### Design Patterns
- **Thin wrapper:** Don't reimplement S3 logic, wrap AWS SDK cleanly
- **Context-aware:** All operations accept `context.Context` for cancellation
- **Exponential backoff:** `time.Sleep(backoff * (1 << retry))` — 1s, 2s, 4s
- **Idempotent retries:** Range GET is safe to retry (read-only operation)

---

## Ordered Reading List

Read these files in order before implementation:

1. **[current.md](../current.md)** — Project status and context
2. **[ADR-001: Chunk Size](../decisions/ADR-001-chunk-size.md)** — Understand 16 MB chunks
3. **[ADR-006: Retry Policy](../decisions/ADR-006-retry-policy.md)** — Exponential backoff with jitter
4. **[planning/03-design.md § S3 Client](../planning/03-design.md#5-s3-client-pkgs3clientgo)** — Component design
5. **[planning/04-implementation-plan.md § M4](../planning/04-implementation-plan.md#milestone-4-s3-client)** — Task breakdown
6. **AWS SDK for Go v2 docs:** [GetObject with Range](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3#Client.GetObject)

---

## Open Questions / Risks

### Questions to Resolve During Implementation
1. **URL parsing:** Use regex or URL parse library? (Recommend: net/url + string parsing)
2. **Connection pooling:** Use default AWS SDK settings or customize? (Recommend: default for MVP)
3. **Timeout values:** What's appropriate for 16 MB downloads? (Recommend: 30s per request, 60s total with retries)
4. **Region detection:** Auto-detect region from URL or require env var? (Recommend: auto-detect from URL if present, fallback to `us-east-1`)

### Known Risks
- **S3 rate limiting (429)** — AWS throttles requests; need retry logic
- **Large latency (slow networks)** — 16 MB download can take 10+ seconds on slow networks
- **Presigned URL expiration** — URLs expire, but that's user's responsibility (manifest generation time)
- **Multi-region buckets** — Region mismatch can cause redirects (handle 307)

### Mitigations
- Retry 429 (rate limit) and 503 (service unavailable) with backoff
- Allow configurable timeout (defer to M7 CLI flags)
- Document presigned URL expiration in README (M9)
- Handle 307 redirects (AWS SDK does this automatically)

---

## Implementation Checklist

**Package structure:**
```go
// pkg/s3/client.go
package s3

import (
    "context"
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
    s3Client *s3.Client
}

func NewClient() (*Client, error)
func (c *Client) GetRange(ctx context.Context, url string, start, end int64) ([]byte, error)
func parseS3URL(url string) (bucket, key, region string, error)
```

**Unit tests:**
```go
// pkg/s3/client_test.go
func TestParseS3URL(t *testing.T)           // Test URL parsing
func TestGetRange(t *testing.T)             // Integration test with real S3
func TestGetRangeRetry(t *testing.T)        // Test retry logic (mock)
func TestGetRangeErrors(t *testing.T)       // Test 403/404 handling
func TestPresignedURL(t *testing.T)         // Test presigned URL support
```

**Retry logic pseudocode:**
```go
func (c *Client) GetRange(ctx, url, start, end) ([]byte, error) {
    bucket, key, region := parseS3URL(url)

    for retry := 0; retry < maxRetries; retry++ {
        resp, err := c.s3Client.GetObject(ctx, &s3.GetObjectInput{
            Bucket: &bucket,
            Key:    &key,
            Range:  fmt.Sprintf("bytes=%d-%d", start, end),
        })

        if err == nil {
            return io.ReadAll(resp.Body)  // Success
        }

        if isPermanentError(err) {
            return nil, err  // Don't retry 403/404
        }

        if isTransientError(err) {
            backoff := calculateBackoffWithJitter(retry)  // 1s, 2s, 4s ±25%
            time.Sleep(backoff)
            continue  // Retry
        }

        return nil, err  // Unknown error, don't retry
    }

    return nil, errors.New("max retries exceeded")
}
```

---

## Success Criteria

**This milestone is complete when:**
1. All unit tests pass: `go test ./pkg/s3 -v`
2. Can create S3 client with default AWS credentials
3. Can fetch byte range from S3 bucket (integration test)
4. Retries transient errors (503) with exponential backoff
5. Fails fast on permanent errors (403, 404)
6. Supports presigned URLs (no auth needed)
7. Parses S3 URLs correctly (bucket, key, region extraction)
8. Returns data as `[]byte` slice

**Integration readiness:**
- M5 (Fetch Manager) can call `s3Client.GetRange()` to fetch 16 MB chunks
- M7 (Mount Command) can create S3 client with default credentials

---

## Integration Test Setup

**Option 1: Real S3 Bucket (Recommended for MVP)**
```bash
# Create test bucket
aws s3 mb s3://mlartifactfs-test

# Upload test file
dd if=/dev/urandom of=test-file bs=1M count=32  # 32 MB file
aws s3 cp test-file s3://mlartifactfs-test/test-file

# Run integration test
go test ./pkg/s3 -v -run TestGetRange
```

**Option 2: LocalStack (Local S3 Emulator)**
```bash
docker run -d -p 4566:4566 localstack/localstack
AWS_ENDPOINT=http://localhost:4566 go test ./pkg/s3 -v
```

**Option 3: Minio (S3-Compatible Local Storage)**
```bash
docker run -d -p 9000:9000 minio/minio server /data
# Configure Minio credentials, run tests
```

**Recommendation:** Use real S3 for MVP (simplest), add LocalStack for CI/CD later.

---

## Notes

- This is a **thin wrapper around AWS SDK** — don't reimplement S3 protocol
- Focus on correct error handling and retry logic
- M5 (Fetch Manager) will orchestrate S3 + cache interactions
- Presigned URLs are already authenticated (query params contain signature)
- AWS SDK automatically handles redirects (307) and region detection

---

**Ready to implement?** Start with `pkg/s3/client.go` and URL parsing, then add retry logic and tests.
