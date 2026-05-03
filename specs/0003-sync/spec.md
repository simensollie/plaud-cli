# Spec 0003: Sync

**Status:** Draft
**Created:** 2026-05-01
**Updated:** 2026-05-03
**Owner:** @simensollie
**Target version:** v0.3

Idempotent, resumable mirror of every recording on the user's account into a local archive. Builds on spec 0002 (`plaud download`) and is the operational tool for keeping a long-lived archive current.

This spec does not implement two-way sync, daemon mode, or per-recording revision history.

---

## 1. Goal

A user runs `plaud sync` once a day (manually, or via cron / launchd / Task Scheduler) and is confident their local archive contains every recording on their Plaud account, with each recording's transcript and summary present and verified. Audio is opt-in (per invocation via `--include audio,...` or globally via `PLAUD_DEFAULT_INCLUDE`); ad-hoc audio fetches go through spec 0002's `plaud download`.

## 2. Commands / interfaces

| Command | Behavior |
|---|---|
| `plaud sync` | Bring the archive up to date with the account. Default archive root same as spec 0002. Default `--include` is `transcript,summary,metadata` (no audio); see F-14. |
| `plaud sync --out DIR` | Override the archive root. |
| `plaud sync --watch [--interval 15m]` | Loop: poll, sync new, sleep. Foreground process, exits cleanly on SIGINT / SIGTERM. |
| `plaud sync --concurrency N` | Default 4, clamped to `[1, 16]` (matches spec 0002). |
| `plaud sync --include audio,transcript,summary,metadata` | Same selectors as spec 0002. |
| `plaud sync --prune` | Move recordings absent from the server (union of `is_trash=0` and `is_trash=1` list calls) to `<archive_root>/.trash/<id>/`. Default: never prune. |
| `plaud sync --prune-empty` | Bypass the mass-deletion guard (F-09). Only meaningful with `--prune`. |
| `plaud sync --include-trashed` | Also fetch recordings with `is_trash: true`. Default: skip them. |
| `plaud sync --format json` | NDJSON event stream on stdout. |
| `plaud sync --dry-run` | Report what would be fetched/skipped/pruned without making changes. |

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | `plaud sync` fetches every recording on the account that is not already present locally, calling `internal/fetch.FetchOne` (extracted in plan Phase 0a from spec 0002's `cmd/plaud/download.go::processRecording`). Sync and `plaud download` share the same per-recording primitive. | Must |
| F-02 | Incremental: a recording whose stored `version` matches the list response AND whose `metadata.json` confirms presence of every artifact in the effective include set is skipped without a detail call. Two consecutive sync runs with no new recordings complete in under 5 seconds (one `list` API call plus state-file rewrite). | Must |
| F-03 | Sync state file: `<archive_root>/.plaud-sync.state` JSON. Acts as a *minimal index*; per-recording artifact truth lives in each recording's `metadata.json` (spec 0002 §4). Schema: `schema_version`, `last_run_started`, `last_run_finished`, `recordings: { <id>: { version, version_ms, folder_path, last_error: { msg, at } } }`. `folder_path` is relative to the archive root. Deleting `.plaud-sync.state` is a safe recovery: the next run rebuilds it from the surviving `metadata.json` files plus the list response. | Must |
| F-04 | State is written atomically (tmp+rename) after every recording's completion within a cycle. SIGINT/SIGTERM cancels the cycle's context; in-flight workers drain to completion of their current HTTP request, then return without marking their recording verified. A second SIGINT within 2 seconds triggers a hard exit (no further state flush). Half-written `.partial` files inherited from spec 0002 F-14 are swept on the next run. | Must |
| F-05 | `--watch [--interval 15m]` polls the API every interval and runs a sync cycle. Interval is *sleep duration* (cycle-end → next cycle-start), not wall-clock cadence. SIGTERM is treated identically to SIGINT. After 5 consecutive failed cycles, the watcher exits non-zero with the redacted last error. **Not a daemon**: ties to a terminal session. Cron / systemd / launchd are recommended for production scheduling and documented in `docs/scheduling.md`. | Should |
| F-06 | Concurrent recording fetches, default 4, configurable via `--concurrency N`, clamped to `[1, 16]` (matches spec 0002 F-06). HTTP 429 is retried at the API client transport layer (`internal/api/backoff.go`) with up to 5 retries per call; backoff schedule (1s, 2s, 4s, 8s, 30s) is used when no `Retry-After` header is present, and `Retry-After` is honored otherwise (capped at 30s). 5xx responses are not retried (inherits spec 0002 F-15). Backoff is worker-local; pool throttling provides natural cross-worker coordination. | Should |
| F-07 | NDJSON event stream with `--format json`. Per-recording granularity (not per-artifact). Events: `discovered`, `skipped`, `fetched`, `verified`, `failed`, `pruned`, `would-fetch`, `would-skip`, `would-prune`, `done`. Common envelope: `{ schema_version, event, ts (UTC ISO 8601), id (or null on done), details }`. `done` always fires at cycle end (one per cycle in watch mode); on SIGINT it carries `details.status: "interrupted"`. `failed` events go to stdout NDJSON; a one-line human-readable error mirrors to stderr. Schema declared in `docs/schemas/0003/`. *Preview* in 0.3.0-rc; field-level stable from 0.3.0 GA (no removed events, no removed or retyped fields; new events and new `details` fields allowed). | Should |
| F-08 | Per-recording errors do not abort the sync. `recordings[id].last_error` records the redacted message and UTC timestamp; `failed` events carry the same. Final exit code is non-zero if any recording failed. | Must |
| F-09 | `--prune` mirrors server-side deletions to `<archive_root>/.trash/<id>/`. "Missing on server" requires absence from the *union* of `is_trash=0` and `is_trash=1` list calls (recordings the user merely trashed in the web UI are NOT pruned locally). If `.trash/<id>/` already exists, the new entry lands at `.trash/<id>__<UTC ISO 8601>/`. The `YYYY/MM` hierarchy is not preserved under `.trash/`. Mass-deletion guards: (a) if both list calls return zero recordings AND the local archive has ≥1 recording in state, refuse to prune; (b) if pruning would reduce the local archive by more than 50% in one run, refuse. Both are bypassed with `--prune-empty`. `.trash/` is not auto-cleaned (user's responsibility). | Must |
| F-10 | Trashed recordings (`is_trash: true` on server) are skipped by default. `--include-trashed` opts in. | Should |
| F-11 | Single-instance write lock on the state file via `flock`-equivalent: at most one cycle holds it at a time. Watch mode acquires per cycle and releases between cycles, so an ad-hoc `plaud sync` during a watch session waits at most one cycle's duration. A second `--watch` against the same archive root is detected via a `<archive_root>/.plaud-sync.watch` advisory sentinel (records `pid`, `hostname`, `started_at_utc`); the second watcher exits with a clear message naming the first. Lock contention messages are structured. When `hostname` matches the current host and the lock-holder PID is no longer alive, the new run takes the lock with a one-line stderr notice. | Should |
| F-12 | `--dry-run` walks list + state, emits `would-fetch` / `would-skip` / `would-prune` events, and exits without writing anything except an updated `last_run_started` checkpoint in state. `--dry-run` takes the same write lock as a real sync; two concurrent `--dry-run` invocations contend just like real syncs. | Should |
| F-13 | Tokens, signed URLs (host + entire query string), and `Authorization` headers are redacted at *both* the API client error layer (`internal/api/redact.go`) and at every persistence/event surface (state file, NDJSON events, stderr). Defense in depth: a future error site missing source-level scrubbing is still backstopped by the surface scrub. Tests cite F-13 against each surface. | Must |
| F-14 | `plaud sync` default `--include` set is `transcript,summary,metadata` (no audio). `PLAUD_DEFAULT_INCLUDE` overrides for both `plaud sync` and `plaud download`. `--include` overrides per invocation. Set `PLAUD_DEFAULT_INCLUDE=audio,transcript,summary,metadata` to make sync mirror audio by default. | Must |
| F-15 | Staleness detection is layered. **Layer A (presence-flip):** the reconciler compares the list response's `is_trans` / `is_summary` against the recording's `metadata.json` sub-object presence; a flip from absent to present schedules a fetch of the now-available artifact. Derives from existing wire fields. **Layer B (content-edit):** when the list response's `version` (or `version_ms`) differs from `recordings[id].version`, schedule a re-fetch of all artifacts in the effective include set. Layer B is gated on the §7 Q3 empirical probe; if `version` does not bump on user-side edits, Layer B is documented as a known gap and `--force` (spec 0002 F-16) is the workaround. | Must |
| F-16 | Server-side rename detection: for each recording in the list response, compute the expected relative path (spec 0002 F-03 slug rules) and compare to `recordings[id].folder_path`. On mismatch, `os.Rename` the folder; on slug collision, append spec 0002 F-03's 6-char-id suffix. Cross-filesystem rename failures and Windows in-use sharing violations surface as per-recording failures (F-08) with `folder_path` unchanged so a re-run retries. Empty `YYYY/MM` parent directories are not auto-removed. If a crash leaves the folder renamed but state stale, the next run treats the recording as locally absent and re-fetches into the new path (no metadata-walk repair). | Should |

## 4. Storage / data model

```
<archive_root>/
├── .plaud-sync.state                     # see Section 3 / F-03
├── .plaud-sync.watch                     # advisory sentinel for watch mode (F-11)
├── .trash/                               # populated only when --prune is used
│   └── <recording-id>/                   # contents of original folder; no YYYY/MM
└── 2026/
    └── 04/
        └── 2026-04-30_1430_kickoff/      # spec 0002 layout
            └── ...
```

`.plaud-sync.state` schema (informally; full JSON schema lives at `docs/schemas/0003/sync-state.json` once Active):

```json
{
  "schema_version": 1,
  "last_run_started":  "2026-05-01T12:00:00Z",
  "last_run_finished": "2026-05-01T12:00:42Z",
  "recordings": {
    "<id>": {
      "version":     "<server version field>",
      "version_ms":  1735689600000,
      "folder_path": "2026/04/2026-04-30_1430_kickoff",
      "last_error":  { "msg": null, "at": null }
    }
  }
}
```

Per-artifact hashes, sizes, and `fetched_at` / `last_verified_at` live in each recording's `metadata.json` (spec 0002 §4). The state file is a navigation index, not a duplicate of artifact-level truth.

## 5. Tech stack

Unchanged from spec 0002. New runtime considerations:

- **flock-equivalent** for F-11. Linux/macOS: `syscall.Flock`. Windows: `LockFileEx`. Both are stdlib-reachable; no new dependency. If the Phase 0 spike (§7 Q1) shows portability problems, the fallback is a PID file with explicit stale-detection (the OS-native paths get auto-release on process death; the PID-file fallback does not, so it must validate the holder is alive on each run).
- **Backoff transport** for F-06 lives in `internal/api/backoff.go`. Standard `http.RoundTripper` wrapping; no new dependency.
- **Redaction module** for F-13 lives in `internal/api/redact.go`. Patterns for signed S3 URLs (`X-Amz-Signature`, `X-Amz-Credential`), JWT-shaped bearer tokens, and JSON `"token":"..."` fields. No new dependency.

## 6. Out of scope

- **Two-way sync.** Local edits do not propagate back to Plaud.
- **Daemon mode.** `--watch` exits when the terminal exits. Background scheduling is the OS's job (cron / systemd / launchd / Task Scheduler).
- **Per-recording revision history.** A recording with an updated transcript is overwritten in-place; we do not keep prior versions.
- **Conflict resolution for files renamed locally.** If the user renames `audio.mp3` → `audio-edited.mp3`, sync re-creates `audio.mp3`. The user's renamed copy is left alone.
- **Cross-device sync.** Each machine has its own archive and `.plaud-sync.state`. Two machines can sync the same account; they will not coordinate.
- **Auto-cleanup of `.trash/`.** Retention is the user's responsibility; `docs/user/sync.md` provides a one-liner.
- **Auto-cleanup of empty `YYYY/MM` parent directories** after a rename or prune.
- **Cloud-sync-aware behavior.** We do not detect Dropbox / iCloud / OneDrive holds; rename failures inside such folders surface as per-recording errors and the user re-runs once the cloud settles.
- **Cross-worker backoff coordination.** v0.3 ships worker-local backoff; revisit in v0.4 if real Plaud rate-limits prove aggressive (§7 Q4).
- **Selective "follow" of specific recording IDs.** That use case is `plaud download`, which spec 0002 already covers.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | `flock` portability across macOS/Linux/Windows. | Phase 0 Probe B. If any platform misbehaves, fall back to a PID file with explicit stale-detection (`signal 0` on Unix, `OpenProcess` on Windows). |
| 2 | Default `--watch` interval. | 15 minutes. Plaud's transcription pipeline takes minutes after upload; tighter intervals waste API calls. |
| 3 | Does Plaud's `version` / `version_ms` bump on (a) the transcription pipeline completing, (b) a user renaming a speaker in web.plaud.ai, (c) summary regeneration? | **Phase 0 Probe A** gates F-15 Layer B. If `version` cooperates, Layer B ships. If it only bumps on the `is_trans` boundary (Layer A already detects that), Layer B is documented as a known gap and `--force` is the workaround. Capture observations in `notes.md`. |
| 4 | Cross-worker backoff coordination on aggressive 429. | Defer to v0.4. v0.3 ships worker-local backoff (F-06). Revisit only if real-world rate-limit behavior demands a shared clock. |

(Closed in this revision: original Q3 — `--prune` interactive confirmation; the F-09 mass-deletion guard is the safety net. Original Q4 — Plaud rename detection; resolved by F-16. Original Q6 — `--dry-run` writing `last_run_started`; accepted, lets watch loops checkpoint "we tried at T". Original Q5 reframed and moved to Q3 above.)

## 8. Acceptance criteria

1. **First-run sync** of an account with `N` recordings creates `N` recording folders in spec-0002 layout (text-only by default per F-14), plus `.plaud-sync.state`.
2. **Second-run sync** with no new recordings completes in under 5 seconds. Verified by `time plaud sync` plus an NDJSON stream showing 0 `fetched` events.
3. **Local file deleted, then sync** re-fetches only that file (other recordings remain skipped).
4. **SIGINT mid-sync, then re-run** completes the partial sync without re-fetching already-completed recordings.
5. **`--prune` after a server-side deletion** moves the deleted recording's folder under `.trash/<id>/`. A subsequent prune of a recording with the same id (e.g. recreated then deleted again) lands at `.trash/<id>__<UTC ISO 8601>/` rather than overwriting.
6. **`--include-trashed` opt-in** fetches trashed recordings into the same layout; `--prune` *without* `--include-trashed` does NOT prune recordings that the user merely trashed in the web UI.
7. **Concurrent run during an active sync** prints a clear lock-contention message identifying the holder's `pid`, `hostname`, and `started_at`, and exits non-zero. Manual `plaud sync` while `--watch` is running waits at most one cycle's duration, then runs cleanly.
8. **`--dry-run`** never modifies any recording folder; it only updates `last_run_started`.
9. **Watch mode** runs at least three cycles, picks up a new recording uploaded between cycles, and exits cleanly on SIGINT (and SIGTERM).
10. **Server-side rename:** changing a recording's filename in web.plaud.ai and re-syncing moves the local folder to the new slug-derived path. `metadata.json` content is unchanged.
11. **Redaction:** an injected error containing a signed S3 URL (`X-Amz-Signature=...&X-Amz-Credential=...`) and a `Bearer eyJ...` JWT does not produce any matching substring in `.plaud-sync.state`, NDJSON output, or stderr.
12. **Two-watch detection:** starting a second `plaud sync --watch` against the same archive root exits non-zero with a structured message naming the first watcher's `pid`, `hostname`, and `started_at`.
13. **Mass-deletion guard:** an artificially empty server response with a non-empty local archive triggers `--prune` to refuse and exit non-zero. Re-running with `--prune-empty` proceeds.
14. **`--include audio` after text-only sync:** an existing text-only archive re-synced with `--include audio,transcript,summary,metadata` fetches only the missing audio bytes (verified via `--format json` showing `fetched` events whose `details.artifacts` contain only `audio.mp3`).
15. Acceptance criteria 1-14 reproduce on macOS, Linux, and Windows for the platform binaries from the GitHub release.
