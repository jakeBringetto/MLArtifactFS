# Step 2 — Research

## High-Risk Assumptions Reviewed

### 1. FUSE in Docker Containers — ✅ VALIDATED

**Assumption:** FUSE can work in Docker containers without full `--privileged` mode

**Findings:**
- FUSE **does not require** full `--privileged` mode
- Minimum requirements:
  - `--cap-add SYS_ADMIN` (provides FUSE device permissions)
  - `--device /dev/fuse` (exposes host FUSE device to container)
  - Optional: `--security-opt apparmor:unconfined` (if AppArmor blocks mount syscalls)

**Example Docker command:**
```bash
docker run -it --cap-add SYS_ADMIN --device /dev/fuse --security-opt apparmor:unconfined <image>
```

**Confidence:** HIGH — This is well-documented and used in production (Azure Storage FUSE, Kopia, etc.)

**Impact on design:** Container must be started with specific capabilities; cannot work in fully sandboxed environments

**Sources:**
- [Docker for Linux FUSE Issue #321](https://github.com/docker/for-linux/issues/321)
- [FUSE and Docker (MITRE fusera)](https://github.com/mitre/fusera/wiki/FUSE-and-Docker)
- [Azure Storage FUSE Issue #244](https://github.com/Azure/azure-storage-fuse/issues/244)

---

### 2. Go FUSE Library Choice — ⚠️ TRADE-OFFS IDENTIFIED

**Assumption:** bazil.org/fuse or jacobsa/fuse would be suitable for MVP

**Findings:**

| Library | Protocol Support | Status | Performance | Notes |
|---------|------------------|--------|-------------|-------|
| **bazil.org/fuse** | 7.8 | ❌ Unmaintained | Unknown | "Seems dead" |
| **jacobsa/fuse** | 7.12 | ⚠️ Low activity | 1.8x slower than hanwen | Inspired by bazil, 427 importers |
| **hanwen/go-fuse** | 7.28 | ✅ Active | Fastest | Comprehensive, production-ready |

**Benchmark data (2020):**
- hanwen/go-fuse: ~1.6-1.8x faster than jacobsa/fuse
- hanwen supports protocol version 7.28 (latest features)
- jacobsa limited to 7.12

**Production experience:**
- rclone switched from bazil → hanwen (though noted some stability issues in edge cases)
- hanwen is most feature-complete and performant

**Recommendation:** Use **hanwen/go-fuse v2** for MVP
- Best performance characteristics
- Active maintenance
- Protocol version 7.28 support (modern kernel features)
- Most widely adopted in production Go projects

**Confidence:** HIGH — Clear winner based on benchmarks and adoption

**Sources:**
- [hanwen/go-fuse GitHub](https://github.com/hanwen/go-fuse)
- [Performance comparison issue (jacobsa #78)](https://github.com/jacobsa/fuse/issues/78)
- [fuse-benchmark](https://github.com/Hughen/fuse-benchmark)
- [rclone switching discussion (#5896)](https://github.com/rclone/rclone/issues/5896)

---

### 3. S3 Range Request Performance — ✅ VALIDATED with GUIDELINES

**Assumption:** S3 range requests are efficient enough for lazy loading

**Findings:**

**✅ Range requests are a recommended S3 pattern:**
- AWS explicitly recommends byte-range fetches for large objects
- **Optimal range size:** 8-16 MB per request
- **Parallel connections:** No S3 limit on concurrent connections
- **Throughput formula:** 1 request per 85-90 MB/s of desired throughput

**Lazy loading best practices:**
1. **Chunk size:** Use 8-16 MB ranges (aligns with S3 optimization)
2. **Prefetching:** Download critical files (config, tokenizer) at mount time
3. **Retry strategy:** Retry failed requests after 2 seconds for <512KB ranges
4. **Multipart alignment:** If model was uploaded via multipart, align GET ranges to part boundaries

**Performance characteristics:**
- Median latency for small requests (<512KB): tens of milliseconds
- Large parallel range requests can saturate gigabit connections
- AWS Transfer Manager SDK automates parallelization

**Limitations:**
- ⚠️ S3 does NOT support multiple ranges in a single GET request (must make separate requests)

**Impact on design:**
- FUSE read operations should request 8-16 MB chunks from S3
- Implement prefetch queue for predicted access patterns
- Use Go SDK with parallel range fetchers

**Confidence:** HIGH — AWS official best practices, proven at scale

**Sources:**
- [AWS S3 Performance Guidelines](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance-guidelines.html)
- [Byte-Range Fetches Best Practices](https://docs.aws.amazon.com/whitepapers/latest/s3-optimizing-performance-best-practices/use-byte-range-fetches.html)
- [S3 Performance Design Patterns](https://docs.aws.amazon.com/AmazonS3/latest/userguide/optimizing-performance-design-patterns.html)

---

## Additional Findings

### 4. Model File Access Patterns (contextual research)

**Observation:** Most ML models have predictable access patterns:
- **Config files** (config.json): Always read first, small (<10KB)
- **Tokenizer files** (tokenizer.json, vocab): Read during initialization, medium (1-50MB)
- **Weight files** (pytorch_model.bin, safetensors): Largest, may be read sequentially or selectively

**Implication for prefetching:**
- Manifest should support prefetch hints (e.g., `"prefetch": ["config.json", "tokenizer.json"]`)
- FUSE implementation should block mount until prefetch completes
- Weights can be lazy-loaded on first read

---

## Changes to Constraints / Scope

### Updated Technology Decision
- **FUSE Library:** hanwen/go-fuse v2 (replaces "bazil or jacobsa")

### New Design Constraints Discovered
1. **Chunk size:** Use 8-16 MB range requests to S3
2. **Docker requirements:** Document `--cap-add SYS_ADMIN --device /dev/fuse` in README
3. **Prefetch blocking:** Mount operation should block until prefetch files are cached

---

## Confidence Summary

| Assumption | Status | Confidence | Risk Level |
|------------|--------|------------|------------|
| FUSE in Docker | ✅ Validated | HIGH | LOW |
| Go FUSE library | ⚠️ Clarified (use hanwen) | HIGH | LOW |
| S3 range requests | ✅ Validated | HIGH | LOW |
| Sequential model access | 🔍 Assumed (typical) | MEDIUM | MEDIUM |
| Cache fits in container | 🔍 Untested | MEDIUM | MEDIUM |

---

## Exit Condition

✅ **COMPLETE** — All critical assumptions validated. No blockers identified.

**Key takeaways:**
1. Docker FUSE support is straightforward with `SYS_ADMIN` + `/dev/fuse`
2. hanwen/go-fuse is the right library choice
3. S3 range requests are designed for this use case (8-16 MB chunks)

**Next Step:** Proceed to Step 3 (Design) — define system architecture and component interactions
