# M3 Cache Manager — Implementation Notes

**Created:** 2026-02-16
**Context:** Observations during Milestone 3 implementation for future production hardening

---

## Design Review Summary

The M3 cache manager implementation follows a **content-addressable storage** pattern with atomic writes. The design is appropriate for POC scope (2-10 models) but will need enhancements for production scale.

**Overall Assessment:**
- ✅ POC/MVP: Good enough - follows industry best practices for core operations
- ⚠️ Production: Needs scalability and observability enhancements

---

## Security Observations

### 1. Directory Traversal Protection
**Status:** ✅ Handled by design

The SHA256 validation regex `^[a-f0-9]{64}$` prevents path traversal attacks:
- Only allows lowercase hex characters (no `../` or path separators)
- Fixed length (64 chars) prevents truncation attacks

**No action needed for M3.**

### 2. File Permissions
**Current:** Cache dir `0755`, chunk files `0644` (world-readable)

**Rationale for POC:**
- Simplifies debugging (can inspect cache as non-root)
- Container environments typically single-tenant
- Model weights are usually not secrets (publicly downloadable)

**Production consideration:**
- If cache contains proprietary/sensitive models, use `0700`/`0600`
- Add flag: `--cache-permissions` to allow user control
- Document security model in deployment guide

**Recommendation:** Add TODO comment, defer to post-MVP

### 3. Cache Poisoning Risk
**Current:** Per-chunk writes have no integrity verification until full file downloaded

**From ADR-003:** SHA256 verification happens after full download only
- Prefetch path: verified before mount ✅
- Lazy load path: unverified until complete ⚠️

**Attack scenario:**
1. Attacker modifies cached chunk on disk
2. Application reads corrupted chunk
3. Corruption not detected (no per-chunk hash)

**Mitigations (current):**
- S3 transport integrity (HTTPS, ETags)
- Filesystem permissions prevent unauthorized writes
- Prefetch critical files (config, tokenizer)

**Future enhancement (out of MVP scope):**
- Add per-chunk SHA256 to manifest (see ADR-003 future enhancements)
- Verify chunk hash on read (optional paranoid mode)
- Add `VerifyChunk(sha256, chunkIndex) bool` method

**Recommendation:** Document risk in ADR-003, defer mitigation to post-MVP

---

## Scalability Observations

### 1. Flat Directory Structure Performance
**Current design:**
```
<cache-dir>/
  abc123.../  # SHA256 as directory name
  def456.../
  ...
```

**Filesystem limits:**
- ext4: ~10 million files per directory (but slow after ~10,000)
- xfs: Better, but still degradation at scale
- Directory listing (readdir) becomes O(n) scan

**When this becomes a problem:**
- 100+ models in cache (unlikely for POC)
- 1000+ models (enterprise scale)

**Industry standard: Hash prefix sharding**
```
<cache-dir>/
  ab/           # First 2 chars of SHA256
    c123.../    # Remaining 62 chars
  de/
    f456.../
```

**Benefits:**
- Limits entries per directory to ~256 (for 2-char prefix)
- Keeps filesystem operations fast
- Used by Git (`.git/objects/ab/c123...`)

**Implementation complexity:**
- Change `ChunkPath()` to insert prefix
- Migration needed for existing caches
- More complex cache inspection (can't just `ls <cache-dir>`)

**Recommendation for M3:** Keep flat structure, add TODO comment
**Recommendation for production:** Implement sharding when cache expected to have >100 models

### 2. Cache Statistics & Observability
**Current:** No visibility into cache state

**Missing features:**
- Total cache size (bytes)
- Number of cached files
- Cache hit/miss rate
- Disk usage warnings

**Useful for production:**
```go
type CacheStats struct {
    TotalSize   int64
    NumFiles    int
    NumVerified int
    HitRate     float64  // Requires tracking hits/misses
}

func (m *Manager) GetStats() (*CacheStats, error)
```

**Use cases:**
- Monitoring/alerting (Prometheus metrics)
- Debugging performance issues
- Capacity planning

**Recommendation:** Defer to post-MVP, add as M10 (Observability milestone)

### 3. Manual Cache Management
**Current:** No API for cache cleanup

**User must manually:**
```bash
rm -rf <cache-dir>  # Nuclear option
```

**Production needs:**
```go
func (m *Manager) DeleteFile(sha256 string) error
func (m *Manager) Clear() error
func (m *Manager) GetSize(sha256 string) (int64, error)
```

**Use cases:**
- CI/CD: clear cache between jobs
- Manual eviction: delete unused models
- Debugging: remove corrupted cache entries

**Recommendation:** Add to M9 (Documentation) with manual cleanup instructions
**Future:** Add cleanup API post-MVP

---

## Concurrency & Race Conditions

### Current Status (M3)
**Assumption:** Single-threaded access (mount process only)

**Safe operations:**
- Multiple reads to same chunk ✅ (filesystem handles this)
- Read + write to different chunks ✅ (different files)

**Unsafe operations:**
- Concurrent writes to same chunk ⚠️ (last write wins, temp file collision)
- Read during write ⚠️ (might read temp file or incomplete chunk)
- Mark verified during active writes ⚠️ (partial file marked verified)

### Future (M5: Fetch Manager)
**M5 will introduce:**
- Concurrent prefetch (parallel chunk downloads)
- Lazy load during prefetch (read while writing other chunks)

**Mitigation strategies:**

**Option 1: Per-chunk locks (recommended)**
```go
type Manager struct {
    dir   string
    locks map[string]*sync.RWMutex  // Key: "sha256:chunkIndex"
}
```

**Option 2: File-level locks**
```go
type Manager struct {
    dir   string
    locks map[string]*sync.RWMutex  // Key: sha256
}
```

**Option 3: Filesystem locks (flock)**
- More complex, platform-specific
- Better for multi-process scenarios

**Recommendation for M5:**
- Add `sync.RWMutex` map for per-chunk locking
- `Read()` takes read lock, `Write()` takes write lock
- Add `Close()` method to clean up locks

**Deferred to M5 implementation.**

---

## Organization & Maintainability

### 1. Cache Directory Validation
**Current:** Cache created at arbitrary location (user-specified)

**Risk:** User specifies `/` as cache dir → fills root filesystem

**Production hardening:**
```go
func NewManager(dir string) (*Manager, error) {
    // Validate cache dir is not a system directory
    if dir == "/" || dir == "/etc" || dir == "/usr" {
        return nil, errors.New("cache directory cannot be a system directory")
    }

    // Check available disk space
    stat := syscall.Statfs(dir)
    if stat.Bavail * stat.Bsize < minRequiredSpace {
        return nil, errors.New("insufficient disk space for cache")
    }

    // Create cache dir...
}
```

**Recommendation:** Add to post-MVP (operational safety)

### 2. Error Context & Logging
**Current:** Errors returned with basic context

**Production needs:**
- Structured logging (e.g., `slog`)
- Error wrapping with full context
- Metrics (cache writes, reads, failures)

**Example:**
```go
func (m *Manager) Write(...) error {
    logger.Debug("cache write started",
        "sha256", sha256[:16],
        "chunk", chunkIndex,
        "size", len(data))

    // ... operation ...

    if err != nil {
        logger.Error("cache write failed",
            "sha256", sha256[:16],
            "chunk", chunkIndex,
            "error", err)
        return err
    }

    logger.Debug("cache write completed")
    return nil
}
```

**Recommendation:** Add logging in M7 (CLI Mount Command) when wiring components

---

## Comparison to Industry Standards

### Similar Systems

**Docker Image Layers:**
- Content-addressable: ✅ (SHA256)
- Atomic writes: ✅ (temp + rename)
- Sharding: ✅ (hash prefix)
- Deduplication: ✅ (shared layers)
- Our design: Similar, no sharding yet

**Git Objects:**
- Content-addressable: ✅ (SHA1, moving to SHA256)
- Directory structure: `objects/ab/c123...` (2-char prefix sharding)
- Atomic writes: ✅ (temp + rename)
- Our design: Same pattern, flat instead of sharded

**IPFS (InterPlanetary File System):**
- Content-addressable: ✅ (multihash)
- Sophisticated sharding + DHT
- Deduplication across network
- Our design: Much simpler (local only)

**Bazel Remote Cache:**
- Content-addressable: ✅ (SHA256)
- Sharding for large repos
- Size limits and eviction
- Our design: Similar for small scale

### Alignment Assessment

**Our cache design aligns with industry standards for:**
- ✅ Content-addressable storage (SHA256)
- ✅ Atomic write pattern (temp + rename)
- ✅ Idempotent operations
- ✅ Verification markers

**Deviations (acceptable for POC):**
- ⚠️ No sharding (fine for <100 models)
- ⚠️ No eviction (ADR-004: unbounded cache)
- ⚠️ No concurrency control (M3 scope: single-threaded)
- ⚠️ No observability (defer to post-MVP)

**Conclusion:** Design follows best practices for POC scale. Production deployment at enterprise scale would need enhancements listed above.

---

## Recommendations Summary

### ✅ For M3 (Implement Now)
1. Keep simple flat directory structure
2. Use atomic write pattern (temp + rename)
3. Validate SHA256 format (regex)
4. Return clear error messages
5. Add TODO comments for future enhancements

### 📝 For M5 (Fetch Manager)
1. Add concurrency control (`sync.RWMutex` per chunk)
2. Test concurrent read/write scenarios
3. Ensure verification marker set after all writes complete

### 🔮 For Post-MVP (Production Hardening)
1. **Scalability:** Hash prefix sharding (2-char)
2. **Observability:** Cache stats API, metrics, structured logging
3. **Management:** Manual cleanup API (`DeleteFile`, `Clear`)
4. **Security:** Configurable permissions, per-chunk verification
5. **Safety:** Cache directory validation, disk space checks, size limits

### 📚 For M9 (Documentation)
1. Document cache directory should be on separate volume
2. Document manual cleanup: `rm -rf <cache-dir>`
3. Document disk requirements (2x largest model size)
4. Document security model (world-readable by default)
5. Add troubleshooting section for "disk full" errors

---

## Open Questions for Future Milestones

1. **M5 Concurrency:** Use per-chunk locks or file-level locks?
2. **M7 CLI:** Should cache directory be validated against system directories?
3. **Post-MVP:** Should we support multiple cache backends (memory, Redis, S3)?
4. **Post-MVP:** Should we add cache compression (zstd) for bandwidth-limited environments?
5. **Post-MVP:** Should we support cache migration between different cache versions?

---

**Next Steps:** Proceed with M3 implementation using simple design, revisit this document during M5 and post-MVP planning.
