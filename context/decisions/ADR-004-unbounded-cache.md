# ADR-004: Unbounded Cache (No Eviction Policy)

**Status:** Accepted (MVP Only)
**Date:** 2026-01-11 (from design doc)
**Context:** Milestone 3 (Cache Manager)

---

## Decision

The cache will **grow unbounded** with no automatic eviction policy in the MVP implementation.

---

## Context

The cache stores downloaded chunks from S3 to avoid repeated fetches. Over time, the cache can grow to match the size of all accessed models.

Options:
1. **No eviction (unbounded):** Cache grows indefinitely
2. **LRU eviction:** Remove least-recently-used chunks when limit reached
3. **TTL eviction:** Expire chunks after time period
4. **Manual cleanup:** User deletes cache directory

---

## Alternatives Considered

| Strategy | Pros | Cons |
|----------|------|------|
| **Unbounded (MVP)** | **Simple, no edge cases** | **Disk can fill up** |
| LRU eviction | Bounded disk usage | Complex (track access times, evict atomically) |
| TTL eviction | Auto-cleanup old models | Doesn't prevent disk overflow |
| Manual cleanup | User controls cache | Requires documentation, prone to errors |

---

## Rationale

1. **MVP Scope:** Cache eviction adds complexity without core value for POC
2. **Container Environment:** Docker containers typically have dedicated volumes
3. **Model Count:** Experimental workflows use 2-5 models, not hundreds
4. **Explicit Management:** Users can delete cache directory between runs
5. **Disk Monitoring:** User responsible for provisioning sufficient disk

### Cache Growth Estimate

**Typical ML model sizes:**
- Small model (DistilBERT): 250 MB
- Medium model (BERT-base): 440 MB
- Large model (Llama-7B): 13 GB
- Very large model (Llama-70B): 130 GB

**Worst case (cache full models):**
- 5 small models: 1.25 GB
- 5 medium models: 2.2 GB
- 5 large models: 65 GB

**POC expectation:** <10 GB cache for typical experimentation

---

## Consequences

### Positive
- Simple implementation (no eviction logic)
- Fast cache hits (no accidental eviction)
- Predictable behavior (cache persists until manually deleted)
- No need to track access times or implement LRU

### Negative
- Cache can fill disk if user accesses many models
- No automatic cleanup
- User must monitor disk usage
- Container may crash if disk fills

### Mitigations
- **Document disk requirements:** Recommend provisioning 2x largest model size
- **Clear error messages:** Fail with "disk full" error on write failure
- **Manual cleanup instructions:** Document `rm -rf <cache-dir>` in README
- **Future enhancement:** Add LRU eviction post-MVP

---

## Implementation Notes

**Cache structure (no eviction):**
```
<cache-dir>/
  <sha256-1>/
    chunk_0
    chunk_1
    _verified
  <sha256-2>/
    chunk_0
    chunk_1
    _verified
  ... (grows unbounded)
```

**Write logic (fails if disk full):**
```go
func (c *CacheManager) Write(sha256 string, chunkIndex int, data []byte) error {
    chunkPath := c.ChunkPath(sha256, chunkIndex)
    os.MkdirAll(filepath.Dir(chunkPath), 0755)

    // Write chunk (will fail if disk full)
    if err := os.WriteFile(chunkPath, data, 0644); err != nil {
        return fmt.Errorf("cache write failed (disk full?): %w", err)
    }
    return nil
}
```

**Error handling:**
```
ERROR: cache write failed (disk full?): no space left on device
→ User must provision more disk or clean cache
```

---

## User Documentation Requirements

**README must include:**
1. Cache location: `--cache-dir` flag (default: `/tmp/mlfs-cache`)
2. Disk requirements: "Provision disk equal to total size of models accessed"
3. Manual cleanup: `rm -rf /tmp/mlfs-cache` to clear cache
4. Docker volume recommendation: `-v mlfs-cache:/tmp/mlfs-cache`

**Example user guidance:**
```bash
# Check cache size
du -sh /tmp/mlfs-cache

# Clear cache
rm -rf /tmp/mlfs-cache

# Mount with custom cache location
mlfs mount --cache-dir /mnt/large-disk/cache --manifest manifest.json /mnt/model
```

---

## Future Enhancements (Out of MVP Scope)

### Post-MVP: LRU Eviction

**Implementation sketch:**
```go
type CacheManager struct {
    maxSize int64
    lru     *LRUCache
}

func (c *CacheManager) Read(sha256, chunkIndex) ([]byte, error) {
    data := readChunk(sha256, chunkIndex)
    c.lru.Touch(sha256, chunkIndex) // Update access time
    return data
}

func (c *CacheManager) Write(sha256, chunkIndex, data) error {
    // Check size
    if c.currentSize + len(data) > c.maxSize {
        c.evictLRU() // Evict until space available
    }
    writeChunk(sha256, chunkIndex, data)
    c.lru.Add(sha256, chunkIndex)
}
```

**Trade-offs:**
- Requires persistent LRU state (survive restarts)
- Atomic eviction (don't evict partially-downloaded files)
- Verification marker invalidation

---

## Related

- **ADR-001:** 16 MB Chunk Size (affects cache growth rate)
- **Milestone 3:** Cache Manager implementation
- **Milestone 9:** Documentation (must document disk requirements)

---

## References

- Design doc: [planning/03-design.md](../mlartifactfs-planning/03-design.md#decision-4-unbounded-cache)
- Implementation plan: [planning/04-implementation-plan.md](../mlartifactfs-planning/04-implementation-plan.md#milestone-3-cache-manager)
