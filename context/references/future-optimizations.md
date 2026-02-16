# Future Optimizations & Enhancements

This document captures ideas for improving MLArtifactFS beyond the MVP scope.

---

## S3 Storage Cost Optimization

### Problem
Currently, each model version stores complete files in S3:
```
s3://models/llama-7b/v1.0/pytorch_model.bin (25 GB)
s3://models/llama-7b/v1.1/pytorch_model.bin (25 GB)
s3://models/llama-7b/v1.2/pytorch_model.bin (25 GB)
```

**Cost:** 75 GB total storage for 3 versions (mostly duplicate data)

### Proposed Solution: Content-Addressable Storage with Deduplication

**Approach 1: Content-Addressable Blobs (Git/Docker-style)**
```
s3://models/blobs/
  sha256:abc123...  (25 GB - base model weights)
  sha256:def456...  (100 MB - delta for v1.1)
  sha256:ghi789...  (200 MB - delta for v1.2)

manifests/
  llama-7b-v1.0.json → references blob sha256:abc123
  llama-7b-v1.1.json → references blobs abc123 + def456
  llama-7b-v1.2.json → references blobs abc123 + ghi789
```

**Savings:** ~50 GB for 3 versions (75 GB → 25.3 GB)

**Approach 2: Binary Diffs (rsync-style)**
```
s3://models/llama-7b/
  base/v1.0/pytorch_model.bin (25 GB)
  diffs/v1.0-to-v1.1.diff     (100 MB)
  diffs/v1.1-to-v1.2.diff     (200 MB)
```

Manifest references either:
- Full file URL (for base version)
- Base URL + diff chain (for derived versions)

**Tradeoff:** Reconstruction requires fetching base + applying diffs (slower first access)

---

## Implementation Considerations

### For Manifest Generator
Add optional flag:
```bash
mlfs generate \
  --id llama-7b \
  --version v1.1 \
  --base-version v1.0 \  # NEW: compute diff against base
  --url-prefix https://s3.../llama-7b \
  ./llama-7b-v1.1 > manifest-v1.1.json
```

**Diff Job Process:**
1. Download base version files (or read from local cache)
2. Compute binary diff using algorithm like:
   - `bsdiff` (good compression for binaries)
   - `xdelta3` (fast, streaming)
   - Custom chunked diff (aligned with 16 MB chunks)
3. Upload only diff files to S3
4. Manifest references base + diffs

### For Fetch Manager
**Reconstruction logic:**
```go
// If file has diff chain, reconstruct
if file.BaseSHA256 != "" && len(file.Diffs) > 0 {
    base := fetchChunk(file.BaseURL)
    for _, diff := range file.Diffs {
        patch := fetchChunk(diff.URL)
        base = applyPatch(base, patch)
    }
    return base
}
```

**Cache both:**
- Diff files (small, fast to cache)
- Reconstructed full file (for subsequent reads)

---

## Storage Cost Analysis

### Example: LLaMA Model Versions

**Assumptions:**
- Model size: 25 GB per version
- 10 versions over project lifetime
- Diff size: ~5% of model size (1.25 GB per diff)

**Current approach (full files):**
```
10 versions × 25 GB = 250 GB
S3 Standard: $250 × $0.023/GB = $5.75/month
```

**With diffs:**
```
1 base (25 GB) + 9 diffs (9 × 1.25 GB) = 36.25 GB
S3 Standard: $36.25 × $0.023/GB = $0.83/month

Savings: $4.92/month (85% reduction)
```

**At scale (100 versions):**
- Current: 2.5 TB = $57.50/month
- With diffs: 148.75 GB = $3.42/month
- **Savings: $54/month (94% reduction)**

---

## Related Optimizations

### 1. S3 Intelligent Tiering
- Move old versions to cheaper storage classes
- Base versions → S3 Glacier (if rarely accessed)
- Recent versions → S3 Standard

### 2. Compression
- Store diffs compressed (gzip, zstd)
- Decompress during reconstruction
- Manifest tracks compression type

### 3. Content-Addressable Chunks
Instead of diffing whole files, break into 16 MB chunks:
```
s3://models/chunks/
  sha256:aaa... (16 MB chunk)
  sha256:bbb... (16 MB chunk)
  ...

Manifest:
{
  "path": "pytorch_model.bin",
  "chunks": [
    {"sha256": "aaa...", "url": "s3://.../sha256:aaa"},
    {"sha256": "bbb...", "url": "s3://.../sha256:bbb"}
  ]
}
```

**Benefits:**
- Automatic deduplication (shared chunks across versions)
- Aligns with FUSE 16 MB chunk size
- Similar to how OCI/Docker layers work

---

## Implementation Priority

**MVP (Current):** Full files per version (simple, works)

**Phase 2 (Post-MVP):**
1. Add content-addressable chunk storage
2. Implement diff generation in `mlfs generate`
3. Update fetch manager to reconstruct from diffs

**Phase 3 (Production):**
1. S3 lifecycle policies (auto-tiering)
2. Compression support
3. Metrics/monitoring for storage costs

---

## Prior Art

### Similar Systems
- **Git:** Content-addressable objects + packfiles with deltas
- **Docker:** Content-addressable layers (shared across images)
- **SOCI:** eStargz format (gzip-compressed, indexed chunks)
- **Perkeep:** Content-addressable blob storage
- **BorgBackup:** Deduplicating backup with chunk-level dedup

### Relevant Algorithms
- **bsdiff/bspatch:** Binary diff/patch (used by Chrome updates)
- **xdelta3:** Fast binary delta encoding
- **rsync algorithm:** Rolling hash for chunk-based sync
- **zstd dictionaries:** Compression with shared dictionary (for similar files)

---

## Open Questions

1. **Diff chain depth limit?**
   - Limit to 5 diffs before creating new base?
   - Prevents long reconstruction times

2. **Diff generation job placement?**
   - User runs locally (requires base version download)
   - Server-side job (requires S3 compute permissions)
   - Separate CI/CD pipeline

3. **Manifest complexity?**
   - Keep manifest simple (URL per file)
   - Or: Support chunk lists, diff chains, etc.

4. **Cache invalidation?**
   - If base version changes, invalidate all derived versions
   - Use manifest hash as cache key

---

## Recommendation

**For now:** Proceed with MVP (full files per version)

**Next step after MVP is validated:** Implement content-addressable chunk storage (simplest, aligns with FUSE chunk boundaries, automatic dedup)

**Monitor:** S3 storage costs in production to justify optimization effort

---

**Date added:** 2026-01-19
**Priority:** Post-MVP
**Estimated effort:** 1-2 weeks for chunk-based storage
