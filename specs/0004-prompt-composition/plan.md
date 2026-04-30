# Plan: Spec 0004 — Prompt composition

Tracer-bullet sequencing. Phases below are an outline; concrete failing-test names and code paths are filled in once the spec moves to `Status: Active`.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`. This spec depends on spec 0002 being Done (transcripts present in the canonical archive layout).

---

## Phase 0: Variable derivation from cached files

**Outcome:** Pure function: given a recording's local archive folder, return a `PromptContext{Transcript, Title, Date, Duration, Speakers, Tags}` value.

**Failing test stub:**
- `internal/prompt/context_test.go::TestContext_F03_AllVariablesFromArchive`
- `internal/prompt/context_test.go::TestContext_MultiSpeakerTranscriptFormatting`
- `internal/prompt/context_test.go::TestContext_MissingArchiveReturnsErrNoLocalTranscript`

**Code stub:**
- `internal/prompt/context.go` — reads `transcript.json` + `metadata.json`, formats values per spec.md §4 table.

---

## Phase 1: Template rendering

**Outcome:** Apply `text/template` over a prompt body and a `PromptContext`.

**Failing test stub:**
- `internal/prompt/render_test.go::TestRender_F03_VariableSubstitution`
- `internal/prompt/render_test.go::TestRender_F04_NoPlaceholdersAppendsTranscript`
- `internal/prompt/render_test.go::TestRender_StrictMissingFieldsErrors` (catches typos in `{{.Trnscript}}`)

**Code stub:**
- `internal/prompt/render.go` — `Render(body string, ctx PromptContext) (string, error)`.

---

## Phase 2: `plaud prompt` command (single-recording)

**Outcome:** End-to-end CLI command for the single-ID, single-template path.

**Failing test stub:**
- `cmd/plaud/prompt_test.go::TestPrompt_F01_InlineToStdout`
- `cmd/plaud/prompt_test.go::TestPrompt_F02_FromFileToStdout`
- `cmd/plaud/prompt_test.go::TestPrompt_F05_DashOutFile`
- `cmd/plaud/prompt_test.go::TestPrompt_F08_NoTranscriptActionableMessage`
- `cmd/plaud/prompt_test.go::TestPrompt_StdinViaInlineDash`

**Code stub:**
- `cmd/plaud/prompt.go` — Cobra command, options pattern matching `login`/`list`.

---

## Phase 3: `plaud save` command

**Outcome:** Pipe-friendly write-back into a recording's archive folder.

**Failing test stub:**
- `cmd/plaud/save_test.go::TestSave_F09_HappyPath`
- `cmd/plaud/save_test.go::TestSave_F09_IdempotentSameContent`
- `cmd/plaud/save_test.go::TestSave_F09_RefusesOverwriteWithoutForce`
- `cmd/plaud/save_test.go::TestSave_F10_RejectsCanonicalFilenames`
- `cmd/plaud/save_test.go::TestSave_F11_MissingArchiveFolderActionableMessage`
- `cmd/plaud/save_test.go::TestSave_WarnsOnUnusualExtension`

**Code stub:**
- `cmd/plaud/save.go` — reads stdin, validates filename, writes atomically to the recording folder.

---

## Phase 4: Example prompt files

**Outcome:** `/examples/prompts/` contains six files that round-trip through `plaud prompt` against a fixture transcript and produce sensible output.

**Failing test stub:**
- `internal/prompt/examples_test.go::TestExamples_F06_AllRenderAgainstFixture`

**Code stub:**
- `examples/prompts/{meeting,meeting-nb,qms-hearing-nb,audit-interview-nb,decision-log,action-items}.md`.
- `internal/prompt/testdata/fixtures/sample-recording/` (a small synthetic transcript + metadata).

---

## Phase 5: Batch mode

**Outcome:** `plaud prompt --since DATE --file X --batch-out DIR/` walks the local archive and produces one rendered prompt per match.

**Failing test stub:**
- `cmd/plaud/prompt_test.go::TestPrompt_F07_BatchOutWritesPerRecording`
- `cmd/plaud/prompt_test.go::TestPrompt_F07_BatchSkipsMissingTranscriptsWithWarning`
- `cmd/plaud/prompt_test.go::TestPrompt_F07_BatchFiltersByDateAndMatch`

---

## Phase 6: No-network audit

**Outcome:** Test verifies that `plaud prompt` and `plaud save` make zero outbound HTTP calls.

**Failing test stub:**
- `cmd/plaud/prompt_test.go::TestPrompt_F12_NoOutboundHTTP`
- `cmd/plaud/save_test.go::TestSave_F12_NoOutboundHTTP`

**Approach:** override `http.DefaultTransport` in tests with a transport that fails any call; both commands must complete normally.

---

## Acceptance walk-through (final sign-off)

Reproduces `spec.md` §8.

1. `plaud prompt <id> --file ./examples/prompts/meeting.md`: rendered prompt on stdout including title, date, duration, transcript.
2. `plaud prompt <id> --inline "Summarize as decision log."`: bare instruction + transcript.
3. Real pipe: `plaud prompt 12345 --file qms.md | claude -p | plaud save 12345 --as summary.qms.md`. End-to-end works.
4. `plaud save <id> --as audio.mp3` refused; same for the four canonical names.
5. `plaud save <id> --as summary.qms.md` twice with same content: no error. Twice with different content: second fails without `--force`.
6. Batch: `plaud prompt --since 2026-04-01 --file meeting.md --batch-out ./out/` produces one file per cached recording in the date range.
7. No-network audit: both commands run with `--no-network` (test-only flag) and complete.
8. Repeat 1–7 on macOS, Linux, Windows.
