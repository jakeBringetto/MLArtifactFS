# Testing Strategy for MLArtifactFS

## Overview

MLArtifactFS requires multiple layers of testing due to its integration with S3, FUSE, and Docker. This document outlines the testing approach for the POC.

---

## Testing Pyramid

```
                    E2E Tests (Docker + Real S3)
                   /                              \
              Integration Tests (Components)
             /                                      \
        Unit Tests (Individual Functions)
```

**Strategy:**
- **Many unit tests** (fast, isolated, no external deps)
- **Some integration tests** (components working together, mock S3)
- **Few E2E tests** (full system, real S3, Docker)

---

## Layer 1: Unit Tests

### **What to Test**

**Milestone 2: Manifest Generator**
- ✅ Compute SHA256 of test files
- ✅ Generate manifest JSON from directory structure
- ✅ Handle nested directories
- ✅ Construct correct URLs from prefix + path
- ✅ Parse prefetch flags
- ✅ Marshal/unmarshal manifest JSON

**Milestone 3: Cache Manager**
- ✅ Create cache directory structure
- ✅ Compute chunk paths correctly
- ✅ Write/read chunks from disk
- ✅ Verify marker file logic
- ✅ Handle missing chunks (return error)

**Milestone 5: Fetch Manager (with mocks)**
- ✅ Calculate correct chunk boundaries (16 MB alignment)
- ✅ Return correct byte ranges from chunks
- ✅ Handle reads spanning multiple chunks
- ✅ Prefetch logic (sequential chunk downloads)
- ✅ SHA256 verification after prefetch

**Milestone 6: FUSE Filesystem (with mocks)**
- ✅ Build directory tree from manifest
- ✅ Lookup files/directories by path
- ✅ Return correct file attributes (size, mode)
- ✅ Handle nested directory traversal

### **How to Run**

```bash
# Run all unit tests
go test ./pkg/...

# Run with coverage
go test -cover ./pkg/...

# Run specific package
go test ./pkg/manifest -v
```

### **Example Unit Test**

```go
// pkg/fetch/manager_test.go
func TestChunkAlignment(t *testing.T) {
    chunkSize := 16 * 1024 * 1024 // 16 MB

    tests := []struct {
        offset    int64
        expected  int64
    }{
        {0, 0},                    // First byte
        {1024, 0},                 // Within first chunk
        {16*1024*1024, 16*1024*1024}, // Exactly second chunk
        {17*1024*1024, 16*1024*1024}, // Within second chunk
    }

    for _, tt := range tests {
        chunkIndex := tt.offset / int64(chunkSize)
        chunkStart := chunkIndex * int64(chunkSize)

        if chunkStart != tt.expected {
            t.Errorf("offset %d: got %d, want %d", tt.offset, chunkStart, tt.expected)
        }
    }
}
```

---

## Layer 2: Integration Tests

### **What to Test**

**S3 Client + Fetch Manager (with MinIO)**
- ✅ Download file chunks from S3
- ✅ Handle S3 errors (404, 403, 503)
- ✅ Retry logic on transient failures
- ✅ Range request correctness

**Fetch Manager + Cache Manager**
- ✅ Fetch chunk from S3, write to cache
- ✅ Second read hits cache (no S3 call)
- ✅ Prefetch writes all chunks
- ✅ SHA256 verification on prefetch

**FUSE + Fetch Manager (in-memory mount)**
- ✅ Read small file (single chunk)
- ✅ Read large file (multiple chunks)
- ✅ Read with offset (partial chunk)
- ✅ List directory contents
- ✅ Stat file attributes

### **How to Run**

**Option A: Use MinIO (Local S3-compatible server)**

```bash
# Start MinIO in Docker
docker run -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=minioadmin \
  minio/minio server /data --console-address ":9001"

# Run integration tests against MinIO
S3_ENDPOINT=http://localhost:9000 go test ./tests/integration -v
```

**Option B: Use AWS S3 test bucket**

```bash
# Create test bucket
aws s3 mb s3://mlartifactfs-test

# Run tests
S3_BUCKET=mlartifactfs-test go test ./tests/integration -v

# Cleanup
aws s3 rb s3://mlartifactfs-test --force
```

### **Example Integration Test**

```go
// tests/integration/fetch_test.go
func TestFetchAndCache(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Setup: Upload test file to MinIO
    s3Client := setupMinIO(t)
    testFile := uploadTestFile(t, s3Client, "test.bin", 32*1024*1024) // 32 MB

    // Create fetch manager
    cacheDir := t.TempDir()
    cacheManager := cache.NewManager(cacheDir)
    fetchManager := fetch.NewManager(s3Client, cacheManager, 16*1024*1024)

    // Test: Fetch first chunk (cold cache)
    data, err := fetchManager.Read(context.Background(),
        testFile.URL, testFile.SHA256, 0, 1024)
    require.NoError(t, err)
    require.Len(t, data, 1024)

    // Test: Fetch same chunk again (warm cache)
    // Should NOT call S3
    data2, err := fetchManager.Read(context.Background(),
        testFile.URL, testFile.SHA256, 0, 1024)
    require.NoError(t, err)
    require.Equal(t, data, data2)

    // Verify cache hit (check S3 call count)
    assert.Equal(t, 1, s3Client.CallCount())
}
```

---

## Layer 3: End-to-End Tests

### **What to Test**

**Full Workflow (Real S3 + Docker)**
- ✅ Generate manifest from test model
- ✅ Upload test model to S3
- ✅ Mount filesystem in Docker container
- ✅ Read files from mount point
- ✅ Verify prefetch files downloaded immediately
- ✅ Verify lazy files downloaded on access
- ✅ Unmount cleanly

### **Test Environment Setup**

**Prerequisites:**
- AWS account with S3 bucket (or MinIO)
- Docker with `--cap-add SYS_ADMIN --device /dev/fuse`
- Small test model (e.g., DistilBERT ~250 MB)

**Test Infrastructure:**
```bash
tests/e2e/
  setup.sh           # Create S3 bucket, upload test model
  test-mount.sh      # Test mounting in Docker
  test-read.sh       # Test reading files
  cleanup.sh         # Delete S3 bucket, remove containers
  fixtures/
    tiny-model/      # Minimal test model (3 files, ~10 MB)
      config.json
      tokenizer.json
      model.safetensors
```

### **E2E Test Script**

```bash
#!/bin/bash
# tests/e2e/test-full-workflow.sh

set -e

echo "=== E2E Test: Full MLArtifactFS Workflow ==="

# 1. Setup
echo "[1/6] Setting up test environment..."
TEST_BUCKET="mlartifactfs-e2e-test-$(date +%s)"
TEST_MODEL="tests/e2e/fixtures/tiny-model"

aws s3 mb s3://$TEST_BUCKET
trap "aws s3 rb s3://$TEST_BUCKET --force" EXIT

# 2. Upload test model
echo "[2/6] Uploading test model to S3..."
aws s3 sync $TEST_MODEL s3://$TEST_BUCKET/tiny-model/v1/

# 3. Generate manifest
echo "[3/6] Generating manifest..."
./mlfs generate \
  --id tiny-model \
  --version v1 \
  --url-prefix https://$TEST_BUCKET.s3.amazonaws.com/tiny-model/v1 \
  --prefetch config.json,tokenizer.json \
  $TEST_MODEL > /tmp/manifest.json

# 4. Build Docker image with mlfs
echo "[4/6] Building test Docker image..."
docker build -t mlartifactfs-test -f tests/e2e/Dockerfile .

# 5. Run container and mount
echo "[5/6] Running container with FUSE mount..."
docker run --rm \
  --cap-add SYS_ADMIN \
  --device /dev/fuse \
  -v /tmp/manifest.json:/manifest.json \
  -e AWS_ACCESS_KEY_ID \
  -e AWS_SECRET_ACCESS_KEY \
  mlartifactfs-test \
  /bin/bash -c '
    # Mount in background
    /usr/local/bin/mlfs mount --manifest /manifest.json /mnt/model &
    MLFS_PID=$!

    # Wait for mount
    sleep 3

    # Test: List files
    echo "Files in /mnt/model:"
    ls -lh /mnt/model

    # Test: Read prefetched file (should be instant)
    echo "Reading config.json..."
    cat /mnt/model/config.json | jq .

    # Test: Read lazy file (should trigger S3 fetch)
    echo "Reading model.safetensors (lazy)..."
    head -c 1024 /mnt/model/model.safetensors | wc -c

    # Test: Read same file again (should hit cache)
    echo "Reading model.safetensors again (cached)..."
    head -c 1024 /mnt/model/model.safetensors | wc -c

    # Cleanup
    kill $MLFS_PID
  '

echo "[6/6] Test completed successfully!"
```

### **Docker Test Image**

```dockerfile
# tests/e2e/Dockerfile
FROM ubuntu:22.04

# Install dependencies
RUN apt-get update && apt-get install -y \
    fuse \
    ca-certificates \
    jq \
    && rm -rf /var/lib/apt/lists/*

# Copy mlfs binary
COPY mlfs /usr/local/bin/mlfs
RUN chmod +x /usr/local/bin/mlfs

# Allow non-root FUSE
RUN echo "user_allow_other" >> /etc/fuse.conf

CMD ["/bin/bash"]
```

### **How to Run E2E Tests**

```bash
# Build mlfs binary first
go build -o mlfs ./cmd/mlfs

# Run E2E test suite
./tests/e2e/test-full-workflow.sh

# Or run specific test
./tests/e2e/test-mount.sh
```

---

## Layer 4: Performance Tests

### **What to Measure**

**Cold Start Performance:**
- ✅ Mount time with prefetch (target: <30 sec for 50 MB)
- ✅ First read latency (target: <500 ms for 16 MB chunk)

**Cache Performance:**
- ✅ Warm read latency (target: <10 ms)
- ✅ Sequential read throughput (target: >100 MB/s)

**S3 Request Efficiency:**
- ✅ Number of S3 requests for typical model load
- ✅ Chunk alignment effectiveness (wasted bandwidth)

### **Performance Test Script**

```bash
#!/bin/bash
# tests/performance/benchmark-mount.sh

MODEL_SIZE=500MB  # 500 MB test model
PREFETCH_SIZE=50MB

echo "=== Performance Benchmark ==="

# Mount with timing
echo "Mounting with prefetch ($PREFETCH_SIZE)..."
time mlfs mount --manifest manifest.json /mnt/model &
MLFS_PID=$!

sleep 5  # Wait for mount

# Benchmark: Cold read
echo "Benchmarking cold read..."
dd if=/mnt/model/model.safetensors of=/dev/null bs=1M count=100 2>&1 | grep MB/s

# Benchmark: Warm read (cached)
echo "Benchmarking warm read..."
dd if=/mnt/model/model.safetensors of=/dev/null bs=1M count=100 2>&1 | grep MB/s

# Cleanup
kill $MLFS_PID
```

---

## Layer 5: Error Handling Tests

### **What to Test**

**S3 Errors:**
- ✅ 404 Not Found (missing file)
- ✅ 403 Forbidden (no permissions)
- ✅ 503 Service Unavailable (retry succeeds)
- ✅ Network timeout (retry with backoff)

**FUSE Errors:**
- ✅ Invalid manifest (malformed JSON)
- ✅ SHA256 mismatch (corrupted prefetch)
- ✅ Mount point doesn't exist
- ✅ Mount point already in use

**Cache Errors:**
- ✅ Cache directory not writable
- ✅ Disk full during chunk write
- ✅ Corrupted cache file

### **How to Test**

```bash
# Test: Missing S3 file
./mlfs mount --manifest manifest-bad-url.json /mnt/model
# Expected: Error on prefetch, mount fails

# Test: Invalid manifest
echo "invalid json" > bad-manifest.json
./mlfs mount --manifest bad-manifest.json /mnt/model
# Expected: JSON parse error

# Test: SHA256 mismatch (simulate corrupted file)
# Modify S3 file, keep old SHA256 in manifest
./mlfs mount --manifest manifest.json /mnt/model
# Expected: Prefetch SHA256 verification fails
```

---

## Testing Checklist for Milestones

### **M2 (Manifest Generator) - Ready to Test**
- [ ] Unit tests pass
- [ ] Generate manifest from `tests/fixtures/tiny-model`
- [ ] Verify SHA256 matches `sha256sum` output
- [ ] Verify JSON schema is valid

### **M3 (Cache Manager) - Ready to Test**
- [ ] Unit tests pass
- [ ] Write/read chunks work
- [ ] Verified marker logic works

### **M4 (S3 Client) - Ready to Test**
- [ ] Unit tests with mock HTTP responses
- [ ] Integration test with MinIO (range requests)
- [ ] Retry logic works (simulate 503 errors)

### **M5 (Fetch Manager) - Ready to Test**
- [ ] Unit tests pass (chunk alignment)
- [ ] Integration test: fetch + cache
- [ ] Integration test: prefetch + SHA256 verify

### **M6 (FUSE) - Ready to Test**
- [ ] Unit tests: directory tree building
- [ ] Integration test: mount + read (mock fetch manager)

### **M7 (CLI Mount) - Ready to Test**
- [ ] E2E test: full workflow (see script above)

### **M8 (E2E Testing) - Ready to Ship**
- [ ] E2E test with Docker passes
- [ ] Performance benchmarks meet targets
- [ ] Error handling tests pass
- [ ] Manual testing with real ML model (DistilBERT)

---

## CI/CD Integration (Future)

```yaml
# .github/workflows/test.yml
name: Test

on: [push, pull_request]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - run: go test -v ./pkg/...

  integration-tests:
    runs-on: ubuntu-latest
    services:
      minio:
        image: minio/minio
        ports:
          - 9000:9000
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: S3_ENDPOINT=http://localhost:9000 go test -v ./tests/integration/...

  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: go build -o mlfs ./cmd/mlfs
      - run: ./tests/e2e/test-full-workflow.sh
        env:
          AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
          AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
```

---

## Quick Start: Testing Right Now

**Day 1: After M2 (Manifest Generator)**
```bash
# Create test fixture
mkdir -p tests/fixtures/tiny-model
echo '{"model": "test"}' > tests/fixtures/tiny-model/config.json
echo "dummy weights" > tests/fixtures/tiny-model/model.bin

# Test generate command
go run ./cmd/mlfs generate \
  --id test \
  --version v1 \
  --url-prefix https://example.com/test \
  tests/fixtures/tiny-model

# Expected: Valid JSON manifest with correct SHA256
```

**Day 3: After M5 (Fetch Manager)**
```bash
# Start MinIO
docker run -d -p 9000:9000 minio/minio server /data

# Run integration tests
go test ./pkg/fetch -v
```

**Day 7: After M7 (CLI Mount)**
```bash
# Full E2E test
./tests/e2e/test-full-workflow.sh
```

---

## Exit Condition

✅ **POC is ready for demo when:**
1. Unit tests pass for all packages
2. Integration tests pass with MinIO
3. E2E test passes with Docker + real S3
4. Manual test: Mount a real HuggingFace model (DistilBERT) and run inference

**Testing is complete when you can demo:**
> "Here's a Docker container. I'm changing one line in the manifest file (model version). Container restarts, new model is mounted in 10 seconds. No image rebuild needed."
