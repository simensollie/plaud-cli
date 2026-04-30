# Plan: Spec 0002 — Download recordings

Tracer-bullet sequencing. Phases below are an outline; each one fills in with concrete failing-test names and code paths once the spec moves to `Status: Active`.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`.

---

## Phase 0: Capture endpoint shapes (no code)

**Outcome:** `notes.md` carries verified request/response shapes for the audio download endpoint, the transcript endpoint, and the summary endpoint. Open questions Q1–Q4 in `spec.md` are closed.

**Deliverable:** a single network-capture session against `web.plaud.ai` that:
1. Clicks a recording open (transcript + summary fetches).
2. Triggers an audio download / play.
3. "Copy as cURL (bash)" on the relevant requests, redacted into `notes.md`.

**Done when:**
- [ ] Audio download request (method, path, response body type) documented.
- [ ] Transcript request shape documented (segments inline vs subfetch).
- [ ] Summary request shape documented.
- [ ] Whether signed URLs are single-use is empirically tested.

---

## Phase 1: Recording detail fetch

**Outcome:** `internal/api` can fetch one recording's detail (transcript + summary + extended metadata) given an ID.

**Failing test stub:**
- `internal/api/detail_test.go::TestDetail_F01_ReturnsTranscriptSegments`
- `internal/api/detail_test.go::TestDetail_F01_ReturnsSummary`
- `internal/api/detail_test.go::TestDetail_F10_Surfaces401`

**Code stub:**
- `internal/api/detail.go` — `(c *Client) Detail(ctx, id) (*RecordingDetail, error)`.

---

## Phase 2: Audio download

**Outcome:** Audio bytes land on disk byte-identical to the server.

**Failing test stub:**
- `internal/api/audio_test.go::TestAudio_F01_StreamsToWriter`
- `internal/api/audio_test.go::TestAudio_F07_ChecksumMatchSkips`

**Code stub:**
- `internal/api/audio.go` — `(c *Client) DownloadAudio(ctx, id, dst io.Writer) (n int64, err error)`. Internally: GET signed-URL endpoint, then GET the URL with retry-after-401-once.

---

## Phase 3: Archive layout

**Outcome:** Given a `Recording` and detail, write the canonical folder shape on disk.

**Failing test stub:**
- `internal/archive/layout_test.go::TestPath_F03_FolderShape`
- `internal/archive/layout_test.go::TestSlug_F03_NorwegianFolding`
- `internal/archive/layout_test.go::TestSlug_F03_CollisionGetsIDSuffix`
- `internal/archive/write_test.go::TestWrite_F07_IdempotentSkip`

**Code stub:**
- `internal/archive/layout.go` — path resolution and slug logic.
- `internal/archive/write.go` — `WriteRecording(root string, r RecordingBundle, opts) error` writes all artifacts atomically.

---

## Phase 4: Transcript renderers

**Outcome:** `transcript.json` is canonical; `.md`, `.srt`, `.vtt`, `.txt` are produced from it.

**Failing test stub:**
- `internal/archive/render_test.go::TestRender_F05_Markdown_Golden`
- `internal/archive/render_test.go::TestRender_F05_SRT_Golden`
- `internal/archive/render_test.go::TestRender_F05_VTT_Golden`
- `internal/archive/render_test.go::TestRender_F05_PlainText_Golden`

**Code stub:**
- `internal/archive/render.go` plus golden fixtures under `internal/archive/testdata/golden/0002/`.

---

## Phase 5: `plaud download` command

**Outcome:** End-to-end CLI command wiring Phases 1–4 plus credentials, ID resolution, and concurrency.

**Failing test stub:**
- `cmd/plaud/download_test.go::TestDownload_F01_HappyPath`
- `cmd/plaud/download_test.go::TestDownload_F06_ParallelMultipleIDs`
- `cmd/plaud/download_test.go::TestDownload_F08_PartialFailureExitsNonZero`
- `cmd/plaud/download_test.go::TestDownload_F10_TokenInvalidActionableMessage`
- `cmd/plaud/download_test.go::TestDownload_F09_TitlePrefixResolution`

**Code stub:**
- `cmd/plaud/download.go` — Cobra command, options pattern matching `login`/`list`.

---

## Phase 6: NDJSON event stream (deferred until F-12 priority lifts)

**Outcome:** `--format json` emits a stable line-per-event stream that downstream tools can consume.

**Failing test stub:**
- `cmd/plaud/download_test.go::TestDownload_F12_NDJSONEvents`

**Code stub:**
- `internal/eventbus/` (new) — small writer that round-trips through the schema in `docs/schemas/0002/`.

---

## Acceptance walk-through (final sign-off)

Reproduces spec.md §8. Runs against a real account on each target OS. Until the user has a working OTP login flow, the run begins with `plaud login --token <jwt>`.

1. Single-ID download: folder + 5 files present, sizes plausible, `transcript.json` parses, `transcript.md` renders correctly.
2. Re-run: zero CDN bytes (proven by NDJSON `skipped` events or a tcpdump if needed).
3. Three-ID parallel: completes in ~one-third the sequential time.
4. Bad ID + good ID: bad fails loudly, good completes, exit non-zero.
5. `--include audio`: only `audio.mp3` written.
6. `--transcript-format json,srt`: both formats present.
7. Token-invalid mid-run: actionable message, abort.
8. Repeat 1–7 on macOS, Linux, Windows.

When all pass, set `Status: Done <YYYY-MM-DD>` in `spec.md`.
