# Plan: Spec 0001 — Authentication and List

Tracer-bullet sequencing. Each phase ends with a user-observable behavior plus the failing test(s) that drove it.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`.

---

## Phase 0: Repo bootstrap

**Outcome:** `go test ./...` runs (with no tests yet) and CI is green on a no-op build. F-11 disclaimer is in place from day one.

**Failing test first (red):**
- `cmd/plaud/main_test.go::TestRoot_F11_HelpStatesUnofficial` — runs the root command with `--help`, asserts the long description contains the unofficial disclaimer.

**Code (green):**
- `go.mod` (`module github.com/simensollie/plaud-cli`, Go 1.23)
- `LICENSE` (MIT)
- `README.md` (skeleton: one-paragraph summary plus a prominent unofficial disclaimer)
- `.gitignore` (Go default + `dist/`, `coverage.out`, `*.local.*`)
- `.github/workflows/ci.yml` (`go vet`, `gofmt -l`, `go test -race ./...` on Linux + macOS + Windows)
- `cmd/plaud/main.go` (Cobra root command with `Long` containing the disclaimer; `--version` only)

**Done when:**
- [x] F-11 test in red, then green
- [x] `go build ./...` clean
- [x] `go vet ./...` clean
- [x] `gofmt -l .` empty
- [x] CI workflow green on a fresh push
- [x] `./plaud --version` prints a version
- [x] `./plaud --help` includes "unofficial community tool, not affiliated with PLAUD LLC"

---

## Phase 1: Region constants and HTTP client skeleton

**Outcome:** Internal API client constructs correct base URLs per region. No network calls yet.

**Failing tests first (red):**
- `internal/api/regions_test.go::TestRegions_F01_BaseURLPerRegion` — table test asserting `us` → `https://api.plaud.ai`, `eu` → `https://api-euc1.plaud.ai`, `jp` → `https://api-jp.plaud.ai`. Unknown region returns an error.
- `internal/api/client_test.go::TestClient_SetsAuthHeader` — when given a token, the client adds `Authorization: bearer <token>` on every request (verified via `httptest.NewServer`).

**Code (green):**
- `internal/api/regions.go` — `Region` type, `BaseURL(Region) (string, error)`.
- `internal/api/client.go` — `Client` struct, `New(region, token, opts...) (*Client, error)`, internal `do(req)` that injects the header.

**Done when:**
- [x] Both tests in red, then green
- [x] No external HTTP made by tests
- [x] Client refuses to construct with empty region or empty token (returns wrapped error)

---

## Phase 2: OTP send and verify

**Outcome:** Three pre-auth package-level functions that drive the captured email + OTP flow:
1. `DiscoverRegionAPI` returns the regional API host for an email (region auto-discovery; informational in v0.1, used by Phase 4 only if user picks unknown region).
2. `SendOTP` requests an OTP code be emailed and returns a short-lived exchange token.
3. `VerifyOTP` redeems the exchange token + 6-digit code, returning the long-lived bearer JWT.
4. `VerifyOTP` surfaces `ErrPasswordNotSet` when the account has no password (otp-login returned `set_password_token`).

**Failing tests first (red):**
- `internal/api/auth_test.go::TestDiscoverRegionAPI_F02_PostsCorrectBodyAndReturnsHost` — `httptest.NewServer` asserts method `POST`, path `/auth/otp-send-code`, JSON body `{username, user_area}`. Server returns `{status:0, data:{domains:{api: srv.URL}}}`. Function returns the host.
- `internal/api/auth_test.go::TestSendOTP_F02_PostsCorrectBodyAndReturnsExchangeToken` — POST to `/auth/otp-send-code`, asserts body shape, server returns `{status:0, token:"exchange-..."}`, function returns `"exchange-..."`.
- `internal/api/auth_test.go::TestVerifyOTP_F02_ReturnsAccessToken` — POST to `/auth/otp-login`, asserts body fields `{token, code, user_area, require_set_password, team_enabled}`, server returns `{status:0, access_token:"jwt...", token_type:"bearer", has_password:true, is_new_user:false}`, function returns `"jwt..."`.
- `internal/api/auth_test.go::TestVerifyOTP_F02_PasswordNotSetReturnsTypedError` — server returns `{status:0, set_password_token:"...", access_token:"...", has_password:false}`, function returns `ErrPasswordNotSet` (we conservatively block here rather than trust an access_token before password set).
- `internal/api/auth_test.go::TestVerifyOTP_F02_BadCodeReturnsTypedError` — server returns `{status:1234, msg:"invalid code"}` (Plaud's pattern: HTTP 200 with non-zero body status), function returns `ErrInvalidOTP`.

**Code (green):**
- `internal/api/auth.go` — package-level `DiscoverRegionAPI`, `SendOTP`, `VerifyOTP`. `AuthOption` (`WithAuthHTTPClient`) for tests. `GlobalAPIBase` constant `https://api.plaud.ai`. Common request envelope helper that decodes Plaud's `{status, msg, ...}` wrapper and surfaces non-zero `status` as a typed error.
- Sentinel errors: `ErrInvalidOTP`, `ErrPasswordNotSet`, `ErrAPIError` (wraps `status` + `msg` for unrecognized codes).

**Done when:**
- [x] All five tests in red, then green
- [x] No real network calls in tests
- [ ] Real-account smoke (manual): `DiscoverRegionAPI` against api.plaud.ai resolves to `api-euc1.plaud.ai` for an EU email. (Defer to Phase 4 wiring.)

---

## Phase 3: Credentials persistence

**Outcome:** A token + region + email round-trips through disk, with mode `0600` on POSIX.

**Failing tests first (red):**
- `internal/auth/credentials_test.go::TestCredentials_F04_RoundTrip` — write, read, assert equal.
- `internal/auth/credentials_test.go::TestCredentials_F04_File0600OnPOSIX` — skip on Windows; assert `os.Stat(...).Mode().Perm() == 0600`.
- `internal/auth/credentials_test.go::TestCredentials_F05_FileShape` — JSON contains `token`, `region`, `email`, `obtained_at`. No `password`.
- `internal/auth/credentials_test.go::TestCredentials_F07_MissingFileReturnsTypedError` — `Load` on absent file returns `ErrNotLoggedIn`.

**Code (green):**
- `internal/auth/credentials.go` — `Credentials` struct, `Save`, `Load`, `Delete`. Path resolution honors `XDG_CONFIG_HOME` on POSIX and `%APPDATA%` on Windows.
- Sentinel: `ErrNotLoggedIn`.

**Done when:**
- [x] All four tests green (plus two extras: idempotent Delete; token-not-in-error-strings)
- [x] Path resolution works in tests via `t.Setenv("XDG_CONFIG_HOME", t.TempDir())`
- [x] Tokens never appear in any error string returned by this package

---

## Phase 4: `plaud login` command (interactive OTP)

**Outcome:** Running `plaud login` end-to-end against a fake API server (in tests) and a real Plaud account (manual smoke) writes a valid credentials file.

**Failing tests first (red):**
- `cmd/plaud/login_test.go::TestLogin_F01_F02_HappyPath` — drives the command with simulated stdin (`region\nemail\ncode\n`) against an `httptest` Plaud, asserts a credentials file lands at the temp `XDG_CONFIG_HOME` with the expected contents.
- `cmd/plaud/login_test.go::TestLogin_F02_InvalidOTPExitsNonZero` — server 401s on verify, command prints a clear actionable message and exits non-zero.
- `cmd/plaud/login_test.go::TestLogin_F09_TokenNeverInOutput` — captured stdout / stderr never contains the bearer token.

**Code (green):**
- `cmd/plaud/login.go` — Cobra command, prompts via `bufio.Scanner` over `cmd.InOrStdin()`, calls `internal/api`, writes via `internal/auth`.
- Wire into `cmd/plaud/main.go`.

**Done when:**
- [ ] All three tests green
- [ ] Manual smoke: real `plaud login` against a live EU account writes a `credentials.json` and the second smoke phase (Phase 6) can read it
- [ ] Error from invalid code is human-readable, no Go stack trace

---

## Phase 5: `plaud login --token <jwt>` (paste path)

**Outcome:** Users with blocked OTP email or SSO accounts can paste a token and skip the OTP exchange. Region prompt still applies.

**Failing tests first (red):**
- `cmd/plaud/login_test.go::TestLogin_F03_TokenFlagSkipsOTP` — `--token eyJ... --region eu --email me@x` writes a credentials file without any HTTP traffic to the OTP endpoints (verify by recording `httptest` request count).
- `cmd/plaud/login_test.go::TestLogin_F03_TokenFlagRejectsEmpty` — empty token after `--token=` exits non-zero with a clear message.

**Code (green):**
- Branch in `cmd/plaud/login.go` that bypasses OTP when `--token` is set.

**Done when:**
- [ ] Both tests green
- [ ] Manual smoke: pastes a token from `localStorage.tokenstr` and `plaud list` (next phase) works against it

---

## Phase 6: `plaud list`

**Outcome:** A logged-in user runs `plaud list` and sees a sorted, human-readable table of every recording on the account.

**Failing tests first (red):**
- `internal/api/list_test.go::TestList_F06_PaginatesUntilExhausted` — `httptest` returns three pages, client returns concatenated slice.
- `internal/api/list_test.go::TestList_F06_RecordingShape` — fields populated: id, title, recorded_at, duration_seconds.
- `cmd/plaud/list_test.go::TestList_F06_TableOutput` — golden file under `testdata/golden/0001/list.txt`. ISO 8601 dates, `HH:MM:SS` durations, sorted newest first.
- `cmd/plaud/list_test.go::TestList_F07_NotLoggedIn` — no credentials file → exits non-zero with "Not logged in. Run `plaud login` first."
- `cmd/plaud/list_test.go::TestList_F08_TokenInvalid` — server 401 → exits non-zero with "Token expired or invalid. Run `plaud login` again." No retry.

**Code (green):**
- `internal/api/list.go` — `List(ctx) ([]Recording, error)`, handles pagination.
- `cmd/plaud/list.go` — loads credentials, calls `List`, formats the table.

**Done when:**
- [ ] All five tests green
- [ ] Golden file matches manual eyeballing on a real account
- [ ] Manual smoke on a real account with at least 5 recordings produces a readable table

---

## Phase 7: `plaud logout`

**Outcome:** `plaud logout` deletes the credentials file. Idempotent.

**Failing tests first (red):**
- `cmd/plaud/logout_test.go::TestLogout_DeletesCredentialsFile`
- `cmd/plaud/logout_test.go::TestLogout_IdempotentWhenAlreadyLoggedOut`

**Code (green):**
- `cmd/plaud/logout.go` — calls `internal/auth.Delete()`.

**Done when:**
- [ ] Both tests green
- [ ] Manual smoke: `plaud logout && plaud list` prints "Not logged in" cleanly

---

## Phase 8: Cross-platform smoke and release

**Outcome:** A signed-or-unsigned binary from a GitHub release runs on macOS, Linux, and Windows; all four commands work end-to-end.

**Code:**
- `goreleaser.yaml` (multi-arch matrix, no signing in v0.1)
- `.github/workflows/release.yml` (tagged push triggers GoReleaser)

**Done when:**
- [ ] `git tag v0.1.0 && git push --tags` produces release artifacts on GitHub
- [ ] `plaud --version` from each binary reports `v0.1.0`
- [ ] Manual end-to-end (login → list → logout) on macOS, Linux, and Windows
- [ ] All §8 acceptance criteria from `spec.md` ticked

---

## Acceptance walk-through (final sign-off)

To be performed on a real Plaud.ai account once Phases 0-8 are checked. Reproduces the spec's §8 criteria in one session:

1. Fresh machine. Download the binary for this platform from the v0.1.0 GitHub release.
2. `plaud --version` prints `v0.1.0`.
3. `plaud login`, pick `eu`, enter email, enter the OTP that arrives, succeed.
4. `cat ~/.config/plaud/credentials.json` shows the four fields, mode `0600` on POSIX.
5. `plaud list` prints every recording on the account, newest first, dates and durations correct.
6. `plaud logout`. `plaud list` then prints "Not logged in".
7. Repeat 1-6 on the other two operating systems.

When all seven steps pass on all three OSes, set `Status: Done <YYYY-MM-DD>` in `spec.md`.
