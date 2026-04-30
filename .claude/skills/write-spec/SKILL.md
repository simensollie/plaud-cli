---
name: write-spec
description: Bootstrap a new spec under specs/000N-<slug>/ in the plaud-cli repo from _template/. Use when the user wants to start a new spec, draft a new feature, or capture a new outcome before any code is written. Triggers on phrases like "new spec", "start a spec", "draft a spec", "spec for X". Repo-specific - only relevant inside plaud-cli.
---

# Write a new spec (plaud-cli)

A spec captures *what* a change delivers, in outcomes, before any code. The conventions live in `specs/README.md` and `/CLAUDE.md`. This skill walks the mechanical part: pick a number, copy the template, fill the required sections, leave plan/notes empty.

## Steps

### 1. Pick the next number and slug

```bash
ls specs/ | grep -E '^[0-9]{4}-' | sort | tail -1
```

Increment the leading 4-digit number. Numbers are monotonic and never re-used. Slug is short, lowercase, hyphenated, human-friendly (e.g. `0002-download-recordings`).

### 2. Copy the template

```bash
cp -r specs/_template specs/000N-<slug>
```

Do not edit `_template/` itself.

### 3. Fill `spec.md`

Open `specs/000N-<slug>/spec.md` and fill the required sections. **Outcomes only, no implementation choices.**

- **Header**: `Status: Draft`, `Created:` and `Updated:` to today's date (ISO 8601), `Owner: @<github-handle>`, `Target version: vX.Y`.
- **Opening paragraph**: one paragraph. What this delivers, why now. State scope you are *not* doing if it could be misread.
- **§1 Goal**: exactly one sentence. If you cannot reduce to one sentence, the spec is too big - split it.
- **§2 Commands / interfaces**: every command, flag, and observable output, in a table. If no surface change, write "None - internal change only" and explain in §3 what proves the change works.
- **§3 Functional requirements**: numbered table with stable IDs `F-NN` (within this spec). Outcome-shaped. Priorities: `Must` / `Should` / `Could`. Tests will cite these IDs.
- **§4 Storage / data model**: directory tree + canonical file formats, or "None."
- **§5 Tech stack**: deps added or changed. Otherwise "Unchanged from prior specs." Adding a dep is a spec change.
- **§6 Out of scope**: explicit and generous. Easier to expand later than argue creep now.
- **§7 Open questions**: numbered table with current best-guess. Drive to closure before `Status: Active`.
- **§8 Acceptance criteria**: concrete, runnable, observable walk-through. The spec is not done until every step here passes on the target platforms.

### 4. Leave plan.md and notes.md mostly empty

Open `plan.md` and update only the title (`# Plan: Spec NNNN - <short title>`). Leave the phase scaffolding for the `write-plan` skill or for after the spec is signed off.

Open `notes.md`, set the title, and add the opening dated entry under "Spec opened" with any decisions made before code (e.g. "binary will be named `plaud`, not `plaudr`").

### 5. Update the index

Add a row to the table at the bottom of `specs/README.md`:

```markdown
| 000N | <Title> | Draft | vX.Y |
```

### 6. Open as a spec-only PR

Per the workflow: open a PR with **the spec only** (no code). Get sign-off. Then implement.

## Hard rules (from /CLAUDE.md)

- **No em dashes anywhere.** Use parentheses for asides, commas for natural pauses.
- **No emojis** in spec, plan, or notes.
- **English only** for spec content (Norwegian CLI output comes in a later spec).
- **ISO 8601 dates**: `2026-04-30`, never `April 30, 2026` or `30/04/2026`.
- **Outcomes, not implementation.** "User can log in with email and OTP code" belongs in §3. "Use Cobra for the CLI" belongs in §5 or in `plan.md`.
- **No `// TODO`, no aspirational FRs.** If you do not know it yet, file it as an Open Question in §7.

## Anti-patterns (from specs/README.md)

- Bullet-point feature lists posing as a spec. A spec without acceptance criteria is not a spec.
- "We will probably need eventually" items in scope. Out of scope, every time. New spec when you actually need it.
- Implementation details disguised as functional requirements. "Cobra command tree" is not an FR. "User can run `plaud login` and is prompted for region" is.

## Example: how the existing 0001 spec opens

See `specs/0001-auth-and-list/spec.md` for the canonical shape. The opening paragraph there is a good model: it states the goal, names what it is *not* doing in v0.1, and points forward to where deferred work lives.
