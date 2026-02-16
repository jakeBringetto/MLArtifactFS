# ADR-006: Retry Policy with Exponential Backoff and Jitter

**Status:** Accepted
**Date:** 2026-02-16
**Context:** Milestone 4 (S3 Client)

---

## Decision

Use **exponential backoff with jitter** for retrying transient S3 errors, with:
- **Base delay:** 1 second
- **Max retries:** 3 attempts (total 4 tries including initial request)
- **Backoff multiplier:** 2x (1s → 2s → 4s)
- **Jitter:** ±25% random variation
- **Total max delay:** ~7 seconds (1s + 2s + 4s)

---

## Context

S3 operations can fail due to:
- **Transient errors:** 503 Service Unavailable, 429 Too Many Requests, connection timeouts
- **Permanent errors:** 403 Forbidden, 404 Not Found (should not retry)

Without retries, temporary S3 issues cause mount failures. With naive retries (fixed delay), multiple clients can create a "thundering herd" problem where all clients retry simultaneously, overwhelming S3.

---

## Alternatives Considered

| Strategy | Pros | Cons |
|----------|------|------|
| No retry | Simple | Fails on transient errors |
| Fixed delay (e.g., 2s) | Simple, predictable | Thundering herd risk, slow for quick recoveries |
| Exponential backoff (no jitter) | Backs off for persistent issues | Thundering herd risk (all clients retry at same intervals) |
| **Exponential + jitter** | **Spreads load, adapts to issue duration** | **Slightly more complex** |
| Adaptive (measure success rate) | Optimal for chronic issues | Too complex for MVP, needs state tracking |

---

## Rationale

### Exponential Backoff
- **Progressive delays** (1s, 2s, 4s) give S3 time to recover from overload
- **Quick recovery** for brief glitches (1s is fast enough for transient blips)
- **Bounded total delay** (~7s) prevents excessive mount latency

### Jitter (±25% random variation)
- **Prevents thundering herd:** 100 clients don't all retry at exactly 1s, 2s, 4s
- **Spreads load:** Retries distributed over time window
- **AWS recommendation:** [AWS Architecture Blog on exponential backoff](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)

### Max 3 Retries
- **Total attempts:** 4 (initial + 3 retries)
- **Total time:** ~7 seconds (worst case)
- **Rationale:** S3 transient errors typically resolve within seconds; if not, likely a systemic issue (outage, permissions)

---

## Implementation

### Retry Logic
```go
const (
    maxRetries = 3
    baseDelay  = 1 * time.Second
)

func (c *Client) GetRange(ctx, url, start, end) ([]byte, error) {
    var lastErr error

    for attempt := 0; attempt <= maxRetries; attempt++ {
        resp, err := c.fetchFromS3(ctx, url, start, end)

        if err == nil {
            return resp, nil  // Success
        }

        lastErr = err

        // Don't retry permanent errors
        if isPermanentError(err) {
            return nil, fmt.Errorf("permanent S3 error: %w", err)
        }

        // Don't retry if we've exhausted attempts
        if attempt == maxRetries {
            break
        }

        // Only retry transient errors
        if !isTransientError(err) {
            return nil, fmt.Errorf("non-retryable error: %w", err)
        }

        // Calculate backoff with jitter
        delay := calculateBackoff(attempt)
        time.Sleep(delay)
    }

    return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

func calculateBackoff(attempt int) time.Duration {
    // Exponential: 1s, 2s, 4s
    delay := baseDelay * (1 << attempt)

    // Add jitter: ±25%
    jitter := time.Duration(rand.Float64() * 0.5 * float64(delay))  // 0-50% of delay
    delay = delay - time.Duration(0.25*float64(delay)) + jitter    // Center at original, ±25%

    return delay
}
```

### Error Classification
```go
func isTransientError(err error) bool {
    // HTTP 503, 429, connection timeouts
    var respErr *http.ResponseError
    if errors.As(err, &respErr) {
        return respErr.StatusCode == 503 || respErr.StatusCode == 429
    }

    // Network errors
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }

    return false
}

func isPermanentError(err error) bool {
    var respErr *http.ResponseError
    if errors.As(err, &respErr) {
        return respErr.StatusCode == 403 ||
               respErr.StatusCode == 404 ||
               respErr.StatusCode == 416
    }
    return false
}
```

---

## Consequences

### Positive
- **Resilient to transient S3 issues** (503, connection blips)
- **Prevents thundering herd** with jitter
- **Fast recovery** for brief glitches (1s first retry)
- **Bounded latency** (~7s max before failure)
- **AWS best practice** alignment

### Negative
- **Added complexity** (jitter calculation, error classification)
- **Delayed failure** for permanent errors if misclassified
- **Non-deterministic timing** (jitter makes timing unpredictable in tests)

### Mitigations
- **Test jitter separately:** Unit test `calculateBackoff()` in isolation
- **Mock clock in tests:** Use `time.Now()` stub for deterministic test timing
- **Log all retries:** Include attempt number, delay, error in logs for debugging
- **Strict error classification:** Err on side of "permanent" if unsure (fail fast)

---

## Jitter Calculation Details

**Formula:** `delay = base * 2^attempt ± 25% random`

**Example distribution for retry 1 (2s base):**
```
Min delay: 1.5s (2s - 25%)
Max delay: 2.5s (2s + 25%)
Average:   2.0s

100 clients retry:
  - Without jitter: All retry at exactly 2.000s
  - With jitter:     Spread over 1.5-2.5s window (1 second spread)
```

**Benefit:** 100x reduction in simultaneous requests during retry storms.

---

## Testing Strategy

### Unit Tests
```go
func TestCalculateBackoff(t *testing.T) {
    // Test exponential growth
    // Test jitter is within ±25%
}

func TestRetryLogic(t *testing.T) {
    // Mock S3 client with failures
    // Verify retry count
    // Verify delays (approximate, due to jitter)
}
```

### Integration Tests
```go
func TestRetryWith503(t *testing.T) {
    // Use real S3 or mock server
    // Return 503 twice, then 200
    // Verify success after retries
}

func TestNoRetryWith404(t *testing.T) {
    // Return 404
    // Verify immediate failure (no retries)
}
```

---

## Future Enhancements (Out of MVP Scope)

### Adaptive Backoff
Track success rate per bucket/region, adjust backoff multiplier:
- High error rate → longer backoff
- Low error rate → shorter backoff

### Circuit Breaker
After N consecutive failures, stop retrying for cooldown period (e.g., 30s):
- Prevents wasting resources on sustained outages
- Faster failure for systemic issues

### Metrics
Export retry metrics to Prometheus:
- `s3_retries_total{reason="503"}`
- `s3_retry_backoff_seconds{attempt="1"}`

---

## Related

- **Milestone 4:** S3 Client implementation
- **Milestone 5:** Fetch Manager (also uses retry logic for chunk fetches)
- **AWS Best Practices:** [Exponential Backoff and Jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/)

---

## References

- AWS Architecture Blog: "Exponential Backoff and Jitter"
- Google SRE Book: Chapter 21 (Handling Overload)
- RFC 6585: 429 Too Many Requests status code
