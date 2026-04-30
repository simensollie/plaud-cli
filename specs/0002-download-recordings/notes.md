# Notes: Spec 0002 — Download recordings

Append-only journal. Newest entry on top.

For the convention, see `specs/README.md`.

---

## 2026-05-01 — Spec opened (Draft)

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
