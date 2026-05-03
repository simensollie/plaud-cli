# Troubleshooting

Errors you might hit and what to do about them.

## "Not logged in. Run `plaud login` first."

Your credentials file is absent. Most common causes:

- You have never run `plaud login` on this machine.
- You ran `plaud logout` (or deleted the credentials file manually).
- You ran with `XDG_CONFIG_HOME` pointing somewhere unexpected.

**Fix:** run `plaud login`. For SSO accounts, use `plaud login --token <jwt> --region eu --email you@example.com` instead. See [`commands/login.md`](./commands/login.md).

## "Token expired or invalid. Run `plaud login` again."

The bearer token Plaud returned (or that you pasted) is no longer accepted. Common causes:

- Token expired (rare for fresh tokens; JWTs Plaud issues are typically long-lived).
- You logged out from web.plaud.ai, which rotates tokens.
- The wrong region was selected at login time, so the request hit a regional API your account is not bound to.

**Fix:** run `plaud login` again. If it keeps failing, try a different region (run `plaud login --region us` or `--region jp`). For SSO accounts, refresh the token from web.plaud.ai's localStorage and `plaud login --token` it again.

## "Your Plaud account does not have a password set"

You logged in via Apple or Google SSO and Plaud has no email-password credential to validate the OTP against.

**Fix (option A):** open https://web.plaud.ai → Account → set a password. Then retry `plaud login`.

**Fix (option B):** use the token paste path. See [getting-started.md, Step 2b](./getting-started.md#step-2b-token-paste-login).

## "region '...' invalid: must be one of us, eu, jp"

You typed a region the CLI does not recognize, OR your credentials file was edited and now contains a bad value.

**Fix:** re-run `plaud login` and pick `us`, `eu`, or `jp`. Norway and most of Europe use `eu`.

## My region is correct but `plaud list` returns 401

The bearer-token authorization path on Plaud's API is undocumented; we use it because reverse-engineered prior art has done so for ~a year, and our v0.1 smoke confirmed it works on the EU host. If 401s start appearing across the board, Plaud may have changed their API. Possibilities:

- **Try the other regions.** Plaud sometimes routes accounts across regions during migrations.
- **Re-paste the token.** A fresh token from `pld_tokenstr` may use a new format the CLI does not know about; in that case, please open an issue.
- **Check status.** Plaud-side outages happen; web.plaud.ai showing the same recording might still be working off cached data.

## `plaud --version` prints `0.1.0-dev` instead of `0.1.0`

You built from source without GoReleaser's `-ldflags`. This is harmless; the binary is functionally identical.

If you want a clean version string, download the official release archive instead of building from source.

## My recording titles contain Norwegian characters but the binary mangles them

Tested on Linux/macOS terminals with UTF-8 (the default). On Windows cmd.exe with a non-UTF-8 codepage, æ/ø/å can render as `?`. Fix:

```powershell
chcp 65001
```

Or use Windows Terminal / PowerShell 7, which default to UTF-8.

## Where is my data going?

The CLI talks to two hosts only:

- `api.plaud.ai` (region discovery during interactive OTP login).
- `api-euc1.plaud.ai` / `api.plaud.ai` / `api-jp.plaud.ai` (the regional API host you select).

No third-party telemetry, no analytics, no LLM provider. The token never leaves your machine via this binary except in the `Authorization: bearer ...` header on the requests above.

## "plaud sync is already running on this archive root (PID X, ...)"

Another `plaud sync` (or a watch-mode cycle) holds the per-archive lock. The message names the holder's `pid`, `hostname`, and `started_at`.

**Fix (normal case):** wait for it to release. Watch mode acquires the lock per cycle and releases between cycles, so a manual `plaud sync` will succeed within one cycle.

**Fix (lock looks stuck):** identify the holder process and check whether it's actually alive. On Linux/macOS, `flock` auto-releases on process death; on Windows, `LockFileEx` does too. If the holder PID matches your current host but the process is gone, the next sync run takes the lock automatically with a one-line stderr notice. If the PID doesn't exist on this host (e.g. the lock file is from a different machine sharing the archive over a network mount), you can manually delete `<archive_root>/.plaud-sync.lock`.

## "A plaud sync watch loop is already active"

A second `plaud sync --watch` against the same archive root. The first watcher's `pid`, `hostname`, and `started_at` are in the message.

**Fix:** stop the first watch loop. If you're sure it's stale (e.g. you rebooted but the sentinel survived), delete `<archive_root>/.plaud-sync.watch`.

This is intentionally an advisory check, not a hard lock — it catches accidental "I started watch in two terminals" footguns. Manual `plaud sync` invocations ignore the sentinel; only watch loops conflict.

## "refusing to prune: server returned 0 recordings..." or "...M of N recordings missing (>50%)"

Sync's mass-deletion guard. Fired because either the server reported zero recordings while your archive has some, or a single run would prune more than half the archive.

**Why this exists:** an API outage or auth glitch could otherwise sweep your whole archive into `.trash/`. Recoverable, but startling.

**Fix (intentional):** if you really did delete most of your account's recordings, pass `--prune-empty` to bypass:

```bash
plaud sync --prune --prune-empty
```

**Fix (unintentional):** investigate why the server returned a near-empty list. Re-run `plaud list` to confirm. Once the server response matches expectations, drop `--prune-empty`.

## "rate limited (HTTP 429): retry budget exhausted"

A single HTTP call hit 429 five times in a row, with exponential backoff (1s, 2s, 4s, 8s, 30s) between retries. Plaud is throttling.

**Fix:** wait a few minutes; re-run. If you see this regularly, lower `--concurrency` (default 4):

```bash
plaud sync --concurrency 2
```

Watch mode counts a 429-exhausted recording as one of the 5 consecutive failed cycles before exiting. If your network is rate-limited consistently, drop the concurrency in your scheduler entry, not just on a single run.

## "Watch loop has failed N cycles in a row. Last error: ..."

Five consecutive failed cycles. Watch exited non-zero rather than keep crash-looping.

**Fix:** read the redacted last error. Most common causes:

- Network outage (Wi-Fi flake, VPN drop, sleep + wake).
- Sustained 429 (back off via `--concurrency`).
- Token expired mid-watch (re-run `plaud login`).
- Plaud-side outage.

Restart watch after fixing the root cause. For unattended scheduling, prefer cron / launchd / systemd (which retry on the next tick regardless) over a long-running `--watch`. See [`scheduling.md`](./scheduling.md).

## My sync state file is corrupt

`.plaud-sync.state` is an index into the archive, not the source of truth. If it's corrupt, malformed, or out of sync with reality, **delete it**. The next run rebuilds it from the surviving `metadata.json` files plus the list response.

```bash
rm <archive_root>/.plaud-sync.state
plaud sync
```

You may see a single fresh-fetch cycle (sync re-asserts that every artifact in the include set is present) and then steady-state idempotency from the next run onward.

## I think I found a bug

Check existing issues at https://github.com/simensollie/plaud-cli/issues, then open a new one. Include:

- `plaud --version` output.
- Your OS (e.g. `macOS 15.4 arm64`, `Ubuntu 24.04`, `Windows 11 amd64`).
- The exact command you ran (with token redacted).
- The error you saw.

Do **not** paste tokens, OTP codes, or `Authorization` headers in issues. The CLI deliberately avoids logging these for the same reason.
