# `plaud download`

Fetch one or more recordings into a structured local archive folder. Default contents: transcript (canonical JSON plus rendered markdown), Plaud's own summary, and a `metadata.json` bookkeeping file. Audio is opt-in.

## Synopsis

```
plaud download <id> [<id>...]
                [--out DIR]
                [--include audio,transcript,summary,metadata]
                [--exclude audio]
                [--transcript-format json,md,srt,vtt,txt]
                [--audio-format mp3]
                [--concurrency N]
                [--force]
                [--format json]
```

At least one positional argument is required. Each is either a 32-hex recording ID or a case-insensitive prefix of the recording's title.

## What you get

Per-recording folder under the archive root:

```
<root>/2026/04/2026-04-30_1430_kickoff_mote/
├── audio.mp3            # only when 'audio' is in the include set
├── transcript.json      # canonical, always written when transcript is included
├── transcript.md        # rendered from transcript.json
├── transcript.srt       # opt-in via --transcript-format
├── summary.plaud.md     # Plaud's own summary, written verbatim
└── metadata.json        # always written when any other artifact lands
```

The `YYYY/MM/<folder>` hierarchy uses **UTC**, derived from the recording's start time. `metadata.json` carries both UTC and the original local time. The default include set is `transcript,summary,metadata`. Audio bytes dominate disk usage and most workflows consume the transcript instead, so audio is opt-in via `--include audio`.

## Identifying recordings

Two ways to name a recording:

- **Direct ID.** Anything matching `^[0-9a-f]{32}$` is treated as a recording ID and resolved without listing.
- **Title prefix.** Anything else is treated as a case-insensitive prefix of the recording's title. The CLI calls `list` once and matches against `Filename`. A unique prefix wins; an ambiguous prefix prints the candidate list and exits non-zero; no match exits non-zero with the input echoed.

```bash
# By direct ID
plaud download a3f9c0210000000000000000000000ab

# By title prefix
plaud download "kickoff mote"
```

Trashed recordings (`is_trash=true` on the server) are reachable only by direct ID. Prefix resolution does not surface them. When you download a trashed recording by ID, stderr carries a one-line warning identifying the trashed state; the download proceeds.

## Selecting artifacts

`--include` and `--exclude` accept any subset of `audio,transcript,summary,metadata`. Passing both is an error.

| Flag | Behavior |
|---|---|
| `--include audio,transcript,summary,metadata` | Replaces the default. |
| `--exclude audio` | Subtracts from the full set. |
| `--transcript-format json,md,srt,vtt,txt` | Multiple per run. Replaces the default `json,md` (does not merge). |
| `--audio-format mp3` | Singular. Other formats require `ffmpeg` on `PATH`. |

`transcript.json` is always written when `transcript` is in the include set, regardless of format selection (it is the canonical source for the rendered formats).

Resolution precedence (CLI flag wins over env var wins over built-in default):

| Setting | CLI flag | Env var | Default |
|---|---|---|---|
| Include set | `--include` / `--exclude` | `PLAUD_DEFAULT_INCLUDE` | `transcript,summary,metadata` |
| Transcript formats | `--transcript-format` | `PLAUD_DEFAULT_TRANSCRIPT_FORMAT` | `json,md` |
| Archive root | `--out DIR` | `PLAUD_ARCHIVE_DIR` | `~/PlaudArchive` (POSIX) / `%USERPROFILE%\PlaudArchive` (Windows) |

`--audio-format` while `audio` is excluded is an error. `--transcript-format` while `transcript` is excluded is an error.

## Output location

The archive root is created on first use. On POSIX the default is `~/PlaudArchive`; on Windows `%USERPROFILE%\PlaudArchive`. Override per invocation with `--out DIR`, or set `PLAUD_ARCHIVE_DIR` in your shell rc.

The `YYYY/MM/<folder>` hierarchy still applies under any chosen root. Folder name is `YYYY-MM-DD_HHMM_<slug>` in UTC. The slug is folded from the recording title in this order:

1. Strip a trailing audio extension (`.mp3 .m4a .wav .aac .flac .ogg`).
2. Apply Norwegian folding: `æ → ae`, `ø → o`, `å → a`.
3. NFKD-decompose remaining combining marks.
4. Lowercase. Non-word characters become `_`. Runs of `_` collapse to one.
5. Cap at 60 characters post-fold (truncate at a `_` boundary inside the last 10 chars when one is available, else hard-cut).
6. Empty slug falls back to `untitled`.
7. On collision (same year, month, timestamp, and slug), a 6-char ID suffix is appended.

For example, a recording titled "Kickoff møte" recorded at 14:30 UTC on 2026-04-30 lands at `<root>/2026/04/2026-04-30_1430_kickoff_mote/`.

## Idempotency and `--force`

Re-runs are cheap. Each artifact is checked separately:

- **Audio.** A HEAD request against the signed audio URL returns S3's `ETag` (the MD5 of the served bytes for single-part uploads). If that matches `metadata.audio.s3_etag`, the byte GET is skipped.
- **Transcript.** The detail endpoint is always called. The SHA-256 of the canonical segment array is compared to `metadata.transcript.sha256`. If equal, `transcript.json` is not rewritten and derived files are not regenerated.
- **Summary.** Same pattern as transcript, against `metadata.summary.sha256`.
- **Metadata.** Always rewritten. `last_verified_at` bumps on every successful run; `fetched_at` bumps only when an artifact write actually occurred.

`--force` bypasses every per-artifact check, re-fetches audio bytes, rewrites canonical files even when hashes match, regenerates derived files, and bumps both `fetched_at` and `last_verified_at`. It does not delete artifacts outside the include set. Use it for suspected local corruption or a verifiable fresh round-trip; for most re-runs the default idempotency is what you want.

## Concurrency

`--concurrency N` sets how many recordings are fetched in parallel. Default is `4`, clamped to `[1, 16]`. Out-of-range values are rejected before any work starts. The flag meters recordings, not HTTP requests; within a single recording the worker fetches detail, signed URL, and artifacts serially.

## JSON output

`--format json` emits one JSON object per recording on stdout when its processing completes:

```
{"id":"a3f9c0210000000000000000000000ab","status":"fetched","files":["metadata.json","summary.plaud.md","transcript.json","transcript.md"],"duration_ms":2341}
```

Status is `fetched`, `skipped`, or `failed`. The `error` key is present only when status is `failed`. Stderr remains plain English regardless of `--format` (warnings, partial-state notices, and per-recording error lines all go to stderr).

The schema is documented in `--help` and is **not** stability-committed before v1.0. Don't pipe it into a long-lived ETL job until v1.0 lands.

## Common errors

| Error | Cause | Fix |
|---|---|---|
| `Not logged in. Run \`plaud login\` first.` | No credentials file. | Run `plaud login`. |
| `Token expired or invalid. Run \`plaud login\` again.` | 401 from any API call. | Re-run `plaud login`. The CLI does not retry. A 401 mid-run cancels in-flight workers and drops queued IDs. |
| `no recording matched "<prefix>"` | Title prefix did not match anything in the active list. | Run `plaud list` to confirm the title; trashed recordings need a direct ID. |
| `ambiguous prefix "<p>" matched N recordings` | Prefix resolves to two or more titles. | Lengthen the prefix or pass the 32-hex ID directly. |
| `ffmpeg not found on PATH; --audio-format <fmt> requires ffmpeg. Falling back to mp3.` | Non-mp3 audio format requested without `ffmpeg`. | Install `ffmpeg`, or accept the mp3 fallback. |
| `<id>: transcript not yet ready, skipped` | Server has `is_trans=false` for the recording. | Wait for Plaud to finish processing, then re-run. The other requested artifacts still land. |
| `<id>: summary not yet ready, skipped` | Same as above for summary. | Same. |

See [`troubleshooting.md`](../troubleshooting.md) for deeper recovery steps on auth and region issues.

## Examples

One recording, defaults (transcript, summary, metadata):

```bash
plaud download a3f9c0210000000000000000000000ab
```

By title prefix:

```bash
plaud download "kickoff mote"
```

Transcript only, SRT format:

```bash
plaud download a3f9c0210000000000000000000000ab \
  --include transcript --transcript-format srt
```

All artifacts, including audio:

```bash
plaud download a3f9c0210000000000000000000000ab \
  --include audio,transcript,summary,metadata
```

Several recordings in parallel (default concurrency 4):

```bash
plaud download a3f9c0210000000000000000000000ab \
                b71c0e5300000000000000000000abcd \
                c4d8e7c100000000000000000000ef01
```

JSON output piped to `jq`:

```bash
plaud download --format json a3f9c0210000000000000000000000ab \
  | jq -r '"\(.id)\t\(.status)\t\(.duration_ms)ms"'
```

Default to transcript-only across all invocations (drop audio AND summary by setting the include default in your shell rc):

```bash
# ~/.zshrc or ~/.bashrc
export PLAUD_DEFAULT_INCLUDE=transcript,metadata
export PLAUD_ARCHIVE_DIR=~/work/plaud
```

## Related

- [`list.md`](./list.md), find the IDs and titles to pass here.
- [`login.md`](./login.md), required before downloading.
- [`troubleshooting.md`](../troubleshooting.md), auth, region, and encoding issues.
