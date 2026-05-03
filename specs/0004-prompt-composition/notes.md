# Notes: Spec 0004: Prompt composition

Append-only journal. Newest entry on top.

For the convention, see `specs/README.md`.

---

## 2026-05-01: Spec opened (Draft)

This is the spec the user asked for in the very first design conversation: "I don't want to integrate any LLMs. I want to specify a prompt directly in CLI command or refer to a prompt file." The spec preserves that decision and codifies it in F-12 (no LLM API calls anywhere).

**Key architectural decisions baked in:**

- **`plaud prompt` requires a locally-cached transcript.** Auto-fetching makes the network behavior of a "prompt rendering" command surprising. Users explicitly run `plaud download` or `plaud sync` first. Documented as Q6 in the spec; recommendation: keep strict.
- **`plaud save` is the pipe-back helper.** Without it, users would have to hand-construct paths into the archive structure, which makes the whole pipeline ugly. With it, the canonical workflow is one shell line.
- **No built-in templates compiled into the binary.** Examples live in `/examples/prompts/` in the repo. Lower binary size, easier for users to fork their own variants, no version-skew between binary and recommended templates.
- **Filename guard on canonical archive files.** `plaud save <id> --as audio.mp3` is rejected; we don't want LLM output silently clobbering the source-of-truth files. Convention encouraged: `summary.<template>.md`, `notes.<topic>.md`.

**Things to watch during implementation:**

- Multi-speaker transcripts vary in shape across recordings. The fixture under `internal/prompt/testdata/fixtures/sample-recording/` should include both single-speaker and multi-speaker examples so the speaker-prefixing logic gets tested both ways.
- `text/template`'s `option("missingkey=error")` would catch typos in placeholder names (e.g. `{{.Trnscript}}`). Worth turning on by default (typos in prompt files are exactly the kind of thing that quietly produces bad output otherwise).
- Norwegian content. Templates need to render correctly with `æ ø å` characters. Add a Norwegian fixture to the test data.

**Soft dependency on spec 0002:**

This spec cannot move to `Active` until spec 0002 is at least Done; we need transcripts in the canonical archive layout to compose against. We can `Draft` it in parallel with 0002's implementation, but failing tests in Phase 0 will need real transcript fixtures that match what 0002 actually produces.
