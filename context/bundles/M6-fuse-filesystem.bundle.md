# M6: FUSE Filesystem — Context Bundle

**Target:** Implement read-only FUSE filesystem using hanwen/go-fuse v2 (`fs` package)
**Estimated Reading Time:** 4 minutes

---

## Objective

Build `pkg/fuse/fs.go` that presents a manifest's file tree as a read-only virtual filesystem. FUSE operations (`Lookup`, `Getattr`, `Open`, `Read`, `Readdir`) are backed by the Fetch Manager (M5) and Manifest (M2). No S3 or cache knowledge lives here.

---

## Deliverable Definition

### Acceptance Criteria

- [ ] `fuse.FS` struct (or root node) with constructor `NewFS(manifest *manifest.Manifest, fetchManager *fetch.Manager) *FS`
- [ ] In-memory directory tree built from `manifest.Files[].Path` at construction time
- [ ] `Lookup` — resolve a name within a directory; return inode for file or directory node
- [ ] `Getattr` — return file attributes (size, mode, mtime) sourced from manifest; no S3 call
- [ ] `Open` — return a file handle; no side effects required for read-only MVP
- [ ] `Read` — delegate to `fetchManager.Read(ctx, file.URL, file.SHA256, offset, size, file.Size)`
- [ ] `Readdir` — list directory entries from in-memory tree
- [ ] Write operations (`Create`, `Mkdir`, `Unlink`, etc.) return `syscall.EROFS`
- [ ] `Mount(root fs.InodeEmbedder, mountPoint string) (*fuse.Server, error)` — wire up go-fuse server
- [ ] Unit tests: directory tree construction, Lookup, Getattr, Readdir (mock fetch manager)
- [ ] `go test ./pkg/fuse` passes

---

## Current State

### What Exists
- [pkg/fetch/manager.go](../pkg/fetch/manager.go) — `Read(ctx, url, sha256, offset, size, fileSize)`, `Prefetch(...)` (M5 complete)
- [pkg/manifest/manifest.go](../pkg/manifest/manifest.go) — `Manifest`, `File` structs, `Load()` (M2 complete)
- `pkg/fuse/` — directory exists, **empty**

### What's Missing
- `pkg/fuse/fs.go` — FUSE filesystem implementation

### Interfaces to Depend On

**Fetch Manager:**
```go
func (m *fetch.Manager) Read(ctx context.Context, url, fileSHA256 string, offset, size, fileSize int64) ([]byte, error)
```

**Manifest types:**
```go
type Manifest struct {
    ArtifactID string
    Version    string
    MountPath  string
    Prefetch   []string
    Files      []File
}

type File struct {
    Path   string
    URL    string
    Size   int64
    SHA256 string
}
```

---

## go-fuse v2 API (`fs` package)

The dependency is `github.com/hanwen/go-fuse/v2 v2.9.0`. Use the `fs` package (not deprecated `nodefs`).

### Node pattern

Every node (file or directory) embeds `fs.Inode`:

```go
import (
    "github.com/hanwen/go-fuse/v2/fs"
    "github.com/hanwen/go-fuse/v2/fuse"
)

type dirNode struct {
    fs.Inode
    children map[string]*manifest.File  // nil entry = subdirectory
}

type fileNode struct {
    fs.Inode
    file    *manifest.File
    fetchMgr *fetch.Manager
}
```

### Interfaces to implement

| Interface | Method signature | Node type |
|---|---|---|
| `fs.NodeLookuper` | `Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno)` | dirNode |
| `fs.NodeGetattrer` | `Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno` | both |
| `fs.NodeReaddirer` | `Readdir(ctx context.Context) (fs.DirStream, syscall.Errno)` | dirNode |
| `fs.NodeOpener` | `Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno)` | fileNode |
| `fs.FileReader` | `Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno)` | fileNode or fileHandle |

### Mounting

```go
// Mount wires up the FUSE server and returns it (caller calls server.Wait()).
func Mount(root fs.InodeEmbedder, mountPoint string) (*fuse.Server, error) {
    opts := &fs.Options{}
    opts.MountOptions.Options = append(opts.MountOptions.Options, "ro")
    return fs.Mount(mountPoint, root, opts)
}
```

### Errno conventions

- Success: return `0` (or `fs.OK`)
- Not found: `syscall.ENOENT`
- Read-only: `syscall.EROFS`
- I/O error (S3 failure): `syscall.EIO`

### Read result

```go
func (f *fileNode) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
    size := int64(len(dest))
    data, err := f.fetchMgr.Read(ctx, f.file.URL, f.file.SHA256, off, size, f.file.Size)
    if err != nil {
        return nil, syscall.EIO
    }
    return fuse.ReadResultData(data), fs.OK
}
```

---

## Directory Tree Construction

Build the tree at `NewFS` time from `manifest.Files[].Path` (e.g., `"weights/pytorch_model.bin"`).

**Approach:** map of maps (trie over path components):

```go
type treeNode struct {
    file     *manifest.File  // non-nil for files, nil for directories
    children map[string]*treeNode
}
```

**Algorithm:**
```
for each file in manifest.Files:
    parts = strings.Split(file.Path, "/")
    walk/create treeNodes for parts[0..n-2] (directories)
    set leaf treeNode.file = &file
```

**In go-fuse**, use `NewPersistentInode` to register child inodes during `Lookup` or pre-populate them in the root constructor:

```go
// In Lookup:
child := &fileNode{file: entry}
return parent.NewInode(ctx, child, fs.StableAttr{Mode: syscall.S_IFREG}), fs.OK
```

For directories:
```go
return parent.NewInode(ctx, &dirNode{...}, fs.StableAttr{Mode: syscall.S_IFDIR}), fs.OK
```

---

## Constraints & Invariants

1. **Read-only** — return `EROFS` for any mutating operation (ADR-005)
2. **No S3 in Getattr/Lookup** — all metadata from manifest; these must be fast and non-blocking
3. **fileNode.Read size** — `dest` slice length is the requested size; clamp if near EOF
4. **Inode stability** — go-fuse v2 `fs` package manages inode numbering; use `StableAttr` with mode only (no custom ino required for MVP)
5. **Mount options** — pass `"ro"` mount option to enforce read-only at kernel level
6. **File mode** — regular files: `0444` (read-only, world-readable); directories: `0555`
7. **mtime/ctime** — manifest has no timestamps; return zero value (`time.Time{}`) — acceptable for MVP

---

## Key Decisions

- **[ADR-005: Read-Only Filesystem](../decisions/ADR-005-read-only-filesystem.md)** — EROFS for writes
- **[ADR-002: Blocking Prefetch](../decisions/ADR-002-blocking-prefetch.md)** — prefetch happens in M7 (CLI), not here; FUSE just reads
- **Design: go-fuse `fs` package over `nodefs`** — `fs` package is the current API; `nodefs` is deprecated

---

## Ordered Reading List

1. **[current.md](../current.md)** — Project status
2. **[milestones/M5-fetch-manager.md](../milestones/M5-fetch-manager.md)** — Fetch API (note: Read takes `fileSize`)
3. **[milestones/M2-manifest-generator.md](../milestones/M2-manifest-generator.md)** — Manifest/File struct fields
4. **[ADR-005: Read-Only](../decisions/ADR-005-read-only-filesystem.md)** — EROFS enforcement
5. **[planning/03-design.md § FUSE Filesystem](../planning/03-design.md)** — FUSE operations overview

---

## Open Questions / Risks

| Question | Recommendation |
|---|---|
| go-fuse `Lookup` caching: does go-fuse cache inode lookups? | Yes — `EntryOut.EntryTimeout` controls TTL; set to `math.MaxInt64` (immutable manifest) |
| `Read` dest slice vs. requested size: what if S3 returns fewer bytes? | Return what was received; go-fuse handles short reads |
| FUSE deadlock: goroutine in kernel callback calling back into FUSE | Avoid recursive FUSE calls; `fetchManager.Read` only touches disk/S3, never FUSE |
| Testing without `/dev/fuse`: how to unit-test FUSE nodes? | Test the node struct methods directly (no mount needed); mock fetch manager |

---

## Implementation Sketch

```go
package fuse

import (
    "context"
    "strings"
    "syscall"

    gofuse "github.com/hanwen/go-fuse/v2/fs"
    "github.com/hanwen/go-fuse/v2/fuse"

    "github.com/jakeBringetto/MLArtifactFS/pkg/fetch"
    "github.com/jakeBringetto/MLArtifactFS/pkg/manifest"
)

// FS holds the root inode and is the entry point for mounting.
type FS struct {
    root     *dirNode
    fetchMgr *fetch.Manager
}

func NewFS(m *manifest.Manifest, fetchMgr *fetch.Manager) *FS {
    root := buildTree(m.Files)
    return &FS{root: root, fetchMgr: fetchMgr}
}

func Mount(mountPoint string, fsys *FS) (*fuse.Server, error) {
    opts := &gofuse.Options{}
    opts.MountOptions.Options = append(opts.MountOptions.Options, "ro")
    return gofuse.Mount(mountPoint, fsys.root, opts)
}
```

---

## Success Criteria

1. `go test ./pkg/fuse -v` passes
2. `Lookup` returns correct inode for files and directories present in manifest
3. `Lookup` returns `ENOENT` for unknown names
4. `Getattr` returns correct size (from manifest) without S3 call
5. `Readdir` lists all direct children of a directory
6. `Read` returns correct bytes by delegating to fetch manager
7. Write operations return `EROFS`
8. M7 (CLI Mount) can call `Mount(mountPoint, NewFS(manifest, fetchManager))`
