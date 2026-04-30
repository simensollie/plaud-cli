# Architecture

How plaud-cli is laid out, why, and where to find what.

## Top-level layout

```
plaud-cli/
├── cmd/plaud/         # CLI entry, Cobra subcommand wiring
├── internal/
│   ├── api/           # Plaud HTTP client (auth, list, low-level do())
│   └── auth/          # Credentials persistence
├── specs/             # Design docs (one folder per spec)
└── docs/              # User docs and these technical docs
```

`cmd/plaud/` is the CLI surface (one file per subcommand: `main.go`, `login.go`, `list.go`, `logout.go`, plus their `*_test.go` siblings). `internal/` holds reusable building blocks. There is no exported `pkg/` library surface in v0.1; we promote curated subsets only when there is real downstream demand.

## Package responsibilities

### `internal/api`

The HTTP client for Plaud's regional API.

- `regions.go` — `Region` type, the `RegionUS / RegionEU / RegionJP` constants, and `BaseURL(Region) (string, error)`. Single source of truth for endpoint hosts; the CLI never hardcodes URLs.
- `client.go` — `Client` struct, `New(region, token, opts...) (*Client, error)`, options `WithBaseURL` (test seam) and `WithHTTPClient`. The unexported `do(req)` injects `Authorization: bearer <token>`.
- `auth.go` — pre-auth, package-level functions: `DiscoverRegionAPI`, `SendOTP`, `VerifyOTP`. These do not use `Client` because there is no token yet. Sentinels: `ErrInvalidOTP`, `ErrPasswordNotSet`, `ErrAPIError`. The shared `envelope` struct decodes Plaud's `{status, msg, ...}` wrapper.
- `list.go` — `Client.List(ctx, opts...)`, `Recording` (public, time.Time-based), `rawRecording` (internal, JSON wire format), `withListPageSize` (test option). Sentinel: `ErrUnauthorized`.

Tests in this package are white-box (`package api`, not `package api_test`) so they can call unexported helpers like `do(req)` directly. This is intentional: the Authorization-header injection is the seam we care most about, and testing it through every higher-level call would be redundant.

### `internal/auth`

Credentials persistence on disk.

- `credentials.go` — `Credentials` struct, `Save`, `Load`, `Delete`, `ErrNotLoggedIn`. Path resolution honors `XDG_CONFIG_HOME` on POSIX and `APPDATA` on Windows. Atomic write via tmp + rename, explicit `os.Chmod 0600` after write to defeat umask.
- Errors that touch the credentials file deliberately do NOT `%w`-wrap JSON-decode errors, because Go's JSON syntax errors can echo input bytes and Marshal reflection errors can echo field values. Both could leak the token.

This package has no dependency on `internal/api`. The `Credentials.Region` field is a plain `string`; the CLI bridges between `auth.Credentials.Region` and `api.Region` at the command boundary.

### `cmd/plaud`

Cobra wiring.

- `main.go` — root command, `--version`, the F-11 unofficial-disclaimer in `Long`, registers subcommands.
- `login.go` — interactive OTP and `--token` paste paths. `loginCmdOpts` carries a `resolveBaseURL` function injected via `withBaseURLResolver`. Production wires `api.BaseURL`; tests wire a closure that returns an `httptest.Server.URL`.
- `list.go` — loads credentials, builds `api.Client`, calls `Client.List`, renders a `text/tabwriter` table.
- `logout.go` — thin wrapper over `auth.Delete`.

Tests in `cmd/plaud` are in-package (`package main`) so they can drive Cobra subcommands via `cmd.SetIn / SetOut / SetErr / SetArgs / SetContext / Execute`.

## Cross-cutting design principles

### Spec-driven, outcome-based

Every change traces to an FR (e.g. `F-03`) in some `specs/<NNNN>-<slug>/spec.md`. Tests cite the FR ID (`TestLogin_F02_OTPExchangesCodeForToken`). If a change does not fit the active spec, update the spec first.

### TDD, red-green-refactor

The full project history shows this in commits: failing test first, then minimal production code, then refactor. The `internal/api` package is unit-tested via `httptest.NewServer`; no test ever hits a real Plaud endpoint.

### Fail fast, fail often

- Errors propagate up wrapped with `fmt.Errorf("doing X: %w", err)`. They are never swallowed.
- `New(region, token, ...)` rejects empty values immediately rather than producing a client that 401s on every call.
- 401 from a protected endpoint surfaces as `ErrUnauthorized`; the CLI translates that to a single actionable message and exits without retrying.
- 200 with `status != 0` (Plaud's RPC-style envelope) is wrapped in `ErrAPIError` so callers can pattern-match.

### Options pattern for testability

Every subcommand that needs a network seam exposes a `with...` option. Production wires the real implementation; tests wire an `httptest.NewServer.URL`. No environment variable hacks, no hidden flags, no global state. See `withBaseURLResolver` (login) and `withListBaseURLResolver` (list).

### Idempotency

`auth.Save` is atomic; `auth.Delete` is idempotent. Future specs (download, sync) will inherit this stance: re-running a command should be safe.

### Tokens never logged

Spec 0001 F-09 forbids tokens, OTP codes, and `Authorization` headers from appearing in logs, error strings, or stderr. This shows up as:

- Lower-case `bearer` injected in `do()` at the last possible moment before sending.
- `auth.Save / auth.Load` errors deliberately do not `%w`-wrap JSON syntax errors (which can include surrounding bytes).
- A test (`TestLogin_F09_TokenNeverInOutput`) that captures stdout / stderr from the login command and asserts the token text never appears.

## Testing conventions

- White-box tests in `internal/api` (same package).
- In-package Cobra tests in `cmd/plaud` driving commands end-to-end against `httptest.NewServer`.
- All tests set both `XDG_CONFIG_HOME` AND `APPDATA` to a `t.TempDir()` to isolate from the real config dir on every supported OS.
- Test names cite the FR ID they cover.
- `go test -race ./...` is part of CI on Linux, macOS, and Windows.

## Build and release

- `go build -o plaud ./cmd/plaud` produces a `0.1.0-dev`-versioned binary.
- GoReleaser at `.goreleaser.yaml` produces multi-arch archives + `checksums.txt` on tagged pushes (`v*`).
- Version string is injected via `-ldflags "-X main.version={{.Version}}"`.
- `.github/workflows/ci.yml` runs vet, gofmt, and `go test -race` on Linux/macOS/Windows.
- `.github/workflows/release.yml` runs the same tests then `goreleaser release --clean` on tag push.

## What is deliberately NOT here

- No `pkg/` exported library. v0.1 is a CLI; the Go API is internal.
- No telemetry, analytics, or crash reporting.
- No LLM SDK dependencies. Spec 0004 (prompt composition) is explicit about why.
- No code-signing yet. v0.1 ships unsigned binaries with SHA-256 checksums; signing earns its place once the tool has external users.
