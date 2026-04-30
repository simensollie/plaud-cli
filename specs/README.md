# Specs

This directory holds the spec-driven, outcome-based design documents that drive everything in this repo. If it isn't in a spec, it isn't getting built.

For coding conventions, TDD discipline, and the "fail fast, fail often" stance, see the root `CLAUDE.md`. This file is about the spec workflow itself.

---

## Folder shape

```
specs/
├── README.md                  # this file
├── _template/                 # copy this when starting a new spec
│   ├── spec.md
│   ├── plan.md
│   └── notes.md
├── 0001-auth-and-list/        # one folder per spec
│   ├── spec.md                # the outcomes, FRs, acceptance
│   ├── plan.md                # tracer-bullet phases, TDD-friendly
│   └── notes.md               # captured facts, decisions, gotchas
├── 0002-<slug>/
└── ...
```

**Numbering.** Four-digit, monotonic, never re-used. The number is the stable handle for cross-references in commits, tests, and PR titles. The slug is human-friendly and may be renamed.

**`_template/`.** Copy it: `cp -r specs/_template specs/000N-<slug>`, then fill in.

---

## File roles

### `spec.md` — the *what*

Outcome-based. Describes what the user can do and observe when this spec is done. Stable surface; treat edits as design changes that need justification.

Required sections (the template enforces them):

1. **Goal** (one paragraph). The single sentence the spec exists to deliver.
2. **Commands** or interfaces, if it adds CLI surface.
3. **Functional requirements** as a numbered table with priorities. Each row gets a stable ID like `F-NN` (within the spec) or `F-0001-NN` (cross-spec). Tests must cite these IDs.
4. **Storage / data model**, if any.
5. **Tech stack** added or changed by this spec.
6. **Out of scope.** What this spec is *not* doing, even though one might assume it would.
7. **Open questions.** Things the spec can't yet decide. Track until resolved or cut.
8. **Acceptance criteria.** The walk-through that proves the spec is done.

### `plan.md` — the *how, in phases*

Tracer-bullet sequencing. Each phase is the smallest end-to-end slice that delivers user-visible value, with the failing test that drives it.

Format per phase:

```markdown
## Phase N: <short name>

**Outcome:** what the user can do after this phase.

**Failing test first (red):**
- `internal/foo/foo_test.go::TestF01_<thing>`

**Code (green):**
- `internal/foo/foo.go`
- `cmd/plaud/<cmd>.go`

**Done when:**
- [ ] Test in red, then green
- [ ] `go test ./... -race` clean
- [ ] Manual smoke: `<exact command>` produces `<exact observable>`
```

Plans are working documents. Edit freely as you learn.

### `notes.md` — the *what we found out*

Append-only journal. Captured facts, decisions made during implementation, undocumented API behavior, dead ends, links to network captures, anything a future contributor needs to understand the choices behind the code.

Format:

```markdown
## YYYY-MM-DD — short title

What we learned. Why it matters. Link to evidence (a fixture path, a commit, an external URL).
```

Newest entry on top.

---

## Lifecycle

A spec moves through four states (set the `Status:` field at the top of `spec.md`):

| Status | Meaning | Mutability |
|---|---|---|
| `Draft` | Being negotiated. Edit freely. | Fully mutable. |
| `Active` | Implementation in progress. | Mutable, but every edit bumps `Updated:` and is justified in `notes.md`. Breaking changes that invalidate already-passing tests are large enough to warrant a stop-and-discuss. |
| `Done <YYYY-MM-DD>` | Acceptance criteria walked, all phases checked. | Immutable. New work goes in a new spec. |
| `Superseded by <spec id>` | Replaced by a newer spec. | Immutable. Pointer at the top of `spec.md` to the replacement. |

There is no `Cancelled` state. If a spec is abandoned, mark it `Superseded by <none>` and explain in `notes.md`.

---

## How to start a new spec

```bash
# from repo root
cp -r specs/_template specs/000N-<slug>
$EDITOR specs/000N-<slug>/spec.md
```

Fill in:

1. Goal — the one sentence.
2. Commands / interfaces — only what the user touches.
3. Functional requirements — numbered, prioritized, outcome-shaped.
4. Out of scope — explicit, generous. Easier to expand later than to argue about creep now.
5. Acceptance criteria — what you'll check at the end.

Leave `plan.md` and `notes.md` mostly empty until you start implementing.

Open a PR with the spec only (no code). Get sign-off. Then implement.

---

## Cross-references

- **In commits:** "F-01, F-02: <subject>" with body line "Spec: specs/0001-auth-and-list/".
- **In tests:** function or subtest name cites the FR ID. Example: `TestLogin_F02_OTPExchangesCodeForToken` or `t.Run("F-02: OTP exchanges code for bearer token", ...)`.
- **In code comments:** rare, but acceptable when the *why* is anchored in a spec decision: `// F-08: 401 means re-login, no retry. spec 0001.`

---

## Anti-patterns

- **Implementation details in the spec.** "Use Cobra" belongs in `plan.md` or in the spec's own "Tech stack" section, not in a functional requirement.
- **Bullet-point feature lists posing as a spec.** A spec without acceptance criteria isn't a spec.
- **Editing a Done spec.** Open a new spec and let the old one stand as historical record.
- **Specs that include things "we'll probably need eventually".** Out of scope, every time. New spec when you actually need it.
- **Plans without failing tests.** A phase that says "implement X" without naming the failing test that proves X is wishful thinking, not a plan.

---

## Index

| ID | Title | Status | Target version |
|---|---|---|---|
| 0001 | Authentication and List | Active (v0.1.0 released 2026-05-01; cross-platform + OTP smoke pending) | v0.1 |
| 0002 | Download recordings | Draft | v0.2 |
| 0003 | Sync | Draft (depends on 0002) | v0.3 |
| 0004 | Prompt composition | Draft (depends on 0002) | v0.4 |
| 0005 | Help and discoverability | Draft | v0.2 |
