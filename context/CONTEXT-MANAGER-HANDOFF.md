# Context Manager Handoff Document

**Created:** 2026-02-16
**For:** Next context manager instance
**Project:** MLArtifactFS

---

## Your Role

You are the **Context Manager** for MLArtifactFS. Your job is to maintain the `/context` directory as a minimal, high-signal knowledge base optimized for downstream LLM consumption.

**Read first:** [context-manager.md](./context-manager.md) — Complete operating instructions

---

## Current Project State (as of 2026-02-16)

### Completed Milestones
- ✅ **M1:** Project Scaffolding
- ✅ **M2:** Manifest Generator (completed 2026-01-19)
- ✅ **M3:** Cache Manager (completed 2026-02-16)

### Current Milestone
- 🔲 **M4:** S3 Client (next to implement)

### Remaining Milestones
- M5: Fetch Manager
- M6: FUSE Filesystem
- M7: CLI Mount Command
- M8: End-to-End Testing
- M9: Documentation

---

## Context Organization

### Directory Structure
```
context/
├── current.md              # ⭐ Single source of truth (update after each milestone)
├── index.md                # ⭐ Navigation hub (update milestone table)
├── context-manager.md      # Your operating instructions (don't modify)
│
├── planning/               # Canonical design docs (rarely change)
│   ├── 03-design.md       # System architecture
│   ├── 04-implementation-plan.md # M1-M9 roadmap
│   ├── 05-testing-strategy.md
│   └── [01, 02 are historical]
│
├── decisions/              # ADRs (add new ones as needed)
│   ├── ADR-001-chunk-size.md
│   ├── ADR-002-blocking-prefetch.md
│   ├── ADR-003-sha256-verification.md
│   ├── ADR-004-unbounded-cache.md
│   ├── ADR-005-read-only-filesystem.md
│   └── ADR-006-retry-policy.md
│
├── milestones/             # Completed milestone docs
│   ├── M2-manifest-generator.md
│   └── M3-cache-manager.md
│
├── bundles/                # Milestone-specific implementation specs
│   ├── M3-cache-manager.bundle.md (spec for completed M3)
│   └── M4-s3-client.bundle.md (spec for next milestone)
│
├── references/             # Supporting docs
│   ├── future-optimizations.md
│   └── M3-implementation-notes.md
│
└── _archive/               # Historical low-relevance docs
    ├── pitch.md
    ├── ppd.md
    └── manifest-generator-implementation.md
```

---

## Your Responsibilities

### When a Milestone Completes

**Example handoff:** "M4 is complete, prepare for M5"

**Your workflow:**

1. **Create milestone completion doc** in `milestones/`
   - Template: See [M3-cache-manager.md](./milestones/M3-cache-manager.md)
   - Include: What was built, tests, decisions made, integration readiness
   - 5-minute read target

2. **Update [current.md](./current.md)**
   - Move milestone from "What's Next" to "What's Done"
   - Update "Current Milestone" to next one
   - Update project structure (mark package ✅)
   - Update test counts
   - Update open questions for next milestone

3. **Extract new ADRs** (if needed)
   - If implementation made significant decisions, formalize them
   - Template: See existing ADRs in `decisions/`
   - Number sequentially (ADR-007, ADR-008, etc.)

4. **Create next milestone bundle** in `bundles/`
   - Template: See [M4-s3-client.bundle.md](./bundles/M4-s3-client.bundle.md)
   - Structure:
     - Objective (1-3 sentences)
     - Deliverable definition (testable acceptance criteria)
     - Current state (what exists, what's missing)
     - Constraints & invariants
     - Key decisions (link to ADRs)
     - Ordered reading list (5-7 docs)
     - Open questions / risks
     - Implementation checklist
     - Success criteria
   - 3-minute read target

5. **Update [index.md](./index.md)**
   - Update milestone table (mark completed, update "Next")
   - Update navigation links
   - Update file organization tree if structure changed

6. **Move implementation notes** (if any)
   - If milestone LLM created notes in `bundles/`, move to `references/`
   - Rename from `.bundle.md` to `.md`

---

## ADR Creation Guidelines

**When to create an ADR:**
- Significant design decision made during implementation
- Cross-cutting concern (affects multiple milestones)
- Security or performance trade-off
- User-facing behavior choice

**When NOT to create an ADR:**
- Trivial implementation detail
- Already covered in existing ADR
- Temporary workaround

**ADR Template Structure:**
```markdown
# ADR-XXX: [Title]

**Status:** Accepted
**Date:** YYYY-MM-DD
**Context:** Milestone X

## Decision
[One sentence: what was decided]

## Context
[Why this decision was needed]

## Alternatives Considered
[Table of options with pros/cons]

## Rationale
[Why this option was chosen]

## Consequences
[Positive, Negative, Mitigations]

## Implementation Notes
[Code examples if helpful]

## Related
[Link to milestones, other ADRs]

## References
[External links]
```

---

## Bundle Creation Guidelines

**Bundle = Self-contained spec for one milestone**

**Must include:**
1. Clear objective (1-3 sentences)
2. Testable acceptance criteria (checkboxes)
3. Current state snapshot
4. All constraints and invariants
5. Links to relevant ADRs
6. Ordered reading list (5-7 docs)
7. Open questions with recommendations
8. Implementation checklist
9. Success criteria

**Bundle quality test:**
> Can an LLM implement the milestone reading ONLY the bundle + its reading list?

If no → bundle is incomplete.

**Reading time target:** 3 minutes

---

## Workflow Examples

### Example 1: M4 Completes Successfully

**User says:** "M4 is done, all tests passing. S3 client works with presigned URLs. We decided to use 30s timeout per request."

**Your actions:**
1. Create `milestones/M4-s3-client.md`
   - What was built: S3 client with retry logic
   - Tests: X passing
   - Decisions: 30s timeout (maybe ADR-007 if significant)
   - Integration: Ready for M5

2. Update `current.md`:
   - M4 added to "What's Done"
   - Current milestone → M5
   - Update test counts

3. Check if timeout decision needs ADR:
   - If it's just "use default SDK timeout" → no ADR
   - If custom logic → maybe ADR-007

4. Create `bundles/M5-fetch-manager.bundle.md`:
   - Read `planning/04-implementation-plan.md` § M5
   - Read `planning/03-design.md` § Fetch Manager
   - Extract requirements
   - Create ordered reading list
   - Include ADRs: 001, 003, 004, 006 (relevant to M5)

5. Update `index.md`:
   - Milestone table: M4 → ✅ Complete
   - Navigation: Point to M5 bundle

### Example 2: M5 Discovers Need for New ADR

**User says:** "M5 implementation complete. We added per-chunk locking with sync.RWMutex. This is critical for concurrent access."

**Your actions:**
1. Create `decisions/ADR-007-concurrency-control.md`
   - Decision: Per-chunk locking with sync.RWMutex
   - Context: M5 needs concurrent prefetch + lazy load
   - Alternatives: File-level locks, no locks, flock
   - Rationale: Granular locks prevent bottlenecks

2. Update `bundles/M5-fetch-manager.bundle.md` (if still open):
   - Add ADR-007 to reading list (for future reference)

3. Update `current.md`:
   - Add ADR-007 to "Key Decisions" list

4. Continue with normal M5 completion workflow

### Example 3: Scope Change Mid-Milestone

**User says:** "We're adding compression support to M6. This changes the manifest format."

**Your actions:**
1. **Stop and clarify:**
   - Ask: "Does this affect completed milestones (M2 manifest generator)?"
   - Ask: "Should I update M2 retroactively or create M6b for compression?"

2. If it affects M2:
   - Create ADR-008-compression-strategy.md
   - Note in M2 completion doc: "Updated post-completion for compression"
   - Update M6 bundle to reference ADR-008

3. If it's M6-only:
   - Add to M6 bundle acceptance criteria
   - May need ADR-008 if significant

---

## Common Pitfalls to Avoid

### ❌ Don't Do This
1. **Don't modify bundles after milestone starts**
   - Bundle = frozen spec
   - If spec is wrong, note it for next time
   - Exception: Critical blocker discovered

2. **Don't duplicate information**
   - Use links, not copy-paste
   - Single source of truth principle

3. **Don't create ADRs for trivial choices**
   - "Use camelCase" → not an ADR
   - "Use 16 MB chunks" → yes, ADR (has trade-offs)

4. **Don't let bundles grow beyond 3-minute read**
   - If too long, you're including too much detail
   - Move details to ADRs or design doc, link them

5. **Don't update `current.md` mid-milestone**
   - Only update after milestone completes
   - Exception: Critical correction

### ✅ Do This
1. **Keep bundles self-contained**
   - Bundle + reading list = all you need

2. **Update `current.md` immediately after milestone**
   - Don't batch updates

3. **Archive, don't delete**
   - Historical docs go to `_archive/`
   - Maintain audit trail

4. **Use TODO comments in bundles**
   - "TODO: Revisit this in M7"
   - "TODO: Create ADR-009 if performance issue confirmed"

---

## File Naming Conventions

### Milestones
- `M{N}-{kebab-case-name}.md`
- Example: `M3-cache-manager.md`

### Bundles
- `M{N}-{kebab-case-name}.bundle.md`
- Example: `M4-s3-client.bundle.md`

### ADRs
- `ADR-{NNN}-{kebab-case-topic}.md`
- Example: `ADR-006-retry-policy.md`
- Number sequentially: 001, 002, ..., 010, 011, etc.

### Implementation Notes
- `M{N}-implementation-notes.md` (in `references/`)
- Example: `M3-implementation-notes.md`

---

## Key Files to Maintain

### Always Update After Milestone:
1. `current.md` — Project status
2. `index.md` — Navigation and milestone table
3. `milestones/M{N}-*.md` — New completion doc
4. `bundles/M{N+1}-*.bundle.md` — Next milestone spec

### Update When Needed:
1. `decisions/ADR-*.md` — New architectural decisions
2. `references/` — Move implementation notes here

### Rarely Update:
1. `planning/03-design.md` — Only if architecture changes
2. `planning/04-implementation-plan.md` — Only if milestone plan changes
3. `context-manager.md` — Your instructions (only if process changes)

---

## Decision Tree: "Should I Create an ADR?"

```
Is this a choice between multiple viable options?
├─ No → Not an ADR (just implementation detail)
└─ Yes
    └─ Does it affect multiple milestones or components?
        ├─ No → Maybe include in milestone doc only
        └─ Yes → Create ADR
            └─ Does it have trade-offs (performance, complexity, cost)?
                ├─ No → Maybe not significant enough for ADR
                └─ Yes → Definitely create ADR
```

**Examples:**
- ✅ ADR: "16 MB chunk size" (affects M3, M4, M5; has trade-offs)
- ✅ ADR: "Retry policy with jitter" (affects M4, maybe M5; prevents thundering herd)
- ❌ Not ADR: "Use fmt.Errorf for errors" (no trade-off, just convention)
- ❌ Not ADR: "Cache directory is ./cache" (trivial, no alternatives considered)

---

## Quick Reference: Milestone Completion Checklist

When user says "Milestone X is complete":

- [ ] Read implementation summary from user
- [ ] Create `milestones/MX-*.md` completion doc
- [ ] Update `current.md` (move milestone to "What's Done", update current)
- [ ] Check if new ADRs needed (significant decisions during implementation?)
- [ ] Create `bundles/M{X+1}-*.bundle.md` for next milestone
- [ ] Update `index.md` milestone table and navigation
- [ ] Move any implementation notes to `references/`
- [ ] Verify all links work (bundles should have 5-7 linked docs)
- [ ] Report back: "Context updated, ready for M{X+1}"

---

## Current Status Summary (2026-02-16)

**What's working:**
- Context structure is clean and navigable
- M3 bundle was effective (implementation succeeded)
- M4 bundle is ready and includes new ADR-006
- All ADRs formalized (001-006)
- No duplication or bloat

**What to watch:**
- M5 will need concurrency ADR (per-chunk locking)
- M6 (FUSE) is complex, may need multiple ADRs
- M7 (CLI) will tie everything together, may expose integration issues

**Next milestone after M4:**
- M5: Fetch Manager
- Will orchestrate S3 client (M4) + cache (M3)
- Needs ADR for concurrency control
- Bundle should reference: ADR-001, 003, 004, 006, and new ADR-007

---

## Emergency Procedures

### If Bundle is Incomplete Mid-Implementation
**User says:** "The M4 bundle is missing X, I need it now"

**Your response:**
1. Don't update bundle (it's frozen)
2. Point to existing doc that has X
3. If X doesn't exist anywhere: create reference doc, tell user to read it
4. Note for next time: Bundle creation needs improvement

### If ADR Conflicts with Implementation
**User says:** "ADR-006 says jitter, but AWS SDK handles retries automatically"

**Your response:**
1. Create ADR-006-AMENDMENT.md or supersede with ADR-007
2. Mark ADR-006 as "Superseded by ADR-007"
3. Update M4 bundle (exception: can update if not yet implemented)
4. Document in M4 completion: "Deviated from ADR-006 due to AWS SDK behavior"

### If Milestone Needs Splitting
**User says:** "M6 is too big, split into M6a (read-only ops) and M6b (directory tree)"

**Your response:**
1. Create `bundles/M6a-*.bundle.md` and `bundles/M6b-*.bundle.md`
2. Update `planning/04-implementation-plan.md` if needed
3. Update `index.md` milestone table
4. Keep ADRs as-is (they apply to both)

---

## Tools and Commands

### Verify Bundle Quality
```bash
# Check bundle reading time (should be ~3 min = ~900 words)
wc -w context/bundles/M4-s3-client.bundle.md

# Check for broken links
grep -o '\[.*\](.*\.md)' context/bundles/*.md | while read link; do
  file=$(echo $link | sed 's/.*(\(.*\))/\1/')
  [ -f "context/$file" ] || echo "Broken: $link"
done
```

### Find Duplicated Content
```bash
# Search for duplicate paragraphs
cd context && grep -r "specific phrase" . --include="*.md"
```

### Check for Private Info Before Commit
```bash
# No actual secrets should appear
grep -ri "password\|secret.*=\|key.*=\|AKIA\|sk-" context/
```

---

## Final Notes

**Your goal:** Minimize tokens for downstream LLMs while maximizing task success rate.

**Success metric:** Can an implementation LLM complete a milestone reading ONLY the bundle + its 5-7 linked docs?

**Remember:**
- Bundles are self-contained, frozen specs
- ADRs are permanent decisions (can be superseded, not deleted)
- `current.md` is the single source of truth for project state
- Archive, don't delete (preserve audit trail)

**Read the full operating manual:** [context-manager.md](./context-manager.md)

---

**Good luck! The context is in good shape. Keep it clean, minimal, and navigable.** 🎯
