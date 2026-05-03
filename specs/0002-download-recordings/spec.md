# Spec 0002: Download recordings

**Status:** Active
**Created:** 2026-05-01
**Updated:** 2026-05-03
**Owner:** @simensollie
**Target version:** v0.2

The smallest unit of fetching: given one or more recording IDs, fetch transcript, summary, and metadata (and optionally audio) into a structured local archive folder. Foundation for spec 0003 (sync) and spec 0004 (prompt composition).

This spec does not implement incremental sync, watch mode, server-side generation of transcripts or summaries, or scheduled background fetching. Sync lives in spec 0003; generation is deferred to a future spec when the relevant Plaud endpoints are captured.

---

## 1. Goal

A user with a logged-in CLI can run `plaud download <id>` and find a structured local archive folder for that recording. By default, the folder contains a transcript (canonical JSON plus rendered markdown), the Plaud-generated summary, and a `metadata.json` bookkeeping file. Audio is opt-in via `--include audio` because audio files dominate disk usage and most workflows consume the transcript instead.

## 2. Commands / interfaces

| Command | Behavior |
|---|---|
| `plaud download <id> [<id>...]` | Fetch one or more recordings. At least one positional argument is required. |
| `plaud download <id> --out DIR` | Override the default archive root. The `YYYY/MM/<folder>` hierarchy still applies under DIR. |
| `plaud download <id> --include audio,transcript,summary,metadata` | Select which artifacts. Default: `transcript,summary,metadata`. Mutually exclusive with `--exclude`. |
| `plaud download <id> --exclude audio` | Subtract from the full artifact set. Mutually exclusive with `--include`. |
| `plaud download <id> --transcript-format json,md,srt,vtt,txt` | Multiple formats per run. Replaces the default `json,md` (does not merge). |
| `plaud download <id> --audio-format mp3` | Default `mp3` (Plaud's served format). Singular: one format per run. Other formats require `ffmpeg`. |
| `plaud download <id> --concurrency N` | Default 4, clamped to `[1, 16]`. |
| `plaud download <id> --force` | Re-fetch every artifact in the include set, bypassing per-artifact idempotency. |
| `plaud download <id> --format json` | Per-recording JSON line on stdout at completion. Schema documented in `--help`; not stability-committed before v1.0. |

Environment variables (CLI flags always win):

| Var | Effect |
|---|---|
| `PLAUD_ARCHIVE_DIR` | Override the default archive root. |
| `PLAUD_DEFAULT_INCLUDE` | Override the built-in default include set. |
| `PLAUD_DEFAULT_TRANSCRIPT_FORMAT` | Override the built-in default `--transcript-format`. |

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | `plaud download <id>` fetches the artifacts in the effective include set for that ID into a per-recording folder under the archive root. The effective include set is resolved by precedence: `--include` flag, else `--exclude` flag (subtract from full set), else `PLAUD_DEFAULT_INCLUDE`, else built-in default `transcript,summary,metadata`. | Must |
| F-02 | Default archive root: `${PLAUD_ARCHIVE_DIR:-~/PlaudArchive}` on POSIX, `%USERPROFILE%\PlaudArchive` on Windows. `--out DIR` overrides per invocation, replacing only the root. Both creators auto-create missing directories recursively (`os.MkdirAll`); on first creation of the default root, a one-line stderr notice is emitted. If `--out` exists and is not a directory, fail before any network call. Write permission is probed up front (sentinel write+remove); failure exits non-zero with an actionable message. | Must |
| F-03 | Per-recording folder layout: `<root>/YYYY/MM/YYYY-MM-DD_HHMM_<slug>/` (UTC). Folder contents depend on the include set: `audio.<ext>`, `transcript.json` plus derived `transcript.{md,srt,vtt,txt}`, `summary.plaud.md`, `metadata.json`. Slug rules (in order): strip a trailing audio extension (`.mp3 .m4a .wav .aac .flac .ogg`); apply `æ→ae`, `ø→o`, `å→a`; NFKD-fold remaining combining marks; lowercase; non-word characters → `_`; cap at 60 chars post-fold (truncate at a `_` boundary inside the last 10 chars if available, else hard-cut). Empty slug fallback: `untitled`. On collision (same year, month, timestamp, and slug), append a 6-char ID suffix to disambiguate. | Must |
| F-04 | `--include` and `--exclude` accept any subset of `audio,transcript,summary,metadata`. Passing both is an error. Default include set = `transcript,summary,metadata`, overridable via `PLAUD_DEFAULT_INCLUDE`. Passing `--audio-format` while `audio` is excluded is an error; passing `--transcript-format` while `transcript` is excluded is an error. | Must |
| F-05 | `--transcript-format` supports `json`, `md`, `srt`, `vtt`, `txt`. Multiple formats per run. Default = `json,md`, overridable via `PLAUD_DEFAULT_TRANSCRIPT_FORMAT`. The flag replaces the default; merging is explicit (`--transcript-format json,md,srt`). `transcript.json` is always written when `transcript` is in the include set, regardless of format selection (it is the canonical source for the other formats). Canonical JSON shape: `{"version": 1, "segments": [{"speaker": "...", "original_speaker": "...", "start_ms": 0, "end_ms": 4320, "text": "..."}]}`. Plaud's wire shape (a bare array of `{start_time, end_time, content, speaker, original_speaker}` served as gzipped JSON from a content-storage S3 URL) is mapped at ingest: `start_time → start_ms`, `end_time → end_ms`, `content → text`. `speaker` is Plaud's display label (may be user-edited, may be empty); `original_speaker` is the raw diarizer label (`Speaker 0`, `Speaker 1`, ...) and is omitted from `transcript.json` when equal to `speaker`. Renderers omit the speaker prefix when `speaker` is empty; `original_speaker` is informational only. | Must |
| F-06 | Multiple IDs fetch in parallel. Default `--concurrency 4`, clamped to `[1, 16]`. Out-of-range values are rejected before any work starts. `--concurrency` meters recordings, not HTTP requests; within a single recording the worker may interleave the detail call, the temp-url call, and per-artifact fetches as it sees fit (the audio HEAD against `temp_url` precedes the audio byte GET; see F-07(a) and F-15). | Should |
| F-07 | Per-artifact idempotency: (a) Audio: HEAD the recording's signed `temp_url` (from `GET /file/temp-url/{id}`); if S3's response `ETag` (single-part uploads return the served-bytes MD5 here) matches `metadata.audio.s3_etag`, skip the audio byte GET. On HEAD failure, ETag mismatch, or absent stored ETag, fetch the bytes, recompute, and update `metadata.audio.s3_etag` plus `metadata.audio.local_md5`. The list response's `file_md5` is the MD5 of Plaud's original `.opus` upload (not the served `.mp3` bytes) and is recorded as `metadata.audio.original_upload_md5` for audit; it is not used for idempotency. (b) Transcript: detail endpoint is always called; SHA-256 of the canonical segment array (post-mapping to our wire shape) compared to `metadata.transcript.sha256`; skip writing `transcript.json` and skip regenerating derived files if hashes match. (c) Summary: same pattern as transcript, against `metadata.summary.sha256`. (d) Derived transcript files are always regenerated when `transcript.json` changes; never touched when it does not. (e) `metadata.json` rewrites bump `last_verified_at` on every successful run; `fetched_at` only bumps when an artifact write actually occurred. | Must |
| F-08 | Per-recording errors do not abort the run. Final exit code is non-zero if any recording failed. Per-recording failures are printed to stderr with the ID and the underlying error string. | Must |
| F-09 | ID resolution: an arg matching `^[0-9a-f]{32}$` resolves directly without listing. Otherwise treat as a case-insensitive prefix of `Recording.Filename`; a single `client.List` call is made; unique prefix wins; ambiguous prefix errors with a candidate list; no match errors with the input. | Should |
| F-10 | 401 from any sub-call surfaces as `api.ErrUnauthorized` with the message "Token expired or invalid. Run `plaud login` again." (same wording as `plaud list`). A 401 mid-run is session-level: it cancels the parent context, hard-cancels in-flight workers, drops queued recordings, and exits non-zero. No retry. | Must |
| F-11 | `--audio-format` is singular (one format per run) and defaults to `mp3` (Plaud's served format). v0.2 ships only the mp3 path: non-mp3 values are accepted on the CLI but the audio is fetched as mp3 with a stderr warning identifying the limitation (the actual `ffmpeg` conversion is deferred to a future spec). Missing `ffmpeg` therefore never fails the recording. | Could |
| F-12 | `--format json` emits one JSON object per recording on stdout when its processing completes: `{"id": "...", "status": "fetched"|"skipped"|"failed", "files": ["audio.mp3", "transcript.json", ...], "duration_ms": 2341, "error": "..."}` (the `error` key is present only when `status` is `failed`). Stderr remains plain English regardless of `--format`. Schema is documented in `--help`; not stability-committed before v1.0. | Should |
| F-13 | Tokens, signed CDN URLs (which embed credentials), and `Authorization` headers are never written to logs or stderr. | Must |
| F-14 | Atomic per-file writes. Each artifact is written to `<name>.partial` inside the target folder, fsync'd, then `os.Rename`'d to its final name (atomic on the same filesystem; tempfile lives next to destination so cross-fs is impossible by construction). Stale `.partial` files are swept at the start of each run, before any idempotency check. If `metadata.json` exists but is unparseable on re-run, rebuild it by hashing local files (with a single stderr notice), rather than refusing or treating the recording as fresh. | Must |
| F-15 | Audio download is two-step: (1) `GET /file/temp-url/{id}` via the API client returns `{temp_url, temp_url_opus}` (the latter may be null and is ignored in v0.2); (2) `GET <temp_url>` via a separate audio HTTP client (no total timeout) retrieves the bytes from `euc1-prod-plaud-bucket.s3.amazonaws.com` directly. The audio response body is wrapped by an idle-read deadline (30s without progress aborts). The signed S3 URL is refetched once (re-call step 1) on a 401/403 from S3 (signature expiry; signed URLs carry `X-Amz-Expires=3600`). Other 4xx and all 5xx surface immediately as a per-recording failure. 429 responses are retried per the shared backoff transport introduced in spec 0003 F-06 (5 retries max, exponential 1s/2s/4s/8s/30s, `Retry-After` honored capped at 30s); 5xx is unchanged. Bookkeeping calls (list, detail, temp-url) keep the existing 30-second total timeout on the API client. Idempotency uses HEAD against the signed URL before issuing GET; see F-07(a). | Must |
| F-16 | `--force` is binary, applies to every artifact in the current effective include set: bypasses per-artifact idempotency, re-fetches audio bytes, rewrites canonical files even when hashes match, regenerates derived files, and bumps both `fetched_at` and `last_verified_at`. `--force` does not delete artifacts outside the include set. | Should |
| F-17 | Recordings with `is_trash=true` requested by direct ID are downloaded, with a stderr warning identifying the trashed state. Prefix resolution does not surface trashed recordings (the list endpoint is called with `is_trash=0`). | Should |
| F-18 | Windows long-path support: absolute output paths are prefixed with `\\?\` to lift the 260-char `MAX_PATH` limit. Behavior is identical across macOS, Linux, and Windows. | Should |
| F-19 | Partial server state. When `is_trans=false` for a recording, transcript artifacts are skipped with a stderr warning (`"<id>: transcript not yet ready, skipped"`). Same for summary when `is_summary=false`. Other artifacts in the include set still fetch normally; the run does not fail. Skipped artifacts also surface as a `skipped` status in `--format json` output (when applicable per artifact granularity). | Must |

## 4. Storage / data model

```
<archive_root>/
└── 2026/
    └── 04/
        └── 2026-04-30_1430_kickoff_meeting/
            ├── audio.mp3                # only when 'audio' in include set
            ├── transcript.json          # canonical
            ├── transcript.md            # rendered from transcript.json
            ├── transcript.srt           # opt-in via --transcript-format
            ├── summary.plaud.md         # Plaud's own summary (markdown)
            └── metadata.json            # always written when any other artifact lands
```

**Canonical files:** `transcript.json` and `metadata.json`. The other transcript formats are deterministically derived and may be deleted; the next run regenerates them.

**Folder timestamp:** `YYYY-MM-DD_HHMM` in **UTC**, derived from `Recording.StartTime`. Local-time alternatives confuse incremental sync when a user travels timezones. `metadata.json` carries both UTC and the original local time for human reference.

**`transcript.json` schema:**

```json
{
  "version": 1,
  "segments": [
    {"speaker": "Simen Sollie", "original_speaker": "Speaker 0", "start_ms": 0,    "end_ms": 4320,  "text": "..."},
    {"speaker": "Speaker 1",                                     "start_ms": 4320, "end_ms": 9100,  "text": "..."},
    {"speaker": "",                                              "start_ms": 9100, "end_ms": 12000, "text": "..."}
  ]
}
```

Renderers (`md`, `srt`, `vtt`, `txt`) consume only this file. Empty `speaker` collapses the speaker prefix in renderers. `original_speaker` is omitted (`omitempty`) when equal to `speaker`; it is informational only and not consumed by renderers (Plaud diarization assigns raw labels like `Speaker 0` and the user can rename them in the web UI; the raw label survives in `original_speaker` for audit). The `version` field bumps only on breaking schema changes (rename, remove, restructure); additive changes do not bump.

**`metadata.json` schema:**

```json
{
  "archive_schema_version": 1,
  "client_version": "0.2.0",
  "id": "a3f9c021...32hex",
  "title": "Kickoff møte",
  "title_slug": "kickoff_mote",
  "plaud_region": "euc1",
  "recorded_at_utc": "2026-04-30T14:30:00Z",
  "recorded_at_local": "2026-04-30T16:30:00+02:00",
  "duration_ms": 3720000,
  "audio": {
    "filename": "audio.mp3",
    "size_bytes": 9876543,
    "s3_etag": "...32hex",
    "original_upload_md5": "...32hex",
    "local_md5": "...32hex",
    "local_sha256": "...64hex"
  },
  "transcript": {
    "filename": "transcript.json",
    "sha256": "...64hex",
    "segment_count": 142,
    "language": "no"
  },
  "summary": {
    "filename": "summary.plaud.md",
    "sha256": "...64hex"
  },
  "fetched_at": "2026-05-01T09:14:21Z",
  "last_verified_at": "2026-05-01T09:14:21Z"
}
```

Per-artifact sub-objects (`audio`, `transcript`, `summary`) appear only when the corresponding artifact was actually written. Their absence is the structural signal for "not present in this folder".

Audio sub-object fields:
- `s3_etag`: served-bytes MD5 (single-part S3 uploads only). Canonical for F-07(a) idempotency. Reported by S3 in the `ETag` response header for both HEAD and GET against the signed `temp_url`.
- `original_upload_md5`: optional. Comes from the list response's `file_md5`. This is the MD5 of Plaud's original `.opus` upload, not the served `.mp3`; we never download those bytes. Recorded for audit (it does not equal `s3_etag`) and not used for any decision.
- `local_md5`: computed locally on the bytes after writing. For single-part uploads `local_md5` should equal `s3_etag` and the comparison is a useful integrity check; deviations indicate corruption in transit or on disk.
- `local_sha256`: stable archival hash, independent of S3's hashing choices. Used by external tooling that prefers SHA-256.

Transcript sub-object fields: `transcript.language` and `transcript.segment_count` are populated when those facts fall out of the detail response without extra work (`extra_data.aiContentHeader.language_code` for the language; segment count from the post-mapping array length).

**JSON formatting:** both `metadata.json` and `transcript.json` are pretty-printed (2-space indent), keys sorted, trailing newline. Predictable diffs for users who put archives under version control or in cloud sync.

**Versioning:**
- `archive_schema_version` (in `metadata.json`) bumps only on breaking changes to the archive layout or `metadata.json` shape. Pair with `client_version` for diagnostics.
- `version` (in `transcript.json`) bumps only on breaking changes to the transcript JSON shape.
- The two are independent; they version different files.

## 5. Tech stack

Unchanged from spec 0001. New runtime:

- **None required for the happy path.** Audio bytes, JSON, and markdown writing are stdlib only.
- **`golang.org/x/text/unicode/norm`** for NFKD slug folding (one new dependency, already part of `golang.org/x` extension modules).
- **`ffmpeg`** is *optional* and runtime-detected via `exec.LookPath` for non-mp3 audio formats. Not vendored.

## 6. Out of scope

- **Sync** (incremental over time, watch mode, sync state file). Spec 0003.
- **Prompt composition.** Spec 0004.
- **Server-side generation** of transcripts or summaries. Future spec, opened once Plaud's generation endpoints are captured.
- **Two-way sync** (uploading edits or new audio to Plaud).
- **Trash management** beyond honoring the `is_trash` filter at list time and the F-17 warning on direct-ID download.
- **Live transcript editing** (renaming speakers, correcting words, etc.).
- **Translation** of transcripts.
- **Audio post-processing** beyond format conversion (no normalization, no noise reduction).
- **Diff against prior versions** when Plaud updates a transcript or summary; v0.2 always overwrites the local cached copy on `--force`, otherwise skips per F-07.
- **Resume of partial audio downloads.** Stale `.partial` files are swept and the next run starts the audio fetch from scratch (F-14).
- **Config file** (`~/.config/plaud/...`) and `plaud config` subcommand. Defer until there are 3+ overridable defaults that warrant a real config system; v0.2 uses `PLAUD_DEFAULT_*` env vars (F-04, F-05).
- **`ffmpeg`-based audio format conversion** (mp3 → wav / m4a / opus / ...). The `--audio-format` flag is parsed and validated, but v0.2 always serves mp3. Conversion belongs in a future spec along with the related concerns (output codec selection, bitrate flags, ffmpeg discovery on Windows).
- **5xx retry** on the audio CDN. A failing 5xx surfaces as a per-recording error; the user re-runs (F-15).
- **Per-recording wall-clock cap** on audio downloads. Idle-read timeout is the only failure clock (F-15).
- **Plaud's `outline` artifact** (chapter / topic / agenda JSON; surfaces as `data_type=outline` in `/file/detail`'s `content_list`, served as gzipped JSON from content-storage S3). Useful for prompt composition; defer to spec 0004.
- **Plaud's `consumer_note`** (user-editable note overlaid on the transcript; `data_type=consumer_note`). Two-way sync territory; out of scope for v0.2.
- **Voice-identification `embeddings`** carried under `extra_data` in the detail response. 256-element float arrays per speaker label; ignored in v0.2 (no downstream use).

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | Are the `temp_url` audio signed URLs single-use, or can they be re-fetched within the 3600-second `X-Amz-Expires` window? | Cannot be settled from the existing HAR (each signed URL was hit exactly once). F-15's "refetch the temp-url endpoint once on 401/403" is the right defensive default regardless; verify empirically during Phase 5 implementation by hitting a signed URL twice via curl. |

The other questions from prior drafts are now closed: Q1-Q4 (audio endpoint, transcript endpoint, summary endpoint, `file_md5` semantics) by the Phase 0 HAR analysis (see `notes.md`'s 2026-05-02 entry); Q5-Q6 (folder timestamp UTC, slug folding for Norwegian) by §4 and F-03.

## 8. Acceptance criteria

1. **Default no-flag invocation.** `plaud download <id>` of a recording with `is_trans=true` and `is_summary=true` produces a folder at `<archive_root>/YYYY/MM/<slug>/` containing `transcript.json`, `transcript.md`, `summary.plaud.md`, `metadata.json` (no `audio.mp3`). Each file is non-empty and well-formed JSON / markdown.
2. **All-on invocation.** `plaud download <id> --include audio,transcript,summary,metadata` produces all 5 files including `audio.mp3`. `metadata.audio.local_md5` matches `md5sum audio.mp3` and equals `metadata.audio.s3_etag`; `metadata.audio.original_upload_md5` matches the list endpoint's `file_md5`.
3. **Idempotent re-run.** Re-running `plaud download <id>` does not re-fetch any audio bytes (verifiable via `--format json` output where the recording's `status` is `skipped`, or via a Content-Length sum if `--format json` is unused) unless `--force` is passed.
4. **Parallel.** `plaud download <id1> <id2> <id3>` with default concurrency completes in roughly one-third the wall time of three sequential `plaud download` invocations.
5. **Partial failure.** `plaud download <bad-id> <good-id>` exits non-zero, prints a clear per-recording error for `<bad-id>` to stderr (with the ID and underlying API error), and successfully fetches `<good-id>`.
6. **Selective include.** `plaud download <id> --include audio` writes `audio.mp3` and `metadata.json` only; no transcript or summary files. (`metadata.json` is always written when any artifact lands.)
7. **Format selection.** `plaud download <id> --transcript-format json,srt` writes `transcript.json` and `transcript.srt`; the SRT validates against an SRT linter. `transcript.md` is *not* written (the flag replaces the default).
8. **Token rotation mid-run.** A 401 surfacing partway through a multi-ID run cancels the parent context, exits non-zero, and prints the actionable message ("Token expired or invalid. Run `plaud login` again.") exactly once on stderr. Queued IDs are reported as `cancelled` (no repeated auth-error lines on either stream; in `--format json` mode their JSON line carries `error: "cancelled"`).
9. **Partial server state.** A recording with `is_trans=false` is downloaded; the folder contains `summary.plaud.md` and `metadata.json` (no transcript files); stderr carries the F-19 warning; exit code is 0.
10. **Trashed direct-ID.** A direct 32-hex ID for a recording with `is_trash=true` downloads normally; stderr carries the F-17 warning. The same recording cannot be reached by title prefix.
11. **`--force` semantics.** `plaud download <id> --force` against an unchanged recording rewrites every canonical file with byte-identical content, bumps both `fetched_at` and `last_verified_at` in `metadata.json`, and re-downloads `audio.mp3` if it is in the include set.
12. **Cross-platform walkthrough.** Acceptance criteria 1-11 reproduce on macOS, Linux, and Windows for the platform binaries from the GitHub release. On Windows, an archive root under `C:\Users\<long-name>\Documents\PlaudArchive\` succeeds even with a 60-char slug (validates F-18).
