# Spec 0003: Sync

**Status:** Draft
**Created:** 2026-05-01
**Updated:** 2026-05-01
**Owner:** @simensollie
**Target version:** v0.3

Idempotent, resumable mirror of every recording on the user's account into a local archive. Builds on spec 0002 (`plaud download`) and is the operational tool for keeping a long-lived archive current.

This spec does not implement two-way sync, daemon mode, or per-recording revision history.

---

## 1. Goal

A user runs `plaud sync` once a day (manually, or via cron / launchd / Task Scheduler) and is confident their local archive contains every recording on their Plaud account, with each recording's audio + transcript + summary present and verified.

## 2. Commands / interfaces

| Command | Behavior |
|---|---|
| `plaud sync` | Bring the archive up to date with the account. Default archive root same as spec 0002. |
| `plaud sync --out DIR` | Override the archive root. |
| `plaud sync --watch [--interval 15m]` | Loop: poll, sync new, sleep. Foreground process, exits cleanly on SIGINT / SIGTERM. |
| `plaud sync --concurrency N` | Default 4. |
| `plaud sync --include audio,transcript,summary,metadata` | Same selectors as spec 0002. |
| `plaud sync --prune` | Delete local recordings that no longer exist on the server. Default: never delete. |
| `plaud sync --include-trashed` | Also fetch recordings with `is_trash: true`. Default: skip them. |
| `plaud sync --format json` | NDJSON event stream on stdout. |
| `plaud sync --dry-run` | Report what would be fetched/skipped/pruned without making changes. |

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | `plaud sync` fetches every recording on the account that is not already present locally, using spec 0002's `plaud download` machinery. | Must |
| F-02 | Incremental: a recording present locally with a verified checksum is skipped. Two consecutive sync runs with no new recordings complete in under 5 seconds (transmits only the `list` API call and rewrites the state file). | Must |
| F-03 | Sync state file: `<archive_root>/.plaud-sync.state` JSON. Fields: `schema_version`, `last_run_started`, `last_run_finished`, `recordings: { <id>: { status, fetched_at, audio_sha256, server_file_md5, error_msg, error_at } }`. | Must |
| F-04 | Resumable: SIGINT during a sync flushes the in-progress state-file update so a re-run picks up where it left off without re-fetching completed recordings. | Must |
| F-05 | `--watch [--interval 15m]` polls the API every interval and runs a sync cycle. Logs each cycle's summary. Exits cleanly on SIGINT / SIGTERM. **Not a daemon**: ties to a terminal session. Cron / systemd / launchd are recommended for production scheduling and documented in `docs/scheduling.md`. | Should |
| F-06 | Concurrent downloads, default 4, configurable via `--concurrency N`. Respect HTTP 429 with exponential backoff (1s, 2s, 4s, 8s, max 30s). | Should |
| F-07 | NDJSON event stream with `--format json`: events `discovered`, `skipped`, `fetched`, `verified`, `failed`, `done`. Schema declared in `docs/schemas/0003/`. Stable from v0.3.0 GA. Each event carries `schema_version`. | Should |
| F-08 | Per-recording errors do not abort the sync. The state file records the error; final exit code is non-zero if any recording failed. | Must |
| F-09 | Recordings deleted on the server are NOT removed locally by default. `--prune` opts into mirroring deletions. With `--prune`, the local folder for the missing recording is moved to `<archive_root>/.trash/<id>/` rather than `rm -rf`'d outright, to give the user a recovery window. | Must |
| F-10 | Trashed recordings (`is_trash: true` on server) are skipped by default. `--include-trashed` opts in. | Should |
| F-11 | Single-instance lock: at most one `plaud sync` (or `--watch`) writes to a given archive root at a time, enforced via a `flock`-equivalent on the state file. Concurrent runs exit with a clear message identifying the holding PID. | Should |
| F-12 | `--dry-run` walks list + state, reports what *would* happen (`would-fetch`, `would-skip`, `would-prune`), and exits without writing anything except an updated `last_run_*` checkpoint in state. | Should |
| F-13 | Tokens, signed URLs, and `Authorization` headers never appear in logs or in the state file. | Must |

## 4. Storage / data model

```
<archive_root>/
├── .plaud-sync.state                     # see Section 3 / F-03
├── .trash/                               # populated only when --prune is used
│   └── <recording-id>/                   # mirrors the original folder
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
      "status": "verified",
      "fetched_at": "2026-04-30T08:22:11Z",
      "audio_sha256": "...",
      "server_file_md5": "...",
      "error_msg": null,
      "error_at": null
    }
  }
}
```

## 5. Tech stack

Unchanged from spec 0002. New runtime considerations:

- **flock-equivalent** for F-11. Linux/macOS: `syscall.Flock`. Windows: `LockFileEx`. Both are stdlib-reachable; no new dependency. If the chosen approach hits portability issues, fall back to a PID file with the same semantics.

## 6. Out of scope

- **Two-way sync.** Local edits do not propagate back to Plaud.
- **Daemon mode.** `--watch` exits when the terminal exits. Background scheduling is the OS's job (cron / systemd / launchd / Task Scheduler).
- **Per-recording revision history.** A recording with an updated transcript is overwritten in-place; we do not keep prior versions.
- **Conflict resolution for files renamed locally.** If the user renames `audio.mp3` → `audio-edited.mp3`, sync re-creates `audio.mp3`. The user's renamed copy is left alone.
- **Cross-device sync.** Each machine has its own archive and `.plaud-sync.state`. Two machines can sync the same account; they will not coordinate.
- **Selective "follow" of specific recording IDs.** That use case is `plaud download`, which spec 0002 already covers.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | `flock` portability across macOS/Linux/Windows. | Validate with a small spike before Phase 4 of the plan. Fall back to a PID file if any platform misbehaves. |
| 2 | Default `--watch` interval. | Recommend 15 minutes. Plaud's transcription pipeline takes minutes to complete after upload, so anything tighter wastes API calls. |
| 3 | Should `--prune` confirm interactively? | No interactive prompt; we are a scriptable CLI. The `.trash/` recovery window is the safety net. |
| 4 | What if Plaud renames a recording (changes `filename`)? Slug changes; folder path changes. Do we move the existing folder, or re-fetch in place? | Detect rename via stable `id`; move the existing folder to the new slug-based path. Test must cover this. |
| 5 | What if Plaud updates a recording's transcript? `is_trans` stays true, but the content has changed. Detect via `version` / `version_ms` in the list response (we noted those fields in spec 0002). Re-fetch when version differs. | Bake into F-02's checksum check: include `version` in the staleness comparison. |
| 6 | `--dry-run` writing to `last_run_started`: too clever or sensible? | Sensible: lets watch-mode loops checkpoint "we tried at T" without doing real work. Document. |

## 8. Acceptance criteria

1. **First-run sync** of an account with `N` recordings creates `N` recording folders in spec-0002 layout, plus `.plaud-sync.state`.
2. **Second-run sync** with no new recordings completes in under 5 seconds. Verified by `time plaud sync` plus an NDJSON stream showing 0 `fetched` events.
3. **Local file deleted, then sync** re-fetches only that file (other recordings remain skipped).
4. **SIGINT mid-sync, then re-run** completes the partial sync without re-fetching already-completed recordings.
5. **`--prune` after a server-side deletion** moves the deleted recording's folder under `.trash/<id>/`. Pre-existing files under `.trash/` are not touched.
6. **`--include-trashed` opt-in** fetches trashed recordings into the same layout.
7. **Concurrent run** during an active sync prints a clear lock-contention message and exits non-zero.
8. **`--dry-run`** never modifies any recording folder; it only updates `last_run_started`.
9. **Watch mode** runs at least three cycles, picks up a new recording uploaded between cycles, and exits cleanly on SIGINT.
10. Acceptance completes on macOS, Linux, and Windows for the platform binaries from the GitHub release.
