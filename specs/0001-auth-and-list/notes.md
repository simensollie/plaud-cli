# Notes: Spec 0001 — Authentication and List

Append-only journal. Newest entry on top. Capture facts, decisions, gotchas, dead ends, links to evidence.

For why this file exists and what to put in it, see `specs/README.md`.

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
