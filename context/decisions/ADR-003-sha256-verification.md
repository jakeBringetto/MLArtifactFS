# ADR-003: SHA256 Verification After Full Download Only

**Status:** Accepted
**Date:** 2026-01-11 (from design doc)
**Context:** Milestone 5 (Fetch Manager)

---

## Decision

Verify SHA256 hashes **only after downloading the complete file**, not per-chunk. Mark files as verified using a `_verified` marker file in the cache.

---

## Context

Content integrity is critical for ML models. Corrupted weights can cause:
- Inference failures
- Silent accuracy degradation
- Security vulnerabilities (model poisoning)

Two verification strategies:
1. **Per-chunk verification:** Verify each 16 MB chunk independently
2. **Post-download verification:** Verify full file after all chunks downloaded

---

## Alternatives Considered

| Strategy | Pros | Cons |
|----------|------|------|
| Per-chunk verification | Detects corruption immediately | Requires chunk-level hashes in manifest (complexity) |
| **Post-download verification** | **Simple, uses existing SHA256** | **Partial reads unverified** |
| No verification | Simplest | Silent corruption risk |

---

## Rationale

1. **Manifest Simplicity:** Manifest already has full-file SHA256 from generation
2. **Chunk Hash Complexity:** Per-chunk hashes require:
   - Modified manifest format
   - More complex generator (`mlfs generate`)
   - Chunk boundary alignment during verification
3. **MVP Scope:** Partial reads without verification acceptable for POC
4. **Prefetch Path:** Prefetch files (config, tokenizer) are verified before mount
5. **Risk Mitigation:** S3 provides transport-level integrity (ETag, CRC32)

### Verification Flow

**Prefetch (verified):**
```
1. Download full file in 16 MB chunks
2. Compute SHA256 of complete file
3. Compare with manifest.Files[].SHA256
4. If match: write _verified marker
5. If mismatch: abort mount with error
```

**Lazy load (unverified):**
```
1. Read request for offset X
2. Check cache for chunk N
3. If miss: fetch chunk N from S3
4. Return chunk N data (no verification)
```

**Verification marker:**
```
<cache-dir>/<sha256>/chunk_0
<cache-dir>/<sha256>/chunk_1
<cache-dir>/<sha256>/_verified  ← marker file
```

---

## Consequences

### Positive
- Simple implementation (reuses existing manifest SHA256)
- No manifest format changes
- Prefetch path (critical files) is fully verified
- Lazy-loaded chunks rely on S3 transport integrity

### Negative
- Partial reads (lazy load) are not cryptographically verified
- Cache corruption not detected until full file downloaded
- Chunk-level tampering possible (low risk in POC context)

### Mitigations
- **Prefetch critical files:** Config/tokenizer verified at mount
- **Use HTTPS:** Transport-level encryption and integrity
- **S3 ETags:** S3 validates chunk integrity during transmission
- **Future enhancement:** Add per-chunk hashes to manifest (out of MVP scope)

---

## Implementation Notes

**Cache structure:**
```
<cache-dir>/
  abc123.../
    chunk_0      # 16 MB chunk
    chunk_1      # 16 MB chunk
    _verified    # Empty marker file (indicates full file verified)
```

**Verification logic:**
```go
func (f *FetchManager) Prefetch(ctx, url, sha256, size) error {
    // Download full file
    var buffer bytes.Buffer
    for chunkIdx := 0; chunkIdx < numChunks; chunkIdx++ {
        chunk := f.fetchChunk(ctx, url, chunkIdx)
        f.cache.Write(sha256, chunkIdx, chunk)
        buffer.Write(chunk)
    }

    // Verify SHA256
    actualHash := sha256sum(buffer.Bytes())
    if actualHash != sha256 {
        f.cache.DeleteAll(sha256) // Clean up corrupt cache
        return ErrHashMismatch
    }

    // Mark as verified
    f.cache.MarkVerified(sha256)
    return nil
}

func (c *CacheManager) MarkVerified(sha256 string) error {
    markerPath := filepath.Join(c.dir, sha256, "_verified")
    return os.WriteFile(markerPath, []byte{}, 0644)
}

func (c *CacheManager) IsVerified(sha256 string) bool {
    markerPath := filepath.Join(c.dir, sha256, "_verified")
    _, err := os.Stat(markerPath)
    return err == nil
}
```

---

## Future Enhancements (Out of MVP Scope)

### Option 1: Chunk-level SHA256
Extend manifest format:
```json
{
  "path": "pytorch_model.bin",
  "sha256": "abc123...",  // Full file hash
  "chunk_hashes": [       // Per-chunk hashes
    "chunk0hash...",
    "chunk1hash...",
    ...
  ]
}
```

**Trade-off:** Larger manifest files, more complex generation

### Option 2: Merkle Tree
Use Merkle tree for hierarchical verification:
- Root hash in manifest
- Internal nodes computed on-demand

**Trade-off:** More complex implementation

---

## Related

- **ADR-001:** 16 MB Chunk Size (chunk granularity for verification)
- **ADR-002:** Blocking Prefetch (where verification happens)
- **Milestone 2:** Manifest generator computes SHA256
- **Milestone 5:** Fetch Manager implements verification

---

## References

- Design doc: [planning/03-design.md](../mlartifactfs-planning/03-design.md#decision-3-sha256-verification-after-full-download)
- Implementation plan: [planning/04-implementation-plan.md](../mlartifactfs-planning/04-implementation-plan.md#milestone-5-fetch-manager)
