# `plaud list`

Print every recording on your Plaud.ai account, sorted newest first.

## Synopsis

```
plaud list
```

No flags in v0.1. Filters and JSON output land in later versions.

## Output

A plain-text table with four columns:

```
DATE              TITLE                                            DURATION  ID
2026-04-30 14:30  Kickoff Meeting                                  00:18:36  e1d9aa6c83378b3182cbc20e94b3c6de
2026-04-30 13:00  1:1 with manager                                 00:24:43  4994830a64bea17ebf68c223a9cb8496
```

| Column | Format | Notes |
|---|---|---|
| `DATE` | ISO 8601 in UTC: `YYYY-MM-DD HH:MM` | The recording's `start_time`, converted to UTC. |
| `TITLE` | Plaud's filename for the recording | Often prefixed by Plaud's auto-naming, e.g. `04-30 Kickoff Meeting`. |
| `DURATION` | `HH:MM:SS` | Duration in hours, minutes, seconds. Recordings longer than 99 hours overflow the format (theoretical only). |
| `ID` | 32-char hex | Stable Plaud recording identifier. Used by future commands like `plaud download`. |

## What it does under the hood

- Reads the bearer token and region from your stored credentials.
- Calls `GET /file/simple/web` against the appropriate regional API host with the token in the `Authorization` header.
- Walks pages until the server's reported total is reached.
- Trashed recordings (`is_trash: true` on the server) are excluded by default.

## Errors and recovery

| Error | What it means | What to do |
|---|---|---|
| `Not logged in. Run `plaud login` first.` | No credentials file present. | Run `plaud login` (or `plaud login --token` for SSO accounts). |
| `Token expired or invalid. Run `plaud login` again.` | Server rejected the token (HTTP 401). | Repeat the login flow. The CLI does not retry. |
| `region "..." invalid` | The credentials file has a region the binary does not recognize (likely from manual editing). | Re-run `plaud login` to refresh. |

## Quirks worth knowing

- **Times are UTC.** A meeting at 14:00 local Norwegian time during summer (UTC+02:00) shows as 12:00. The `metadata.json` stored alongside future downloaded recordings will carry both UTC and local time.
- **Empty output is not an error.** If you have zero (active) recordings on the account, `plaud list` prints just the header.
- **Title is whatever Plaud's auto-namer chose** unless you renamed it in the Plaud app or web UI. Plaud's default format is `MM-DD <generated topic>`.

## Related

- [`login.md`](./login.md): getting set up.
- [`logout.md`](./logout.md): undoing it.
