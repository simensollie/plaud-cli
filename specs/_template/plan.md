# Plan: Spec NNNN — <short title>

Tracer-bullet sequencing. Each phase ends with a user-observable behavior plus the failing test(s) that drove it. Edit freely as you learn.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`.

---

## Phase 0: <bootstrap>

**Outcome:** what the user (or the developer) can do after this phase.

**Failing tests first (red):**
- `<package>/<file>_test.go::Test<Thing>_F<NN>_<aspect>` — what it asserts.

**Code (green):**
- `<package>/<file>.go` — minimal change to pass the test.

**Done when:**
- [ ] Tests in red, then green
- [ ] `go test ./... -race` clean
- [ ] `go vet ./...` clean
- [ ] Manual smoke (if applicable): `<exact command>` produces `<exact observable>`

---

## Phase 1: <next slice>

(Repeat the structure above. Keep phases small. If a phase has more than 3-4 failing tests, it is probably two phases.)

---

## Acceptance walk-through (final sign-off)

Reproduces the spec's §8 acceptance criteria in one session. Performed once all phases are checked.

1. ...
2. ...

When all steps pass on the target platforms, set `Status: Done <YYYY-MM-DD>` in `spec.md`.
