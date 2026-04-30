# Getting started

This walkthrough takes you from a fresh `plaud` install to "I can list every recording on my Plaud account" in about five minutes.

You will need:

- The `plaud` binary on your `PATH` (see [`install.md`](./install.md) if you do not have it yet).
- Your Plaud.ai account credentials (the email and the way you sign in).

## Step 1: choose your login path

plaud-cli supports two ways to log in:

| Path | When to use it |
|---|---|
| **Interactive OTP** (`plaud login`) | Your account uses email + password, OR you already have a password set in addition to SSO. |
| **Token paste** (`plaud login --token ...`) | Your account uses Apple or Google sign-in only and does not have a password set. Plaud's OTP flow rejects accounts without a password. |

If you are not sure, try the interactive path first. If it fails with "your Plaud account does not have a password set", use the token paste path.

## Step 2a: interactive OTP login

```bash
plaud login
```

The CLI prompts for three things in order:

1. **Region.** `us`, `eu`, or `jp`. Pick the one that matches where Plaud routes your account. EU customers (most of Europe, including Norway) use `eu`.
2. **Email.** The address you log in with on web.plaud.ai.
3. **OTP code.** Plaud emails you a 6-digit code; type it when prompted.

If everything works, you see:

```
Logged in successfully.
```

A file at `~/.config/plaud/credentials.json` (POSIX) or `%APPDATA%\plaud\credentials.json` (Windows) now holds your bearer token, with file permissions locked to your user only on POSIX.

## Step 2b: token paste login

If interactive OTP is not an option, you can copy your token directly from web.plaud.ai.

1. Open https://web.plaud.ai in a browser and log in.
2. Open the browser DevTools (F12 or Cmd+Option+I).
3. Go to **Application** tab → **Local Storage** → `https://web.plaud.ai`.
4. Find the key `pld_tokenstr`. Its value looks like `"bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IlVUIn0.eyJzdWIi...etc...Qw5U"`.
5. Copy just the `eyJ...` JWT (drop the `bearer ` prefix and the surrounding quotes).
6. Run:

```bash
plaud login \
  --token "eyJhbGc...REPLACE_WITH_THE_FULL_THREE_PART_JWT...Qw5U" \
  --region eu \
  --email "you@example.com"
```

You should see:

```
Token stored. You can now run `plaud list`.
```

The token has roughly the same lifetime as your browser session. When it expires, repeat the steps above with a fresh token.

## Step 3: list your recordings

```bash
plaud list
```

Output is a table sorted newest first:

```
DATE              TITLE                                            DURATION  ID
2026-04-30 14:30  Kickoff Meeting                                  00:18:36  e1d9aa6c83378b3182cbc20e94b3c6de
2026-04-30 13:00  1:1 with manager                                 00:24:43  4994830a64bea17ebf68c223a9cb8496
...
```

If the table is empty, you have no recordings on the account; create one with your Plaud device and try again.

## Step 4: log out (when you are done)

```bash
plaud logout
```

This deletes the local credentials file. Your Plaud account itself is untouched. To invalidate the token everywhere, log out from web.plaud.ai or rotate it by logging back in.

## What's next

- [`commands/login.md`](./commands/login.md) — full login reference.
- [`commands/list.md`](./commands/list.md) — full list reference.
- [`commands/logout.md`](./commands/logout.md) — full logout reference.
- [`troubleshooting.md`](./troubleshooting.md) — what to do when something goes wrong.
