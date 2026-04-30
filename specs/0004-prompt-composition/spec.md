# Spec 0004: Prompt composition

**Status:** Draft
**Created:** 2026-05-01
**Updated:** 2026-05-01
**Owner:** @simensollie
**Target version:** v0.4

Render a user-supplied prompt template plus a locally-cached transcript into stdout, for the user to pipe into any LLM CLI of their choice (Claude CLI, `llm` by Simon Willison, `ollama run`, paste into a web UI).

This spec deliberately does NOT add LLM provider integrations. The CLI is a prompt composer, not an LLM client. That decision was made jointly with the user during spec-shaping in April 2026.

---

## 1. Goal

A user with locally-cached transcripts (from spec 0002 or 0003) can compose a prompt against any recording or batch of recordings, pipe the output to whatever LLM tool they already have installed, and (optionally) save the LLM's response back into the canonical archive folder for that recording.

## 2. Commands / interfaces

| Command | Behavior |
|---|---|
| `plaud prompt <id> --inline "<text>"` | Compose prompt with inline text + transcript variables, write to stdout. |
| `plaud prompt <id> --file <path>` | Same, body read from file. |
| `plaud prompt <id> --file <path> -o <output>` | Write to file instead of stdout. |
| `plaud prompt --since DATE [--until DATE] [--match REGEX] --file <path> --batch-out DIR/` | Render one prompt file per matching local recording. |
| `plaud save <id> --as <filename>` | Read stdin and write into that recording's archive folder. |
| `plaud save <id> --as <filename> --force` | Overwrite an existing file with the same name. |

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | `plaud prompt <id> --inline "<text>"` reads the local transcript for `<id>`, substitutes variables, writes the rendered prompt to stdout. | Must |
| F-02 | `plaud prompt <id> --file <path>` reads the prompt body from a file, substitutes variables, writes to stdout. | Must |
| F-03 | Prompt body supports Go `text/template` placeholders: `{{.Transcript}}`, `{{.Title}}`, `{{.Date}}`, `{{.Duration}}`, `{{.Speakers}}`, `{{.Tags}}`. | Must |
| F-04 | If the prompt body contains no placeholders at all, the transcript is appended after a blank line. The simplest possible prompt file is plain text instructions. | Must |
| F-05 | `-o <file>` writes to the file instead of stdout. Refuses to overwrite without `--force`. | Should |
| F-06 | Example prompt files ship in `/examples/prompts/` in the **repo** (NOT embedded in the binary): `meeting.md`, `meeting-nb.md`, `qms-hearing-nb.md`, `audit-interview-nb.md`, `decision-log.md`, `action-items.md`. | Should |
| F-07 | Batch: `plaud prompt --since DATE [--until DATE] [--match REGEX] --file <path> --batch-out DIR/` writes one rendered prompt file per matching local recording, named `<YYYY-MM-DD_HHMM_slug>.prompt.md` under `DIR/`. | Could |
| F-08 | Requires the recording's transcript to be locally cached (downloaded by spec 0002 or synced by 0003). On miss, returns `ErrNoLocalTranscript` with the message: "Transcript for recording `<id>` is not in the local archive. Run `plaud download <id>` first." | Must |
| F-09 | `plaud save <id> --as <filename>` reads stdin and writes it into that recording's archive folder. Refuses to overwrite without `--force`. Idempotent on repeat with identical content. | Must |
| F-10 | `plaud save` rejects filenames that would overwrite canonical archive files (`audio.*`, `transcript.*`, `summary.plaud.md`, `metadata.json`). | Must |
| F-11 | `plaud save` requires the target recording's archive folder to exist. On miss, suggests `plaud download <id>` and exits non-zero. | Must |
| F-12 | The CLI never makes any LLM API calls. The only external network calls in this spec are the standard ones from `plaud login`, `plaud list`, and (in batch mode) `plaud sync` if data is missing. | Must |

## 4. Storage / data model

Reads from spec 0002's archive layout. `plaud save` writes into the same layout:

```
2026-04-30_1430_kickoff_meeting/
├── audio.mp3                # untouched
├── transcript.json          # untouched
├── transcript.md            # untouched
├── summary.plaud.md         # untouched
├── metadata.json            # untouched
└── summary.qms.md           # written via `plaud save 12345 --as summary.qms.md`
```

User-saved files use a free-form name (with the canonical-name guard from F-10). Convention encouraged in docs: `summary.<template-name>.md`, `notes.<topic>.md`. Not enforced.

**Variable derivation from cached files:**

| Placeholder | Source |
|---|---|
| `{{.Transcript}}` | Concatenated `text` fields from `transcript.json` segments. Speaker labels prefixed if multi-speaker. |
| `{{.Title}}` | `metadata.json:title`. |
| `{{.Date}}` | `metadata.json:start_time_utc`, formatted ISO 8601 (`2026-04-30 14:30 UTC`). |
| `{{.Duration}}` | `metadata.json:duration_ms`, formatted `HH:MM:SS`. |
| `{{.Speakers}}` | Distinct speaker labels from `transcript.json`, in order of first appearance, comma-joined. |
| `{{.Tags}}` | `metadata.json:tags`, comma-joined. |

## 5. Tech stack

Unchanged. No LLM SDKs. `text/template` is stdlib.

## 6. Out of scope

- **LLM API calls of any kind.** The CLI does not embed Anthropic, OpenAI, Ollama, or any other client. Period.
- **Built-in templates compiled into the binary.** Examples ship in the repo for users to copy and adapt.
- **Server-side template management.** Plaud's web UI has its own template engine; we don't drive it.
- **Streaming pipelines** beyond what `>>` and `|` give you. The CLI emits a complete prompt file or stream and exits.
- **Translation** (Norwegian → English of transcripts before composition). Pipe through a translation step yourself if needed.
- **Token counting / cost estimation.** Without LLM integration, we don't know the model so we can't count tokens accurately. Use your LLM tool's own cost reporting.
- **Speaker re-labeling.** The `{{.Speakers}}` placeholder uses what `transcript.json` already says. Renaming speakers is a different feature.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | Should `--inline -` read the prompt body from stdin? Useful for `cat my-prompt.md \| plaud prompt 123 --inline -`. | Yes, support `-` as the conventional stdin sentinel. Cheap, idiomatic. |
| 2 | Should `{{.Transcript}}` include timestamps inline or just plain text? | Plain text by default; offer `{{.TranscriptWithTimestamps}}` as an alternative placeholder if users ask. Adding both up front is over-design. |
| 3 | Multi-speaker transcripts: how are speakers prefixed in `{{.Transcript}}`? | `Speaker: <name or label>\n<text>\n\n`-style blocks. Match the conventions a human would use when typing notes. |
| 4 | `plaud save` filename validation: how strict? | Reject canonical filenames (F-10); allow anything else within the recording folder; warn (not fail) if the file extension differs from `.md` or `.txt` (LLM output is usually one of those). |
| 5 | Batch mode (`--batch-out`) when some recordings are missing transcripts: skip with warning, or fail the run? | Skip with warning per missing recording; final exit code non-zero only if every recording was missing. Matches spec 0002's per-recording-error policy. |
| 6 | Does `plaud prompt` ever fetch a missing transcript on demand (via spec 0002's machinery), or strictly require local cache? | Strictly require local cache. Auto-fetch makes the command's network behavior surprising. Users explicitly run `plaud download` or `plaud sync` first. |

## 8. Acceptance criteria

1. `echo > /tmp/p.md "Summarize:"; plaud prompt <id> --file /tmp/p.md` outputs `"Summarize:"` followed by the transcript on stdout.
2. `plaud prompt <id> --file <path>` with a body containing `{{.Title}}`, `{{.Date}}`, `{{.Duration}}`, and `{{.Transcript}}` substitutes all four placeholders correctly.
3. `plaud prompt <id> --inline "..."` against a recording whose transcript is NOT locally cached exits non-zero with the spec's `ErrNoLocalTranscript` message and a clear "run `plaud download <id>` first" hint.
4. `plaud prompt 12345 --file qms.md | claude -p | plaud save 12345 --as summary.qms.md` works end-to-end: rendered prompt streams into `claude`, its output streams into `plaud save`, and `summary.qms.md` lands in the recording folder.
5. `plaud save <id> --as audio.mp3` rejects with "refusing to overwrite canonical archive file"; same for `transcript.json`, `summary.plaud.md`, `metadata.json`.
6. `plaud save <id> --as summary.qms.md` twice with the same content is idempotent (second run is a no-op or harmless rewrite); twice with *different* content fails the second run unless `--force`.
7. Batch: `plaud prompt --since 2026-04-01 --file meeting.md --batch-out ./out/` produces one `*.prompt.md` per matching cached recording. Recordings without transcripts are reported and skipped.
8. The CLI makes zero outbound HTTP calls during `plaud prompt` and `plaud save`. Verifiable via `strace` / `dtruss` / Wireshark.
9. Acceptance completes on macOS, Linux, and Windows.
