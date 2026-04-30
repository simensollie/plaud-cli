# Working in plaud-cli

This file tells contributors (humans and Claude) how work happens in this repo. Read it before touching code.

The detailed spec workflow lives in `specs/README.md`. This file covers the principles, conventions, and what NOT to do.

---

## Principles

### 1. Spec-driven, outcome-based

Every change traces to a Functional Requirement ID (e.g. `F-03`) in some `specs/<NNNN>-<slug>/spec.md`. If a change does not fit the active spec, update the spec first or open a new one. **No code without a spec.**

Specs describe *outcomes* (what the user can do, what they observe), not implementation choices. "User can log in with email and OTP code" is an outcome. "Use OAuth 2.0 PKCE" is not.

If you are tempted to start coding before the spec is clear, that is a signal the spec is wrong. Stop, fix the spec, then code.

### 2. Test-driven (TDD), red-green-refactor

For every functional requirement:

1. **Red.** Write a failing test that asserts the FR's outcome. The test name cites the FR ID.
2. **Green.** Make it pass with the smallest change. No bonus features.
3. **Refactor.** Improve the design now that the test is green. Keep the test green.

This is non-negotiable for `internal/` code. CLI plumbing in `cmd/` may be tested via end-to-end command tests rather than per-function unit tests, but each command still has at least one test.

### 3. Fail fast, fail often

- **Surface errors immediately.** Wrap with context (`fmt.Errorf("loading credentials: %w", err)`), do not swallow.
- **No defensive coding for impossible states.** If an internal invariant is violated, `log.Fatalf` or `panic`. Validate at boundaries (HTTP responses, user input, file reads), trust internal calls.
- **Network errors are loud.** Bad regions, expired tokens, 5xx responses surface as exit non-zero with a clear, actionable message. No silent retries beyond what the spec mandates.
- **Tests are loud.** `t.Fatalf` early when preconditions fail. Don't `t.Skip` to make a flake go away. Diagnose.
- **Run tests on every change.** `go test ./...` should be cheap (under 30s for the full suite). If it gets slow, gate slow tests behind `-tags=integration`.

### 4. Outcome over output

"Done" means the spec's acceptance criteria pass on a real machine, not just `go build` succeeded. The `cmd/` smoke walk-through in the spec's plan is the final sign-off, not CI green.

### 5. Stay narrow

The active spec lists what is in scope. Nothing else ships. No half-finished features, no premature abstractions, no surrounding refactors. If you find yourself adding "while I'm here" changes, stop and capture them as future-spec candidates instead.

---

## Repo layout

```
plaud-cli/
├── CLAUDE.md                       # this file
├── README.md                       # user-facing (added in v0.1 final)
├── go.mod, go.sum                  # added when first Go code lands
├── specs/
│   ├── README.md                   # how spec-driven works here
│   ├── _template/                  # copy this when starting a new spec
│   │   ├── spec.md
│   │   ├── plan.md
│   │   └── notes.md
│   └── 0001-auth-and-list/
│       ├── spec.md                 # the spec (outcomes, FRs, acceptance)
│       ├── plan.md                 # implementation phases, TDD-friendly
│       └── notes.md                # captured facts, decisions, gotchas
├── cmd/plaud/                      # CLI entry, subcommand wiring
├── internal/                       # implementation
│   ├── api/                        # Plaud HTTP client
│   ├── auth/                       # credential storage, OTP flow orchestration
│   └── ...                         # added per spec
└── testdata/                       # golden files, recorded HTTP fixtures
```

`internal/` only. No `pkg/` exported library surface until v1.0+ (and only if there is real demand).

---

## Workflow

When you are about to do work:

1. Identify the active spec. Currently: `specs/0001-auth-and-list/`.
2. Read `spec.md` (the *what*) and `plan.md` (the *how, in phases*).
3. Pick the next unfinished phase from `plan.md`.
4. **Write the failing test first**, named with the FR ID it covers.
5. Make it pass with the smallest change.
6. Refactor.
7. Tick the phase's checkbox in `plan.md`. Commit.
8. If you discovered something non-obvious (an undocumented endpoint shape, a region quirk, a flaky test cause), append a short note to `notes.md`.
9. When all phases are checked, walk through the spec's acceptance criteria manually on a real machine. Only then is the spec done.

---

## Test conventions

- Every Functional Requirement has at least one test that names the FR ID, e.g.
  ```go
  func TestLogin_F02_OTPExchangesCodeForToken(t *testing.T) { ... }
  ```
  or
  ```go
  t.Run("F-02: OTP exchanges code for bearer token", func(t *testing.T) { ... })
  ```
- Use `httptest.NewServer` for the API client. Do not hit real Plaud in CI.
- Recorded HTTP fixtures (when used) live under `testdata/http/<spec-id>/`.
- Golden files live under `testdata/golden/<spec-id>/`. Update with a documented `-update` flag, never by hand.
- Integration tests that hit a real account are tagged `//go:build integration` and run locally only, never in CI.
- `go test -race ./...` is part of CI.

---

## Coding conventions

- **Go 1.23+.**
- **`gofmt`, `goimports`, `go vet` clean.** CI fails on diffs.
- **Errors returned, never swallowed.** Wrap with context: `fmt.Errorf("doing X: %w", err)`. Sentinel errors via `errors.Is`.
- **No `init()` for user-visible behavior.** Initialization that the user might need to debug belongs in an explicit `New...` constructor.
- **Constants for endpoint hosts.** `internal/api/regions.go` owns the region → host map. Nothing else hardcodes URLs.
- **Dates:** ISO 8601 in displayed output (`2026-04-30 14:30`), UTC + ISO 8601 in metadata files. Durations as `HH:MM:SS`.
- **English** for code, comments, identifiers, log messages, error strings, tests, and developer-facing docs. CLI output is English in v0.1 (Norwegian comes in a later spec).
- **No em dashes (—)** anywhere in the repo. Use parentheses for asides, commas for natural pauses.
- **No emojis** in code or developer docs. CLI output may use status indicators if a future spec calls for it; nothing else.
- **Comments are rare.** Only when the *why* is non-obvious (a hidden constraint, a workaround for a known Plaud API quirk). Never explain *what* the code does.

---

## Commit conventions

- Imperative mood: "Add OTP login command", not "added" or "adds".
- Cite the FR IDs being addressed in the subject:
  ```
  F-01, F-02: implement region prompt and OTP send
  ```
- In the body, reference the spec folder:
  ```
  Spec: specs/0001-auth-and-list/
  Phase: 2 (OTP send + verify)
  ```
- Each commit leaves the repo green: `go test ./... && go vet ./... && gofmt -l . | wc -l` is `0`.
- Squash WIP commits before merging to `main`.

---

## What NOT to do

- **Do not add features not listed in the active spec's §2 / §3.** If you think it belongs, update the spec first.
- **Do not introduce dependencies** beyond what the spec's tech stack section lists. Adding a dep is a spec change.
- **Do not stub function returns to "test" the API client.** Use `httptest` so the real HTTP layer is exercised.
- **Do not run integration tests against real Plaud in CI.** They leak credentials and hit the real API. Local-only, build-tagged.
- **Do not commit `// TODO:` or `// FIXME:`.** Open an issue or update the spec.
- **Do not bypass `gofmt` / `go vet`** with `//nolint`-style escapes. Fix the underlying code.
- **Do not write tests that assert on log strings** unless the log format is part of the spec. Logs are debugging aids, not contracts.
- **Do not catch errors broadly to "make tests pass".** If a test surfaces a real error, fix the cause.
- **Do not use em dashes.**
- **Do not write multi-paragraph docstrings or comment blocks.** One short line max.

---

## Definition of done (for a spec)

A spec moves from `Active` to `Done` when:

1. Every FR has at least one test, and the test cites the FR ID.
2. `go test -race ./...` passes locally and in CI.
3. `go vet ./...`, `gofmt -l .`, `goimports -l .` are all clean.
4. Every phase checkbox in `plan.md` is ticked.
5. The spec's acceptance criteria walk-through passes on macOS, Linux, and Windows (or the spec explicitly limits platforms).
6. `notes.md` is current. Anything surprising is captured.
7. The spec's `Status` field is set to `Done <YYYY-MM-DD>`. After this point the spec is immutable; new work goes in a new spec.

---

## When the spec is wrong

Specs are written ahead of full knowledge. They will be wrong sometimes. When you discover a spec is wrong:

1. Stop coding.
2. Update the spec. Bump the `Updated:` date. If the change is large enough to break tests already written against the old version, treat it as a breaking spec change and bump the spec's status note accordingly.
3. Capture *why* in `notes.md`.
4. Resume.

The cost of editing a spec is small. The cost of code that does not match the spec it claims to fulfill is much larger.
