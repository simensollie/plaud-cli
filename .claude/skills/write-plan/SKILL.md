---
name: write-plan
description: Author or extend the plan.md for a plaud-cli spec, breaking work into tracer-bullet phases that each begin with a failing test. Use when a spec has been signed off and the user wants to plan the implementation, or when adding a new phase to an existing plan. Triggers on phrases like "write the plan", "add a phase", "plan the implementation". Repo-specific - only relevant inside plaud-cli.
---

# Write or extend a plan (plaud-cli)

A plan turns a spec's functional requirements into a sequence of tracer-bullet phases. Each phase delivers user-observable value end-to-end and starts with a failing test that cites an FR ID. Plans are working documents - edit freely as you learn.

## When to use

- A new spec has just been signed off and `plan.md` is still mostly empty.
- An existing plan needs another phase appended because the work expanded.
- A phase was found to be too large during implementation and needs splitting.

## Steps

### 1. Identify the active spec

```bash
ls specs/ | grep -E '^[0-9]{4}-' | sort
```

Open the candidate's `spec.md`. Confirm `Status: Active` (or `Draft` if writing the plan as part of the proposal). If `Status: Done`, stop - that spec is immutable. New work goes in a new spec.

### 2. Read the spec's §3 (Functional requirements)

The phases are ordered to deliver FRs in tracer-bullet sequence: each phase a thin end-to-end slice that the user can observe. Group FRs that naturally land in the same slice; do not pad phases just to spread them out.

### 3. Write each phase using the template structure

Per `specs/README.md` and `specs/_template/plan.md`:

```markdown
## Phase N: <short name>

**Outcome:** what the user (or developer) can do after this phase. One sentence.

**Failing tests first (red):**
- `<package>/<file>_test.go::Test<Thing>_F<NN>_<aspect>` - what it asserts.

**Code (green):**
- `<package>/<file>.go` - minimal change to pass the test.
- `cmd/plaud/<cmd>.go` - if the phase adds CLI surface.

**Done when:**
- [ ] Tests in red, then green
- [ ] `go test ./... -race` clean
- [ ] `go vet ./...` clean
- [ ] Manual smoke (if applicable): `<exact command>` produces `<exact observable>`
```

Rules of thumb:

- **Each phase ends with a user-observable behavior.** "Refactor the API client" alone is not a phase. "User can run `plaud list` and see one page of recordings" is.
- **Each phase has 1-3 failing tests.** More than 3-4 means it is probably two phases.
- **Test names cite the FR ID.** `TestLogin_F02_OTPExchangesCodeForToken` or `t.Run("F-02: OTP exchanges code for bearer token", ...)`.
- **Smallest change to green.** No bonus features. Refactor *after* the test is green.
- **Code (green) lists files, not pseudo-code.** The actual files that will land in this phase. If you need to invent a file path, fine - but do not list types/methods.

### 4. Phase 0 is bootstrap

For the first plan in a spec, Phase 0 is usually repo or surface scaffolding (module init, root command, CI workflow). Even Phase 0 has at least one failing test (e.g. `TestRoot_F11_HelpStatesUnofficial`). No phase is too small to skip TDD.

### 5. Final phase: cross-platform smoke and release (when applicable)

If the spec is shipping a release, the last phase is the cross-platform walk-through plus a `goreleaser` or equivalent. The "Done when" checklist must include all of §8's acceptance criteria from `spec.md`.

### 6. Acceptance walk-through section

After the last numbered phase, copy the spec's §8 into a "Acceptance walk-through (final sign-off)" section at the bottom of the plan. This is the script you run on a real machine after every phase checkbox is ticked. When all steps pass, the `finish-spec` skill flips `Status:` to `Done <date>`.

## Hard rules (from /CLAUDE.md)

- **No em dashes.**
- **No `// TODO` references.** If a phase needs more thought, capture it in `notes.md` instead.
- **Failing test first, every time.** A phase that says "implement X" without naming the failing test is wishful thinking, not a plan.
- **English only** for plan content.
- **Files in `internal/`** unless the spec explicitly authorizes a `pkg/` surface. v1.0+ before any `pkg/`.

## Common mistakes to avoid

- **Phase boundaries on technical layers** ("Phase 1: data layer, Phase 2: API layer"). Phases are vertical slices, not horizontal layers.
- **Plans without failing tests for `cmd/` phases.** CLI plumbing tests via end-to-end command tests (driven stdin / captured stdout), not skipped.
- **Dropping the manual smoke.** "go test passes" is not enough. Each phase that adds CLI surface has a manual smoke command + observable.
- **Listing every helper function under "Code (green)".** List the files; the helpers are an implementation detail.

## Example: existing plan

See `specs/0001-auth-and-list/plan.md` for the canonical shape. Note how Phase 1 is just region constants + HTTP client skeleton (no network yet), and Phase 2 introduces real OTP traffic. That is tracer-bullet sequencing in practice.
