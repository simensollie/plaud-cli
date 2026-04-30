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

## I think I found a bug

Check existing issues at https://github.com/simensollie/plaud-cli/issues, then open a new one. Include:

- `plaud --version` output.
- Your OS (e.g. `macOS 15.4 arm64`, `Ubuntu 24.04`, `Windows 11 amd64`).
- The exact command you ran (with token redacted).
- The error you saw.

Do **not** paste tokens, OTP codes, or `Authorization` headers in issues. The CLI deliberately avoids logging these for the same reason.
