# Notes: Spec 0003: Sync

Append-only journal. Newest entry on top.

For the convention, see `specs/README.md`.

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
