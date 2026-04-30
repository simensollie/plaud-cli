---
name: finish-phase
description: Verify a phase is genuinely done in plaud-cli - run tests, vet, gofmt, and the phase's manual smoke command, then tick the checkboxes in plan.md and prepare a commit citing the FR IDs. Use when the user says a phase is finished, wants to mark a phase done, or is about to commit phase work. Triggers on phrases like "finish this phase", "phase N done", "tick the phase". Repo-specific - only relevant inside plaud-cli.
---

# Finish a phase (plaud-cli)

A phase is "done" only when its checkboxes pass on a real machine. This skill walks the verification checklist, ticks the boxes in `plan.md`, and stages the commit per the project's commit convention.

Per `/CLAUDE.md`: **"Done" means the spec's acceptance criteria pass on a real machine, not just `go build` succeeded.** This skill enforces that.

## Steps

### 1. Identify the phase

Open `specs/<active-spec>/plan.md`. Find the phase with unticked checkboxes whose code has just been written. If unsure which phase the user means, ask before proceeding.

### 2. Run the project gates

In parallel, run the four gates that every commit must pass:

```bash
go test ./... -race
go vet ./...
gofmt -l .
goimports -l .
```

If any fail, **stop**. Surface the failure to the user with the actual output. Do not paper over with `t.Skip` or by deleting the failing assertion. Per `/CLAUDE.md`: tests are loud, errors surface immediately.

### 3. Run the phase's manual smoke

Each phase's "Done when" section names a manual smoke command and an exact observable, e.g.:

```
- [ ] Manual smoke: `./plaud --version` prints v0.1.0
```

Run it. Verify the output matches. If the smoke is a multi-step interactive walk (e.g. `plaud login` against a real Plaud account), the user must run it - the agent cannot type interactive prompts. Ask the user to run it and paste the result.

### 4. Tick the boxes

Once every gate and the smoke pass, edit `plan.md`: change `- [ ]` to `- [x]` on each line under the phase's "Done when". Do **not** tick a box that did not actually pass. Better to leave it unticked and capture why in `notes.md`.

### 5. Stage the commit

Per `/CLAUDE.md` commit conventions:

- Imperative mood ("Add OTP login command").
- Subject cites FR IDs: `F-01, F-02: implement region prompt and OTP send`.
- Body references spec folder and phase:
  ```
  Spec: specs/0001-auth-and-list/
  Phase: 2 (OTP send + verify)
  ```
- Each commit must leave the repo green: gates from step 2 all clean.

Use the project's `commit-commands:commit` skill or stage and commit manually:

```bash
git add specs/<spec>/plan.md <implementation files>
git commit -m "$(cat <<'EOF'
F-NN, F-MM: <short subject>

Spec: specs/<spec-id>/
Phase: N (<short name>)
EOF
)"
```

Do **not** squash WIP commits at this stage; squashing happens before merge to `main`.

### 6. If the phase surfaced something non-obvious, log a note

Did this phase reveal something a future contributor would need to know? An undocumented API shape, a Windows-specific path quirk, a workaround for a Plaud bug? Use the `log-note` skill to append a dated entry to `notes.md` before moving on.

## Hard rules (from /CLAUDE.md)

- **No bypassing gates.** No `//nolint`, no `--no-verify`. If a hook fails, fix the cause.
- **No `t.Skip` to make a flake go away.** Diagnose the cause.
- **No half-finished phases.** A phase that builds but the smoke fails is not done.
- **No surrounding refactors.** Stay narrow. "While I'm here" changes go in their own future commit (or new spec).

## What "manual smoke" means in this repo

For phases that touch CLI surface, "manual smoke" is the actual command typed in a real shell, with the actual observable - not a `go test` output. The cmd-level test asserts the wiring; the smoke proves it on a real binary. For phases that are purely internal (`internal/api/`, `internal/auth/`), the unit tests are sufficient and the "Manual smoke" line may be omitted from the plan.

## After all phases are ticked

When every phase in `plan.md` has all its boxes ticked, the next step is the spec's acceptance walk-through. Use the `finish-spec` skill for that.
