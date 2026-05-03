# Notes: Spec 0003: Sync

Append-only journal. Newest entry on top.

For the convention, see `specs/README.md`.

---

## 2026-05-04: Status flipped Draft → Active

All eight phases of the plan landed in commit `6e4f8fb` (`F-01..F-16: implement spec 0003 sync end-to-end`). `go test -race ./...` green; cross-compiles green for linux/darwin/windows × amd64/arm64; `go vet`, `gofmt` clean. 252 tests, 0 failures.

Status flipped to Active. Three preconditions remain before Done:

1. **Probe A** (spec.md §7 Q3): real-account `version`/`version_ms` observation across (a) pipeline completion, (b) speaker rename in web.plaud.ai, (c) summary regen. Until run, F-15 Layer B is wired but disabled (`ReconcileOptions.EnableVersionLayer` defaults to false). The `Reconcile_F15_LayerB_VersionBumpReFetches` test passes but is gated behind that flag.
2. **Cross-OS lock validation** (spec.md §7 Q1): the Phase 0 spike covered Linux only. macOS (BSD flock) is expected to behave identically; Windows (`LockFileEx`) needs a live reproduction of the contention + auto-release semantics. `TestLock_F11_*` runs on Linux today.
3. **§8 acceptance walk** on macOS, Linux, and Windows against a real Plaud account using the platform binaries from the GitHub release (acceptance criterion 15).

Until those three close, the spec is Active rather than Done; new feature work flows through a follow-up spec rather than amendments here.

---

## 2026-05-03: Phase 0 probes

**Probe B — `flock` portability (Linux, partial):**

A 30-line spike (`syscall.Flock(fd, LOCK_EX)` / `LOCK_EX|LOCK_NB`) ran cleanly on Linux:

- Blocking acquisition under contention: second invocation blocks until the holder releases (~2.7s wait observed for a 3s hold).
- Non-blocking under contention: returns `EAGAIN` ("resource temporarily unavailable") immediately. Maps to a clean structured contention message.
- Auto-release on SIGKILL: the next non-blocking attempt acquires within microseconds. Kernel-managed; no PID-file fallback needed for crash recovery on Linux.

Phase 5 implementation can target `syscall.Flock` directly. macOS uses the same BSD flock semantics and is expected to behave identically. Windows uses `LockFileEx` with similar semantics; deferred verification to the cross-OS acceptance walk-through (§8.15). PID-file fallback design remains documented in the plan as a contingency.

**Probe A — `version` / `version_ms` behavior:** deferred. Requires a real Plaud account with the ability to (a) rename a speaker in the web UI, (b) trigger a fresh upload and observe the `is_trans=false → true` boundary, (c) regenerate a summary if Plaud exposes that affordance. Captured as a known precondition for shipping F-15 Layer B; until run, the `Reconcile_F15_LayerB_VersionBumpReFetches` test is gated and the corresponding code path is wired but defaults to "Layer B disabled" via a build flag-equivalent boolean. Document the gap in `docs/user/sync.md` when the docs land.

---

## 2026-05-03: Grilling pass — spec rewritten end-to-end

Walked the design tree via `/grill-me`. Eleven branches resolved; `spec.md` and `plan.md` updated. Key shifts from the 2026-05-01 draft:

- **State file demoted to a minimal index** (`schema_version`, `last_run_*`, per-id `version` + `folder_path` + `last_error`). Per-recording `metadata.json` is canonical for artifact-level truth (hashes, sizes, presence, `fetched_at`, `last_verified_at`). The original F-03 stored hashes in two places, which would have drifted on any manual recovery; the demoted shape is recovery-safe (`rm .plaud-sync.state` rebuilds cleanly from surviving `metadata.json` files plus the list response).
- **Default `--include` for `plaud sync` is text-only** (`transcript,summary,metadata`). §1 goal updated to match. Audio is opt-in via `PLAUD_DEFAULT_INCLUDE` or per-invocation flag. `plaud download` and `plaud sync` are two commands with two different jobs; the inherited spec 0002 default fits sync.
- **Three new FRs.** F-14 (default include), F-15 (layered staleness: presence-flip + version-bump), F-16 (rename detection via stable id and relative `folder_path`).
- **Five new acceptance criteria.** Server-side rename, redaction, two-watch detection, mass-deletion guard, and `--include audio` on a previously text-only archive (§8.10–§8.14).
- **Phase 0 is now empirical probes** (was: state-file work). Probe A gates F-15 Layer B (does Plaud's `version` cooperate?); Probe B gates Phase 5 (is `flock` portable?). State file moved to Phase 1.
- **Phase 0a inserted: extract `internal/archive/fetch.go`** from `cmd/plaud/download.go::processRecording`. Shared per-recording primitive between `plaud download` and `plaud sync`. Behavior-preserving refactor of spec 0002 territory. Also extracts/creates `internal/api/redact.go` (F-13 source-level scrub).
- **Lock granularity is per-cycle**, not per-process. Watch releases between cycles, so ad-hoc `plaud sync` during a long watch session waits at most one cycle's duration. Two-watch detection lives in a separate `.plaud-sync.watch` advisory sentinel (records `pid`, `hostname`, `started_at_utc`).
- **Backoff is transport-layer** (`http.RoundTripper` wrapper inside `internal/api/`). 5 retries max per call; honors `Retry-After` capped at 30s. Spec 0002 F-15 acquired a one-sentence amendment to acknowledge that 429 now retries (5xx is unchanged). Worker-local; no cross-worker coordination in v0.3 (deferred to v0.4 if Plaud rate-limits prove aggressive).
- **NDJSON contract locked field-level at v0.3.0 GA.** Per-recording granularity. Separate `would-*` event names for `--dry-run` (explicit beats a `dry_run: true` flag — easier to grep for in shell scripts; harder to misuse). `done` always fires, including on SIGINT (carries `details.status: "interrupted"`).
- **`--prune` got real safety nets.** "Missing on server" requires absence from the *union* of `is_trash=0` and `is_trash=1` list calls (web-UI-trashed recordings are not pruned locally). New `--prune-empty` flag bypasses two guards: (a) empty server response with non-empty local archive, (b) >50% local-archive shrink in one run. Closing original §7 Q3 (interactive prune confirmation) — the guard plus `.trash/` recovery is the safety net.
- **Redaction is defense in depth.** Source-level scrub in `internal/api/redact.go` (signed S3 URLs, JWT-shaped bearer tokens, JSON `"token":"..."`). Surface-level scrub at state, NDJSON, and stderr emitters as a backstop. F-13 acceptance test asserts no signed-URL or `Bearer eyJ` substrings appear anywhere.
- **Signal lifecycle pinned.** SIGTERM = SIGINT (clean drain). Second SIGINT within 2 seconds = hard exit. Watch interval is sleep-duration (cycle-end → next cycle-start), not wall clock. Watch exits non-zero after 5 consecutive failed cycles.

**Open empirical work (now visible in §7 and Phase 0):**

- Probe A: `version` / `version_ms` behavior on (a) transcription pipeline completion, (b) speaker rename in web.plaud.ai, (c) summary regeneration. If `version` doesn't bump on user-side edits, F-15 Layer B is documented as a known gap and the `LayerB_VersionBumpReFetches` test gets removed from Phase 2.
- Probe B: cross-platform `flock` validation. PID-file fallback is designed but only ships if a platform misbehaves.
- During Phase 0a, check whether spec 0001 already has a sanitizer worth promoting to `internal/api/redact.go` rather than writing fresh code.

**Why we touched spec 0002:** F-15 acquired one sentence acknowledging that 429 now retries through the shared transport (introduced in spec 0003 F-06). 5xx behavior is unchanged. This is the cleanest place to capture the cross-spec contract; the alternative was gating the transport behind a flag and only enabling it from sync, which is artificial.

---

## 2026-05-01: Spec opened (Draft)

This spec is a thin layer of state-tracking + concurrency on top of spec 0002's per-recording fetch. It cannot move to `Active` until spec 0002 is at least Phase 5 (CLI wired) so the runner has something to call.

**Key design decisions baked in:**

- **Watch mode is foreground only.** Background scheduling is outside our scope: cron / launchd / systemd / Task Scheduler are all better at it than a Go process trying to be a daemon. Documented this position in spec.md §6 and again in F-05.
- **`--prune` defaults to off.** Server-side deletion is not always intended (a Plaud quirk could mass-delete). The `.trash/<id>/` recovery dir gives users a safety window even when they opt in.
- **Concurrent runs are an error, not a queue.** A second `plaud sync` against the same archive root exits non-zero with a "PID X is currently syncing" message. Cleaner than queueing and surprising the user with delayed work.

**Open spike before Phase 4 of the plan:** validate `flock` semantics on macOS, Linux, and Windows. Write a 30-line program that opens the state file with shared/exclusive locks and watches what happens under contention. If any platform misbehaves, fall back to a PID file with the same observable contract.

**Empirical work that comes from spec 0002 first:**

- The recording-versioning question (Q5 in spec.md) cannot be answered until 0002 has the detail endpoint and we observe what `version` / `version_ms` look like for a freshly-edited recording. Without that, "is this transcript stale?" reduces to "do the bytes I have on disk match what the server claims?" which is a weaker check.
- `file_md5` semantics from 0002 also feed F-02's idempotency check here.

**Performance target rationale:**

The 5-second threshold for a no-op sync (F-02) is set to make `plaud sync --watch --interval 1m` viable: 60s minus ~5s of work plus ~55s of sleep is comfortable headroom. If real-world API latency makes 5s unrealistic, revisit during Phase 5 smoke; the spec's threshold is an outcome target, not a budget line item.
