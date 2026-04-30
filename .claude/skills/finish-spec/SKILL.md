---
name: finish-spec
description: Walk a plaud-cli spec's §8 acceptance criteria on a real machine, confirm every phase in plan.md is ticked, then flip Status to Done <today> in spec.md and update the index. Use when every phase is complete and the user is ready to ship the spec. Triggers on phrases like "finish the spec", "mark spec done", "spec acceptance walk", "ship this spec". Repo-specific - only relevant inside plaud-cli.
---

# Finish a spec (plaud-cli)

A spec moves from `Active` to `Done <YYYY-MM-DD>` only after the acceptance criteria walk-through passes on the target platforms. After this point the spec is **immutable** - new work goes in a new spec. So the bar for flipping the status is high.

## Pre-flight

### 1. Confirm every phase is ticked

```bash
grep -nE '^- \[ \]' specs/<spec-id>/plan.md
```

Output should be empty (apart from the acceptance walk-through section, which is not a phase). If anything is unticked, **stop**: use the `finish-phase` skill for the remaining phases first.

### 2. Confirm the gates are clean

```bash
go test ./... -race
go vet ./...
gofmt -l .
goimports -l .
```

All clean. If not, fix before continuing.

## The acceptance walk-through

### 3. Run §8 from spec.md on a real machine

Open `specs/<spec-id>/spec.md` §8 (Acceptance criteria). Run every step in order, on a real binary built from `main`, against real services where the spec calls for it (e.g. a real Plaud.ai account).

The agent cannot type interactive prompts or test on three operating systems. The user runs the walk-through; the agent confirms readiness, prepares the build commands, and watches the output. Specifically:

- If the spec targets multiple platforms (macOS, Linux, Windows) the user must run the walk on each. Do **not** flip status until all platforms pass.
- If a step fails, the spec is not done. Capture the failure in `notes.md` (use the `log-note` skill), then either fix the underlying code (which may mean reopening a phase) or update the spec to reflect reality.

### 4. Confirm `notes.md` is current

Open `specs/<spec-id>/notes.md`. Anything surprising encountered during implementation should be captured. If the user mentions something during the walk-through that is not in `notes.md`, capture it before flipping status.

## Flip status

### 5. Edit spec.md

```markdown
**Status:** Done 2026-MM-DD
**Updated:** 2026-MM-DD
```

Use today's date (ISO 8601). Bump `Updated:` to match.

### 6. Update the index in specs/README.md

Change the spec's row at the bottom of `specs/README.md`:

```markdown
| 000N | <Title> | Done | vX.Y |
```

(Date can be omitted from the index; the spec header is the source of truth.)

### 7. Commit the status flip

```bash
git add specs/<spec-id>/spec.md specs/README.md
git commit -m "$(cat <<'EOF'
Mark spec <id> as Done

Acceptance walk-through passed on <platforms>.

Spec: specs/<spec-id>/
EOF
)"
```

Subject is imperative. Body cites the platforms and links to the spec.

## After Done

The spec is **immutable**. If a bug is discovered later, do not edit the Done spec - open a new spec that supersedes or amends. Per `specs/README.md`:

> | `Done <YYYY-MM-DD>` | Acceptance criteria walked, all phases checked. | Immutable. |

If the work needs to continue (e.g. v0.2 extends v0.1), the next spec gets the next number and references the prior one in its opening paragraph.

## Hard rules (from /CLAUDE.md)

- **`go test -race ./...` passes locally and in CI.**
- **`go vet ./...`, `gofmt -l .`, `goimports -l .` are all clean.**
- **Every phase checkbox is ticked.**
- **The acceptance walk-through passes on every target platform** the spec names.
- **`notes.md` is current.** Anything surprising captured.
- **The agent does not flip status without explicit user confirmation that the walk-through passed.** This is a one-way door.

## What to do if the walk-through fails

The spec is not done. Pick the cheapest path:

1. **The code is wrong** - fix the code, push a new commit, retry the walk.
2. **The spec is wrong** - per `/CLAUDE.md` "When the spec is wrong" section: stop, update the spec, bump `Updated:`, capture why in `notes.md`, then resume.
3. **The acceptance step is too strict** - rewrite the step in §8 to match reality, only if the change does not lower the bar (you cannot weaken acceptance to make a spec ship).

Cost of editing a spec is small. Cost of marking a spec Done when it is not is much larger.
