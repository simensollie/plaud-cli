# Spec 0001: Authentication and List

**Status:** Active
**Created:** 2026-04-30
**Updated:** 2026-04-30
**Owner:** @simensollie
**Target version:** v0.1

A small, single-binary CLI that authenticates against your Plaud.ai account and lists your recordings. **Nothing more in v0.1.** Download, sync, prompt composition, etc. live in their own future specs and are explicitly out of scope here.

---

## 1. Goal

Prove the end-to-end loop: log in to Plaud.ai with the user's own credentials, store a bearer token locally, and use it to list every recording on the account. If this works reliably across macOS, Linux, and Windows, all later features (download, sync, prompts) reuse the same primitives.

## 2. Commands

| Command | Behavior |
|---|---|
| `plaud login` | Interactive OTP login. Prompts: region → email → 6-digit code (emailed by Plaud) → done. Stores bearer token on disk. |
| `plaud login --token <jwt>` | Skip OTP. Accept a token pasted from `localStorage.tokenstr` in the browser. Useful if OTP email is blocked or the user is on Google SSO. Region prompt still applies. |
| `plaud list` | Print every recording on the account as a plain-text table: `date`, `title`, `duration`, `id`. Sorted newest first. |
| `plaud logout` | Delete the stored credentials file. Does not invalidate the session on Plaud's side. |
| `plaud --version` | Print version. |
| `plaud --help` | Print usage. |

That is the entire surface for v0.1. No flags beyond what's listed.

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | `plaud login` prompts for region (`us`, `eu`, `jp`) and uses the corresponding API host. EU = `api-euc1.plaud.ai`, US = `api.plaud.ai`, JP = `api-jp.plaud.ai`. | Must |
| F-02 | OTP flow: POST email → server emails a 6-digit code → user types code → CLI exchanges code for a bearer token. | Must |
| F-03 | `--token <jwt>` accepts a pre-acquired token directly. | Must |
| F-04 | Credentials persisted to `${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json` (POSIX) or `%APPDATA%\plaud\credentials.json` (Windows), file mode `0600` on POSIX. | Must |
| F-05 | Credentials file contains: `token`, `region`, `email`, `obtained_at` (ISO 8601). No password is ever stored. | Must |
| F-06 | `plaud list` reads the stored credentials, calls the Plaud API to enumerate recordings, prints a human-readable table. Pagination handled internally. | Must |
| F-07 | If no credentials file exists, `plaud list` prints a clear message: "Not logged in. Run `plaud login` first." | Must |
| F-08 | If the token is rejected by the server (HTTP 401), print "Token expired or invalid. Run `plaud login` again." Do not retry. | Must |
| F-09 | Tokens, OTP codes, and `Authorization` headers are never written to logs or stderr, even with verbose output. | Must |
| F-10 | All dates in output use ISO 8601 (`2026-04-30 14:30`). Durations shown as `HH:MM:SS`. | Must |
| F-11 | `plaud --help` and the project README state that this is an unofficial community tool, not affiliated with or endorsed by PLAUD LLC. The disclaimer text appears once in `--help` (e.g. in the long description) and prominently in the README. | Must |

## 4. Storage

```
~/.config/plaud/
└── credentials.json          # 0600, JSON: {token, region, email, obtained_at}
```

That is the entire on-disk footprint for v0.1.

## 5. Tech stack

- **Language:** Go 1.23+. Single static binary, cross-compile for Linux / macOS / Windows.
- **CLI framework:** `cobra`.
- **HTTP:** stdlib `net/http`.
- **Build:** `go build` for v0.1. GoReleaser comes in v0.2.
- **Tests:** unit tests on the API client (using `httptest`), unit tests on credentials read/write. No end-to-end tests against real Plaud in v0.1.

## 6. Out of scope for v0.1

Everything not listed in §2 and §3. Specifically:

- Downloading audio, transcripts, or summaries.
- Sync, watch mode, incremental fetch.
- Prompt composition / piping to LLMs.
- Multi-account profiles.
- Filter flags (`--since`, `--tag`, `--match`).
- JSON output (`--format json`).
- Auto-refresh of expired tokens.
- OS keychain integration.
- Cross-platform installers (Homebrew, Scoop, winget). Until v1.0, install = `go install` or download a binary from a GitHub release.
- Config file. CLI flags + env vars only in v0.1.
- Localization (Norwegian). English-only output in v0.1.
- Region auto-detection. User picks at login.

These are tracked in a separate roadmap document once we have v0.1 working.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | Does `Authorization: bearer <jwt>` work against `api-euc1.plaud.ai` protected endpoints, or do we need a cookie jar? | Validate during Phase 6 with a real-account smoke. Prior art has used bearer for ~10 months; expectation is that it works. If it 401s, pivot to cookie-jar client. |

Resolved:

- **Repo / binary name.** Repo `plaud-cli`, binary `plaud`. Fallback `plaudr` held in reserve. See `notes.md` 2026-04-30.
- **OTP endpoints.** Captured. `POST {global}/auth/otp-send-code` for region discovery, `POST {region}/auth/otp-send-code` to obtain a one-time exchange token, `POST {region}/auth/otp-login` to redeem. See `notes.md` 2026-05-01.
- **EU host parity.** Confirmed against `api-euc1.plaud.ai`. Same shape as global; differs only in domain.

## 8. Acceptance criteria for v0.1

v0.1 is done when:

1. A new user can run `plaud login`, complete the OTP flow against a real Plaud.ai account, and see their token persisted at the documented path with mode `0600`.
2. `plaud list` prints every recording on that account as a readable table, with correct titles, dates, and durations.
3. Steps 1 and 2 work on macOS, Linux, and Windows.
4. The two unit-test suites (API client, credentials) pass in CI.
5. A binary built from `main` is downloadable from a GitHub release and `plaud --version` reports the correct version.
