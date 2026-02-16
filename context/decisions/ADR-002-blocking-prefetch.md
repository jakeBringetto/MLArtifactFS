# ADR-002: Blocking Prefetch at Mount Time

**Status:** Accepted
**Date:** 2026-01-11 (from design doc)
**Context:** Milestone 7 (CLI Mount Command)

---

## Decision

**Prefetch** files listed in `manifest.prefetch[]` **synchronously at mount time**, blocking the mount operation until all prefetch files are downloaded and verified.

---

## Context

ML models typically have small critical files (config.json, tokenizer.json, tokenizer_config.json) that are accessed immediately during model initialization. These files are <1 MB but essential for model loading.

Two options:
1. **Blocking prefetch:** Download before mount completes
2. **Async prefetch:** Download in background after mount

---

## Alternatives Considered

| Approach | Pros | Cons |
|----------|------|------|
| **Blocking Prefetch** | **Zero startup latency for critical files** | **Mount operation takes 1-10 seconds** |
| Async Prefetch | Mount completes instantly | First model load has latency spike |
| No Prefetch | Simplest implementation | Every small file incurs S3 round-trip |

---

## Rationale

1. **Startup Pattern:** ML inference scripts *always* read config/tokenizer first
2. **Latency Impact:** Async prefetch = 100-500ms latency spike on first model load
3. **User Expectation:** After mount completes, model should be "ready to use"
4. **Acceptable Delay:** 1-10 second mount time is acceptable for POC workflow

### Performance Analysis

**Without prefetch (cold cache):**
```
Model load sequence:
1. Read config.json (500ms S3 round-trip)
2. Read tokenizer.json (500ms S3 round-trip)
3. Read pytorch_model.bin (lazy, chunked)
Total cold start: ~1 second + model load time
```

**With blocking prefetch:**
```
Mount operation:
1. Prefetch config.json (500ms)
2. Prefetch tokenizer.json (500ms)
3. Mount completes (total: 1 second)

Model load sequence:
1. Read config.json (0ms - cached)
2. Read tokenizer.json (0ms - cached)
3. Read pytorch_model.bin (lazy, chunked)
Total cold start: ~model load time only
```

---

## Consequences

### Positive
- Eliminates latency for critical files during model initialization
- Simple implementation (sequential downloads, single error path)
- Predictable mount behavior (completes when ready)
- SHA256 verification completes before mount

### Negative
- Mount operation can take 1-10 seconds (depending on prefetch size)
- Failure during prefetch aborts entire mount
- User must wait for mount to complete

### Mitigations
- **Keep prefetch list small:** Only config/tokenizer files (<5 MB total)
- **Log progress:** Show "Prefetching config.json..." messages
- **Fail fast:** Exit immediately on S3 404/403 errors
- **Recommend:** Use prefetch for <50 MB total

---

## Implementation Notes

**Mount flow:**
```go
// Load manifest
manifest := LoadManifest(manifestPath)

// Initialize managers
s3Client := NewS3Client()
cache := NewCacheManager(cacheDir)
fetch := NewFetchManager(s3Client, cache)

// Prefetch files (blocks here)
for _, file := range manifest.Files {
    if contains(manifest.Prefetch, file.Path) {
        log.Printf("Prefetching %s...", file.Path)
        if err := fetch.Prefetch(ctx, file.URL, file.SHA256, file.Size); err != nil {
            log.Fatalf("Prefetch failed: %v", err)
        }
    }
}

// Now mount FUSE
fs := NewFS(manifest, fetch)
Mount(fs, mountPoint)
```

**Prefetch verification:**
```go
func (f *FetchManager) Prefetch(ctx, url, sha256, size) error {
    // Download full file in chunks
    data := downloadFullFile(ctx, url)

    // Verify SHA256
    actualHash := sha256sum(data)
    if actualHash != sha256 {
        return ErrHashMismatch
    }

    // Mark as verified
    cache.MarkVerified(sha256)
    return nil
}
```

---

## Related

- **ADR-001:** 16 MB Chunk Size (affects prefetch download)
- **ADR-003:** SHA256 Post-Download Verification (prefetch is where verification happens)
- **Milestone 7:** CLI Mount Command implementation

---

## References

- Design doc: [planning/03-design.md](../mlartifactfs-planning/03-design.md#decision-2-prefetch-at-mount-time)
- Implementation plan: [planning/04-implementation-plan.md](../mlartifactfs-planning/04-implementation-plan.md#milestone-7-cli-mount-command)
