# Architecture

How plaud-cli is laid out, why, and where to find what.

## Top-level layout

```
plaud-cli/
├── cmd/plaud/         # CLI entry, Cobra subcommand wiring
├── internal/
│   ├── api/           # Plaud HTTP client (auth, list, detail, temp-url, audio)
│   ├── archive/       # On-disk archive layout, slug, atomic writes, metadata
│   └── auth/          # Credentials persistence
├── specs/             # Design docs (one folder per spec)
└── docs/              # User docs and these technical docs
```

`cmd/plaud/` is the CLI surface (one file per subcommand: `main.go`, `login.go`, `list.go`, `logout.go`, `download.go`, plus their `*_test.go` siblings). `internal/` holds reusable building blocks. There is no exported `pkg/` library surface in v0.1; we promote curated subsets only when there is real downstream demand.

## Package responsibilities

### `internal/api`

The HTTP client for Plaud's regional API.

- `regions.go`: `Region` type, the `RegionUS / RegionEU / RegionJP` constants, and `BaseURL(Region) (string, error)`. Single source of truth for endpoint hosts; the CLI never hardcodes URLs.
- `client.go`: `Client` struct, `New(region, token, opts...) (*Client, error)`, options `WithBaseURL` (test seam) and `WithHTTPClient`. The unexported `do(req)` injects `Authorization: bearer <token>`. The `audioClient` field holds a separate `http.Client` with no total timeout, used for the signed-URL S3 calls; bookkeeping calls keep the 30-second total timeout.
- `auth.go`: pre-auth, package-level functions: `DiscoverRegionAPI`, `SendOTP`, `VerifyOTP`. These do not use `Client` because there is no token yet. Sentinels: `ErrInvalidOTP`, `ErrPasswordNotSet`, `ErrAPIError`. The shared `envelope` struct decodes Plaud's `{status, msg, ...}` wrapper.
- `list.go`: `Client.List(ctx, opts...)`, `Recording` (public, time.Time-based), `rawRecording` (internal, JSON wire format), `withListPageSize` (test option). Sentinel: `ErrUnauthorized`.
- `detail.go`, `Client.Detail(ctx, id) (*RecordingDetail, error)`. Calls `/file/detail/{id}`, walks `content_list` for the `transaction` (transcript) and `auto_sum_note` (summary) artifacts, and resolves them: summary inline from `pre_download_content_list` when available, otherwise via the signed S3 URL. Maps Plaud's wire segment shape (`start_time`, `end_time`, `content`, `speaker`, `original_speaker`) to canonical `Segment` (`start_ms`, `end_ms`, `text`). `Segment` mirrors `archive.Segment`'s JSON tags so the CLI converts between the two via plain struct conversion at the package boundary.
- `temp_url.go`, `Client.TempURL(ctx, id) (string, error)`. One-call wrapper around `/file/temp-url/{id}`; returns the audio `temp_url`. `temp_url_opus` is currently ignored (v0.2 scope).
- `audio.go`, `Client.HeadAudio` and `Client.DownloadAudio` for the signed S3 URL. Never sends `Authorization` (auth is in the URL). HTTP 401/403 from S3 surfaces as `ErrSignedURLExpired` so the caller can re-fetch the temp-url and retry once. `DownloadAudio` streams via `idleTimeoutReader` (30 s without progress aborts) and computes the served-bytes MD5 inline.
- `transcript_fetch.go`, unexported `fetchSignedJSON` helper that GETs a content-storage URL with no `Authorization`. Go's transport handles `Content-Encoding: gzip` transparently, so the returned bytes are the decoded payload.
- `idle_reader.go`, `idleTimeoutReader` wraps an `io.ReadCloser` and aborts the read when no bytes arrive within the configured idle window. Used by `DownloadAudio` (F-15).

Tests in this package are white-box (`package api`, not `package api_test`) so they can call unexported helpers like `do(req)` directly. This is intentional: the Authorization-header injection is the seam we care most about, and testing it through every higher-level call would be redundant.

### `internal/auth`

Credentials persistence on disk.

- `credentials.go`: `Credentials` struct, `Save`, `Load`, `Delete`, `ErrNotLoggedIn`. Path resolution honors `XDG_CONFIG_HOME` on POSIX and `APPDATA` on Windows. Atomic write via tmp + rename, explicit `os.Chmod 0600` after write to defeat umask.
- Errors that touch the credentials file deliberately do NOT `%w`-wrap JSON-decode errors, because Go's JSON syntax errors can echo input bytes and Marshal reflection errors can echo field values. Both could leak the token.

This package has no dependency on `internal/api`. The `Credentials.Region` field is a plain `string`; the CLI bridges between `auth.Credentials.Region` and `api.Region` at the command boundary.

### `internal/archive`

The on-disk archive layer. Owns the per-recording folder shape, slug folding, atomic writes, the canonical `metadata.json` and `transcript.json` schemas, the transcript renderers, and Windows long-path handling. Independent of the HTTP layer.

- `layout.go`, `RecordingFolder(root, r) (string, error)` (UTC `<root>/YYYY/MM/YYYY-MM-DD_HHMM_<slug>/`), `EnsureRoot(root)` (auto-create with first-creation signal), `ProbeWritable(dir)` (sentinel write+remove, run before any network call). Sentinel: `ErrPathNotDirectory`.
- `slug.go`, `Slug(title)` and `SlugWithCollision(title, id, collide)`. Strip trailing audio extension, fold `æ→ae` / `ø→o` / `å→a`, NFKD-decompose remaining combining marks, lowercase, non-word to `_`, collapse, trim. Cap at 60 chars post-fold with word-boundary truncate inside the last 10 chars when available. Empty slug falls back to `untitled`. The single non-stdlib dependency in this package, `golang.org/x/text/unicode/norm`, lives here.
- `atomic.go`, `WriteAtomic(path, data)` and `SweepPartials(folder)`. Each artifact is written to `<name>.partial` next to the destination, fsync'd, then `os.Rename`'d (atomic on the same filesystem; the temp lives in the target folder so cross-fs renames are impossible by construction). Stale `.partial` files are swept at the start of each run, before any idempotency check.
- `metadata.go`, the `Metadata`, `MetaAudio`, `MetaTranscript`, `MetaSummary`, and `IncludeSet` types, plus `NewMetadata`, `MarshalMetadata` / `UnmarshalMetadata`, `RebuildMetadataFromDisk`, the `ShouldRewriteTranscript` / `ShouldRewriteSummary` predicates, and the `MarkVerified` / `MarkArtifactWritten` setters. JSON is pretty-printed with sorted keys and a trailing newline, so transcript SHA-256 is stable across rewrites and diffs are predictable for users versioning the archive.
- `render.go`, the `Transcript` and `Segment` types, plus `Render(tr, format)` for `md`, `srt`, `vtt`, `txt`. The renderers consume only `Transcript`; they never touch the network.
- `winpath.go` (and `winpath_other.go`), `PrefixLongPath(p)`. On Windows, returns `p` prefixed with `\\?\` (or `\\?\UNC\...` for UNC inputs) to lift the 260-char `MAX_PATH` limit. On non-Windows it is a no-op.

**Layering.** `cmd/plaud → internal/archive → (no upward deps)`. `internal/archive` does **not** import `internal/api`. The orchestration in `cmd/plaud/download.go` does the conversion between `api.Segment` and `archive.Segment` (the two types share JSON tags and identical layouts, so it is a plain struct conversion at the boundary). This keeps the archive package unit-testable without pulling the HTTP layer in, and keeps the dependency direction one-way.

**Design decisions worth preserving** (each cross-references its FR ID):

- **Per-artifact idempotency** ([F-07](../../specs/0002-download-recordings/spec.md#3-functional-requirements)). Audio is gated by S3 `ETag` (`metadata.audio.s3_etag`), the served-bytes MD5 for single-part uploads. Transcript and summary are gated by SHA-256 of the canonical bytes (`metadata.transcript.sha256`, `metadata.summary.sha256`). Derived transcript files (`transcript.{md,srt,vtt,txt}`) are always regenerated when `transcript.json` changes, never touched when it does not.
- **Two metadata timestamps.** `fetched_at` bumps only when an artifact write actually occurred. `last_verified_at` bumps on every successful run, including no-op verifications. `--force` bumps both even when bytes are unchanged.
- **`file_md5` vs `s3_etag`.** The list endpoint's `file_md5` is the MD5 of Plaud's original `.opus` upload, **not** the served `.mp3` bytes. plaud-cli records it as `metadata.audio.original_upload_md5` for audit only and never uses it for idempotency. The audit field is `omitempty`.
- **Atomic writes via sibling tempfile.** `<name>.partial` lives next to its destination so the rename cannot cross a filesystem boundary, and a crashed run leaves at most a stale `.partial` that the next run sweeps.
- **Slug folding pipeline.** Strip audio extension, fold Norwegian diacritics, NFKD-decompose, lowercase, non-word to `_`, cap to 60 chars with word-boundary truncate, fall back to `untitled`, append a 6-char ID suffix on collision. Cross-platform identical (rejected per-OS truncation as it would produce different folder names for the same recording).
- **Windows long-path.** Absolute output paths are prefixed with `\\?\` to lift the 260-char `MAX_PATH` limit. Behavior is identical across macOS, Linux, and Windows; no per-OS quirks leak into the rest of the CLI.

### `cmd/plaud`

Cobra wiring.

- `main.go`: root command, `--version`, the F-11 unofficial-disclaimer in `Long`, registers subcommands.
- `login.go`: interactive OTP and `--token` paste paths. `loginCmdOpts` carries a `resolveBaseURL` function injected via `withBaseURLResolver`. Production wires `api.BaseURL`; tests wire a closure that returns an `httptest.Server.URL`.
- `list.go`: loads credentials, builds `api.Client`, calls `Client.List`, renders a `text/tabwriter` table.
- `logout.go`: thin wrapper over `auth.Delete`.
- `download.go`, orchestrates spec 0002. Resolves the include set (CLI flag > env var > built-in default), resolves IDs (hex pass-through, otherwise prefix-match against one cached `client.List` call), runs a worker pool capped at `--concurrency`, and for each recording calls `client.Detail` then routes the result through `archive`. A 401 from any worker cancels the parent context and drops queued IDs (F-10). Per-recording errors are non-fatal but flip the final exit code.

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
