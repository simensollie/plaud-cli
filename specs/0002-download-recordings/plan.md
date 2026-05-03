# Plan: Spec 0002: Download recordings

Tracer-bullet sequencing. Phases below are an outline; each one fills in with concrete failing-test names and code paths once the spec moves to `Status: Active`.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`.

---

## Phase 0: Capture endpoint shapes (no code)

**Outcome:** `notes.md` carries verified request/response shapes for the audio download endpoint, the transcript endpoint, and the summary endpoint. Open questions Q1-Q4 and Q7 in `spec.md` are closed.

**Deliverable:** a single network-capture session against `web.plaud.ai` that:
1. Clicks a recording open (transcript + summary fetches).
2. Triggers an audio download / play.
3. "Copy as cURL (bash)" on the relevant requests, redacted into `notes.md`.

**Done when:**
- [ ] Audio download request (method, path, response body type) documented.
- [ ] Transcript request shape documented (segments inline vs subfetch).
- [ ] Summary request shape documented.
- [ ] Whether `file_md5` matches the served audio bytes' MD5 is empirically tested (Q4).
- [ ] Whether signed URLs are single-use is empirically tested (Q7).
- [ ] If a Plaud "generate transcript / summary" trigger is observable in DevTools, capture its endpoint into `notes.md` for the future generation spec (out of 0002 scope).

---

## Phase 1: Recording detail fetch

**Status:** Done 2026-05-02

**Outcome:** `internal/api` can fetch one recording's detail (transcript segments + summary + extended metadata) given an ID. Detail is a multi-call dance: `/file/detail/{id}` returns pointers; the transcript is always a separate GET against a signed content-storage URL; the summary may be inlined in `pre_download_content_list` or require a separate GET.

**Done when:**
- [x] `Client.Detail` parses the envelope, extracts language from `extra_data.aiContentHeader`, and resolves transcript + summary via `content_list[]` / `pre_download_content_list[]`.
- [x] Plaud's wire shape mapped to canonical `Segment` (`start_time → start_ms`, `content → text`, `original_speaker` omitted when equal to `speaker`).
- [x] `fetchSignedJSON` helper hits content-storage signed URLs without forwarding `Authorization` (F-13).
- [x] F-19 partial state: missing `transaction` entry or `task_status != 1` returns nil segments without error; same for summary.
- [x] F-10: 401 surfaces as `api.ErrUnauthorized`; non-zero envelope status wraps `api.ErrAPIError`.
- [x] All Phase 1 tests pass under `go test -race ./internal/api/...`; vet+fmt clean.

**Endpoint shapes (from notes.md 2026-05-02 Phase 0):**
- `GET /file/detail/{id}` returns `{status, msg, data: {file_id, file_name, file_version, duration, is_trash, start_time, content_list[], pre_download_content_list[], extra_data: {aiContentHeader: {language_code, ...}, tranConfig, task_id_info, ...}}}`.
- `content_list[]` carries one entry per artifact, keyed by `data_type`. For transcript: `data_type=transaction`, `data_link` is a SigV4 presigned URL into `euc1-prod-plaud-content-storage.s3.amazonaws.com` (gzipped JSON, `X-Amz-Expires=300`). For Plaud-generated summary: `data_type=auto_sum_note`, same kind of link.
- `pre_download_content_list[]` may inline small artifacts as `{data_id, data_content}`. The summary is typically inlined; the transcript is not.
- Transcript bytes (after gunzip): bare JSON array of `{start_time, end_time, content, speaker, original_speaker}`. Mapped to our canonical `{speaker, original_speaker, start_ms, end_ms, text}` per F-05 at the API/archive boundary.

**Failing test stubs:**
- `internal/api/detail_test.go::TestDetail_F01_ParsesTopLevelEnvelope`
- `internal/api/detail_test.go::TestDetail_F01_PicksTranscriptArtifactFromContentList`
- `internal/api/detail_test.go::TestDetail_F01_PrefersInlinedSummaryOverContentLink`
- `internal/api/detail_test.go::TestDetail_F01_FallsBackToSummaryContentLinkWhenNotInlined`
- `internal/api/detail_test.go::TestDetail_F05_MapsPlaudWireShapeToCanonicalSegments`
- `internal/api/detail_test.go::TestDetail_F05_OmitsOriginalSpeakerWhenEqualToSpeaker`
- `internal/api/detail_test.go::TestDetail_F01_PopulatesLanguageFromAIContentHeader`
- `internal/api/detail_test.go::TestDetail_F19_HandlesIsTransFalseGracefully` (no `transaction` entry in `content_list`, or `task_status != 1`; returns nil transcript without error)
- `internal/api/detail_test.go::TestDetail_F10_Surfaces401`
- `internal/api/transcript_fetch_test.go::TestFetchTranscript_F01_GunzipsAndDecodes`
- `internal/api/transcript_fetch_test.go::TestFetchTranscript_F01_HandlesAlreadyDecompressedResponse` (some HTTP clients auto-decompress when `Content-Encoding: gzip`)

**Code stubs:**
- `internal/api/detail.go`: `(c *Client) Detail(ctx, id) (*RecordingDetail, error)`. Returns a struct with mapped segments, summary markdown, and language. Uses the API client (30s total timeout).
- `internal/api/transcript_fetch.go`: `(c *Client) fetchSignedJSON(ctx, url) ([]byte, error)`. GETs a content-storage signed URL, handles `Content-Encoding: gzip` (or the auto-decompressed body), returns raw bytes for the caller to unmarshal. Used for transcript and for non-inlined summary.

---

## Phase 2: Audio download

**Status:** Done 2026-05-02

**Outcome:** Audio bytes land on disk byte-identical to the server, with the two-step signed-URL flow plus HEAD-based idempotency F-15 / F-07(a) require.

**Done when:**
- [x] `Client.TempURL` wraps `/file/temp-url/{id}` and returns the signed URL (`temp_url_opus` ignored in v0.2).
- [x] `Client.HeadAudio` returns `{ETag, SizeBytes}` from S3 HEAD; ETag is unquoted.
- [x] `Client.DownloadAudio` streams bytes through `io.MultiWriter(dst, md5.New())`, returns `(n, etag, localMD5, err)`. For single-part uploads `localMD5 == etag`.
- [x] `audioClient` field added to `Client` with `Timeout: 0` (no total cap); `WithAudioHTTPClient` option for tests; existing `httpClient` and `do` unchanged.
- [x] F-13: no `Authorization` sent to S3 (verified by tests that fail if any auth header reaches the server).
- [x] F-15: idle-read deadline (default 30s, configurable for tests) aborts stalled streams via `inner.Close()` from a `time.AfterFunc` goroutine; surfaces as `ErrIdleTimeout`.
- [x] F-15: 401/403 from S3 surface as `ErrSignedURLExpired` so callers can refetch the temp_url; other 4xx and all 5xx surface as wrapped errors that do NOT trigger refetch.
- [x] All Phase 2 tests pass under `go test -race ./internal/api/...`; vet+fmt clean.

**Endpoint shapes (from notes.md 2026-05-02 Phase 0):**
- `GET /file/temp-url/{id}` (API client) returns `{status, temp_url, temp_url_opus}`. `temp_url_opus` may be null; ignore in v0.2.
- `temp_url` is a SigV4 presigned URL into `euc1-prod-plaud-bucket.s3.amazonaws.com/audiofiles/{id}.mp3`, valid 3600 seconds.
- HEAD against `temp_url` returns S3 headers including `ETag` (single-part upload's MD5 of served bytes; the value F-07(a) compares against `metadata.audio.s3_etag`), `Content-Length`, `Last-Modified`, `Accept-Ranges: bytes`.
- GET against the same URL returns the audio bytes directly (`Content-Type: binary/octet-stream`).

**Failing test stubs:**
- `internal/api/temp_url_test.go::TestTempURL_F15_ReturnsSignedURL`
- `internal/api/temp_url_test.go::TestTempURL_F10_Surfaces401`
- `internal/api/audio_test.go::TestAudio_F07_HEADReturnsETag`
- `internal/api/audio_test.go::TestAudio_F07_ETagMatchPredicateSkipsGET` (caller-side: HEAD result + stored etag → boolean "skip")
- `internal/api/audio_test.go::TestAudio_F01_StreamsToWriterWithMD5`
- `internal/api/audio_test.go::TestAudio_F01_LocalMD5EqualsETagOnSinglePartUpload`
- `internal/api/audio_test.go::TestAudio_F15_IdleTimeoutAbortsStalledRead`
- `internal/api/audio_test.go::TestAudio_F15_SignedURLRefetchedOnce_On403`
- `internal/api/audio_test.go::TestAudio_F15_NoRetryOn5xx`
- `internal/api/audio_test.go::TestAudio_F10_Surfaces401OnAPIPathOnly` (the S3 leg never carries `Authorization`; a 401 from S3 is signature expiry, not session)

**Code stubs:**
- `internal/api/temp_url.go`: `(c *Client) TempURL(ctx, id) (string, error)`. Wraps the API endpoint, returns just `temp_url` (we ignore opus). Uses the bookkeeping HTTP client.
- `internal/api/audio.go`: `(c *Client) HeadAudio(ctx, signedURL) (etag string, size int64, err error)` and `(c *Client) DownloadAudio(ctx, signedURL, dst io.Writer) (n int64, etag string, localMD5 string, err error)`. Both compute the local MD5 stream-side via `io.MultiWriter(dst, md5.New())`. Both use the audio client (no total timeout) with `idleTimeoutReader`-wrapped bodies.
- `internal/api/client.go`: extend `Client` with a separate `audioClient *http.Client`. Construct in `New(...)` with `Timeout: 0`. Bookkeeping client keeps the 30-second total timeout.
- `internal/api/idle_reader.go`: `idleTimeoutReader` wraps `io.ReadCloser`, resets a `time.Timer` on every successful `Read`, returns timeout error on stall.

---

## Phase 3: Archive layout (atomic writes, slug, metadata)

**Status:** Done 2026-05-02

**Outcome:** Given a `Recording` plus its detail, write the canonical folder shape on disk safely and idempotently.

**Done when:**
- [x] Slug folding implemented and tested for F-03 (audio-extension strip, Norwegian fold, NFKD, post-fold word-boundary truncate, `untitled` fallback, 6-char ID suffix on collision).
- [x] Atomic per-file writes via `<name>.partial` + fsync + rename; partial-sweep helper; parent-dir auto-create.
- [x] Metadata schema with nested per-artifact sub-objects (omitted when nil), sorted-keys pretty-print with trailing newline, `fetched_at` vs `last_verified_at` semantics, rebuild-from-disk recovery for corrupt `metadata.json`.
- [x] Layout helpers for path resolution, root auto-create with one-time stderr notice, `--out` file rejection, write-permission probe.
- [x] All Phase 3 tests pass under `go test -race`; `go vet` and `gofmt -l` clean.

**Failing test stubs (atomic writes, F-14):**
- `internal/archive/atomic_test.go::TestWrite_F14_TempRenameAtomicity`
- `internal/archive/atomic_test.go::TestWrite_F14_PartialSweepBeforeRun`
- `internal/archive/atomic_test.go::TestWrite_F14_FsyncBeforeRename`

**Failing test stubs (slug, F-03):**
- `internal/archive/slug_test.go::TestSlug_F03_StripsAudioExtension`
- `internal/archive/slug_test.go::TestSlug_F03_NorwegianFolding`
- `internal/archive/slug_test.go::TestSlug_F03_NFKDFoldsCombiningMarks`
- `internal/archive/slug_test.go::TestSlug_F03_PostFoldTruncatesAtWordBoundary`
- `internal/archive/slug_test.go::TestSlug_F03_EmptySlugFallsBackToUntitled`
- `internal/archive/slug_test.go::TestSlug_F03_CollisionAppendsSixCharIDSuffix`

**Failing test stubs (metadata schema and idempotency, F-07 / §4):**
- `internal/archive/metadata_test.go::TestMetadata_F07_FetchedAtVsLastVerifiedAtSemantics`
- `internal/archive/metadata_test.go::TestMetadata_F07_TranscriptSHA256MismatchTriggersRewrite`
- `internal/archive/metadata_test.go::TestMetadata_F07_TranscriptSHA256MatchSkipsRewrite`
- `internal/archive/metadata_test.go::TestMetadata_F09c_PrettyPrintedSortedKeysTrailingNewline`
- `internal/archive/metadata_test.go::TestMetadata_F14_RebuildFromLocalFilesWhenCorrupt`
- `internal/archive/metadata_test.go::TestMetadata_PerArtifactSubobjectsAbsentWhenSkipped`

**Failing test stubs (path resolution and `--out`, F-02):**
- `internal/archive/layout_test.go::TestPath_F03_FolderShape`
- `internal/archive/layout_test.go::TestPath_F02_AutoCreatesArchiveRoot`
- `internal/archive/layout_test.go::TestPath_F02_OutFlagReplacesRootOnly`
- `internal/archive/layout_test.go::TestPath_F02_RejectsOutPointingAtFile`
- `internal/archive/layout_test.go::TestPath_F02_ProbesWritePermissionEarly`

**Code stubs:**
- `internal/archive/layout.go`: path resolution.
- `internal/archive/slug.go`: slug folding plus collision handling.
- `internal/archive/atomic.go`: `WriteAtomic(path string, data []byte) error` and `SweepPartials(folder string) error`.
- `internal/archive/metadata.go`: `Metadata` struct, marshaling, hash helpers, rebuild-from-disk recovery path.
- `internal/archive/write.go`: `WriteRecording(root string, r RecordingBundle, opts) error` orchestrates the per-artifact writes through `WriteAtomic`.

---

## Phase 4: Transcript renderers

**Status:** Done 2026-05-02

**Outcome:** `transcript.json` is canonical and wrapped as `{"version": 1, "segments": [...]}`. `.md`, `.srt`, `.vtt`, `.txt` are produced from it deterministically.

**Done when:**
- [x] `Transcript` and `Segment` types match the canonical wire shape (object wrapper, integer milliseconds, snake_case).
- [x] `md` / `srt` / `vtt` / `txt` renderers produce golden-stable output for a 3-segment Norwegian fixture.
- [x] Empty-speaker prefix collapse verified across all four formats with a dedicated golden fixture.
- [x] Unknown format returns a wrapped error.
- [x] All Phase 4 tests pass under `go test -race`; goldens regenerable via `-update`.

**Failing test stubs:**
- `internal/archive/render_test.go::TestRender_F05_TranscriptJSONIsObjectWithVersionField`
- `internal/archive/render_test.go::TestRender_F05_Markdown_Golden`
- `internal/archive/render_test.go::TestRender_F05_SRT_Golden`
- `internal/archive/render_test.go::TestRender_F05_VTT_Golden`
- `internal/archive/render_test.go::TestRender_F05_PlainText_Golden`
- `internal/archive/render_test.go::TestRender_F05_EmptySpeakerOmitsPrefix`
- `internal/archive/render_test.go::TestRender_F05_StartMsEndMsRoundTripPreservesPrecision`

**Code stubs:**
- `internal/archive/render.go` plus golden fixtures under `internal/archive/testdata/golden/0002/`.

---

## Phase 5: `plaud download` command

**Status:** Done 2026-05-02

**Outcome:** End-to-end CLI command wiring Phases 1-4 plus credentials, ID resolution, concurrency, include-set resolution, error semantics, and trashed/partial-server-state warnings.

**Done when:**
- [x] `cmd/plaud/download.go` Cobra command registered in `main.go`; options pattern matches `list`/`login`.
- [x] Effective include set resolved by F-04 precedence (CLI flag > env var > built-in default `transcript,summary,metadata`); `--include` and `--exclude` mutually exclusive.
- [x] Effective transcript-format resolved by F-05 precedence; format flag replaces default; `transcript.json` always written when transcript is in include.
- [x] ID resolution: 32-hex direct, else single `client.List` call with case-insensitive prefix on `Filename`; ambiguous lists candidates; trashed not visible via prefix (F-09, F-17).
- [x] Worker pool meters recordings; concurrency clamped `[1, 16]` and 0/negative rejected up front (F-06).
- [x] Per-recording orchestration: TempURL → HeadAudio (refetch URL once on 401/403) → conditional DownloadAudio → Detail → write transcript/summary/metadata atomically.
- [x] `--force` bypasses idempotency across the include set; bumps both `fetched_at` and `last_verified_at` even on byte-identical writes (F-16).
- [x] 401 mid-run cancels the parent context and surfaces the actionable message exactly once (F-10).
- [x] Per-recording errors do not abort the run; final exit code non-zero if any failed (F-08).
- [x] F-19 partial server state continues with stderr warning; F-17 trashed direct-ID downloads with stderr warning.
- [x] F-11 ffmpeg fallback: missing ffmpeg with non-mp3 `--audio-format` emits stderr warning and falls back to mp3 (does not fail the recording).
- [x] F-12 `--format json` flag is parsed but emits no JSON yet (Phase 6 fills the implementation).
- [x] Additive surface changes: `api.Recording` gains `FileMD5`; `api.RecordingDetail` gains `IsTrash`, `StartTime`, `Duration` so direct-ID recordings can be downloaded without a preceding `List` call.
- [x] All 23 Phase 5 tests pass under `go test -race ./...`; vet+fmt clean.

**Failing test stubs (happy path and core flow, F-01 / F-06 / F-08 / F-10):**
- `cmd/plaud/download_test.go::TestDownload_F01_HappyPathDefaultInclude`
- `cmd/plaud/download_test.go::TestDownload_F06_ParallelMultipleIDs`
- `cmd/plaud/download_test.go::TestDownload_F06_ConcurrencyClampedToSixteen`
- `cmd/plaud/download_test.go::TestDownload_F06_RejectsConcurrencyZero`
- `cmd/plaud/download_test.go::TestDownload_F08_PartialFailureExitsNonZero`
- `cmd/plaud/download_test.go::TestDownload_F10_TokenInvalidActionableMessage`
- `cmd/plaud/download_test.go::TestDownload_F10_401MidRunCancelsWorkerPool`

**Failing test stubs (include / exclude resolution, F-04):**
- `cmd/plaud/download_test.go::TestDownload_F04_DefaultIncludeExcludesAudio`
- `cmd/plaud/download_test.go::TestDownload_F04_IncludeAndExcludeMutuallyExclusive`
- `cmd/plaud/download_test.go::TestDownload_F04_EnvVarOverridesBuiltInDefault`
- `cmd/plaud/download_test.go::TestDownload_F04_CLIFlagOverridesEnvVar`

**Failing test stubs (transcript / audio format flags, F-05 / F-11):**
- `cmd/plaud/download_test.go::TestDownload_F05_TranscriptFormatReplacesDefault`
- `cmd/plaud/download_test.go::TestDownload_F05_TranscriptFormatRequiresTranscriptInInclude`
- `cmd/plaud/download_test.go::TestDownload_F11_AudioFormatRequiresAudioInInclude`
- `cmd/plaud/download_test.go::TestDownload_F11_FfmpegMissingSkipsWithWarning`

**Failing test stubs (resolution, force, trash, partial state, F-09 / F-16 / F-17 / F-19):**
- `cmd/plaud/download_test.go::TestDownload_F09_TitlePrefixCaseInsensitive`
- `cmd/plaud/download_test.go::TestDownload_F09_AmbiguousPrefixListsCandidates`
- `cmd/plaud/download_test.go::TestDownload_F16_ForceBumpsBothTimestampsOnUnchangedBytes`
- `cmd/plaud/download_test.go::TestDownload_F16_ForceRewritesAcrossIncludeSet`
- `cmd/plaud/download_test.go::TestDownload_F17_TrashedDirectIDDownloadsWithWarning`
- `cmd/plaud/download_test.go::TestDownload_F17_TrashedNotReachableByPrefix`
- `cmd/plaud/download_test.go::TestDownload_F19_IsTransFalseSkipsTranscriptKeepsRest`
- `cmd/plaud/download_test.go::TestDownload_F19_IsSummaryFalseSkipsSummaryKeepsRest`

**Code stubs:**
- `cmd/plaud/download.go`: Cobra command, options pattern matching `login`/`list`. Resolves the effective include set (CLI flag → env var → built-in), runs the worker pool, dispatches per-recording work to the archive package.

---

## Phase 6: Per-recording JSON output (`--format json`)

**Status:** Done 2026-05-02

**Outcome:** `--format json` emits one JSON object per recording on stdout when its processing completes. Used by acceptance criterion 3 to verify idempotent re-run behavior automatically.

**Done when:**
- [x] One JSON object per recording on stdout, emitted at per-recording completion (not batched).
- [x] Object shape `{id, status, files, duration_ms, error?}`; `error` key present only when `status == "failed"`.
- [x] `files` is alphabetically sorted; sentinel pseudo-filenames (e.g. `(transcript-not-ready)`) filtered out.
- [x] Stdout under `--format json` carries only JSON lines (no preamble); stderr remains plain English regardless of format.
- [x] F-13 redaction: `redactErrorString` regex-strips `https?://...` from error strings before they enter either stdout JSON or stderr human output.
- [x] `--help` text on the download command documents the JSON shape.
- [x] All 8 Phase 6 tests pass under `go test -race ./...`; the existing 23 Phase 5 tests still pass; vet+fmt clean.

**Failing test stubs:**
- `cmd/plaud/download_test.go::TestDownload_F12_PerRecordingJSONLine`
- `cmd/plaud/download_test.go::TestDownload_F12_StatusFetchedFiledList`
- `cmd/plaud/download_test.go::TestDownload_F12_StatusSkippedOnIdempotentRerun`
- `cmd/plaud/download_test.go::TestDownload_F12_StatusFailedCarriesError`
- `cmd/plaud/download_test.go::TestDownload_F12_StderrStaysPlainEnglishUnderJSONFormat`
- `cmd/plaud/download_test.go::TestDownload_F13_NoTokensOrSignedURLsLeakIntoOutput`

**Code stubs:**
- `cmd/plaud/download.go`: emit the JSON object inline next to the human-readable renderer. No separate eventbus package; the output is small enough to live alongside the worker loop. Use `encoding/json` with stable field order.

---

## Phase 7: Windows long-path support

**Status:** Done 2026-05-02

**Outcome:** Archive paths under deeply-nested user directories work on Windows even when total path length exceeds 260 chars.

**Done when:**
- [x] `PrefixLongPath` build-tagged across `windows` and non-windows.
- [x] Windows: `\\?\` prefix, UNC rewrite, abs conversion, idempotent when already prefixed.
- [x] POSIX: identity (no-op).
- [x] `GOOS=windows go build ./...` is clean from a Linux host.
- [x] POSIX tests pass; Windows tests will run on the Windows leg of CI.

**Failing test stubs:**
- `internal/archive/winpath_test.go::TestWinPath_F18_LongPathPrefixOnWindows` (build-tagged `//go:build windows`)
- `internal/archive/winpath_test.go::TestWinPath_F18_NoOpOnPOSIX` (build-tagged `//go:build !windows`)

**Code stubs:**
- `internal/archive/winpath.go`: `prefixLongPath(p string) string` returns `\\?\` + absolute path on Windows, returns input unchanged on POSIX. Called from every archive-write entry point.

**CI:** ensure the GitHub Actions matrix has a Windows runner so this phase is exercised in CI, not just locally.

---

## Acceptance walk-through (final sign-off)

Reproduces spec.md §8 against a real account on each target OS. Until OTP login lands, runs begin with `plaud login --token <jwt>`.

1. Default no-flag: folder contains `transcript.json`, `transcript.md`, `summary.plaud.md`, `metadata.json`. No `audio.mp3`.
2. All-on (`--include audio,transcript,summary,metadata`): all 5 files; `metadata.audio.local_md5` matches `md5sum audio.mp3`.
3. Idempotent re-run: zero CDN audio bytes; `--format json` shows `status: "skipped"`.
4. Three-ID parallel: ~one-third sequential time.
5. Bad ID + good ID: bad fails loudly with the underlying API error; good completes; exit non-zero.
6. `--include audio`: `audio.mp3` plus `metadata.json` only.
7. `--transcript-format json,srt`: `transcript.json` plus `transcript.srt`; no `transcript.md`.
8. Token-invalid mid-run: actionable message once, worker pool cancelled, exit non-zero, no further per-recording errors.
9. Partial server state (`is_trans=false`): summary plus metadata land; transcript skipped with stderr warning; exit 0.
10. Trashed direct-ID: downloads with stderr warning; same recording not reachable by title prefix.
11. `--force` round-trip on unchanged recording: byte-identical canonical files; both `fetched_at` and `last_verified_at` bumped.
12. Cross-platform: 1-11 reproduce on macOS, Linux, and Windows. Windows additionally validates F-18 with an archive root under `C:\Users\<long-name>\Documents\PlaudArchive\` and a 60-char slug.

When all pass, set `Status: Done <YYYY-MM-DD>` in `spec.md`.
