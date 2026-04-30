# Notes: Spec 0001 — Authentication and List

Append-only journal. Newest entry on top. Capture facts, decisions, gotchas, dead ends, links to evidence.

For why this file exists and what to put in it, see `specs/README.md`.

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
