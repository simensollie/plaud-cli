# Notes: Spec 0001 — Authentication and List

Append-only journal. Newest entry on top. Capture facts, decisions, gotchas, dead ends, links to evidence.

For why this file exists and what to put in it, see `specs/README.md`.

---

## 2026-05-01 — Phase 4 OTP-login smoke blocked on web "set password"

User's existing Apple-SSO Plaud account does not have a password set. The web "Set password" UI does not appear to work for this account; user has filed a support ticket with Plaud.

Implication: we cannot manually smoke `plaud login` (the OTP path) end-to-end yet, because `otp-login` would return `set_password_token` for this account and our CLI would correctly stop with `ErrPasswordNotSet`. Phase 4 ships with full unit test coverage but the real-account smoke is deferred.

Workaround for the rest of v0.1 development: implement Phase 5 (`plaud login --token <jwt>`) next, then use a JWT pasted from the browser's localStorage to test Phases 6-7 against the real account. This validates everything except the OTP exchange itself, which the unit tests already cover at the request/response shape level.

If the support ticket resolves before v0.1 ships, do the OTP smoke then. If not, document the workaround in the README's troubleshooting section and proceed.

---

## 2026-05-01 — OTP flow captured (full walkthrough from new-account create)

User captured a second HAR (`create.web.plaud.ai.har`, gitignored) covering an email + OTP login and the post-login flow. This is the spec's target auth path. All endpoints + body shapes documented below.

### The flow

**Step 1 — Region discovery (global host).**
`POST https://api.plaud.ai/auth/otp-send-code`
- Request: `{username: "<email>", user_area: "<2-letter ISO 3166-1 country code, e.g. NO, US, JP>"}`
- Response: `{status, msg, data: {domains: {api: "https://api-euc1.plaud.ai"}}}`

`user_area` value observed: `"NO"`. For v0.1, default to a sensible value derived from `$LANG` (e.g. `nb_NO.UTF-8` → `NO`) with `US` as the fallback, and expose `--user-area` for override. We do not yet know whether the server rejects the request if the area is missing or wrong; first implementation should test both presence and obvious-wrong values.

The global host returns the correct regional API host for this user. **Implication:** region auto-detection is feasible. F-AUTH-07 currently asks the user to pick at login; we could instead just ask for email and resolve the region. Out of scope for v0.1 but worth a future spec.

**Step 2 — Send OTP (regional host).**
`POST https://api-euc1.plaud.ai/auth/otp-send-code`
- Request: `{username: "<email>", user_area: "<area-code>"}`
- Response: `{status, msg, request_id, token: "<one-time exchange token>"}`

The `token` here is **not** the bearer token. It's a one-time identifier tying the email to the in-flight OTP. We hold it client-side and pass it to the next call.

**Step 3 — Verify OTP and obtain access token.**
`POST https://api-euc1.plaud.ai/auth/otp-login`
- Request: `{token: "<from step 2>", code: "<6-digit OTP>", user_area, require_set_password: bool, team_enabled: bool}`
- Response: `{status, msg, request_id, access_token: "<JWT>", token_type: "bearer", has_password: bool, is_new_user: bool, set_password_token: "<...>" | null}`

- `access_token` is the bearer JWT we persist.
- If `set_password_token` is present (typically when `is_new_user: true` or `has_password: false`), the user has not yet set a password. The web client forces a password set before the access_token works for protected calls. We do not yet know if the access_token alone is usable in this state.

**Step 4 (only when password unset) — Set password.**
`POST https://api-euc1.plaud.ai/auth/set-password-issue-token`
- Request: `{password, password_encrypted: bool, set_password_token, user_area, team_enabled}`
- Response: `{status, msg, request_id, access_token: "<JWT>", token_type: "bearer", login_count_per_hour, login_total_per_hour}`

This mints a fresh `access_token` that supersedes the one from step 3.

### v0.1 design choice for the password-set edge case

If `otp-login` returns `set_password_token` (account never set a password, e.g. SSO-only or brand-new), the v0.1 CLI surfaces a clear error and stops:

> Your Plaud account does not have a password set. Open https://web.plaud.ai, set a password under Account, then run `plaud login` again.

Implementing the password-set flow ourselves doubles the auth surface area for an edge case most users won't hit. Defer to a future spec if demand emerges.

### List endpoint (Phase 6 prep)

`GET https://api-euc1.plaud.ai/file/simple/web?skip=0&limit=99999&is_trash=2&sort_by=start_time&is_desc=true`
- Response wrapper: `{status, msg, request_id, data_file_list: [...], data_file_total: N}`
- Per-recording fields (all 25 captured): `id` (32-char hex), `filename` (user title), `fullname` (36-char hex, likely the audio file name on storage), `filesize` (number, bytes), `filetype` (string, ~9 chars, e.g. file extension), `file_md5` (32-char hex), `start_time`, `end_time`, `edit_time`, `duration`, `timezone`, `zonemins`, `scene`, `serial_number` (7-char), `version`, `version_ms`, `wait_pull` (numbers), `is_trash`, `is_trans` (has transcript), `is_summary`, `is_markmemo`, `ori_ready` (booleans), `keywords`, `filetag_id_list` (arrays), `edit_from` (e.g. `"web"`).

Numeric times: fields without `_ms` suffix are likely epoch seconds (`start_time`, `end_time`, `edit_time`); those with `_ms` are milliseconds (`version_ms`). To verify on first real call.

### Custom request headers used by the web client

Every authenticated GET sends:

| Header | Value |
|---|---|
| `app-language` | `en` (or user's UI language) |
| `app-platform` | `web` |
| `edit-from` | `web` |
| `x-device-id` | 16-hex chars, randomized per browser session |
| `x-pld-user` | 64-hex chars = user's account ID (returned by `/user/me`, NOT a credential) |
| `x-request-id` | random short string per request |
| `timezone` | IANA tz, e.g. `Europe/Oslo` |

Our client should mimic at least `app-platform`, `edit-from`, `x-device-id` (generated once at install, persisted), and `x-request-id` (random per call). `x-pld-user` we cannot send until we've called `/user/me` once after login; we can either pre-fetch on login and store, or fetch lazily on first protected call.

### Bearer-vs-cookie auth question — still open

The HAR still has cookies stripped (Chrome's HAR export default). The web client almost certainly uses session cookies (the otp-login response had `access-control-allow-credentials: true`, which is required for cross-origin credentialed cookies). But the JWT-shaped `access_token` returned by otp-login is the same kind of thing the prior-art tools (`jaisonerick/plaud-cli`, `sergivalverde/plaud-toolkit`, the Obsidian plugin) use as `Authorization: Bearer <jwt>` against the same endpoints.

Decision: ship Phase 2 with `Authorization: bearer <access_token>` and the custom headers above. Validate against `/user/me` immediately after login as a smoke test. If it 401s, pivot to a cookie-jar client (cookies returned by the otp-login response would survive a non-stripping HTTP client even if Chrome's HAR export hides them).

### Status of open questions in spec.md §7

- **Q1 (OTP send endpoint):** RESOLVED. `POST {global}/auth/otp-send-code` with `{username, user_area}`, then `POST {region}/auth/otp-send-code` with the same body.
- **Q2 (EU host accepts the same flow):** RESOLVED. Confirmed working against `api-euc1.plaud.ai`.

---

## 2026-04-30 — Findings from Apple-SSO HAR capture

User captured a HAR from web.plaud.ai during an Apple SSO login. File at `specs/0001-auth-and-list/web.plaud.ai.har` (gitignored, never commit).

**Confirmed.**
- EU host: `https://api-euc1.plaud.ai`. Matches our `RegionEU` constant.
- List endpoint: `GET /file/simple/web?skip=0&limit=99999&is_trash=2&sort_by=start_time&is_desc=true`. Pagination via `skip` + `limit`. Filter via `is_trash` (`0` = active, `2` = include deleted). Sort via `sort_by` + `is_desc`.
- List response shape: `{data_file_list: [...], data_file_total: N, msg, request_id, status}`. Wrapper, not bare array.
- Apple SSO callback: `POST /auth/sso-callback` with body `{id_token, sso_from, sso_type, user_area}` returns `{access_token: <273-char JWT>, token_type: "bearer", login_count_per_hour, login_total_per_hour, msg, request_id, status}`.
- Workspace token: `POST /user-app/auth/workspace/token/{ws_id}` returns `{data: {workspace_token, refresh_token, expires_in: 86400, wt_expires_at, refresh_expires_at, member_id, workspace_id, role, status}}`. Workspace tokens are 548-char strings, ttl 24h.
- Custom request headers on every authenticated call: `app-language: en`, `app-platform: web`, `edit-from: web`, `x-device-id: [object Object]` (yes, literally — web client bug, server tolerates it), `x-pld-user: <64-hex>`, `x-request-id: <random>`.

**Important caveat: HAR strips cookies on export.**
Chrome DevTools redacts the `Cookie` header (and `Set-Cookie`) by default when saving HAR. `:authority`, `:method` etc. are visible but no `Authorization` and no `Cookie` header are present anywhere on api-euc1 calls. The web client almost certainly uses cookie-based session auth set by `Set-Cookie` on `/auth/sso-callback`, which we can't see.

**`x-pld-user` is the user's account ID, not an auth credential.**
The 64-hex value (`6370ef50d62844e3...`) appears in the response bodies of `/user/me` and `/user-app/profile/account/me`, so it's the user_id used for routing or telemetry, not a secret. Our auth carrier is *probably* a cookie set by sso-callback that the HAR redacted.

**Implications for our client.**
1. The current `Authorization: bearer <token>` in `internal/api/client.go` is unverified against the web app's cookie-based scheme. The same endpoints almost certainly accept `Authorization: Bearer <jwt>` for non-browser clients (prior-art Obsidian plugin has used this for ~10 months without breakage), but we have not confirmed it against the live API yet.
2. We should add the custom request headers (`app-platform: web` etc.) to `do(req)` so we present as a known client. `x-pld-user` we cannot send until we know the user's account id, which means the *first* call after login is `GET /user/me` (no `x-pld-user`) to learn it, then subsequent calls include it.
3. To confirm the bearer-auth path works against `/file/simple/web`, the simplest test is: paste the 273-char `access_token` from the HAR's sso-callback response into `plaud login --token <jwt>` (once Phase 5 lands) and call `plaud list`. If it 401s, we know we need cookie-based auth.

**What's still unknown.**
- Email+OTP send endpoint (path, body shape).
- Email+OTP verify endpoint (path, body shape, response field name for the bearer token — likely also `access_token`).
- Whether `Authorization: Bearer` works against api-euc1 endpoints, or whether we must replicate the cookie-based session.

**Plan.**
- Phase 1 stays as shipped; the auth-header decision is captured as "to verify" rather than "wrong".
- Before Phase 2: ask user for an **email-OTP login capture**, ideally as `Copy as cURL (bash)` on the two key requests (OTP send and OTP verify). cURL export includes cookies; HAR export does not.
- During Phase 6 implementation, exercise the bearer-auth assumption against a real account using the JWT from sso-callback. If it 401s, pivot to a cookie-jar based client.

---

## 2026-04-30 — Phase 0 complete; Node 20 deprecation deferred

Phase 0 done. CI green on Linux + macOS + Windows + lint. F-11 disclaimer tested and shipping in `--help` and README + NOTICE.

Deferred: GitHub Actions emitted a soft deprecation warning that `actions/checkout@v4` and `actions/setup-go@v5` use Node.js 20, which becomes mandatory Node.js 24 on 2026-06-02. Not blocking; we will revisit before that date or when v5+ of those actions ship.

---

## 2026-04-30 — Binary name and trademark posture

Decided: binary is `plaud`. Repo stays `plaud-cli`.

Risk considered: PLAUD LLC ships an official CLI later. Likelihood low, impact rename-in-a-release. Trademark legal trouble assessed as very low given (a) clear unofficial framing in `--help` and README (now F-11), (b) no logo or branding reuse, (c) precedent of community CLIs using product names freely (`youtube-dl`, `slack-cli`, `notion-cli`, etc.).

Fallback name `plaudr` held in reserve. If we ever need to rename, the migration is: ship a release where `plaud` is a thin shim that prints a deprecation notice and execs `plaudr`, give users one minor version to switch, then remove `plaud`.

---

## 2026-04-30 — Spec opened

Initial draft. v0.1 scope deliberately narrow: login, list, logout, version, help. Everything else (download, sync, prompt composition) waits for its own spec.

Decisions made before any code:

- **Language:** Go 1.23+. Single static binary, cross-compile easy. Counter (.NET) considered and rejected for OSS distribution ergonomics.
- **License:** MIT.
- **Binary name:** `plaud`. Repo name: `plaud-cli`. Conflict potential with `jaisonerick/plaud-cli` exists; flagged in spec §7 but not blocking.
- **Auth:** OTP-first. `--token` paste fallback included in v0.1 (not deferred) so SSO users have a path on day one.
- **Region:** prompted at login. Auto-detection deferred because picking the wrong region returns 401 with no useful error, and the user is closer to "what country am I in" than the binary is.

Open facts to confirm during implementation (will land in this file as they resolve):

- Exact OTP send endpoint path + body shape on the EU host.
- Exact OTP verify endpoint and the token field name in the response.
- Whether the `list` endpoint paginates with cursor + limit, offset + limit, or page + page_size.

Sources to cross-reference (do not copy code, look up endpoint shapes only):

- https://github.com/jaisonerick/plaud-cli (Go, has `cmd/login.go` and `cmd/list.go`; useful for endpoint paths)
- https://github.com/sergivalverde/plaud-toolkit (TS, `packages/core/src/auth.ts` and `client.ts`)

When endpoint shapes are confirmed via a network capture, record the request method, path, headers, body, and response shape here, with the date and capture method.
