# MLArtifactFS Context Index

**Purpose:** Route you to the right context in <30 seconds.

---

## Quick Start

**New to the project?** Start here:
1. [current.md](./current.md) — Current state, what's done, what's next (2 min read)
2. [planning/03-design.md](./planning/03-design.md) — System architecture (7 min read)
3. [planning/04-implementation-plan.md](./planning/04-implementation-plan.md) — Milestone roadmap (5 min read)

**Working on a specific milestone?**
→ See [Milestone Bundles](#milestone-bundles) below

**Last updated:** 2026-04-09 (M5 complete, M6 next)

---

## Navigation by Intent

### "I need to understand the current state"
→ **[current.md](./current.md)** — Single source of truth for project status

### "I need to implement the next milestone"
→ **[bundles/M6-fuse-filesystem.bundle.md](./bundles/M6-fuse-filesystem.bundle.md)** — Focused context for M6

### "I need to understand the architecture"
→ **[planning/03-design.md](./planning/03-design.md)** — Component design, data flow, interfaces

### "I need to see the full milestone plan"
→ **[planning/04-implementation-plan.md](./planning/04-implementation-plan.md)** — M1-M9 breakdown

### "I need to understand a specific design decision"
→ **[decisions/](./decisions/)** — ADRs (Architecture Decision Records)
- (To be created as decisions are extracted)

### "I need to review completed work"
→ **[milestones/](./milestones/)** — Completed milestone documentation
- [milestones/M2-manifest-generator.md](./milestones/M2-manifest-generator.md) — Manifest generator
- [milestones/M3-cache-manager.md](./milestones/M3-cache-manager.md) — Cache manager
- [milestones/M4-s3-client.md](./milestones/M4-s3-client.md) — S3 client
- [milestones/M5-fetch-manager.md](./milestones/M5-fetch-manager.md) — Fetch manager

### "I want to see future plans"
→ **[references/future-optimizations.md](./references/future-optimizations.md)** — Content deduplication, compression ideas

---

## File Organization

```
context/
├── current.md                    # ⭐ START HERE - Current truth
├── index.md                      # ⭐ This file - Navigation guide
├── context-manager.md            # Instructions for maintaining /context
│
├── planning/                     # Design & roadmap docs
│   ├── 03-design.md             # ⭐ System architecture (canonical)
│   ├── 04-implementation-plan.md # ⭐ M1-M9 milestone plan (canonical)
│   ├── 05-testing-strategy.md   # Testing approach
│   ├── 01-idea-honing.md        # Historical
│   └── 02-research.md           # Historical
│
├── decisions/                    # Architecture Decision Records (ADRs)
│   ├── ADR-001-chunk-size.md
│   ├── ADR-002-blocking-prefetch.md
│   ├── ADR-003-sha256-verification.md
│   ├── ADR-004-unbounded-cache.md
│   ├── ADR-005-read-only-filesystem.md
│   └── ADR-006-retry-policy.md
│
├── milestones/                   # Completed milestone docs
│   ├── M2-manifest-generator.md # M2 completion summary
│   ├── M3-cache-manager.md      # M3 completion summary
│   ├── M4-s3-client.md          # M4 completion summary
│   └── M5-fetch-manager.md      # M5 completion summary
│
├── bundles/                      # Milestone-specific context bundles
│   ├── M3-cache-manager.bundle.md # M3 spec (completed)
│   ├── M4-s3-client.bundle.md   # M4 spec (completed)
│   ├── M5-fetch-manager.bundle.md # M5 spec (completed)
│   └── M6-fuse-filesystem.bundle.md # M6 spec (next milestone)
│
├── references/                   # Supporting docs, future plans
│   ├── future-optimizations.md  # Dedup/compression ideas
│   └── M3-implementation-notes.md # Cache manager design review
│
└── _archive/                     # Historical docs (low relevance)
    ├── pitch.md                 # Original pitch
    ├── ppd.md                   # Product positioning
    └── manifest-generator-implementation.md # Detailed M2 notes
```

---

## Milestone Bundles

Bundles are optimized, minimal context packages for implementing specific milestones.

| Milestone | Status | Bundle/Completion Link | Est. Reading Time |
|-----------|--------|------------------------|-------------------|
| M1: Project Scaffolding | ✅ Complete | N/A | N/A |
| M2: Manifest Generator | ✅ Complete | [milestones/M2-manifest-generator.md](./milestones/M2-manifest-generator.md) | 5 min |
| M3: Cache Manager | ✅ Complete | [milestones/M3-cache-manager.md](./milestones/M3-cache-manager.md) | 5 min |
| M4: S3 Client | ✅ Complete | [milestones/M4-s3-client.md](./milestones/M4-s3-client.md) | 3 min |
| M5: Fetch Manager | ✅ Complete | [milestones/M5-fetch-manager.md](./milestones/M5-fetch-manager.md) | 3 min |
| M6: FUSE Filesystem | 🔲 Next | [bundles/M6-fuse-filesystem.bundle.md](./bundles/M6-fuse-filesystem.bundle.md) | 4 min |
| M7: CLI Mount Command | 🔲 Pending | Not yet created | TBD |
| M8: End-to-End Testing | 🔲 Pending | Not yet created | TBD |
| M9: Documentation | 🔲 Pending | Not yet created | TBD |

---

## Key Canonical Documents

These are the single source of truth for their respective domains:

1. **[current.md](./current.md)** — Project status, what's done, what's next
2. **[planning/03-design.md](./planning/03-design.md)** — System architecture
3. **[planning/04-implementation-plan.md](./planning/04-implementation-plan.md)** — Milestone roadmap
4. **[planning/05-testing-strategy.md](./planning/05-testing-strategy.md)** — Testing approach
5. **[decisions/](./decisions/)** — Architecture Decision Records (ADRs)

All other documents either support these or have been archived.

---

## Archived Documents

Historical documents with limited current relevance:

- **[_archive/pitch.md](./_archive/pitch.md)** — Original project pitch (Jan 2026)
- **[_archive/ppd.md](./_archive/ppd.md)** — Product positioning document (Jan 2026)
- **[_archive/manifest-generator-implementation.md](./_archive/manifest-generator-implementation.md)** — Detailed M2 implementation notes (superseded by M2 completion doc)
- **[planning/01-idea-honing.md](./planning/01-idea-honing.md)** — Early ideation (historical)
- **[planning/02-research.md](./planning/02-research.md)** — FUSE/S3 research notes (historical)

---

## Decision Records (ADRs)

Architecture Decision Records will be created as decisions are formalized:

| ADR | Title | Status | Key Decision |
|-----|-------|--------|--------------|
| ADR-001 | 16 MB Chunk Size | To be created | Chunk alignment for S3 range requests |
| ADR-002 | Blocking Prefetch at Mount | To be created | Trade-off: startup latency vs runtime performance |
| ADR-003 | SHA256 Post-Download Verification | To be created | Verify after full file, not per-chunk |
| ADR-004 | Unbounded Cache (MVP) | To be created | No eviction policy for POC |
| ADR-005 | Read-Only Filesystem | To be created | Immutable model artifacts |

---

## Context Maintenance

This `/context` directory is managed according to **[context-manager.md](./context-manager.md)**.

**Key principles:**
- Minimize token usage via links + summaries
- Single source of truth for critical facts
- Archive superseded content, don't delete
- Milestone bundles are the primary work artifacts

**Last major reorganization:** 2026-02-16
**Last milestone update:** 2026-04-09 (M5 complete)

---

## Quick Reference Commands

**Build:**
```bash
go build -o mlfs ./cmd/mlfs
```

**Run tests:**
```bash
go test ./pkg/...
```

**Generate manifest:**
```bash
./mlfs generate --id <id> --version <ver> --url-prefix <url> --prefetch <files> <dir>
```

---

**Need help?** Start with [current.md](./current.md) for the latest project state.
