# Spec NNNN: <short title>

**Status:** Draft
**Created:** YYYY-MM-DD
**Updated:** YYYY-MM-DD
**Owner:** @<github-handle>
**Target version:** vX.Y

One-paragraph summary of what this spec delivers and why now. State what is **not** in this spec in the same paragraph if scope is likely to be misread.

---

## 1. Goal

The single sentence this spec exists to deliver. If you can't reduce it to one sentence, the spec is too big — split it.

## 2. Commands / interfaces

If this spec adds CLI surface, list every command, flag, and observable output here. If it adds an internal API, sketch the function signatures.

| Command | Behavior |
|---|---|
| `plaud <verb>` | What the user types and what they observe. |

If no surface change, write "None — internal change only" and explain in §3 what externally observable difference proves the change works.

## 3. Functional requirements

Outcome-shaped, numbered, prioritized. Each row gets a stable ID (`F-NN` within this spec). Tests cite these IDs.

| ID | Requirement | Priority |
|---|---|---|
| F-01 | <observable behavior the user can verify>. | Must |
| F-02 | ... | Must |
| F-03 | ... | Should |

Priorities: `Must` (spec is not done without it), `Should` (cut only with explicit decision in `notes.md`), `Could` (nice-to-have, defer freely).

## 4. Storage / data model

If this spec changes anything on disk, document the layout. Show the directory tree and the canonical formats. Mark which files are derived (regeneratable) and which are user data (preserved across operations).

If no on-disk change, write "None."

## 5. Tech stack

List dependencies added or changed by this spec. If this spec changes the language, framework, or build tool, justify.

If no change, write "Unchanged from prior specs."

## 6. Out of scope

Explicit and generous. Things one might assume this spec does, but it does not. New specs cover them later.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | <thing the spec can't yet decide> | <best current guess> |

Drive these to closure before `Status: Active`. Or accept them as `TBD` with a date by which they must resolve.

## 8. Acceptance criteria

The walk-through that proves the spec is done. Concrete, runnable, observable. The spec is not `Done` until every step here passes on the target platforms.

1. <step the user runs and what they observe>
2. ...

## 9. Documentation deliverables

Per the project's definition of done (see `/CLAUDE.md`), every spec lands with documentation in the same set of changes as the code.

| Audience | File(s) | What lands |
|---|---|---|
| User | `docs/user/commands/<cmd>.md` for each new or changed command | Purpose, syntax, example invocations, common errors and how to recover. |
| User | `docs/user/troubleshooting.md` | New entries for any user-visible error states this spec introduces. |
| User | `docs/user/getting-started.md` | Update if first-run flow changes. |
| Technical | `docs/technical/architecture.md` | New internal packages, cross-cutting patterns, decisions worth surfacing. |
| Technical | `docs/technical/plaud-api.md` | Any new Plaud API endpoint shapes confirmed by this spec. |

If a row above does not apply, write "n/a — <one-sentence reason>" in this section. Empty deliverables get pushed back at review.
