# Step 1 — Idea Honing

## Refined Problem Statement

Build a FUSE-based filesystem that enables lazy-loading of ML models from S3 storage into containerized ML inference workloads. The system allows developers to decouple model artifacts from container images, enabling fast version switching without image rebuilds.

**Core Value Proposition:**
- Eliminate large model files from Docker images
- Enable instant model version swapping via manifest files
- Reduce cold start times through selective prefetching
- Content-addressable, version-controlled model storage

## Explicit Constraints

### Platform
- **Target:** Linux containers (Docker/Kubernetes)
- **FUSE:** Must work in containerized environments with FUSE support (requires privileged mode or specific capabilities)

### Timeline & Scope
- **Target:** Proof-of-concept in 1-2 weeks
- **MVP Focus:** Core functionality working end-to-end
- **Acceptable tradeoffs:** Minimal error handling, basic testing, limited edge cases

### Storage
- **S3-only** for MVP
- Public read access or presigned URLs
- No multi-backend abstraction needed initially

### Technology Stack
- **Language:** Go
- **Rationale:** Best FUSE library support (bazil.org/fuse or jacobsa/fuse), single binary deployment, good performance

## Explicit Non-Goals (MVP)

- ❌ macOS support (FUSE compatibility complexity)
- ❌ Write operations to models (read-only FS only)
- ❌ Multi-cloud storage backends (GCS, Azure)
- ❌ Authentication/authorization beyond S3 presigned URLs
- ❌ Advanced caching policies (LRU eviction, cache limits)
- ❌ Monitoring/observability (basic logging only)
- ❌ Concurrent multi-model mounts (single mount point per process)
- ❌ Hot-reloading manifests (requires unmount/remount)

## Components (from pitch)

1. **Manifest Format** — JSON file with artifact metadata, file URLs, hashes, prefetch hints
2. **FUSE Filesystem (`mlfs mount`)** — Virtual FS that lazy-fetches files from S3 on read
3. **Manifest Generator CLI (`mlfs generate`)** — Walks local model directory, produces manifest
4. **S3 Model Store** — Standardized layout for versioned models
5. **Automation Script** — Download from HuggingFace → S3 upload → manifest generation

## Open Questions

1. **FUSE in containers:** Do we require `--privileged` or can we use `--cap-add SYS_ADMIN --device /dev/fuse`?
2. **Caching strategy:** Where does cache live? (container local disk, volume mount, tmpfs?)
3. **Prefetch behavior:** Blocking at mount time, or async background fetch?
4. **Error handling:** What happens when S3 is unreachable mid-read? (fail-fast vs retry)
5. **File integrity:** Should we verify SHA256 on every read, or cache validation?

## Assumptions (labeled)

**ASSUMPTION:** S3 files are immutable once uploaded (content-addressed by hash)
**ASSUMPTION:** Model files are read sequentially or in predictable patterns (prefetch helps)
**ASSUMPTION:** Container has network access to S3 at runtime
**ASSUMPTION:** Models fit in memory or cache (no explicit cache eviction needed for MVP)
**ASSUMPTION:** Single-threaded access to mounted filesystem is acceptable for POC
**ASSUMPTION:** Go FUSE libraries (bazil or jacobsa) will work in Docker with appropriate capabilities

## Exit Condition

✅ **COMPLETE** — Requirements are sufficiently defined to proceed to research phase.

**Next Step:** Validate high-risk assumptions (FUSE in containers, Go FUSE library choice, S3 performance characteristics).
