# Plan: Spec 0003: Sync

Tracer-bullet sequencing. Phases below are an outline; concrete failing-test names and code paths are filled in once the spec moves to `Status: Active`.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`. This spec depends on spec 0002 being Active or Done.

---

## Phase 0: Sync state file

**Outcome:** `internal/sync/state.go` round-trips the `.plaud-sync.state` JSON safely under SIGINT.

**Failing test stub:**
- `internal/sync/state_test.go::TestState_F03_RoundTrip`
- `internal/sync/state_test.go::TestState_F03_AtomicWriteSurvivesCrashMidWrite`
- `internal/sync/state_test.go::TestState_F13_NoTokenInState`

**Code stub:**
- `internal/sync/state.go`: `Load(root) (*State, error)`, `Save(root, *State) error`, atomic via tmp+rename. Schema versioning baked in.

---

## Phase 1: Reconciliation engine

**Outcome:** Given `[]api.Recording` (from `plaud list`) and an existing state file, produce a list of `Action{recording, kind: fetch|skip|verify|prune}`.

**Failing test stub:**
- `internal/sync/reconcile_test.go::TestReconcile_F01_NewRecordingsScheduledForFetch`
- `internal/sync/reconcile_test.go::TestReconcile_F02_VerifiedRecordingsSkipped`
- `internal/sync/reconcile_test.go::TestReconcile_F09_ServerDeletedSchedulesPrune_OnlyWithFlag`
- `internal/sync/reconcile_test.go::TestReconcile_F10_TrashedHonorsIncludeTrashedFlag`
- `internal/sync/reconcile_test.go::TestReconcile_StaleVersionRefetches` (open question Q5)

**Code stub:**
- `internal/sync/reconcile.go`: pure function, no IO.

---

## Phase 2: Sync runner

**Outcome:** Drives the reconciliation list against `internal/api` + `internal/archive` (from spec 0002) with concurrency, backoff, and per-recording error capture.

**Failing test stub:**
- `internal/sync/runner_test.go::TestRunner_F01_FetchesScheduled`
- `internal/sync/runner_test.go::TestRunner_F06_RespectsConcurrency`
- `internal/sync/runner_test.go::TestRunner_F06_BackoffOn429`
- `internal/sync/runner_test.go::TestRunner_F08_PerRecordingErrorsDoNotAbort`
- `internal/sync/runner_test.go::TestRunner_F04_ResumableAfterSIGINT`

**Code stub:**
- `internal/sync/runner.go`: `Run(ctx, list, state, opts) Result`.

---

## Phase 3: Pruning + trash dir

**Outcome:** `--prune` moves vanished folders into `.trash/<id>/`. Idempotent on repeat.

**Failing test stub:**
- `internal/sync/prune_test.go::TestPrune_F09_MovesToTrash`
- `internal/sync/prune_test.go::TestPrune_F09_DoesNotOverwriteExistingTrash`

---

## Phase 4: Single-instance lock

**Outcome:** Two concurrent `plaud sync` invocations on the same root: one runs, the other exits with a clear message.

**Failing test stub:**
- `internal/sync/lock_test.go::TestLock_F11_SecondInvocationFails`
- `internal/sync/lock_test.go::TestLock_F11_PIDInMessage`

**Code stub:**
- `internal/sync/lock_unix.go` (build tag `unix`) and `internal/sync/lock_windows.go` (build tag `windows`).

**Decision point:** if cross-platform `flock` semantics get hairy, swap for a PID-file approach. Document in `notes.md`.

---

## Phase 5: `plaud sync` command

**Outcome:** Cobra command wiring the runner + lock + state + reconciliation. Smoke covers F-01 through F-08.

**Failing test stub:**
- `cmd/plaud/sync_test.go::TestSync_F01_FirstRunFetchesAll`
- `cmd/plaud/sync_test.go::TestSync_F02_SecondRunNoFetches`
- `cmd/plaud/sync_test.go::TestSync_F12_DryRunNoMutation`
- `cmd/plaud/sync_test.go::TestSync_F09_PruneOnlyWithFlag`

---

## Phase 6: Watch mode

**Outcome:** `plaud sync --watch` loops, syncs, sleeps, exits cleanly on signal.

**Failing test stub:**
- `cmd/plaud/sync_test.go::TestSync_F05_WatchRunsMultipleCycles`
- `cmd/plaud/sync_test.go::TestSync_F05_WatchExitsOnSIGINT`

**Code stub:**
- `cmd/plaud/sync.go`: adds the loop branch.

---

## Acceptance walk-through (final sign-off)

Reproduces `spec.md` §8 against a real account on each target OS.

1. First-run on a fresh `<archive_root>`: every recording fetched, state file created.
2. Second run within minutes: under 5 seconds, no fetches.
3. Delete `2026/04/.../audio.mp3` and re-sync: only that file re-fetched.
4. `kill -INT` mid-sync, re-run: completes without re-fetching already-done recordings.
5. Trash a recording on web.plaud.ai, then `plaud sync --prune`: folder moves to `.trash/<id>/`.
6. `plaud sync --include-trashed` fetches the trashed recording.
7. Two terminals: `plaud sync` in one, `plaud sync` in the other. Second one fails fast with a lock message naming the first PID.
8. `plaud sync --dry-run` on a stale archive: prints would-fetch list without touching files.
9. `plaud sync --watch --interval 1m` for ~5 minutes; upload a new recording in the middle; verify it lands.
10. Repeat 1–9 on macOS, Linux, Windows.
