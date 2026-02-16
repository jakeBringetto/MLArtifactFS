# Prompt-Driven Development Script (Minimal)

## Purpose
Guide an LLM through idea → design → implementation **one step at a time**, producing durable artifacts in the project context.

---

## Global Rules (must follow)
1. **One step at a time** — never proceed until the current step is complete.
2. **Write outputs to disk** — each step produces a file in a shared planning directory.
3. **Be concise** — no filler, no long explanations, no generic frameworks.
4. **No assumptions without labeling** — unclear points must be stated explicitly.
5. **Do not revisit earlier steps** unless new information forces it.

---

## Output Structure (required)

All outputs must be written to:

/context/
/<projectname>-planning/
01-idea-honing.md
02-research.md
03-design.md
04-implementation-plan.md


Each step **appends or overwrites only its own file**.

---

## Step 1 — Idea Honing

### Goal
Turn a vague idea into **clear design constraints**.

### Process
- Ask **only essential clarifying questions** needed to remove ambiguity.
- Focus on:
  - What is being built
  - Who/what it is for
  - Hard constraints (time, scope, platform, performance)
  - Explicit non-goals
- Stop once requirements are sufficiently defined to design against.

### Output → `01-idea-honing.md`
- Refined problem statement
- Explicit constraints
- Explicit non-goals
- Open questions (if any)
- Assumptions (clearly labeled)

### Exit Condition
- If ambiguity remains: ask only the missing questions.
- Otherwise: mark the file as **COMPLETE** and proceed to Step 2.

---

## Step 2 — Research

### Goal
Validate or challenge the **key assumptions** from Step 1.

### Process
- Identify only **high-risk or load-bearing assumptions**.
- For each:
  - What is being assumed
  - How it can be validated (docs, benchmarks, experiments, prior art)
- Do not over-research — aim for “good enough to design”.

### Output → `02-research.md`
- Assumptions reviewed
- Findings / confidence level
- Any changes to constraints or scope

### Exit Condition
- If critical assumptions remain unresolved: state them explicitly.
- Otherwise: proceed to Step 3.

---

## Step 3 — Design

### Goal
Define **how the system works** at a high level.

### Process
- Define system boundaries and major components.
- Describe data/control flow.
- Make key architectural decisions.
- Avoid implementation detail unless it affects design.

### Output → `03-design.md`
- System overview
- Major components
- Interfaces / interactions
- Key design decisions
- Known risks or tradeoffs

### Exit Condition
- Design is specific enough that implementation planning is mechanical.
- Proceed to Step 4.

---

## Step 4 — Implementation Plan

### Goal
Translate the design into **actionable work**.

### Process
- Break work into ordered steps or milestones.
- Identify dependencies.
- Keep it execution-focused.

### Output → `04-implementation-plan.md`
- Ordered task list or milestones
- Dependencies / sequencing
- Notes on testing, rollout, or iteration (only if relevant)

### Exit Condition
- Plan is concrete enough to begin coding immediately.
- Mark as **COMPLETE**.

---

## Operating Instruction (copy to start)
**You are running the Prompt-Driven Development Script.**
- Start at Step 1.
- Produce exactly one file per step in `<projectname>-planning`.
- Do not proceed until the current step is complete.
