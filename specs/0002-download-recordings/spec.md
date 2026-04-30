# Spec 0002: Download recordings

**Status:** Draft
**Created:** 2026-05-01
**Updated:** 2026-05-01
**Owner:** @simensollie
**Target version:** v0.2

The smallest unit of fetching: given one or more recording IDs, fetch audio + transcript + Plaud-generated summary + metadata into a structured local archive folder. Foundation for spec 0003 (sync) and spec 0004 (prompt composition).

This spec does not implement incremental sync, watch mode, or scheduled background fetching. Those live in spec 0003.

---

## 1. Goal

A user with a logged-in CLI can run `plaud download <id>` and find a complete, usable archive folder for that recording on disk: audio file they can play, transcript in machine-readable JSON plus rendered markdown, the Plaud-generated summary, and metadata for future bookkeeping.

## 2. Commands / interfaces

| Command | Behavior |
|---|---|
| `plaud download <id> [<id>...]` | Fetch one or more recordings into the archive. |
| `plaud download <id> --out DIR` | Override the default archive root. |
| `plaud download <id> --include audio,transcript,summary,metadata` | Select which artifacts (default: all). |
| `plaud download <id> --exclude audio` | Inverse of `--include`. |
| `plaud download <id> --transcript-format json,md,srt,vtt,txt` | Multiple formats per run; `json` and `md` by default. |
| `plaud download <id> --audio-format mp3` | Default mp3 (Plaud's served format). Other formats require `ffmpeg`. |
| `plaud download <id> --concurrency N` | Default 4 when multiple IDs are given. |
| `plaud download <id> --force` | Re-fetch even if local files match server checksums. |
| `plaud download <id> --format json` | NDJSON event stream on stdout. |

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | `plaud download <id>` fetches audio + transcript + Plaud-generated summary + metadata for that ID into the archive. | Must |
| F-02 | Default archive root: `${PLAUD_ARCHIVE_DIR:-~/PlaudArchive}` on POSIX, `%USERPROFILE%\PlaudArchive` on Windows. `--out DIR` overrides per invocation. | Must |
| F-03 | Per-recording folder layout: `<root>/YYYY/MM/YYYY-MM-DD_HHMM_<slug>/` containing `audio.<ext>`, `transcript.json`, `transcript.md`, `summary.plaud.md`, `metadata.json`. Slug rules: ASCII-folded, lowercase, non-word → `_`, max 60 chars. | Must |
| F-04 | `--include` and `--exclude` accept any subset of `audio,transcript,summary,metadata`. Default: all. | Must |
| F-05 | `--transcript-format` supports `json` (canonical, with `[{speaker, start, end, text}]` segments), `md` (rendered with timestamps), `srt`, `vtt`, `txt` (plain). Multiple formats can be requested per run. JSON is canonical and never optional when transcript is included; other formats are deterministically derived. | Must |
| F-06 | Multiple IDs in one invocation: parallel fetches with default concurrency 4. Configurable via `--concurrency N`. | Should |
| F-07 | Idempotent: existing files whose on-disk SHA-256 matches `file_md5` (or size + last-modified) are skipped. `--force` overrides. | Must |
| F-08 | Per-recording errors do not abort the run. Final exit code is non-zero if any recording failed; per-recording failures printed to stderr with the ID. | Must |
| F-09 | ID resolution: exact 32-hex match wins. A unique title prefix is also accepted. Otherwise, error listing candidates. | Should |
| F-10 | 401 from any sub-call → `api.ErrUnauthorized` → "Token expired or invalid. Run `plaud login` again." (same wording as `plaud list`). No retry. | Must |
| F-11 | `--audio-format` other than `mp3` requires `ffmpeg` on `PATH`; absent → skip with a warning, do not fail the recording. | Could |
| F-12 | NDJSON event stream with `--format json`: events `started`, `skipped`, `fetched`, `failed`, `done` per recording. Schema declared in `docs/schemas/0002/`. Stable from v0.2.0 GA. | Could |
| F-13 | Tokens, URLs containing signed credentials (S3 presigned URLs), and `Authorization` headers are never written to logs or stderr. | Must |

## 4. Storage / data model

```
<archive_root>/
└── 2026/
    └── 04/
        └── 2026-04-30_1430_kickoff_meeting/
            ├── audio.mp3                # downloaded bytes
            ├── transcript.json          # canonical: [{speaker, start_ms, end_ms, text}]
            ├── transcript.md            # rendered from transcript.json
            ├── summary.plaud.md         # Plaud's own summary (markdown)
            └── metadata.json            # id, title, dates (UTC + local), duration_ms,
                                          # source_md5, audio_sha256, fetched_at,
                                          # plaud_region, archive_schema_version
```

**Canonical files:** `transcript.json` and `metadata.json`. Other files (`transcript.md`, `transcript.srt`, etc.) are deterministically derived and may be deleted/regenerated without data loss.

**Folder naming:** `YYYY-MM-DD_HHMM_<slug>` in **UTC**. Local-time alternatives confuse incremental sync when a user travels timezones. The metadata file carries both UTC and the recording's original local time for human reference.

## 5. Tech stack

Unchanged from spec 0001. New dependencies considered:

- **None required** for the happy path (audio + JSON + markdown writing is stdlib only).
- `ffmpeg` is *optional* and runtime-detected via `exec.LookPath` for non-mp3 audio formats. We do not vendor it.

## 6. Out of scope

- **Sync** (incremental over time, watch mode, sync state file). That is spec 0003.
- **Prompt composition.** That is spec 0004.
- **Two-way sync** (uploading edits or new audio to Plaud).
- **Trash management** beyond honoring the `is_trash` filter at list time.
- **Live transcript editing** (renaming speakers, correcting words, etc.).
- **Translation** of transcripts.
- **Audio post-processing** beyond format conversion (no normalization, no noise reduction).
- **Diff against prior versions** when Plaud updates a transcript or summary; v0.2 always overwrites the local cached copy on `--force`, otherwise skips.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | What endpoint serves the audio bytes? Reverse-engineered prior art uses `GET /file/download/{file_id}` returning a signed URL (S3) or the bytes directly. | Capture during first implementation session via DevTools "Copy as cURL" on a download click. |
| 2 | Where do transcript segments live? Likely `GET /file/detail/{id}` per `jaisonerick/plaud-cli`, but we have not captured the response shape against the live API. | Same network capture as Q1 (download a file end-to-end and capture every request involved). |
| 3 | Where does the Plaud-generated summary live? Possibly inside the detail response, possibly its own endpoint (`/summary/...` showed up in the v0.1 HAR, only for templates). | Same capture session. |
| 4 | Is `file_md5` (32 hex in the list response) the audio bytes' MD5, or the post-encoding container's MD5? We need it for the idempotency check; if it does not match `md5sum audio.mp3`, fall back to size + Last-Modified. | Verify by computing `md5sum` on a downloaded file. |
| 5 | Folder timestamp: UTC or recording-local? | Default **UTC**, per `metadata.json` we surface both. Counter: humans recognize their local times more readily. UTC wins on stability across travel; documented in README. |
| 6 | Slug folding for Norwegian: `æ → ae`, `ø → o`, `å → a` works for FAT32-safe filenames but may collide ("møte" and "mote" both → `mote`). | Append a 6-char ID suffix to the slug if a collision is detected. Document in README. |
| 7 | Are signed download URLs single-use? If so, retry-after-401 needs to refetch the URL, not re-call the same one. | Design the download path as: get signed URL → fetch bytes; on 4xx during fetch, refetch the URL once. |

## 8. Acceptance criteria

1. `plaud download <id>` of an existing recording produces a folder at `<archive_root>/YYYY/MM/<slug>/` containing `audio.mp3`, `transcript.json`, `transcript.md`, `summary.plaud.md`, `metadata.json`. Each file is non-empty and well-formed.
2. Re-running `plaud download <id>` does not re-fetch any bytes from the audio CDN (verifiable via response Content-Length sum or NDJSON `skipped` events) unless `--force` is passed.
3. `plaud download <id1> <id2> <id3>` runs the three fetches in parallel with default concurrency 4, completing in ~one-third the time of three sequential downloads.
4. `plaud download <bad-id> <good-id>` exits non-zero, prints a clear per-recording error for `<bad-id>` to stderr, and successfully fetches `<good-id>`.
5. `plaud download <id> --include audio` writes only `audio.mp3` (and skips `transcript.*`, `summary.plaud.md`, `metadata.json`).
6. `plaud download <id> --transcript-format json,srt` writes both formats and the SRT validates against an SRT linter.
7. Token rotation midway through a multi-ID run: a 401 surfaces the spec's actionable message and aborts further fetches.
8. Acceptance walk-through completes on macOS, Linux, and Windows for the platform binaries from the GitHub release.
