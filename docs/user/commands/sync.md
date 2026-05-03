# `plaud sync`

Mirror every recording from your Plaud.ai account into a local archive. Idempotent and resumable: re-running is cheap, and the second run after the first is a no-op (one `list` API call plus a state-file rewrite).

## Synopsis

```
plaud sync [--out DIR]
           [--watch [--interval 15m]]
           [--concurrency N]
           [--include audio,transcript,summary,metadata]
           [--prune] [--prune-empty]
           [--include-trashed]
           [--format json]
           [--dry-run]
           [--force]
```

No positional arguments. The set of recordings to fetch comes from your account.

## What you get

By default, sync produces a text-only archive: transcripts, summaries, and `metadata.json` per recording. Audio is opt-in (audio bytes dominate disk usage; most workflows consume the transcript). Folder layout is identical to `plaud download`:

```
<archive_root>/
├── .plaud-sync.state                     # sync's index file
├── .plaud-sync.watch                     # advisory: a watch loop is here (only while --watch runs)
├── .trash/                               # populated only when --prune is used
│   └── <recording-id>/                   # contents of the original folder, no YYYY/MM
└── 2026/
    └── 04/
        └── 2026-04-30_1430_kickoff/
            ├── transcript.json
            ├── transcript.md
            ├── summary.plaud.md
            └── metadata.json
```

## Choosing what to mirror

The default include set is `transcript,summary,metadata` (no audio). Override per invocation with `--include`, or globally with `PLAUD_DEFAULT_INCLUDE` in your shell rc:

```bash
# One run with audio
plaud sync --include audio,transcript,summary,metadata

# Default to a full audio mirror across every invocation
export PLAUD_DEFAULT_INCLUDE=audio,transcript,summary,metadata
plaud sync
```

`--include` accepts any subset of `audio,transcript,summary,metadata`. The same flag works in `plaud download`; both commands honor `PLAUD_DEFAULT_INCLUDE`.

## How idempotency works

The sync state file (`<archive_root>/.plaud-sync.state`) is a navigation index. Per-recording artifact truth (hashes, sizes, fetched-at timestamps) lives in each recording's `metadata.json`. Deleting `.plaud-sync.state` is a safe recovery: the next run rebuilds it from the surviving `metadata.json` files plus the list response.

A recording is **skipped** when:

1. The server's `version` (and `is_trans` / `is_summary` flags) matches what state recorded last time, AND
2. The recording's `metadata.json` confirms every artifact in the effective include set is present locally.

A recording is **fetched** when any of the following hold:

- The server reports a recording you've never seen.
- The recording's folder is missing locally (e.g. you deleted it).
- The server's `is_trans` or `is_summary` flipped from `false` to `true` since the last run (Plaud's transcription pipeline finished). Sync re-fetches just the now-available artifact.

A recording's content edited server-side (e.g. a speaker rename in `web.plaud.ai`) is **not auto-detected by default**. The flag for it (`version`/`version_ms` staleness) is wired but disabled until empirical observation confirms Plaud bumps `version` on user-side edits. Until then, use `--force` (or `plaud download <id> --force`) to refresh a specific recording.

## Watch mode

`--watch` loops sync cycles. Use it for a desk session:

```bash
plaud sync --watch                   # default 15-minute interval
plaud sync --watch --interval 1m     # tighter polling
```

Watch mode is **foreground only** — it ties to your terminal session. For unattended scheduling (laptop locked, server box, daily cron at 03:00), use your OS scheduler instead. See [`scheduling.md`](../scheduling.md) for cron / launchd / systemd / Task Scheduler examples.

A few things worth knowing:

- Interval is sleep-duration (cycle-end → next cycle-start), not wall-clock cadence. A 15m interval with a 12m cycle starts cycles at T, T+27m, T+54m. Prevents pile-up if a cycle ever exceeds the interval.
- `SIGINT` (Ctrl+C) and `SIGTERM` both exit cleanly. In-flight workers drain to completion of their current HTTP request, then state is flushed.
- A second `Ctrl+C` within ~2 seconds of the first triggers a hard exit (no further state flush). Useful if the drain is stuck.
- After 5 consecutive failed cycles (e.g. sustained network outage), watch exits non-zero with a redacted last error. Restart it after fixing the underlying issue.

## Pruning

`--prune` mirrors **server-side deletions** to a local `.trash/<id>/` folder so you have a recovery window. Default is off; never silently delete.

A recording is "missing on the server" only when it's absent from **both** the active list (`is_trash=0`) AND the trashed list (`is_trash=1`). Recordings the user merely trashed in the Plaud web UI are not pruned locally — they're recoverable on Plaud's side.

Two safety guards trip without the `--prune-empty` bypass:

1. **Empty server**: if the server returns 0 recordings while your local archive has any, sync refuses to prune.
2. **Mass deletion**: if a single run would prune more than 50% of the archive, sync refuses.

Both guard against API outages or auth glitches that would otherwise move your whole archive to `.trash/`. Bypass when you really mean it:

```bash
plaud sync --prune --prune-empty
```

`.trash/` is never auto-cleaned. Empty it manually when you're confident the recordings are truly gone:

```bash
# Linux/macOS: delete trash entries older than 30 days
find ~/PlaudArchive/.trash -mindepth 1 -maxdepth 1 -mtime +30 -exec rm -rf {} +
```

If a recording with the same id ever lands in `.trash/` twice (recreated then deleted again), the second entry uses `.trash/<id>__<UTC ISO 8601>/` so the first is never overwritten.

## Trashed recordings

Recordings the user trashed in `web.plaud.ai` (`is_trash=true` on the server) are **skipped by default**. Pass `--include-trashed` to fetch them too:

```bash
plaud sync --include-trashed
```

A trashed recording fetched this way carries a one-line stderr warning identifying the trashed state.

## Renames

If you rename a recording in the Plaud web UI, the slug changes and so does the local folder name. Sync detects the rename via the stable recording id and `os.Rename`s the existing folder to the new path. No re-download.

If the new path collides with another recording's folder (rare; same year, month, timestamp, and slug), the rename target gets a 6-char id suffix appended (matching `plaud download`'s F-03 collision rule). Cross-filesystem rename failures and Windows in-use file locks surface as per-recording errors; sync continues with other recordings, and the next run retries.

Empty `YYYY/MM` parent directories left behind by a rename or prune are not auto-removed.

## JSON output

`--format json` emits an NDJSON event stream on stdout. One JSON object per line, each with a stable envelope:

```json
{"schema_version":1,"event":"discovered","ts":"2026-05-04T12:00:00Z","details":{"count":42,"trashed_count":0}}
{"schema_version":1,"event":"fetched","ts":"2026-05-04T12:00:01Z","id":"a3f9c021000000000000000000000001","details":{"reason":"new","folder":"...","artifacts":["transcript.json","transcript.md","summary.plaud.md","metadata.json"]}}
{"schema_version":1,"event":"skipped","ts":"2026-05-04T12:00:02Z","id":"...","details":{"reason":"verified"}}
{"schema_version":1,"event":"done","ts":"2026-05-04T12:00:42Z","details":{"status":"ok","fetched":1,"skipped":41,"failed":0,"pruned":0}}
```

Event names: `discovered`, `skipped`, `fetched`, `verified`, `failed`, `pruned`, `would-fetch`, `would-skip`, `would-prune`, `done`. The `done` event always fires at cycle end (one per cycle in watch mode); on `Ctrl+C` it carries `details.status: "interrupted"` so consumers tailing the stream never wait forever.

Schema is **preview** in 0.3.0-rc and field-level **stable** from v0.3.0 GA: no event type or field gets removed or retyped without a major version bump. New event types and new `details` fields may land additively. Stderr remains plain English regardless of `--format`.

## Dry-run

`--dry-run` walks the list and the state, prints what *would* happen, and exits without modifying any recording folder:

```bash
plaud sync --dry-run --format json
```

Dry-run events use a separate vocabulary (`would-fetch`, `would-skip`, `would-prune`) so a script can't accidentally treat a preview as a real run. The only state mutation a dry-run makes is bumping `last_run_started`, which lets watch loops checkpoint "we tried at T".

## Concurrency

`--concurrency N` sets how many recordings are fetched in parallel. Default `4`, clamped to `[1, 16]`. The flag meters recordings, not HTTP requests; within a single recording the worker fetches detail, signed URL, and artifacts serially.

HTTP 429 (rate-limited) responses are retried at the API client transport with up to 5 retries per call (1s, 2s, 4s, 8s, 30s). `Retry-After` is honored capped at 30s. 5xx and network errors surface immediately — no silent retries.

## Single-instance lock

At most one sync writes to a given archive root at a time (F-11). A second invocation prints a structured contention message and exits non-zero:

```
plaud sync is already running on this archive root (PID 4521, host "kingsnake.local", started 2026-05-04T12:00:00Z).
```

Watch mode acquires the lock per cycle and releases it between cycles, so an ad-hoc `plaud sync` during a long watch session waits at most one cycle's duration, then runs cleanly. A second `plaud sync --watch` against the same archive root is detected via a `.plaud-sync.watch` advisory file and exits with a clear message naming the first watcher.

## `--force`

Bypasses every per-artifact idempotency check, re-fetches audio bytes, rewrites canonical files even when hashes match, regenerates derived files, and bumps both `fetched_at` and `last_verified_at`. Use it for suspected local corruption or a verifiable fresh round-trip; for most re-runs the default idempotency is what you want.

## Common errors

| Error | Cause | Fix |
|---|---|---|
| `Not logged in. Run \`plaud login\` first.` | No credentials file. | Run `plaud login`. |
| `Token expired or invalid. Run \`plaud login\` again.` | 401 from any API call. | Re-run `plaud login`. The CLI does not retry. |
| `plaud sync is already running on this archive root (PID X, ...)` | Another sync (or watch loop's cycle) holds the lock. | Wait for it to release. If it's stuck, kill the holder; on Linux/macOS `flock` auto-releases, on Windows `LockFileEx` does too. |
| `A plaud sync watch loop is already active on this archive root` | A second `plaud sync --watch` against the same root. | Stop the first, or remove `<archive_root>/.plaud-sync.watch` if the holding PID is gone. |
| `refusing to prune: server returned 0 recordings but local archive has N` | The mass-deletion empty-server guard tripped. | Verify the server really has nothing. If yes, pass `--prune-empty`. |
| `refusing to prune: M of N recordings missing (>50%)` | The 50% mass-deletion guard tripped. | Same as above. Verify intentional, then `--prune-empty`. |
| `rate limited (HTTP 429): retry budget exhausted` | Plaud rate-limited a single call past 5 retries. | Wait a few minutes; re-run. If sustained, lower `--concurrency`. |
| `Watch loop has failed N cycles in a row.` | Five consecutive failed cycles. | Resolve the underlying issue (network, auth, rate-limit), then restart watch. |

See [`troubleshooting.md`](../troubleshooting.md) for deeper recovery walks.

## Examples

First-time mirror (text-only, default):

```bash
plaud sync
```

Full mirror including audio:

```bash
plaud sync --include audio,transcript,summary,metadata
```

Daily watch loop (foreground, 15 min):

```bash
plaud sync --watch
```

Preview what would change (no writes):

```bash
plaud sync --dry-run --format json | jq -r '"\(.event)\t\(.id // "")\t\(.details.reason // "")"'
```

Mirror once a day from cron (Linux/macOS):

```cron
0 3 * * *  /usr/local/bin/plaud sync --format json >> /var/log/plaud-sync.log 2>&1
```

See [`scheduling.md`](../scheduling.md) for the launchd, systemd, and Task Scheduler equivalents.

## Related

- [`download.md`](./download.md): fetch a single recording. Same per-recording machinery, different entry point.
- [`list.md`](./list.md): inspect the recordings on your account.
- [`scheduling.md`](../scheduling.md): run sync unattended via cron / launchd / systemd / Task Scheduler.
- [`troubleshooting.md`](../troubleshooting.md): recovery paths for the lock, prune, and rate-limit cases.
