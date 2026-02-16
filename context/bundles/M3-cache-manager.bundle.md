# M3: Cache Manager — Context Bundle

**Target:** Implement local disk cache with chunk storage and SHA256 verification
**Estimated Reading Time:** 3 minutes

---

## Objective

Build a cache manager that stores and retrieves 16 MB chunks from local disk, organized by SHA256 hash, with verification markers for integrity-checked files.

---

## Deliverable Definition

### Acceptance Criteria

- [ ] `CacheManager` struct with constructor `NewManager(dir string)`
- [ ] `Has(sha256, chunkIndex) bool` — check if chunk exists on disk
- [ ] `Read(sha256, chunkIndex) ([]byte, error)` — read chunk from disk
- [ ] `Write(sha256, chunkIndex, data []byte) error` — write chunk to disk
- [ ] `IsVerified(sha256) bool` — check if `_verified` marker exists
- [ ] `MarkVerified(sha256) error` — create `_verified` marker file
- [ ] Cache directory structure: `<dir>/<sha256>/chunk_N` and `_verified`
- [ ] Unit tests for all operations (create, read, write, verify)
- [ ] Unit tests for edge cases (missing dir, invalid sha256, disk full simulation)
- [ ] `go test ./pkg/cache` passes with >80% coverage

---

## Current State

### What Exists
- Project scaffolding (M1 complete)
- Manifest generator (M2 complete) that produces SHA256 hashes
- Directory structure: `pkg/cache/` exists but is empty
- SHA256 hashes are lowercase hexadecimal strings (64 characters)

### What's Missing
- Cache manager implementation
- Cache directory initialization logic
- Chunk storage and retrieval
- Verification marker system

---

## Constraints & Invariants

### Non-Negotiables
1. **16 MB chunk size** — Chunks must align with `16 * 1024 * 1024` byte boundaries (see [ADR-001](../decisions/ADR-001-chunk-size.md))
2. **SHA256-based paths** — Cache organized as `<dir>/<sha256>/chunk_<index>`
3. **Atomic writes** — Use temp file + rename pattern to prevent partial writes
4. **No eviction** — Cache grows unbounded (see [ADR-004](../decisions/ADR-004-unbounded-cache.md))
5. **Verification marker** — `_verified` file indicates full file has passed SHA256 check

### Directory Structure
```
<cache-dir>/
  abc123.../           # SHA256 hash (64 hex chars)
    chunk_0            # Bytes 0-16MB
    chunk_1            # Bytes 16-32MB
    chunk_2            # Bytes 32-48MB
    _verified          # Empty marker file (created after SHA256 verification)
  def456.../
    chunk_0
    chunk_1
    ...
```

### Error Handling
- Return errors for:
  - Disk full (`ENOSPC`)
  - Permission denied (`EACCES`)
  - Invalid SHA256 (not 64 hex chars)
- Create cache directory if it doesn't exist (`os.MkdirAll`)
- Fail fast on unrecoverable errors

---

## Key Decisions

### ADRs to Respect
- **[ADR-001: 16 MB Chunk Size](../decisions/ADR-001-chunk-size.md)** — Chunk index calculation: `chunkIndex = offset / (16 * 1024 * 1024)`
- **[ADR-003: SHA256 Verification](../decisions/ADR-003-sha256-verification.md)** — Verification happens after full file download, not per-chunk
- **[ADR-004: Unbounded Cache](../decisions/ADR-004-unbounded-cache.md)** — No eviction logic in MVP

### Design Patterns
- **Atomic writes:** Write to temp file (`chunk_N.tmp`), then rename to `chunk_N`
- **Idempotent operations:** `Write()` should overwrite existing chunks safely
- **Simple paths:** `ChunkPath(sha256, idx) = filepath.Join(dir, sha256, fmt.Sprintf("chunk_%d", idx))`

---

## Ordered Reading List

Read these files in order before implementation:

1. **[current.md](../current.md)** — Project status and context (already read if you're here)
2. **[ADR-001: Chunk Size](../decisions/ADR-001-chunk-size.md)** — Understand 16 MB chunk alignment
3. **[ADR-003: SHA256 Verification](../decisions/ADR-003-sha256-verification.md)** — Understand when verification happens
4. **[ADR-004: Unbounded Cache](../decisions/ADR-004-unbounded-cache.md)** — Understand why no eviction
5. **[planning/03-design.md § Cache Manager](../planning/03-design.md#6-cache-manager-pkgcachemanagergo)** — Component design
6. **[planning/04-implementation-plan.md § M3](../planning/04-implementation-plan.md#milestone-3-cache-manager)** — Milestone task breakdown

---

## Open Questions / Risks

### Questions to Resolve During Implementation
1. **Cache permissions** — Should cache dir be 0755 or 0700? (Recommend: 0755 for ease of debugging)
2. **SHA256 validation** — Should `Write()` validate SHA256 format? (Recommend: yes, fail fast on invalid input)
3. **Concurrent access** — M3 doesn't need locking (single-threaded), but M5 (Fetch Manager) will. Add `sync.RWMutex` now or later? (Recommend: later)

### Known Risks
- **Disk full during write** — Must return clear error; document that user must provision sufficient disk
- **Partial write recovery** — If write fails mid-stream, temp file pattern prevents corruption
- **Invalid SHA256 input** — Validate format early to prevent weird directory names

### Mitigations
- Unit test disk full scenario (if possible with temp filesystem limits)
- Test atomic write pattern (create temp, fail before rename)
- Validate SHA256 format: `^[a-f0-9]{64}$`

---

## Implementation Checklist

**Package structure:**
```go
// pkg/cache/manager.go
package cache

type Manager struct {
    dir string
}

func NewManager(dir string) (*Manager, error)
func (m *Manager) Has(sha256 string, chunkIndex int) bool
func (m *Manager) Read(sha256 string, chunkIndex int) ([]byte, error)
func (m *Manager) Write(sha256 string, chunkIndex int, data []byte) error
func (m *Manager) IsVerified(sha256 string) bool
func (m *Manager) MarkVerified(sha256 string) error
func (m *Manager) ChunkPath(sha256 string, chunkIndex int) string
```

**Unit tests:**
```go
// pkg/cache/manager_test.go
func TestNewManager(t *testing.T)
func TestWriteAndRead(t *testing.T)
func TestHas(t *testing.T)
func TestMarkVerified(t *testing.T)
func TestIsVerified(t *testing.T)
func TestInvalidSHA256(t *testing.T)
func TestMissingChunk(t *testing.T)
func TestConcurrentAccess(t *testing.T) // Optional for M3
```

---

## Success Criteria

**This milestone is complete when:**
1. All unit tests pass: `go test ./pkg/cache -v`
2. Can create cache manager with arbitrary directory
3. Can write 16 MB chunk to cache
4. Can read chunk back with identical contents
5. Can check chunk existence with `Has()`
6. Can mark file as verified and check with `IsVerified()`
7. Invalid SHA256 inputs fail gracefully with clear errors
8. Missing chunks return appropriate errors (not panic)

**Integration readiness:**
- M5 (Fetch Manager) can call `cache.Write()` after S3 fetch
- M5 can call `cache.Read()` on cache hit
- M7 (Mount Command) can create cache manager with `--cache-dir` flag

---

## Notes

- This is a **pure local filesystem** milestone — no S3, no FUSE, no networking
- Keep implementation simple — just files and directories
- Focus on correctness over performance (no premature optimization)
- Defer concurrency to M5 if needed
- No need to test multi-TB files; 16 MB chunks are sufficient for unit tests

---

**Ready to implement?** Start with `pkg/cache/manager.go` and follow the implementation plan in the checklist above.
