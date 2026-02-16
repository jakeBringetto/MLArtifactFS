# Context Manager Script — Precise Goal & Operating Instructions

This script maintains `/context` as a **minimal, high-signal, milestone-targeted knowledge base** for downstream LLM tasks.

Its purpose is **not** to preserve everything; it is to preserve *exactly what is needed* (plus traceable references) so an LLM can complete the next milestone with minimal context-window waste.

---

## North Star

**Given a target milestone/task, produce the smallest set of files and summaries that still make the task succeed with high confidence.**

If a piece of information does not change the output of the milestone task, it is *noise* and must be removed from the active bundle (archived if valuable historically).

---

## Core Output (What the script must produce)

For any `TARGET` (e.g., `M3` or `design`):

1. **A milestone bundle**
   - Path: `/context/bundles/<TARGET>.bundle.md`
   - Contains only:
     - Objective + success criteria
     - Current state relevant to the target
     - Constraints & invariants
     - Key decisions (linked)
     - Ordered reading list (links only)
     - Open questions / known risks

2. **A canonical “current truth” summary**
   - Path: `/context/current.md`
   - Always updated to reflect:
     - What we’re building (tight)
     - Current milestone (tight)
     - What’s done / what’s next (tight)
     - Constraints/invariants (tight)
     - Pointers to the bundle + key ADRs

3. **A navigable index**
   - Path: `/context/index.md`
   - Must route any LLM to the right bundle in <30 seconds.

Everything else exists to support these outputs.

---

## Hard Constraints (Optimization Rules)

### Token efficiency
- Prefer **links + summaries** over copying text.
- Any bundle should be “pasteable” and should avoid deep history.

### Recency
- The active view must reflect *current* decisions and current state.
- Superseded content must be removed from active docs and moved to archive (with pointers).

### Single source of truth
- For every critical fact/decision/contract, there must be exactly one canonical location.
- All other mentions must become links.

### Traceability without clutter
- Never delete valuable context; **archive it**.
- Active docs should only contain what is needed *now*.

---

## What “Relevant Context” Means

A context item is relevant to `TARGET` if it satisfies at least one:

- It is an explicit requirement or acceptance criterion for the target milestone.
- It defines constraints (performance, security, compliance, platform, budget, deadlines).
- It captures a decision that the milestone must respect (architecture, API contracts, chosen tools).
- It describes current state necessary for incremental work (what exists, what’s broken, what’s next).
- It contains known pitfalls, risks, or failure modes that would change implementation/testing.

If it doesn’t affect the milestone deliverable, it is irrelevant.

---

## Required Behaviors (What the script must do)

### 1) Classify every file
For each file in `/context`, classify it as one of:
- **Canonical** (must stay active): `index.md`, `current.md`, ADRs, active milestone docs, active design/requirements docs
- **Supporting** (linked, not pasted): references, research notes, long implementation notes
- **Superseded** (archive): replaced designs, outdated milestone notes, old summaries
- **Noise** (archive or delete if truly redundant): duplicates, partial drafts with no unique content

### 2) Enforce structure
- No random files at `/context` root other than canonical top-level docs.
- Everything is moved into: `planning/`, `decisions/`, `milestones/`, `bundles/`, `references/`, `_archive/`.

### 3) Remove duplication
If two files overlap:
- Pick a canonical home.
- Merge the best parts into canonical.
- Replace the other with a short pointer or archive it.

### 4) Promote decisions into ADRs
If a decision is embedded in prose:
- Extract it into `decisions/ADR-XXXX-*.md`
- Replace the prose with a link to the ADR and a 1–2 line summary.

### 5) Prune aggressively for the target
When producing `<TARGET>.bundle.md`:
- Include only what is necessary for the target.
- Exclude verbose history, exploration, and tangents.
- If something might be needed but is long, include a **1–3 line summary + link**.

### 6) Preserve an audit trail
When archiving/moving:
- Move to `_archive/YYYY-MM-DD/…`
- Leave behind a stub or add a link in `index.md` changelog.

---

## Bundle Construction Instructions (Precise)

When generating `/context/bundles/<TARGET>.bundle.md`:

Include only these sections, in this order:

1. **Objective**
   - 1–3 sentences.

2. **Deliverable definition**
   - Acceptance criteria as bullet points (testable where possible).

3. **Current state**
   - What exists that matters to TARGET (short, factual).

4. **Constraints & invariants**
   - Non-negotiables (interfaces, security rules, performance goals, format constraints).

5. **Key decisions**
   - Bullet list with links to ADRs.

6. **Ordered reading list**
   - Only the 5–12 most relevant files, ordered.

7. **Open questions / risks**
   - Only items that could block delivery or change design.

**Prohibited in bundles:**
- Entire research dumps
- Long rationale narratives
- Repeated background
- Multiple competing designs (choose the current one; archive others)

---

## Acceptance Test (How the script knows it succeeded)

The script succeeds if a downstream LLM, given only:
- `/context/bundles/<TARGET>.bundle.md`
- and the linked files in its reading list

can produce the milestone deliverable with:
- minimal back-and-forth clarification
- no conflicting requirements
- no missing constraints
- no time wasted on irrelevant history

A quick human check:
- Bundle reads in ~3–7 minutes.
- `current.md` reads in ~1–2 minutes.
- `index.md` routes you immediately.

---

## Default Policy When Unsure

If uncertain whether something is needed:
- Keep a **1–2 line summary + link** in a supporting file
- Do *not* paste the full content into the bundle
- Prefer archiving verbose details rather than bloating active context

---

## Final intent statement

This script is a **lossy compressor** for project knowledge:
- It preserves correctness and constraints.
- It discards verbosity and historical clutter.
- It produces milestone-specific bundles optimized for context windows.
