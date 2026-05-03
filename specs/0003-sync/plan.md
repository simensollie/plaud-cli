# Plan: Spec 0003: Sync

Tracer-bullet sequencing. Phases below are an outline; concrete failing-test names and code paths are filled in once the spec moves to `Status: Active`.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`. This spec depends on spec 0002 being Active or Done.

---

## Phase 0: Empirical probes (gates Phase 2 and Phase 5)

Two probes captured in `notes.md` before the reconciler or the lock land. Each is one page or less.

**Probe A — `version` / `version_ms` behavior (spec.md §7 Q3, gates F-15 Layer B):**

1. List a known recording; capture `version` / `version_ms`.
2. In web.plaud.ai, rename a speaker in that recording's transcript. Re-list. Did the value change?
3. Upload a fresh recording. Capture `version` / `version_ms` while `is_trans=false`. Wait for the pipeline. Re-list when `is_trans=true`. Did the value change on that boundary?
4. If web.plaud.ai exposes a "regenerate summary" affordance, exercise it and re-list.

If `version` bumps reliably on user-side edits, Layer B ships per F-15. Otherwise, document the gap in `docs/user/sync.md` and remove the Layer B test from Phase 2.

**Probe B — `flock` portability (spec.md §7 Q1, gates Phase 5):**

1. 30-line Go program that opens `.plaud-sync.state` with an exclusive lock.
2. Run two copies on macOS, Linux, and Windows. Confirm the second blocks (or returns `EAGAIN`) and that termination releases the lock.
3. If any platform misbehaves, design the PID-file fallback in `notes.md` and proceed to Phase 5 with that fallback (with `signal 0` on Unix and `OpenProcess` on Windows for stale-detection).

---

## Phase 0a: Extract fetch primitive

**Outcome:** `internal/fetch/fetch.go` exposes `FetchOne(ctx, client, audioClient, root, rec, opts) FetchResult`. `cmd/plaud/download.go` becomes a thin caller wrapping `FetchOne` in its existing worker pool. `internal/api/redact.go` exists (extracted from any spec 0001 sanitizer if present, or written fresh).

This is a behavior-preserving refactor of spec 0002 territory; required before Phase 3 can drive recordings through the same primitive without duplication.

**Failing test stub:**
- `internal/fetch/fetch_test.go::TestFetchOne_HappyPath` (uses `httptest.NewServer`)
- `internal/fetch/fetch_test.go::TestFetchOne_RespectsContextCancel`
- `internal/api/redact_test.go::TestRedact_F13_StripsSignedURLQueryString`
- `internal/api/redact_test.go::TestRedact_F13_StripsBearerJWT`

**Definition of done:**
- `go test ./...` and `go vet ./...` green.
- `cmd/plaud/download_test.go` passes unmodified.
- `processRecording`'s body is gone from `cmd/plaud/download.go`; the file delegates to `fetch.FetchOne`.

---

## Phase 1: Sync state file

**Outcome:** `internal/sync/state.go` round-trips the minimal-index `.plaud-sync.state` JSON safely under SIGINT.

**Failing test stub:**
- `internal/sync/state_test.go::TestState_F03_RoundTrip`
- `internal/sync/state_test.go::TestState_F03_FolderPathIsRelativeToArchiveRoot`
- `internal/sync/state_test.go::TestState_F04_AtomicWriteSurvivesCrashMidWrite`
- `internal/sync/state_test.go::TestState_F03_MissingStateFileTreatedAsFreshIndex`
- `internal/sync/state_test.go::TestState_F13_LastErrorRedactedOnSave`

**Code stub:**
- `internal/sync/state.go`: `Load(root) (*State, error)`, `Save(root, *State) error`, atomic via tmp+rename. Schema versioning baked in.

---

## Phase 2: Reconciliation engine

**Outcome:** Given `[]api.Recording` (from `client.List` with `is_trash=0` *and* `is_trash=1`) and an existing state, produce a list of `Action{recording, kind: fetch|skip|verify|prune|rename|partial-fetch}`.

**Failing test stub:**
- `internal/sync/reconcile_test.go::TestReconcile_F01_NewRecordingsScheduledForFetch`
- `internal/sync/reconcile_test.go::TestReconcile_F02_VerifiedRecordingsSkipped`
- `internal/sync/reconcile_test.go::TestReconcile_F09_ServerDeletedSchedulesPrune_OnlyWithFlag`
- `internal/sync/reconcile_test.go::TestReconcile_F09_UnionOfTrashFiltersExcludesWebUITrashed`
- `internal/sync/reconcile_test.go::TestReconcile_F09_MassDeletionGuardRefusesWithoutPruneEmpty`
- `internal/sync/reconcile_test.go::TestReconcile_F10_TrashedHonorsIncludeTrashedFlag`
- `internal/sync/reconcile_test.go::TestReconcile_F14_DefaultIncludeIsTextOnly`
- `internal/sync/reconcile_test.go::TestReconcile_F15_LayerA_PresenceFlipReFetchesNewlyAvailableArtifact`
- `internal/sync/reconcile_test.go::TestReconcile_F15_LayerB_VersionBumpReFetches` (gated on Phase 0 Probe A)
- `internal/sync/reconcile_test.go::TestReconcile_F16_RenameDetectedViaIDAndRelativePath`

**Code stub:**
- `internal/sync/reconcile.go`: pure function, no IO.

---

## Phase 3: Sync runner

**Outcome:** Drives the reconciliation list against `fetch.FetchOne` (Phase 0a) with concurrency, transport-layer backoff, per-recording error capture, and per-recording state writes.

**Failing test stub:**
- `internal/sync/runner_test.go::TestRunner_F01_FetchesScheduled`
- `internal/sync/runner_test.go::TestRunner_F06_RespectsConcurrencyClamp`
- `internal/sync/runner_test.go::TestRunner_F06_BackoffOn429AtTransportLayer`
- `internal/sync/runner_test.go::TestRunner_F06_HonorsRetryAfterCappedAt30s`
- `internal/sync/runner_test.go::TestRunner_F06_5xxNotRetried`
- `internal/sync/runner_test.go::TestRunner_F08_PerRecordingErrorsDoNotAbort`
- `internal/sync/runner_test.go::TestRunner_F04_StateWrittenAfterEveryRecording`
- `internal/sync/runner_test.go::TestRunner_F04_ResumableAfterSIGINT`
- `internal/sync/runner_test.go::TestRunner_F04_DoubleSIGINTHardExit`
- `internal/sync/runner_test.go::TestRunner_F16_RenameMovesFolder`
- `internal/sync/runner_test.go::TestRunner_F16_RenameCollisionUsesIDSuffix`

**Code stub:**
- `internal/sync/runner.go`: `Run(ctx, list, state, opts) Result`.
- `internal/api/backoff.go`: `RoundTripper` wrapper (with sleep injection for tests).

---

## Phase 4: Pruning + trash dir

**Outcome:** `--prune` moves vanished folders into `.trash/<id>/`. Idempotent on repeat. Mass-deletion guards in place.

**Failing test stub:**
- `internal/sync/prune_test.go::TestPrune_F09_MovesToTrash`
- `internal/sync/prune_test.go::TestPrune_F09_CollisionAppendsTimestampSuffix`
- `internal/sync/prune_test.go::TestPrune_F09_DoesNotOverwriteExistingTrash`
- `internal/sync/prune_test.go::TestPrune_F09_EmptyServerGuardRefuses`
- `internal/sync/prune_test.go::TestPrune_F09_FiftyPercentGuardRefuses`
- `internal/sync/prune_test.go::TestPrune_F09_PruneEmptyBypassesGuards`

---

## Phase 5: Single-instance lock

**Outcome:** Per-cycle write lock on the state file plus advisory watch sentinel. Two concurrent `plaud sync` invocations: one runs, the other exits with a clear structured message.

**Failing test stub:**
- `internal/sync/lock_test.go::TestLock_F11_SecondInvocationFails`
- `internal/sync/lock_test.go::TestLock_F11_StructuredContentionMessage`
- `internal/sync/lock_test.go::TestLock_F11_StaleLockTakenWhenPIDDead`
- `internal/sync/lock_test.go::TestLock_F11_WatchReleasesLockBetweenCycles`
- `internal/sync/lock_test.go::TestLock_F11_TwoWatchesDetectedViaSentinel`

**Code stub:**
- `internal/sync/lock_unix.go` (build tag `unix`) and `internal/sync/lock_windows.go` (build tag `windows`).
- `internal/sync/watch_sentinel.go`: `.plaud-sync.watch` advisory file (records `pid`, `hostname`, `started_at_utc`).

**Decision point:** Phase 0 Probe B may swap the OS-native flock for a PID-file fallback with explicit stale-detection. Document in `notes.md`.

---

## Phase 6: `plaud sync` command

**Outcome:** Cobra command wiring the runner + lock + state + reconciliation. NDJSON event emitter with surface-level redaction.

**Failing test stub:**
- `cmd/plaud/sync_test.go::TestSync_F01_FirstRunFetchesAll`
- `cmd/plaud/sync_test.go::TestSync_F02_SecondRunNoFetches`
- `cmd/plaud/sync_test.go::TestSync_F07_NDJSONEventEnvelope`
- `cmd/plaud/sync_test.go::TestSync_F07_DoneFiresOnSIGINTWithStatusInterrupted`
- `cmd/plaud/sync_test.go::TestSync_F12_DryRunNoMutation`
- `cmd/plaud/sync_test.go::TestSync_F09_PruneOnlyWithFlag`
- `cmd/plaud/sync_test.go::TestSync_F13_NoSignedURLLeaksToNDJSON`
- `cmd/plaud/sync_test.go::TestSync_F13_NoBearerTokenLeaksToStderr`
- `cmd/plaud/sync_test.go::TestSync_F14_DefaultIncludeOmitsAudio`

**Code stub:**
- `cmd/plaud/sync.go`: command surface, flag parsing, NDJSON emitter.
- `internal/sync/events.go`: event taxonomy + envelope + surface-level redaction.

---

## Phase 7: Watch mode

**Outcome:** `plaud sync --watch` loops, syncs, sleeps, exits cleanly on signal, bounded retry on consecutive failures.

**Failing test stub:**
- `cmd/plaud/sync_test.go::TestSync_F05_WatchRunsMultipleCycles`
- `cmd/plaud/sync_test.go::TestSync_F05_WatchExitsOnSIGINT`
- `cmd/plaud/sync_test.go::TestSync_F05_WatchExitsOnSIGTERM`
- `cmd/plaud/sync_test.go::TestSync_F05_WatchIntervalIsSleepDurationNotWallClock`
- `cmd/plaud/sync_test.go::TestSync_F05_WatchExitsAfter5ConsecutiveFailures`

---

## Acceptance walk-through (final sign-off)

Reproduces `spec.md` §8 against a real account on each target OS.

1. First-run on a fresh `<archive_root>`: every recording fetched (text-only by default), state file created.
2. Second run within minutes: under 5 seconds, no fetches.
3. Delete `2026/04/.../transcript.json` and re-sync: only that file re-fetched.
4. `kill -INT` mid-sync, re-run: completes without re-fetching already-done recordings.
5. Permanently delete a recording on web.plaud.ai, then `plaud sync --prune`: folder moves to `.trash/<id>/`. Trash a recording on web.plaud.ai (web-UI trash, not deletion), then `plaud sync --prune`: the recording is *not* moved to `.trash/`.
6. `plaud sync --include-trashed` fetches the web-UI-trashed recording.
7. Two terminals: `plaud sync --watch` in one, ad-hoc `plaud sync` in the other. The ad-hoc waits at most one cycle for the watch to release between cycles, then runs cleanly.
8. Two terminals both running `plaud sync --watch`: the second exits with a structured advisory message naming the first.
9. `plaud sync --dry-run` on a stale archive: prints would-fetch list without touching files; `last_run_started` advances.
10. `plaud sync --watch --interval 1m` for ~5 minutes; upload a new recording in the middle; verify it lands.
11. Server-side rename: change a recording's filename in web.plaud.ai, run sync; the local folder moves to the new slug-derived path.
12. Inject a redaction trigger (e.g. force an audio fetch failure carrying a signed URL): `.plaud-sync.state`, NDJSON, and stderr contain no `X-Amz-Signature`, `X-Amz-Credential`, or `Bearer eyJ` substrings.
13. Mass-deletion guard: simulate an empty server response (or an archive shrink >50%); `--prune` refuses; `--prune-empty` proceeds.
14. `--include audio` after text-only sync: re-running with `--include audio,transcript,summary,metadata` only fetches the missing audio bytes (verified via `--format json` showing `fetched` events whose `details.artifacts` contain only `audio.mp3`).
15. Repeat 1-14 on macOS, Linux, Windows.
