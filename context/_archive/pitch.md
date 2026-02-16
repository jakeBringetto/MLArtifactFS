**MLArtifactFS: MVP Pitch Document**

---

**Overview** MLArtifactFS is a FUSE-based filesystem and runtime snapshotter designed to lazily mount machine learning models directly from remote storage (e.g., S3), enabling fast iteration, smaller container images, and version-controlled, content-addressable model deployments.

This MVP focuses on providing a fast, composable alternative to OCI image-based model delivery, tailored for A/B testing and controlled cache policies.

---

**Core Components**

1. **Manifest Format**

   * JSON file defining the artifact ID, version, mount path, prefetch list, and each file's metadata (URL, size, SHA256, compression).
   * Enables reproducible mounting behavior across environments.

2. **FUSE Filesystem (**``**)**

   * User-space virtual filesystem that maps manifest paths to remote files.
   * Files are lazily fetched and cached on first access.
   * Optional prefetching at mount time based on manifest hints.

3. **Manifest Generator CLI (**``**)**

   * Walks a model directory, computes file sizes, hashes, and emits a manifest.
   * Fast and deterministic, avoiding full image rebuilds.
   * CLI options include ID, version, prefetch paths, and URL prefix.

4. **S3-Optimized Model Store**

   * Standardized layout for versioned models (e.g., `/config/`, `/weights/`, etc).
   * Easy to mirror Hugging Face or internal models.
   * Remote files served directly with full control over caching and access policies.

5. **Automation Script**

   * Downloads a model from Hugging Face.
   * Restructures the file tree.
   * Generates manifest.
   * Uploads to S3 with public read or versioned paths.

---

**Container Runtime Integration**

In a development or staging container environment, an ML engineer can mount models at runtime using MLArtifactFS instead of embedding models into the container image.

**Runtime Flow:**

* A minimal base container image includes:

  * ML inference code (e.g., `run_model.py`)
  * MLArtifactFS binary and startup scripts
* On container start:

  1. The container downloads a small `manifest.json` (or mounts it via config volume).
  2. MLArtifactFS mounts the model at `/mnt/mlmodel` using `mlfs mount`.
  3. Only prefetched files are downloaded immediately.
  4. As `run_model.py` accesses model files, they are fetched and cached on demand.

**Dev Use Case (A/B Testing):**

* Engineers run different containers, each pointing to a different model version via manifest:

  ```bash
  docker run -v ./manifest-v1.1.json:/manifest.json my-image python run_model.py --model /mnt/mlmodel
  docker run -v ./manifest-v1.2.json:/manifest.json my-image python run_model.py --model /mnt/mlmodel
  ```
* No need to rebuild or repush large model images.
* Artifact changes are version-controlled in S3.

---

**Benefits**

* ✨ Zero need to build or rebuild full Docker images.
* 🔄 Instant swaps between model versions.
* 💡 Transparent caching and prefetch control.
* ✅ Decoupled model management for container-based ML infra.

---

**Example Workflow**

```bash
mlfs generate --id llama --version v1.1 --url-prefix https://s3.com/llama/v1.1 ./llama > manifest.json
mlfs mount --manifest ./manifest.json /mnt/mlmodel
python run_model.py --model /mnt/mlmodel
```

---

This MVP is usable today and optimized for containerized environments where model cold start latency, version control, and resource isolation are top priorities.
