---
name: log-note
description: Append a dated entry to the active spec's notes.md in the plaud-cli repo. Use when the user discovers something non-obvious during implementation - an undocumented API shape, a region quirk, a flaky test root cause, a decision to record. Triggers on phrases like "log a note", "capture this in notes", "add to notes.md", "note this". Repo-specific - only relevant inside plaud-cli.
---

# Log a note (plaud-cli)

`notes.md` is the append-only journal for each spec. It captures facts, decisions, gotchas, dead ends, and links to evidence (fixtures, commits, network captures). Future contributors read this to understand the *why* behind the code.

## When to use

Anything non-obvious that a future contributor would need to know:

- A reverse-engineered API endpoint shape, with the source it came from.
- A platform quirk (Windows path resolution, EU region behavior).
- A decision made during implementation that diverged from the original plan, with the reason.
- A flaky test diagnosed at root cause.
- A dead end you walked down so the next person does not repeat it.
- A link to evidence: a fixture path, a commit, a Plaud forum post, a network capture.

Do **not** log:

- What the code does (the code does that).
- TODOs (open an issue or update the spec).
- General commentary or progress diary entries.

## Steps

### 1. Find the active spec

```bash
ls specs/ | grep -E '^[0-9]{4}-' | sort
```

Pick the one with `Status: Active` in its `spec.md`. If multiple are active, use the one the current branch is targeting.

### 2. Open notes.md and prepend the entry

**Newest entry on top.** Format:

```markdown
## YYYY-MM-DD - short title

What we learned. Why it matters. Link to evidence (a fixture path, a commit, an external URL).
```

Date is today's date in ISO 8601. Title is short (4-8 words). Body is 1-3 short paragraphs - tight, factual, with a pointer to the evidence.

### 3. If the note changes a spec decision, also update the spec

If the note implies the spec is wrong (e.g. "we discovered the EU host actually requires a different OTP shape"), update `spec.md` too: bump `Updated:`, edit the relevant FR or §5, and reference the notes entry from the spec edit's commit message. See the "When the spec is wrong" section of `/CLAUDE.md`.

## Hard rules (from /CLAUDE.md and specs/README.md)

- **Append-only, newest on top.** Do not edit old entries. If a prior note turns out wrong, write a new entry that supersedes it and explain why.
- **No em dashes.**
- **English only.**
- **ISO 8601 dates** in entry headers.
- **Link evidence.** A note that says "the EU endpoint is different" without a capture path or external URL is half a note.

## Example entries

Good:

```markdown
## 2026-05-04 - EU host uses /v3 not /v2 for OTP send

The reverse-engineered prior art (jaisonerick/plaud-cli) hits `/v2/users/sendCode`
on US, but a 2026-05-04 capture against `api-euc1.plaud.ai` returned 404 there
and 200 on `/v3/users/sendCode`. We now branch in `internal/api/auth.go` by
region. Capture saved at `testdata/http/0001/eu-otp-send.json`.
```

Bad (too vague, no evidence):

```markdown
## 2026-05-04 - EU is different

EU works differently from US. Be careful.
```

## Where notes live

`specs/<spec-id>/notes.md`. One file per spec. Notes from spec 0001 do not migrate to spec 0002 - if a 0001 fact is still load-bearing for 0002, the 0002 notes can link back.
