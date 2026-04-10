package fuse

import (
	"context"
	"math"
	"strings"
	"syscall"

	gofs "github.com/hanwen/go-fuse/v2/fs"
	gofuse "github.com/hanwen/go-fuse/v2/fuse"

	"github.com/jakeBringetto/MLArtifactFS/pkg/fetch"
	"github.com/jakeBringetto/MLArtifactFS/pkg/manifest"
)

// fetcher is satisfied by *fetch.Manager.
type fetcher interface {
	Read(ctx context.Context, url, fileSHA256 string, offset, size, fileSize int64) ([]byte, error)
}

// treeNode is an internal trie node; file != nil means leaf, children != nil means directory.
type treeNode struct {
	file     *manifest.File
	children map[string]*treeNode
}

// buildTree builds an in-memory path trie from the manifest file list.
func buildTree(files []manifest.File) *treeNode {
	root := &treeNode{children: make(map[string]*treeNode)}
	for i := range files {
		parts := strings.Split(files[i].Path, "/")
		cur := root
		for _, part := range parts[:len(parts)-1] {
			if cur.children[part] == nil {
				cur.children[part] = &treeNode{children: make(map[string]*treeNode)}
			}
			cur = cur.children[part]
		}
		name := parts[len(parts)-1]
		fileCopy := files[i]
		cur.children[name] = &treeNode{file: &fileCopy}
	}
	return root
}

// dirNode is a FUSE directory node.
type dirNode struct {
	gofs.Inode
	tree     *treeNode
	fetchMgr fetcher
}

// fileNode is a FUSE regular-file node.
type fileNode struct {
	gofs.Inode
	file     *manifest.File
	fetchMgr fetcher
}

// FS is the top-level filesystem object.
type FS struct {
	root *dirNode
}

// NewFS builds the directory tree from the manifest and returns a mount-ready FS.
func NewFS(m *manifest.Manifest, fetchMgr *fetch.Manager) *FS {
	tree := buildTree(m.Files)
	root := &dirNode{tree: tree, fetchMgr: fetchMgr}
	return &FS{root: root}
}

// Mount wires up the go-fuse server. The caller calls server.Wait() when done.
func Mount(mountPoint string, fsys *FS) (*gofuse.Server, error) {
	opts := &gofs.Options{}
	opts.MountOptions.Options = append(opts.MountOptions.Options, "ro")
	return gofs.Mount(mountPoint, fsys.root, opts)
}

// lookupChild returns the named child treeNode, or nil. Separated from Lookup
// to allow testing tree resolution without a live inode context.
func (d *dirNode) lookupChild(name string) *treeNode {
	return d.tree.children[name]
}

func (d *dirNode) Lookup(ctx context.Context, name string, out *gofuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	child := d.lookupChild(name)
	if child == nil {
		return nil, syscall.ENOENT
	}
	out.EntryValid = math.MaxUint32
	out.AttrValid = math.MaxUint32

	if child.file != nil {
		out.Attr.Mode = syscall.S_IFREG | 0444
		out.Attr.Size = uint64(child.file.Size)
		node := &fileNode{file: child.file, fetchMgr: d.fetchMgr}
		return d.NewInode(ctx, node, gofs.StableAttr{Mode: syscall.S_IFREG}), gofs.OK
	}

	out.Attr.Mode = syscall.S_IFDIR | 0555
	node := &dirNode{tree: child, fetchMgr: d.fetchMgr}
	return d.NewInode(ctx, node, gofs.StableAttr{Mode: syscall.S_IFDIR}), gofs.OK
}

func (d *dirNode) Getattr(_ context.Context, _ gofs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFDIR | 0555
	return gofs.OK
}

func (d *dirNode) Readdir(_ context.Context) (gofs.DirStream, syscall.Errno) {
	entries := make([]gofuse.DirEntry, 0, len(d.tree.children))
	for name, child := range d.tree.children {
		entry := gofuse.DirEntry{Name: name}
		if child.file != nil {
			entry.Mode = syscall.S_IFREG
		} else {
			entry.Mode = syscall.S_IFDIR
		}
		entries = append(entries, entry)
	}
	return gofs.NewListDirStream(entries), gofs.OK
}

func (d *dirNode) Create(_ context.Context, _ string, _ uint32, _ uint32, _ *gofuse.EntryOut) (*gofs.Inode, gofs.FileHandle, uint32, syscall.Errno) {
	return nil, nil, 0, syscall.EROFS
}

func (d *dirNode) Mkdir(_ context.Context, _ string, _ uint32, _ *gofuse.EntryOut) (*gofs.Inode, syscall.Errno) {
	return nil, syscall.EROFS
}

func (d *dirNode) Unlink(_ context.Context, _ string) syscall.Errno {
	return syscall.EROFS
}

func (d *dirNode) Rmdir(_ context.Context, _ string) syscall.Errno {
	return syscall.EROFS
}

func (d *dirNode) Rename(_ context.Context, _ string, _ gofs.InodeEmbedder, _ string, _ uint32) syscall.Errno {
	return syscall.EROFS
}

func (f *fileNode) Getattr(_ context.Context, _ gofs.FileHandle, out *gofuse.AttrOut) syscall.Errno {
	out.Mode = syscall.S_IFREG | 0444
	out.Size = uint64(f.file.Size)
	return gofs.OK
}

func (f *fileNode) Open(_ context.Context, _ uint32) (gofs.FileHandle, uint32, syscall.Errno) {
	return nil, gofuse.FOPEN_KEEP_CACHE, gofs.OK
}

func (f *fileNode) Read(ctx context.Context, dest []byte, off int64) (gofuse.ReadResult, syscall.Errno) {
	data, err := f.fetchMgr.Read(ctx, f.file.URL, f.file.SHA256, off, int64(len(dest)), f.file.Size)
	if err != nil {
		return nil, syscall.EIO
	}
	return gofuse.ReadResultData(data), gofs.OK
}

func (f *fileNode) Write(_ context.Context, _ gofs.FileHandle, _ []byte, _ int64) (uint32, syscall.Errno) {
	return 0, syscall.EROFS
}

func (f *fileNode) Setattr(_ context.Context, _ gofs.FileHandle, _ *gofuse.SetAttrIn, _ *gofuse.AttrOut) syscall.Errno {
	return syscall.EROFS
}
