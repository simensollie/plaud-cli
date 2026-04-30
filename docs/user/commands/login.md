# `plaud login`

Authenticate with your Plaud.ai account and store a bearer token locally for subsequent commands.

## Synopsis

```
plaud login [--region us|eu|jp] [--email <addr>] [--token <jwt>]
```

## Two paths

### Interactive OTP

```bash
plaud login
```

Prompts for:

1. Region (`us`, `eu`, or `jp`).
2. Email.
3. The 6-digit code Plaud emails to you.

You can also pre-supply the region and email via flags to skip those prompts:

```bash
plaud login --region eu --email you@example.com
```

The OTP code is always entered interactively (no flag to bypass; treating an OTP as a flag value is a common security mistake).

### Token paste

For accounts where OTP is not available (Apple or Google SSO without a password set):

```bash
plaud login --token <jwt> --region <us|eu|jp> --email <addr>
```

Where `<jwt>` is the value of `pld_tokenstr` from your browser's localStorage on web.plaud.ai (with the `bearer ` prefix and surrounding quotes removed). See [getting-started.md](../getting-started.md#step-2b-token-paste-login) for the exact extraction steps.

All three flags are required when `--token` is set.

## Where the token is stored

| OS | Path |
|---|---|
| Linux / macOS | `${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json` |
| Windows | `%APPDATA%\plaud\credentials.json` |

File permissions on POSIX: `0600` (owner read/write only). On Windows the file lives under your per-user `%APPDATA%`, which inherits the user-only ACL of that folder.

The file contains four fields: `token`, `region`, `email`, `obtained_at`. No password is ever stored.

## Errors and recovery

| Error | What it means | What to do |
|---|---|---|
| `Your Plaud account does not have a password set...` | Account is SSO-only. | Either set a password at https://web.plaud.ai/account, or use `plaud login --token`. |
| `region "..." invalid: must be one of us, eu, jp` | Typo, or region not recognized. | Re-run with one of the three valid values. |
| `--token requires --region` | You passed `--token` without `--region`. | Add `--region`. |
| `--token must be a non-empty bearer JWT` | The flag value was empty. | Verify the token was actually copied into the command (not just the placeholder text). |

See [`troubleshooting.md`](../troubleshooting.md) for deeper recovery steps.

## Privacy notes

- The CLI never logs the token, the OTP code, or the `Authorization` header. Even with future verbose-output flags, these are redacted at source.
- The token persisted on disk is the bearer JWT, not your password (passwords are never asked for).
- `plaud logout` removes the local file; the server-side session is unaffected. Rotate by logging out from web.plaud.ai and back in if you want true revocation.

## Related

- [`list.md`](./list.md) — what to do once you are logged in.
- [`logout.md`](./logout.md) — undoing this command.
