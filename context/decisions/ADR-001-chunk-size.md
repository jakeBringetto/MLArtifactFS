# ADR-001: 16 MB Chunk Size for S3 Range Requests

**Status:** Accepted
**Date:** 2026-01-11 (from design doc)
**Context:** Milestone 3-5 (Cache, S3, Fetch Manager)

---

## Decision

Use **16 MB chunks** for S3 range requests and cache granularity.

---

## Context

MLArtifactFS needs to fetch file chunks from S3 using range requests. The chunk size determines:
- Granularity of cache storage
- S3 request overhead (smaller chunks = more requests)
- Bandwidth waste (larger chunks = more overread)
- Memory footprint per read operation

ML model files (pytorch_model.bin, safetensors) are typically accessed sequentially during model loading.

---

## Alternatives Considered

| Chunk Size | Pros | Cons |
|------------|------|------|
| 4 MB | Less overread, smaller memory footprint | More S3 requests, higher latency |
| 8 MB | AWS recommended minimum for performance | Still higher request overhead |
| **16 MB** | **Optimal S3 throughput, fewer requests** | **Some wasted bandwidth on random access** |
| 64 MB | Minimal requests | High overread, large memory usage |

---

## Rationale

1. **AWS Best Practice:** AWS recommends 8-16 MB range requests for optimal throughput
2. **Access Pattern:** ML models are typically read sequentially (config → weights), favoring larger chunks
3. **Request Overhead:** Fewer requests = lower latency, better throughput, reduced S3 costs
4. **Trade-off:** Acceptable overread for POC scope (reading 1 byte fetches 16 MB, but this is rare)

### Performance Implications

- **Sequential read:** Near-zero overhead (each 16 MB chunk serves many reads)
- **Random read:** Up to 16 MB overread per unique 16 MB region
- **Cache efficiency:** 16 MB chunks balance disk I/O and memory usage

---

## Consequences

### Positive
- High throughput for sequential reads (typical ML model access)
- Fewer S3 requests → lower latency, lower cost
- Simple chunk alignment logic (`offset / chunkSize`)

### Negative
- Random access patterns waste bandwidth (up to 16 MB per chunk)
- 16 MB memory footprint per read operation

### Mitigations
- Prefetch critical files (config, tokenizer) at mount time
- ML model access is predominantly sequential, minimizing waste
- Future: Adaptive chunk sizing based on access patterns (out of MVP scope)

---

## Implementation Notes

```go
const ChunkSize = 16 * 1024 * 1024 // 16 MB

// Calculate chunk index and aligned range
chunkIndex := offset / ChunkSize
chunkStart := chunkIndex * ChunkSize
chunkEnd := chunkStart + ChunkSize
```

Cache structure:
```
<cache-dir>/<sha256>/chunk_0  # bytes 0-16MB
<cache-dir>/<sha256>/chunk_1  # bytes 16-32MB
...
```

---

## Related

- **ADR-002:** Blocking Prefetch at Mount (reduces random access)
- **Milestone 3:** Cache Manager implementation
- **Milestone 5:** Fetch Manager uses chunk alignment

---

## References

- AWS S3 Performance Best Practices: 8-16 MB range requests recommended
- Design doc: [planning/03-design.md](../mlartifactfs-planning/03-design.md#decision-1-chunk-size-16-mb)
