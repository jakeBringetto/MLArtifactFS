# ADR-005: Read-Only Filesystem

**Status:** Accepted
**Date:** 2026-01-11 (from design doc)
**Context:** Milestone 6 (FUSE Filesystem)

---

## Decision

MLArtifactFS implements a **read-only filesystem**. All write operations return `EROFS` (Read-Only File System error).

---

## Context

ML model artifacts are immutable by design. Once trained and published to S3, models should not be modified in-place. Supporting writes would require:
- S3 upload logic (PutObject)
- Cache invalidation
- Conflict resolution (local vs remote changes)
- Atomic write guarantees

Use case analysis:
- **Model serving:** Read-only access sufficient
- **Experimentation:** Swap models by changing manifest, not modifying files
- **Training outputs:** Written to separate storage, not the model mount

---

## Alternatives Considered

| Approach | Pros | Cons |
|----------|------|------|
| **Read-only** | **Simple, safe, matches use case** | **Cannot modify mounted files** |
| Read-write (local only) | Allows temp modifications | Confusing (changes not persisted to S3) |
| Read-write (sync to S3) | Full filesystem semantics | Complex, slow, out of scope |

---

## Rationale

1. **Immutable Artifacts:** ML models are published as immutable versions (v1, v2, etc.)
2. **No Write Use Case:** Model serving and inference only read model files
3. **Simplicity:** Read-only FUSE implementation is significantly simpler
4. **Safety:** Prevents accidental model corruption
5. **Performance:** No upload bandwidth required

### Use Case Analysis

**Supported (read-only):**
- ✅ Load model for inference
- ✅ Read config.json, tokenizer.json
- ✅ Copy files out of mount (`cp /mnt/model/file.txt ~/file.txt`)
- ✅ Swap models (unmount, mount new manifest)

**Not supported (would require write):**
- ❌ Modify model weights in-place
- ❌ Write training checkpoints to mount
- ❌ Create new files in mount

**Workaround for writes:**
- Write outputs to separate directory: `--output-dir /tmp/outputs`
- Publish new model versions to S3 separately

---

## Consequences

### Positive
- Simple FUSE implementation (no write paths)
- No S3 upload logic needed
- No cache invalidation complexity
- No conflict resolution (local vs remote)
- Matches immutable artifact pattern
- Prevents accidental corruption

### Negative
- Cannot modify files in-place
- Cannot write temp files to mount
- Users must understand read-only nature

### Mitigations
- **Clear error messages:** `EROFS` with helpful message
- **Documentation:** Explain read-only nature in README
- **Mount options:** Show mount as `ro` (read-only) in `/proc/mounts`

---

## Implementation Notes

**FUSE operations:**
```go
// Allowed (read operations)
func (fs *FS) Lookup(name string) (Node, error)   // ✅ Find file
func (fs *FS) Getattr() (Attr, error)             // ✅ Get file size/mode
func (fs *FS) Open() (FileHandle, error)          // ✅ Open for reading
func (fs *FS) Read(offset, size) ([]byte, error)  // ✅ Read file contents
func (fs *FS) Readdir() ([]DirEntry, error)       // ✅ List directory

// Denied (write operations)
func (fs *FS) Create(name string) error            // ❌ Return EROFS
func (fs *FS) Write(data []byte) error             // ❌ Return EROFS
func (fs *FS) Mkdir(name string) error             // ❌ Return EROFS
func (fs *FS) Unlink(name string) error            // ❌ Return EROFS
func (fs *FS) Rename(old, new string) error        // ❌ Return EROFS
func (fs *FS) Chmod(mode os.FileMode) error        // ❌ Return EROFS
```

**Error handling:**
```go
func (fs *FS) Create(name string) error {
    return syscall.EROFS // Read-only file system
}

func (fs *FS) Write(data []byte) error {
    return syscall.EROFS
}
```

**User experience:**
```bash
$ echo "test" > /mnt/model/newfile.txt
bash: /mnt/model/newfile.txt: Read-only file system

$ rm /mnt/model/config.json
rm: cannot remove '/mnt/model/config.json': Read-only file system

$ cat /mnt/model/config.json
{ ... }  # ✅ Reads work fine
```

---

## File Attributes

Files mounted with read-only mode:
```go
func (fs *FS) Getattr() (Attr, error) {
    return Attr{
        Mode: 0444,        // r--r--r-- (read-only)
        Size: file.Size,
        Mtime: time.Unix(0, 0), // Fixed timestamp
    }, nil
}
```

**Directory attributes:**
```go
func (fs *FS) Getattr() (Attr, error) {
    return Attr{
        Mode: 0555 | os.ModeDir,  // dr-xr-xr-x (executable for traversal)
    }, nil
}
```

---

## Mount Output

**Filesystem shows as read-only:**
```bash
$ mount | grep mlfs
mlfs on /mnt/model type fuse.mlfs (ro,nosuid,nodev,relatime,user_id=1000,group_id=1000)
                                   ^^ read-only flag
```

---

## Related

- **Milestone 6:** FUSE Filesystem implementation
- **Design pattern:** Immutable infrastructure, content-addressable storage

---

## References

- Design doc: [planning/03-design.md](../mlartifactfs-planning/03-design.md#decision-6-read-only-filesystem)
- FUSE documentation: Read-only filesystem best practices
