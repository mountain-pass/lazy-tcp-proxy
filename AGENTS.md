# Agent Instructions

## Core Workflow: Requirements-First Development

Every user request for feature development, bug fixes, or changes MUST follow this workflow. Never skip any step.

---

## Step 1: Requirements Discovery & Documentation

### 1.1 Always Start with a Requirements Check

Before implementing anything, check for existing requirements and understand the request:

1. What specific problem are you trying to solve?
2. What should the end result look like or behave like?
3. Are there any constraints or edge cases to consider?
4. How does this relate to existing requirements?

### 1.2 Read Existing Requirements

- Always read `requirements/_index.md`
- Always list the `requirements/` directory to see all requirement files
- If the directory doesn't exist, create it along with `_index.md` — this is the first requirement
- If it exists, read `_index.md` and summarize current requirements for context before proceeding

### 1.3 Requirement Elicitation

Help the user articulate their requirement by working through these areas:

1. **Functional Requirements**: What exactly should this feature do?
2. **User Experience**: How should users interact with this?
3. **Technical Constraints**: Are there any technical limitations or preferences?
4. **Success Criteria**: How will we know this is working correctly?
5. **Priority**: How important is this relative to existing requirements?
6. **Dependencies**: Does this depend on or affect other features?

### 1.4 Requirement Documentation Template

Each requirement lives in its own file inside `requirements/`.

**Filename format**: `YYYY-MM-DD-short-descriptive-slug.md`

- Use the current date
- Use a short kebab-case slug describing the feature/fix
- The slug should be descriptive enough to be unique across concurrent work
- Examples: `2026-03-18-zod-shared-validation.md`, `2026-03-19-fix-shared-vercel-build.md`

**File template**:

```markdown
# [Short Title]

**Date Added**: [Current Date]
**Priority**: [High/Medium/Low]
**Status**: [Planned/In Progress/Completed/On Hold]

## Problem Statement

[What problem does this solve?]

## Functional Requirements

[What should the system do?]

## User Experience Requirements

[How should users interact with this?]

## Technical Requirements

[Any specific technical constraints or preferences]

## Acceptance Criteria

- [ ] [Specific testable criteria]
- [ ] [Another testable criteria]

## Dependencies

[What this depends on or affects]

## Implementation Notes

[Any technical notes or considerations]
```

---

## Step 2: Requirements File Management

### 2.1 File Structure

Requirements are stored as one file per requirement to avoid git merge conflicts when multiple people (or agents) work simultaneously. Each requirement also gets a companion plan file, created during Phase 2.

```
requirements/
  _index.md                                       # Summary table only
  2026-03-18-zod-shared-validation.md             # Design (requirement)
  2026-03-18-zod-shared-validation-plan.md        # Plan (technical guide)
  2026-03-19-fix-shared-vercel-build.md
  2026-03-19-fix-shared-vercel-build-plan.md
  ...
```

### 2.2 Creating a New Requirement

1. **Git pull first**: Run `git pull` to get the latest state before creating any file
2. **Create the requirement file** in `requirements/` using the format `YYYY-MM-DD-short-slug.md`
3. **Append a row to `requirements/_index.md`** — add to the bottom of the table only
4. **Commit and push** promptly to minimise conflict windows

### 2.3 Index File Structure (`requirements/_index.md`)

The index is a lightweight summary table. `REQ-NNN` IDs are assigned here (next sequential number) and are NOT part of the filename.

```markdown
# Requirements Index

| ID      | Title   | Priority | Status  | Date Added | File                       |
| ------- | ------- | -------- | ------- | ---------- | -------------------------- |
| REQ-001 | [Title] | High     | Planned | 2026-03-18 | [filename.md](filename.md) |
```

Do NOT put counters or summary statistics in the index (e.g. "Total: 5, Completed: 3"). These cause merge conflicts every time anyone adds a requirement. Count rows when stats are needed.

### 2.4 Git Conflict Prevention Rules

1. **Always `git pull`** before creating or editing any file in `requirements/`
2. **One requirement = one file** — never combine multiple requirements into a single file
3. **Append-only index** — only add rows to the bottom of the table in `_index.md`, never reorder
4. **Commit and push promptly** after creating or updating requirement files
5. **Filenames use date + slug**, not sequential numbers, so concurrent requirement creation won't clash
6. If `git push` fails due to conflicts, run `git pull --rebase` and resolve — conflicts should be minimal (adjacent table rows at worst)

---

## Step 3: Design → Plan → Build Workflow

Every feature follows three phases with a human review gate between each. Never advance to the next phase without explicit user approval.

```
Phase 1: Design  →  [STOP: awaits approval]  →  Phase 2: Plan  →  [STOP: awaits approval]  →  Phase 3: Build
```

---

### Phase 1: Design

**Goal**: Fully understand and document the requirement before any technical work begins.

**Process**:

1. Read `requirements/_index.md` and check for existing related requirements
2. Ask clarifying questions to resolve any ambiguities in the request
3. Suggest best-practice alternatives where relevant (but don't impose them)
4. Check whether the request overlaps with or modifies an existing requirement
5. Write the requirement file using the template in Section 1.4

**Output**: `requirements/YYYY-MM-DD-slug.md` — fully populated design document

**Stop gate**: Present the completed design file to the user and ask:
> "Does this capture the requirement correctly? Approve to proceed to the Plan phase, or let me know what needs changing."

Do not begin Phase 2 until the user explicitly approves (e.g. "approved", "looks good", "proceed").

---

### Phase 2: Plan

**Goal**: Produce a detailed technical guide that can be executed mechanically in Phase 3. No code is written yet.

**Process**:

1. Update requirement status to "In Progress" in the requirement file and the index
2. Analyse the codebase to understand what needs to change and where
3. Write the plan file covering all of the following that apply:
   - Step-by-step implementation sequence (ordered list of discrete changes)
   - File-level change summary (which files are created, modified, or deleted)
   - API contracts (endpoints, request/response shapes)
   - Data model diagrams (entity relationships, schema changes)
   - Code snippets for non-obvious or critical logic
   - Unit test cases (inputs, expected outputs, edge cases)
4. Commit and push the plan file

**Output**: `requirements/YYYY-MM-DD-slug-plan.md`

**Plan file template**:

```markdown
# [Short Title] — Implementation Plan

**Requirement**: [link to design file]
**Date**: [Current Date]
**Status**: [Draft/Approved/Implemented]

## Implementation Steps

1. [First discrete change — file, what changes, why]
2. [Second discrete change]
...

## File Change Summary

| File | Action | Description |
|------|--------|-------------|
| `path/to/file.ts` | Modify | [what changes] |
| `path/to/new.ts`  | Create | [what it does] |

## API Contracts

[Endpoint definitions, request/response shapes — omit if not applicable]

## Data Models

[Entity diagrams, schema changes — omit if not applicable]

## Key Code Snippets

[Non-obvious logic, critical algorithms — omit if straightforward]

## Unit Tests

| Test | Input | Expected Output |
|------|-------|-----------------|
| [test name] | [input] | [expected] |

## Risks & Open Questions

[Anything uncertain or worth flagging before implementation]
```

**Stop gate**: Present the completed plan file to the user and ask:
> "Does this plan look correct? Approve to proceed to implementation, or let me know what needs changing."

Do not begin Phase 3 until the user explicitly approves.

---

### Phase 3: Build

**Goal**: Execute the approved plan exactly as written. Do not deviate from the plan without flagging it to the user.

**Process**:

1. Work through the plan's implementation steps in order
2. Call out each step as you begin it (e.g. "Step 3: adding validation to `user.ts`...")
3. If something unexpected arises that requires deviating from the plan, stop and flag it before continuing
4. Run tests after implementation if a test suite exists
5. Verify each acceptance criterion from the design file is met
6. Update the requirement status to "Completed" in the requirement file and the index
7. Update the plan file status to "Implemented"
8. Add implementation notes (actual vs. planned, any deviations) to the plan file
9. Commit and push all changes

**Deviations from plan**: If the plan turns out to be incorrect or incomplete mid-build, pause and describe the discrepancy. Ask the user whether to adjust the plan and re-approve, or proceed with the proposed deviation.

---

## Step 4: Verification & Completion

After Phase 3:

- Confirm each acceptance criterion from the design file is met
- Update the requirement file with final implementation notes
- Mark requirement as "Completed" in both the requirement file and the index
- Update dependency information in related requirement files if needed
- Commit and push all final status updates

---

## Rules

1. Never skip a phase — Design must be approved before Plan, Plan must be approved before Build
2. Never implement without a documented and approved requirement and plan
3. Always read existing requirements (index + relevant files) before starting Phase 1
4. Always `git pull` before creating or editing any file in `requirements/`
5. Always update requirement status at the start and end of Phase 3
6. Always verify acceptance criteria are met before marking a requirement complete
7. Never assume what the user wants — ask clarifying questions during Phase 1
8. Document ALL changes as requirements, no matter how small. Even single-line fixes, minor UX tweaks, copy changes, and styling adjustments must be recorded. If code changed, a requirement must exist for it.
9. Never deviate from the approved plan during Build without pausing to flag the discrepancy and get user input
10. Commit and push requirement and plan file changes promptly to reduce conflict windows
11. Never merge feature branches into local `main` — always use the feature branch → push → PR workflow. Let GitHub merge into `main` via the PR.
12. Before creating a PR, always run `golangci-lint run` from the `lazy-tcp-proxy/` directory and fix all issues. Do not open a PR with lint failures.
13. Before creating a PR, always run `go test ./...` from the `lazy-tcp-proxy/` directory and fix all failures. Do not open a PR with test failures.

---

## Handling Edge Cases

### If the user wants to skip a phase

Acknowledge the urgency. Briefly explain that the phase gates exist to catch misalignments before they become rework. Offer a lightweight version — a minimal design or plan is better than none. Then proceed once the user has approved even a brief version.

If the user explicitly overrides and insists on skipping, note the skip in the requirement file and proceed — their project, their call.

### If the requirements directory is lost or corrupted

1. Acknowledge the issue clearly
2. Ask the user which requirements they remember
3. Recreate `requirements/` and `_index.md` with available information
4. Mark recovered requirements as "Needs Verification"
