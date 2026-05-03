# Notes: Spec 0002: Download recordings

Append-only journal. Newest entry on top.

For the convention, see `specs/README.md`.

---

## 2026-05-03: Plaud signals auth failure as envelope status -3900 under HTTP 200, not HTTP 401

Discovered during the §8.8 smoke walk (token-rotation mid-run). With an invalid bearer, Plaud's API endpoints (`/file/simple/web`, `/file/detail/{id}`, `/file/temp-url/{id}`) return:

```
HTTP/1.1 200 OK
Content-Type: application/json

{"status":-3900,"msg":"invalid auth header","data":...}
```

This is the same shape as a successful response, just with a non-zero envelope status. Our existing F-10 handling only mapped HTTP 401 to `ErrUnauthorized`; the envelope-status-only path fell through to a generic `ErrAPIError`, so:
- The actionable message ("Token expired or invalid. Run `plaud login` again.") never fired.
- F-10's "cancel parent context, drop queued recordings" never fired either: every queued ID hit the same envelope error, surfaced its own per-recording line, and the run looked like five independent failures rather than one auth event.

Fix landed in `internal/api/{list,detail,temp_url}.go`: a new package constant `apiStatusInvalidAuthHeader = -3900` is checked before the generic non-zero-status branch in each envelope handler, returning `ErrUnauthorized` instead. Three new tests (`TestList_F10_EnvelopeInvalidAuthHeaderReturnsUnauthorized`, equivalents for Detail and TempURL) lock this in.

Status code -3900 is the only auth-failure code we have empirical evidence of so far. If a different code surfaces later (token-revoked vs. token-malformed vs. token-expired might be distinct in Plaud's enum), extend the constant or promote it to a small list. The design grilling section below ("Q4: file_md5 semantics") had assumed Plaud uses HTTP-level status codes for auth; that assumption is wrong for these endpoints.

Pattern note: this is the second smoke-walk bug (after the GET-only signed URL above) where unit tests mocked the wire shape we *expected* and the real API used a different one. For spec 0003 (sync) and beyond, default-assume that any error path needs at least one real-account check before the spec can flip to Done.

---

## 2026-05-03: Plaud's `temp_url` is signed for GET only; HEAD returns 403

Discovered during the §8.2 smoke walk. The Phase 0 capture entry below claims HEAD against `temp_url` works; that was wrong. AWS SigV4 presigned URLs include the HTTP method in the canonical request, and Plaud's backend signs for `GET`, so `HEAD` against the same URL fails the signature check.

Empirical verification (fresh signed URL, real account):
- `curl -I <url>` → `403 Forbidden` (`Content-Type: application/xml`, S3 SignatureDoesNotMatch envelope).
- `curl -H 'Range: bytes=0-0' <url>` → `206 Partial Content` with full headers including `ETag`, `Content-Range: bytes 0-0/<total>`, `Last-Modified`, `Accept-Ranges: bytes`.

Fix landed in `internal/api/audio.go`: the idempotency probe is a one-byte ranged `GET` instead of `HEAD`. Total size is parsed from `Content-Range` (the 206's `Content-Length` is the chunk length, not the object size). The function and result type are renamed `HeadAudio`/`AudioHead` → `ProbeAudio`/`AudioProbe` for honesty. The F-15 `ErrSignedURLExpired`-then-refetch-once retry path is unchanged and still applies (now triggered by 401/403 on the ranged GET).

Knock-on: the 2026-05-02 design grilling section below proposed a "cheap `HEAD` on the signed `temp_url`" as the F-07(a) idempotency mechanism. Treat those paragraphs as historical context only; the live implementation now uses the ranged-GET probe described above. The browser in the Phase 0 capture session never actually issued `HEAD`; it streamed via `GET` and the design discussion assumed S3's general HEAD capability would carry over to a presigned URL. SigV4 signing makes that assumption wrong.

---

## 2026-05-02: Spec example correction: `kickoff_møte` slug

The §4 metadata.json example in `spec.md` previously read `"title_slug": "kickoff_m_te"`. That value is wrong: the F-03 slug rules apply `ø → o` *before* NFKD, so `"Kickoff møte"` folds to `"kickoff_mote"` (the implementation in `slug.go` already produced this; the test fixture in `internal/archive/slug_test.go::TestSlug_F03_NorwegianFolding` already covered `"Sjær på Gøteborg" → "sjaer_pa_goteborg"`, which exercises the same `ø` codepoint).

The Phase 3 hand-off note rationalised the `m_te` value by claiming `ø` decomposes through NFKD into `o` plus a stroke combining mark; that is incorrect. `ø` (U+00F8 LATIN SMALL LETTER O WITH STROKE) is canonical and does not decompose. Norwegian `ø` only folds to `o` via the explicit `foldNorwegian` step. The bug was in the spec example, not in the code.

Corrections:
- `spec.md` §4 metadata example: `kickoff_m_te` → `kickoff_mote`.
- `slug_test.go` adds two cases to lock this down: `"Kickoff møte" → "kickoff_mote"` and `"møte" → "mote"`.
- `docs/user/commands/download.md` already used `kickoff_mote` (the docs author followed the rules text rather than the broken example, which was the right call).

No code change. No FR change.

---

## 2026-05-02: Phase 0 network capture analysis

Source HAR: `specs/0002-download-recordings/resources/export.web.plaud.ai.har` (14 MB). Adjacent under `resources/`: the user's web-UI exports of the same recording (`*.mp3`, `-Summary.txt`, `-transcript.txt`, `-Meeting Highlights.txt`). The `resources/` folder is gitignored (`specs/*/resources/`) and contains live tokens / signed URLs / customer PII; do NOT commit anything from it and do NOT paste excerpts verbatim into spec / plan / notes (paraphrase or use synthetic data).

Capture covers a single recording (`e1d9aa6c83378b3182cbc20e94b3c6de`) opened in the web UI followed by an audio export, transcript export, and summary export. The same recording is also present in `specs/0001-auth-and-list/web.plaud.ai.har` (list response), so cross-referencing list metadata vs served-audio bytes is possible.

Common headers on every authenticated `api-euc1.plaud.ai` GET in this capture: `app-language`, `app-platform: web`, `edit-from: web`, `x-device-id`, `x-pld-user`, `x-request-id`, `timezone`, `origin: https://web.plaud.ai`, `referer`. Same set as documented in spec 0001 notes. Bearer is presumed sent via cookie (HAR strips it); our CLI sends `Authorization: bearer <jwt>` and that has been confirmed working in spec 0001 Phase 6.

### Q1: Audio download endpoint

Two-step flow.

**Step 1: request a signed URL.**
`GET https://api-euc1.plaud.ai/file/temp-url/{file_id}`
- No query params, no body.
- Response `200 application/json`:
  ```
  {
    "status": 0,
    "temp_url": "https://euc1-prod-plaud-bucket.s3.amazonaws.com/audiofiles/{file_id}.mp3?X-Amz-Algorithm=...&X-Amz-Credential=<redacted>&X-Amz-Date=<YYYYMMDDTHHMMSSZ>&X-Amz-Expires=3600&X-Amz-SignedHeaders=host&X-Amz-Security-Token=<redacted>&X-Amz-Signature=<redacted>",
    "temp_url_opus": null
  }
  ```
- `temp_url` is an AWS SigV4 presigned S3 URL, valid 3600 s (`X-Amz-Expires=3600`).
- `temp_url_opus` was `null` for this recording (an `.opus` original existed per the list response, but the audio endpoint serves the `.mp3` transcoded copy and the opus link was not produced). Treat as nullable/optional; ignore for v0.2.
- Path host is `euc1-prod-plaud-bucket.s3.amazonaws.com`, key shape `audiofiles/{file_id}.mp3`. Region is encoded in `X-Amz-Credential` (`eu-central-1`).

**Step 2: fetch the audio bytes directly from S3.**
`GET <temp_url>`
- No `Authorization` header (auth is in the URL). Browser sent only standard CORS headers (`origin`, `referer`, `sec-fetch-*`).
- Response `200`. Headers:
  - `Content-Type: binary/octet-stream` (NOT `audio/mpeg`; S3 returns the bucket default).
  - `Content-Length: 4465808`
  - `Accept-Ranges: bytes` (range requests are supported, useful for resume in a future spec).
  - `ETag: "5bf9e2f09de63a76a980d1ae0f24c66e"` (32 hex, quoted; this is an MD5 of the served bytes for single-part S3 uploads).
  - `Last-Modified: Thu, 30 Apr 2026 19:41:33 GMT`
  - `x-amz-server-side-encryption: AES256`
  - `Access-Control-Allow-Origin: *`, `Access-Control-Expose-Headers: ETag` (so the browser can read the ETag for MD5 idempotency).
- Body is the raw audio bytes (not a JSON wrapper).

### Q2: Transcript endpoint

Two layers, again.

**Layer 1: recording detail.**
`GET https://api-euc1.plaud.ai/file/detail/{file_id}`
- No query params, no body.
- Response `200 application/json`. Top-level wrapper: `{status: 0, msg: "success", request_id: "", data: {...}}` (same envelope shape as `/file/simple/web`).
- `data` keys (verified):
  - `file_id`: 32-hex string
  - `file_name`: string (user title)
  - `file_version`: number (epoch seconds; matches list `version`)
  - `duration`: number (milliseconds; for this recording 1116000 ms = 18:36, matches list)
  - `is_trash`: bool
  - `start_time`: number (milliseconds; e.g. 1777546223000)
  - `scene`: number
  - `serial_number`: string (~16 chars for hardware recordings; epoch-ish for web/app)
  - `session_id`: number (epoch seconds)
  - `wait_pull`: number
  - `filetag_id_list`: array
  - `content_list`: array of "artifact pointers" (see below)
  - `pre_download_content_list`: array of inlined artifacts (see below)
  - `download_path_mapping`: object, was empty `{}` in this capture (purpose unknown)
  - `embeddings`: object keyed by `"Speaker 1"`, `"Speaker 2"`, ... (raw speaker labels), values are 256-element float arrays. Voice-identification embeddings; ignore for v0.2.
  - `extra_data`: object with sub-objects:
    - `aiContentHeader`: `{category, headline, language_code, original_category, recommend_questions: [...], summary_id, used_template, ...}`. The `language_code` here is the source-of-truth for `metadata.transcript.language` (e.g. `"no"`).
    - `tranConfig`: `{created_at: "<ISO 8601 with microseconds, no tz>", diarization: 0|1, language, llm, type, type_type}`. `language` is the user-selected transcription language; can differ from `aiContentHeader.language_code` (the auto-detected one) but in this sample they matched.
    - `task_id_info`: `{outline_task_id, summary_id, trans_task_id}`
    - `actionData`, `aiContentForm`, `model`, `last_trans_app_platform`, `last_trans_device_id`, `has_replaced_speaker`, `used_template`. Not needed for v0.2.
  - `has_thought_partner`: bool

**`content_list` shape (one entry per derivable artifact).**
Array of objects, each with keys: `data_id` (string, ~80 chars, prefix encodes type, e.g. `source_transaction:6bf84df9:<file_id>`), `data_type` (string, see types below), `task_status` (number; `1` = ready in this sample), `err_code` (string, empty when ok), `err_msg` (string, empty when ok), `data_title` (string, may be empty), `data_tab_name` (string, e.g. `"Summary"`, `"Meeting Highlights"`), `data_link` (string, AWS SigV4 presigned URL into `euc1-prod-plaud-content-storage.s3.amazonaws.com`, `X-Amz-Expires=300`), `extra` (object with type-dependent fields).

Observed `data_type` values in this capture:
| `data_type` | Meaning | `data_link` target | `extra` keys |
|---|---|---|---|
| `transaction` | Raw transcript (the segments) | `permanent/<user_id_hash>/<member_id>/file_transcript/<file_id>/trans_result.json.gz` | `task_id` |
| `outline` | Topic / chapter list | `permanent/.../file_outline/<file_id>/outline.json.gz` | `task_id` |
| `auto_sum_note` | Plaud-generated summary | `permanent/.../file_summary/<file_id>/ai_content.md.gz` | `summ_type`, `summ_type_type`, `summary_id`, `used_template` |
| `consumer_note` | User-editable note ("Meeting Highlights") | `permanent/.../general/note%3A<short>%3A<note_id>` | `summary_id`, `used_template` |

`pre_download_content_list` shape (subset of artifacts inlined to save a round-trip): array of `{data_id, data_content}`. In this capture only the `auto_sum_note` (summary markdown) was inlined; the transcript and outline were NOT. Decision rule appears to be "inline anything that's small enough"; the summary fit, the transcript (5.7 KB gzipped) did not.

**Layer 2: transcript bytes (always a separate fetch).**
`GET <data_link from content_list[0]>` against `euc1-prod-plaud-content-storage.s3.amazonaws.com`.
- Response `200`. Headers:
  - `Content-Type: application/json`
  - `Content-Encoding: gzip` (S3 stored object is `.json.gz` and S3 sets `Content-Encoding: gzip`, so HTTP clients with auto-decompression get plain JSON; raw clients get gzip).
  - `Content-Length: 5688` (compressed)
  - `ETag: "<32-hex>"` (MD5 of the gzipped object)
  - `Last-Modified: <RFC 1123 GMT>`
  - `x-amz-meta-compressed: true`
  - `x-amz-meta-updated_at: <ISO 8601>` (transcript regeneration timestamp; potentially useful for `metadata.transcript.fetched_at`).
- Body (after gunzip) is a **bare JSON array** (not wrapped). Each segment:
  ```
  {
    "start_time": <number, ms>,
    "end_time":   <number, ms>,
    "content":    <string, the spoken text>,
    "speaker":    <string, the user-edited / display name, may match original_speaker, e.g. "Simen Sollie">,
    "original_speaker": <string, raw "Speaker N" label as Plaud's diarizer produced it>
  }
  ```
- Sample had 68 segments; last segment ended at `1114770` ms vs recording duration `1116000` ms. Confirms ms semantics.
- No `language` field, no `version`, no top-level metadata. Language is in `/file/detail`'s `extra_data.aiContentHeader.language_code`.

**Implication for canonical transcript JSON (F-05).**
Plaud's keys are `start_time`, `end_time`, `content`, `speaker`, `original_speaker` (NOT the spec's draft `start_ms`, `end_ms`, `text`, `speaker`). We have a choice:
- (a) Adopt Plaud's keys verbatim. Pro: no translation. Con: ambiguous "is `start_time` ms or s?" (it's ms, but the name doesn't say so), and we lose an opportunity to drop the redundant `original_speaker` for users who edited speakers. We also locked `start_ms`/`end_ms` in the 2026-05-02 grilling session for round-trip-safety reasons.
- (b) Translate to canonical `{speaker, start_ms, end_ms, text}` plus optionally retain `original_speaker` as an additional key. Pro: explicit ms in name, slim JSON. Con: lossy if `speaker` and `original_speaker` differ and we drop the latter.
- (c) Translate but carry both: `{speaker, original_speaker, start_ms, end_ms, text}`.
**Recommendation:** option (c). The grilling already locked `start_ms`/`end_ms`. Carrying `original_speaker` is one extra string per segment and lets future spec features (re-rendering with raw labels, speaker-edit history) work without re-fetching. F-05 should be amended to include `original_speaker` as an optional field, default-omitted only when equal to `speaker`.

### Q3: Summary endpoint

Inline. `data.pre_download_content_list[0].data_content` is the markdown summary (verified byte-equivalent to the S3 `ai_content.md.gz` body once that's gunzipped). The web client uses the inlined copy and only fetches the S3 link as a fallback.

For the CLI, this means: **no second HTTP call needed for the summary in the common case.** The detail response already carries it. Only fall back to the S3 `data_link` if `pre_download_content_list` does not contain an entry whose `data_id` starts with `auto_sum:`.

Format: GitHub-flavored markdown with H2 sections (`## Møteinformasjon`, `## Møtereferat`, `## Neste steg`, `## AI-forslag`). The schema is template-driven (see `extra_data.aiContentHeader.used_template = {template_id: "meeting", ...}`); other templates may produce different sections but the format is always markdown. Write it verbatim to `summary.plaud.md` per F-05.

### Q4: `file_md5` semantics

**Resolved empirically.** The list response from spec 0001 captured this same recording with:
- `fullname`: `e1d9aa6c83378b3182cbc20e94b3c6de.opus`
- `filesize`: `4465280` bytes
- `file_md5`: `9c0d803da5941b2741092d49029e622c`

The audio served by `/file/temp-url`-then-S3 was:
- Path: `audiofiles/e1d9aa6c83378b3182cbc20e94b3c6de.mp3`
- `Content-Length`: `4465808` bytes
- S3 `ETag`: `5bf9e2f09de63a76a980d1ae0f24c66e`
- Local `md5sum` of the downloaded `.mp3`: `5bf9e2f09de63a76a980d1ae0f24c66e` (matches ETag exactly).

So the served audio's MD5 is the S3 ETag (single-part S3 upload, ETag = MD5 of bytes), and **`file_md5` from the list endpoint is the MD5 of a *different* file** (the original `.opus` upload), which differs in size by 528 bytes and entirely in MD5.

**Implication for F-07.** As written, F-07 says "Audio: skip when local MD5 of `audio.<ext>` matches the server's `file_md5` from the list response." That comparison will ALWAYS mismatch for any recording where Plaud transcoded the original on the server side (`.opus` upload from the hardware → `.mp3` served via `temp-url`). F-07 needs to change.

Two viable replacements, in order of preference:
1. **Use the S3 `ETag` from the audio fetch's response headers.** Single-part S3 uploads (which Plaud uses for the audio bucket) yield `ETag = MD5(bytes)`. The CLI can do a cheap `HEAD` on the signed `temp_url`, read the ETag, and compare to the local file's MD5 before re-downloading. Cost: one extra HTTP HEAD per recording per re-run. Caveat: multi-part S3 uploads have a non-MD5 ETag (suffixed `-<N>`); detect and fall back.
2. **Use the local `metadata.audio.local_md5` from the previous run.** No server roundtrip; rely on the user's archive folder being trusted. Re-fetch only when `--force`, when `audio.<ext>` is missing, when sizes differ, or when stored `version_ms` < server `version_ms` (the list response already gives us this). Counter: a recording re-encoded server-side with the same `version_ms` (rare? unverified) would not trigger a re-fetch.

A hybrid is safest: use `version_ms` and `Content-Length` (cheap, no extra round-trip if we already have the detail call cached) as the primary "is the server copy different" signal, and use the ETag-vs-`local_md5` check as the verification step when `--force` or paranoia is desired. F-07 wording should be rewritten to:
- (a) Audio idempotency = compare server `version_ms` (from list) and `Content-Length` (from S3 HEAD if needed) to stored `metadata.audio.size_bytes` and stored `metadata.recording_version_ms`. If unchanged, skip. If we actually fetch, verify post-download by comparing the response ETag (when single-part) against the streamed-bytes MD5.
- (b) Drop the `server_md5` field from `metadata.json` (it's misleading: the list's `file_md5` does not match the served bytes). Replace with `audio.original_upload_md5` and a comment that this is the original-format MD5, not the served-bytes MD5. Or omit entirely.

This is a substantive F-07 rewrite. Capture as a separate notes entry once the design is confirmed.

### Q7: Signed URL single-use behavior

**Cannot be settled empirically from this HAR.** Each of the five S3 URLs (audio, transcript, outline, summary, consumer note) appears exactly once. No reuse was triggered.

Documented characteristics for downstream reasoning:
- Audio URL: `X-Amz-Expires=3600` (1 hour). One signed URL per call to `/file/temp-url`. Calling `/file/temp-url` twice in succession would produce two different signatures (the URL embeds `X-Amz-Date` and `X-Amz-Signature`, both rotate). Whether the *first* URL keeps working after the *second* is generated is the unknown (AWS SigV4 says yes, signatures don't invalidate each other, but Plaud's IAM policy via `X-Amz-Security-Token` could narrow that).
- Content-storage URLs (transcript, outline, summary, note): `X-Amz-Expires=300` (5 min). All four are minted in the same `/file/detail` response, sharing a single `X-Amz-Credential` and `X-Amz-Security-Token` (visible by inspecting the credential prefix), but each has its own per-key `X-Amz-Date` and `X-Amz-Signature`.
- All URLs use AWS SigV4 + STS session tokens (`X-Amz-Security-Token`), which means the underlying credentials are short-lived AWS STS credentials, not long-lived IAM users. The tokens themselves expire on the order of the `X-Amz-Expires` window plus a session-credential TTL Plaud chose at mint time.

**Recommendation for F-15.** Keep the "refetch URL once on 401/403" wording. It costs one extra `/file/detail` (or `/file/temp-url`) call per audio failure, which is cheap, and it covers both the single-use case (URL just stops working) and the expiry case (URL was minted >1h ago and the session crossed the boundary). If we later discover URLs are reusable, the refetch is harmless extra work; if they're single-use, the refetch is correct. No change needed.

A separate concern surfaced by the X-Amz-Expires=300 on the content-storage URLs: in a multi-recording run with `--concurrency 4`, if a worker stalls for >5 minutes between getting the detail response and fetching the transcript, the URL has expired by the time the worker reaches it. Mitigation: each worker should fetch detail and transcript/summary back-to-back (tight loop), not buffer detail responses for batch-fetching. F-06 already says "the detail call must precede audio fetch (its `file_md5` drives the audio idempotency check)"; the same per-recording sequencing covers the transcript-URL-expiry case implicitly. No new FR needed, but worth a comment in the code.

### Bonus: generation triggers (for future spec)

This HAR captured a recording where the transcript and summary already existed (`is_trans=true`, `is_summary=true`). No transcribe/generate calls were fired. The only `/ai/...` endpoint observed was `GET /ai/file-task-status` (no query string), which returns `{status, msg, data: {file_status_list: [{file_id, post_id, task_id, task_status, task_type: "transcript"|"summary", sum_type, sum_type_type, ppc_status, is_chatllm, auto_save}]}}`, a status query for in-flight async jobs across the user's recordings, not a trigger.

Triggering transcription/summarization endpoints are likely under `/file/...` or `/summary/...`, but nothing matching that shape fired in this capture (the user did not click "transcribe" or "summarize"). A future spec author needs a separate HAR captured against a `is_trans=false` recording with the user clicking "Generate transcript" / "Generate summary".

What this HAR does establish:
- The `task_id` values (e.g. `20260430194349-v3@f671be9da4c1b1f886c6c9` for transcript, `20260430194421-v2@e0c9e79a2d10e97242d872` for summary) are returned by `/file/detail`'s `extra_data.task_id_info`. Once a generation trigger endpoint is found, the response presumably returns a new `task_id` of this shape, which the client then polls via `/ai/file-task-status` until `task_status=1`.
- Summary generation is template-driven (`/summary/community/templates/weekly_recommend` returned 28 KB of available templates as POST). The trigger endpoint will likely need a `template_id` ("meeting", "interview", etc.).

### Implications for spec.md / FRs

The HAR resolves Q1 and Q3 cleanly, partially resolves Q2 (transcript shape known but key names differ from spec), resolves Q4 with a finding that invalidates F-07's audio idempotency design, and leaves Q7 partially open (signed-URL reuse cannot be tested from this capture; F-15's "refetch once on 401/403" is the right defensive default regardless).

Concrete spec edits needed (do NOT apply without owner review):
- **F-07 (a):** Audio idempotency design needs a rewrite. The list endpoint's `file_md5` does not hash the served audio bytes (it hashes the original upload, which is in a different format). Replace with: "compare server `version_ms` and S3 `Content-Length` to stored values; on mismatch or absence, fetch and verify ETag against streamed-bytes MD5 when ETag is unsuffixed (single-part)." See Q4 above.
- **§4 metadata schema:** Drop or rename `audio.server_md5`. The current name implies "the MD5 the server says the audio has," which is misleading. Rename to `audio.original_upload_md5` (and omit the field unless we actually have a use for it, which currently we don't), and add `audio.s3_etag` (the ETag from the audio S3 fetch, which IS the MD5 of the served bytes when single-part).
- **F-05 (transcript JSON shape):** Accept that Plaud's wire keys are `start_time`/`end_time`/`content`/`speaker`/`original_speaker`. Either (preferred) translate to canonical `{speaker, original_speaker, start_ms, end_ms, text}` and omit `original_speaker` when it equals `speaker`, or update F-05 to use Plaud's keys verbatim. Either way, document the chosen mapping explicitly in F-05; the current spec text doesn't mention `original_speaker` and doesn't say what to do when `speaker` is the literal string `"Speaker 1"` vs a real name.
- **F-06 / new comment in code:** Note that content-storage signed URLs expire after 300 s. Each worker must fetch detail and follow its `data_link`s within that window; do not pre-batch detail responses across workers.
- **No spec change needed for Q7.** F-15's single retry on 401/403 covers both the "single-use" interpretation and the "expired" interpretation.

Implementation simplification surfaced by the HAR: **the summary does not need a separate HTTP call.** It comes inline in `/file/detail`'s `pre_download_content_list[0].data_content` for the auto-summary, which means a "transcript + summary + metadata" download is two HTTP calls (detail + transcript-S3), not three. Update plan.md when the relevant phase is rewritten.

---

## 2026-05-02: Design grilled and locked

Walked the spec end-to-end with an LLM in a `/grill-me` session. Every branch of the design tree resolved before any code is written. Spec rewrite folds the decisions into FRs and §4. Phases in `plan.md` need a parallel review (some phases shift; new ones for atomic writes, idle-timeout reader, and per-recording JSON output land).

Resolved decisions, organised by FR:

- **F-01 / F-19 (partial server state).** When `is_trans=false` or `is_summary=false`, skip the affected artifact with a stderr warning rather than fail the recording. `is_trans` and `is_summary` are explicit readiness flags; treating them as errors inverts API semantics. (Considered: error by default, opt-in `--allow-partial`. Rejected: every fresh recording would fail.)
- **F-04 (default include set).** Flipped from `audio,transcript,summary,metadata` to `transcript,summary,metadata`. Audio is opt-in. Reasoning: audio dominates disk, the primary workflow is transcript-driven, and forcing `--exclude audio` on every invocation is the textbook broken default. Counter to be aware of: audio is the only re-derivable source-of-truth if Plaud deletes a recording server-side; documented in `docs/user/`.
- **F-04 / F-05 (config mechanism).** Env-var only for v0.2 (`PLAUD_DEFAULT_INCLUDE`, `PLAUD_DEFAULT_TRANSCRIPT_FORMAT`). No config file, no `plaud config` subcommand. Graduate to a config-file spec when the third overridable default lands. Consistent with the existing `PLAUD_ARCHIVE_DIR` precedent from F-02.
- **F-05 (transcript JSON canonical shape).** `{"version": 1, "segments": [{"speaker", "start_ms", "end_ms", "text"}]}`. Integer milliseconds (Plaud's wire format is ms; round-tripping to float seconds loses precision). Snake_case. Object wrapper (not bare array) for forward-compat. Speaker is Plaud's raw label, possibly empty string; renderers handle the empty case by omitting the speaker prefix. Earlier draft had a contradiction between §3 (`start`/`end`) and §4 (`start_ms`/`end_ms`); resolved in favour of §4.
- **F-06 (concurrency unit).** `--concurrency 4` = recordings in parallel; serial within each recording. Detail call must precede audio fetch (its `file_md5` drives the audio idempotency check), so per-recording parallelism is illusory anyway. Clamp `[1, 16]` and reject out-of-range up front.
- **F-07 (idempotency).** Per-artifact strategy: audio = MD5 vs server `file_md5`; transcript = SHA-256 of canonical segments vs `metadata.transcript.sha256`; summary = SHA-256 vs `metadata.summary.sha256`; derived files always regenerated when source changed, never touched when not. Two timestamps in metadata: `fetched_at` (last write) and `last_verified_at` (last successful end-to-end verification). Earlier F-07 wording was structurally broken (`SHA-256` cannot match `file_md5` since lengths differ).
- **F-09 (ID resolution).** Dispatch on `^[0-9a-f]{32}$` for the cheap path; only call `client.List` when the arg looks like a title fragment. Case-insensitive prefix on `Recording.Filename`. No substring matching (Norwegian collation surprises).
- **F-10 (401 mid-run).** Session-level: cancels parent context, hard-cancels in-flight workers, drops queued IDs. Earlier wording bundled it under F-08 ("per-recording errors do not abort"); 401 is not a per-recording error and explicit separation removes the contradiction.
- **F-11 (audio format).** Singular (one format per run) by design. `--transcript-format` is plural because renderers are cheap and produce different presentation files from the same canonical JSON; audio formats multiply on-disk weight without proportional benefit.
- **F-12 (NDJSON event stream).** Shrunk from a five-event stream (`started`/`skipped`/`fetched`/`failed`/`done`) with a stability promise to one JSON object per recording at completion. Bumped from Could to Should because acceptance criterion 3 needs structured output for automated idempotency verification. The "Stable from v0.2.0 GA" promise was contradictory with a Could-priority feature; dropped. Schema documented in `--help`, no `docs/schemas/0002/` directory.
- **F-14 (atomic writes).** Per-file atomic via `<name>.partial` next to destination plus `os.Rename` (cross-fs impossible by construction since temp lives in the target folder). Stale `.partial` files swept at start of each run before any idempotency check. Corrupt `metadata.json` triggers local-hash rebuild (with stderr notice), not a refusal or a full re-fetch.
- **F-15 (audio HTTP timeout).** Separate audio HTTP client with no total timeout; idle-read deadline (30s without progress aborts). Bookkeeping calls keep the existing 30-second total timeout. Signed CDN URL refetched once on 401/403 (signature expiry). No 5xx retry in v0.2 (re-running is the user's recourse; idempotency makes it cheap). No per-recording wall-clock cap (slow-but-progressing connections are a legitimate use case).
- **F-16 (`--force`).** Binary, applies across the current effective include set. Bumps both `fetched_at` and `last_verified_at` even when bytes are unchanged (`--force` *is* extra verification, not just a write). Granular per-artifact force flags rejected as feature creep; equivalent achievable via `--include X --force`.
- **F-17 (trashed direct ID).** Download with stderr warning. The user explicitly typed an ID; honour it but flag the non-default state. Title-prefix resolution does not surface trashed recordings (the list endpoint already filters with `is_trash=0`).
- **F-18 (Windows long-path).** Prefix absolute output paths with `\\?\` to lift the 260-char limit. Cross-platform behaviour stays uniform (rejected: per-OS slug truncation, which would produce different folder names on the same recording across OSes).
- **§4 metadata schema.** Nested per-artifact sub-objects (`audio`, `transcript`, `summary`); their absence is the structural signal for "not present". `archive_schema_version: 1`, bumps on breaking changes only. Pretty-printed JSON with sorted keys for diff-friendliness. Both `recorded_at_utc` (folder-name source, canonical) and `recorded_at_local` (human reference) carried.
- **§4 slug rules.** Strip trailing audio extension (allowlist); apply `æ→ae`, `ø→o`, `å→a`; NFKD-fold combining marks; lowercase; non-word → `_`; cap 60 chars post-fold with word-boundary truncate in last 10. Empty slug → `untitled`. 6-char ID suffix on collision (or every "untitled" recording, since they collide by definition).
- **§8 acceptance criteria.** Edited to reflect the new default (no audio in default invocation), the new F-12 shape (per-recording JSON object), and the F-18 Windows path validation. Added explicit ACs for partial server state (F-19), trashed direct-ID (F-17), and `--force` round-trip (F-16).

Open questions remaining (Phase 0 capture should close them all):
- Q1-3: endpoint shapes for audio download, transcript, summary.
- Q4: whether `file_md5` matches the served audio bytes' MD5 (drives F-07 audio idempotency design).
- Q7: signed-URL reuse / single-use behaviour (drives F-15 retry semantics).

The deferred future work that came out of this session:
- A `plaud transcribe` / `plaud summarize` command for triggering server-side generation when a recording has `is_trans=false`. Capture the relevant Plaud endpoints during Phase 0; open as a future spec.
- A config-file spec when the third `PLAUD_DEFAULT_*` env var would be added.

---

## 2026-05-01: Spec opened (Draft)

Initial draft. Pre-implementation work needed before promoting to `Active`:

**Required network capture session (Phase 0 of the plan):**

Open https://web.plaud.ai in DevTools with "Preserve log" on. Pick a recording and:

1. Open the recording (the detail panel / transcript view loads). Note every request fired against `api-euc1.plaud.ai` during this. Likely candidates: `GET /file/detail/<id>`, possibly a separate transcript endpoint.
2. Trigger an audio download (the download button in the UI, or play, then check what the audio element fetches). Capture the `<audio>` source URL or the download URL.
3. View / regenerate / read the summary. Capture any `/summary/...` requests.

For each captured request, record in this file:

```
**<endpoint> (<method>)**
- Path: ...
- Query params (if any): ...
- Request body shape (keys only, no values): ...
- Response status: 200
- Response body shape (keys, value types and approximate sizes; no PII):
  - field_a: string (~32 chars)
  - field_b: number
  - segments: array of objects {speaker, start_ms, end_ms, text}
- Notes: ...
```

The download request specifically: confirm whether the response body is the audio bytes directly (`Content-Type: audio/mp4` etc.) or a JSON wrapper carrying a presigned URL we then fetch. Either is fine; we just need to know.

**Open questions surfaced by the v0.1 work that 0002 may want to revisit:**

- The recording's `file_md5` is 32 hex chars in `/file/simple/web`. Verify against `md5sum` of the downloaded audio. If it matches, our idempotency story is trivial (compare local file's md5 to server's claimed md5). If not, document what `file_md5` actually hashes.
- `is_trans` and `is_summary` booleans on the list response are presumably "ready to be fetched". But what if the user triggers a recording that has never been transcribed? The download command should not error noisily; it should write the audio + metadata and skip the transcript/summary cleanly. Need to test this against a fresh-uploaded recording.

**Decisions baked in before any code:**

- Folder name uses **UTC** for stability across travel. Counter: humans recognize their local time better. Stability wins; both timestamps land in `metadata.json`.
- Audio format defaults to `mp3` (passthrough of what Plaud serves). Other formats require optional `ffmpeg`.
- Idempotency uses server-claimed `file_md5` first; falls back to size + `Last-Modified` if md5 does not match the bytes (common when Plaud's md5 is over the original upload, not the served container).
- Tokens / signed URLs never log. Same constraint as v0.1 F-09.
