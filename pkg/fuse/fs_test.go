package fuse

import (
	"context"
	"errors"
	"syscall"
	"testing"

	gofuse "github.com/hanwen/go-fuse/v2/fuse"

	"github.com/jakeBringetto/MLArtifactFS/pkg/manifest"
)

// mockFetcher implements the fetcher interface for tests.
type mockFetcher struct {
	data map[string][]byte // keyed by URL
	err  error
}

func (m *mockFetcher) Read(_ context.Context, url, _ string, offset, size, _ int64) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	d := m.data[url]
	if offset >= int64(len(d)) {
		return []byte{}, nil
	}
	end := offset + size
	if end > int64(len(d)) {
		end = int64(len(d))
	}
	return d[offset:end], nil
}

// --- buildTree ---

func TestBuildTree_FlatFiles(t *testing.T) {
	files := []manifest.File{
		{Path: "config.json", Size: 100},
		{Path: "tokenizer.json", Size: 200},
	}
	root := buildTree(files)

	if len(root.children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(root.children))
	}
	for _, name := range []string{"config.json", "tokenizer.json"} {
		node, ok := root.children[name]
		if !ok {
			t.Errorf("missing child %q", name)
			continue
		}
		if node.file == nil {
			t.Errorf("child %q should be a file node", name)
		}
		if node.children != nil {
			t.Errorf("child %q should not have children", name)
		}
	}
}

func TestBuildTree_NestedFiles(t *testing.T) {
	files := []manifest.File{
		{Path: "weights/pytorch_model.bin", Size: 1000},
		{Path: "weights/vocab.txt", Size: 50},
		{Path: "config.json", Size: 100},
	}
	root := buildTree(files)

	// Root should have "weights" dir and "config.json" file.
	if len(root.children) != 2 {
		t.Fatalf("expected 2 root children, got %d", len(root.children))
	}

	weightsNode, ok := root.children["weights"]
	if !ok {
		t.Fatal("missing 'weights' directory node")
	}
	if weightsNode.file != nil {
		t.Error("'weights' should be a directory node, not a file node")
	}
	if len(weightsNode.children) != 2 {
		t.Fatalf("expected 2 children under 'weights', got %d", len(weightsNode.children))
	}

	binNode := weightsNode.children["pytorch_model.bin"]
	if binNode == nil || binNode.file == nil {
		t.Error("expected file node for 'pytorch_model.bin'")
	}
	if binNode.file.Size != 1000 {
		t.Errorf("expected size 1000, got %d", binNode.file.Size)
	}
}

func TestBuildTree_DeeplyNested(t *testing.T) {
	files := []manifest.File{
		{Path: "a/b/c/deep.bin", Size: 42},
	}
	root := buildTree(files)

	cur := root
	for _, part := range []string{"a", "b", "c"} {
		node := cur.children[part]
		if node == nil {
			t.Fatalf("missing intermediate directory %q", part)
		}
		if node.file != nil {
			t.Errorf("intermediate node %q should be a directory", part)
		}
		cur = node
	}

	leaf := cur.children["deep.bin"]
	if leaf == nil || leaf.file == nil {
		t.Error("expected file leaf at 'deep.bin'")
	}
}

func TestBuildTree_FilepathsPreservedOnLeaf(t *testing.T) {
	files := []manifest.File{
		{Path: "model.bin", URL: "s3://bucket/model.bin", SHA256: "abc123", Size: 999},
	}
	root := buildTree(files)

	node := root.children["model.bin"]
	if node == nil || node.file == nil {
		t.Fatal("expected file node")
	}
	if node.file.URL != "s3://bucket/model.bin" {
		t.Errorf("unexpected URL: %s", node.file.URL)
	}
	if node.file.SHA256 != "abc123" {
		t.Errorf("unexpected SHA256: %s", node.file.SHA256)
	}
}

// --- lookupChild ---

func TestLookupChild_Found(t *testing.T) {
	files := []manifest.File{
		{Path: "weights/model.bin", Size: 500},
		{Path: "config.json", Size: 100},
	}
	root := buildTree(files)
	d := &dirNode{tree: root}

	if child := d.lookupChild("config.json"); child == nil {
		t.Error("expected to find config.json")
	}
	if child := d.lookupChild("weights"); child == nil {
		t.Error("expected to find weights directory")
	}
}

func TestLookupChild_NotFound(t *testing.T) {
	root := buildTree([]manifest.File{{Path: "config.json", Size: 1}})
	d := &dirNode{tree: root}

	if child := d.lookupChild("missing.bin"); child != nil {
		t.Error("expected nil for missing child")
	}
}

func TestLookupChild_FileVsDir(t *testing.T) {
	files := []manifest.File{
		{Path: "dir/file.txt", Size: 10},
		{Path: "flat.txt", Size: 5},
	}
	root := buildTree(files)
	d := &dirNode{tree: root}

	dirChild := d.lookupChild("dir")
	if dirChild == nil {
		t.Fatal("expected dir child")
	}
	if dirChild.file != nil {
		t.Error("dir child should not have a file pointer")
	}
	if dirChild.children == nil {
		t.Error("dir child should have children map")
	}

	fileChild := d.lookupChild("flat.txt")
	if fileChild == nil {
		t.Fatal("expected file child")
	}
	if fileChild.file == nil {
		t.Error("file child should have a file pointer")
	}
}

// --- dirNode.Getattr ---

func TestDirNode_Getattr(t *testing.T) {
	d := &dirNode{tree: &treeNode{children: map[string]*treeNode{}}}
	var out gofuse.AttrOut
	errno := d.Getattr(context.Background(), nil, &out)

	if errno != 0 {
		t.Errorf("expected errno 0, got %v", errno)
	}
	if out.Mode&syscall.S_IFDIR == 0 {
		t.Error("expected S_IFDIR bit set")
	}
	if out.Mode&0555 != 0555 {
		t.Errorf("expected mode 0555, got %04o", out.Mode&0777)
	}
}

// --- fileNode.Getattr ---

func TestFileNode_Getattr(t *testing.T) {
	f := &fileNode{
		file: &manifest.File{Path: "model.bin", Size: 4096},
	}
	var out gofuse.AttrOut
	errno := f.Getattr(context.Background(), nil, &out)

	if errno != 0 {
		t.Errorf("expected errno 0, got %v", errno)
	}
	if out.Mode&syscall.S_IFREG == 0 {
		t.Error("expected S_IFREG bit set")
	}
	if out.Mode&0444 != 0444 {
		t.Errorf("expected mode 0444, got %04o", out.Mode&0777)
	}
	if out.Size != 4096 {
		t.Errorf("expected size 4096, got %d", out.Size)
	}
}

// --- dirNode.Readdir ---

func TestDirNode_Readdir_Empty(t *testing.T) {
	d := &dirNode{tree: &treeNode{children: map[string]*treeNode{}}}
	stream, errno := d.Readdir(context.Background())

	if errno != 0 {
		t.Fatalf("expected errno 0, got %v", errno)
	}
	if stream.HasNext() {
		t.Error("expected empty dir stream")
	}
}

func TestDirNode_Readdir_MixedChildren(t *testing.T) {
	files := []manifest.File{
		{Path: "subdir/nested.bin", Size: 1},
		{Path: "config.json", Size: 2},
		{Path: "weights.bin", Size: 3},
	}
	root := buildTree(files)
	d := &dirNode{tree: root}

	stream, errno := d.Readdir(context.Background())
	if errno != 0 {
		t.Fatalf("unexpected errno: %v", errno)
	}

	got := map[string]uint32{}
	for stream.HasNext() {
		entry, errno := stream.Next()
		if errno != 0 {
			t.Fatalf("stream.Next errno: %v", errno)
		}
		got[entry.Name] = entry.Mode
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}

	if got["subdir"]&syscall.S_IFDIR == 0 {
		t.Error("'subdir' should have S_IFDIR mode")
	}
	if got["config.json"]&syscall.S_IFREG == 0 {
		t.Error("'config.json' should have S_IFREG mode")
	}
	if got["weights.bin"]&syscall.S_IFREG == 0 {
		t.Error("'weights.bin' should have S_IFREG mode")
	}
}

// --- fileNode.Open ---

func TestFileNode_Open(t *testing.T) {
	f := &fileNode{file: &manifest.File{Path: "x.bin", Size: 1}}
	fh, flags, errno := f.Open(context.Background(), 0)

	if errno != 0 {
		t.Errorf("expected errno 0, got %v", errno)
	}
	if fh != nil {
		t.Error("expected nil file handle")
	}
	if flags&gofuse.FOPEN_KEEP_CACHE == 0 {
		t.Error("expected FOPEN_KEEP_CACHE flag")
	}
}

// --- fileNode.Read ---

func TestFileNode_Read_Success(t *testing.T) {
	content := []byte("hello, model world!")
	mock := &mockFetcher{data: map[string][]byte{"s3://bucket/model.bin": content}}
	f := &fileNode{
		file:     &manifest.File{URL: "s3://bucket/model.bin", SHA256: "deadbeef", Size: int64(len(content))},
		fetchMgr: mock,
	}

	dest := make([]byte, len(content))
	result, errno := f.Read(context.Background(), dest, 0)

	if errno != 0 {
		t.Fatalf("expected errno 0, got %v", errno)
	}
	buf := make([]byte, len(content))
	n, status := result.Bytes(buf)
	if status != 0 {
		t.Fatalf("expected status OK, got %v", status)
	}
	if string(n) != string(content) {
		t.Errorf("expected %q, got %q", content, n)
	}
}

func TestFileNode_Read_Offset(t *testing.T) {
	content := []byte("abcdefghij")
	mock := &mockFetcher{data: map[string][]byte{"s3://bucket/f.bin": content}}
	f := &fileNode{
		file:     &manifest.File{URL: "s3://bucket/f.bin", SHA256: "x", Size: int64(len(content))},
		fetchMgr: mock,
	}

	dest := make([]byte, 4)
	result, errno := f.Read(context.Background(), dest, 3)

	if errno != 0 {
		t.Fatalf("expected errno 0, got %v", errno)
	}
	buf := make([]byte, 4)
	n, _ := result.Bytes(buf)
	if string(n) != "defg" {
		t.Errorf("expected 'defg', got %q", n)
	}
}

func TestFileNode_Read_FetchError(t *testing.T) {
	mock := &mockFetcher{err: errors.New("S3 unavailable")}
	f := &fileNode{
		file:     &manifest.File{URL: "s3://bucket/f.bin", SHA256: "x", Size: 100},
		fetchMgr: mock,
	}

	_, errno := f.Read(context.Background(), make([]byte, 10), 0)
	if errno != syscall.EIO {
		t.Errorf("expected EIO, got %v", errno)
	}
}

// --- write operations return EROFS ---

func TestDirNode_WriteOps_EROFS(t *testing.T) {
	d := &dirNode{tree: &treeNode{children: map[string]*treeNode{}}}

	if _, _, _, errno := d.Create(context.Background(), "f", 0, 0, nil); errno != syscall.EROFS {
		t.Errorf("Create: expected EROFS, got %v", errno)
	}
	if _, errno := d.Mkdir(context.Background(), "d", 0, nil); errno != syscall.EROFS {
		t.Errorf("Mkdir: expected EROFS, got %v", errno)
	}
	if errno := d.Unlink(context.Background(), "f"); errno != syscall.EROFS {
		t.Errorf("Unlink: expected EROFS, got %v", errno)
	}
	if errno := d.Rmdir(context.Background(), "d"); errno != syscall.EROFS {
		t.Errorf("Rmdir: expected EROFS, got %v", errno)
	}
	if errno := d.Rename(context.Background(), "a", nil, "b", 0); errno != syscall.EROFS {
		t.Errorf("Rename: expected EROFS, got %v", errno)
	}
}

func TestFileNode_WriteOps_EROFS(t *testing.T) {
	f := &fileNode{file: &manifest.File{Path: "x.bin", Size: 1}}

	if _, errno := f.Write(context.Background(), nil, []byte("data"), 0); errno != syscall.EROFS {
		t.Errorf("Write: expected EROFS, got %v", errno)
	}
	if errno := f.Setattr(context.Background(), nil, nil, nil); errno != syscall.EROFS {
		t.Errorf("Setattr: expected EROFS, got %v", errno)
	}
}
