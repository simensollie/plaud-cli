# `plaud logout`

Delete the locally stored Plaud bearer token.

## Synopsis

```
plaud logout
```

No flags. Idempotent: running it when already logged out is a quiet success.

## Output

```
Logged out.
```

## What it does

- Removes `${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json` (POSIX) or `%APPDATA%\plaud\credentials.json` (Windows).
- That is all.

## What it does NOT do

- **Does not invalidate the token on Plaud's side.** The bearer token you held is still valid until it naturally expires (long-lived JWTs typically have multi-month lifetimes) or until you rotate it in the web app.
- **Does not delete any recordings or other data.** Local-only operation.
- **Does not log you out of any other Plaud session** (web, mobile, other devices).

If you need true revocation (lost a device, etc.), log out from https://web.plaud.ai or rotate the token by signing out and back in there.

## Errors and recovery

This command rarely errors. If it does, the message will name the file path that could not be removed (e.g. permission denied because the file is owned by another user). Fix the underlying file permission and try again.

## Related

- [`login.md`](./login.md): getting set up again after a logout.
