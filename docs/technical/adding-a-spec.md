# Adding a spec

The spec workflow itself is documented at [`/specs/README.md`](../../specs/README.md). This page is a quick pointer plus the contributor-facing context that lives alongside it.

## Quick reference

```bash
cp -r specs/_template specs/000N-<slug>
$EDITOR specs/000N-<slug>/spec.md
```

Fill in:

1. **Goal** — one sentence. If you can't reduce it to one sentence, the spec is too big.
2. **Commands / interfaces** — only what the user touches.
3. **Functional requirements** — numbered, prioritized, outcome-shaped.
4. **Out of scope** — generous list. Easier to expand later than to argue about creep mid-implementation.
5. **Acceptance criteria** — concrete, runnable, observable.
6. **§9 Documentation deliverables** — which user/technical docs land alongside the code.

Commit the spec alone (no code), open a PR, get sign-off, then implement.

## Documentation discipline

Per [`/CLAUDE.md`](../../CLAUDE.md), a spec is not Done until `docs/` reflects the change. The template's §9 makes this explicit. Skipped doc rows must be marked "n/a — <one-sentence reason>" rather than left blank.

User-visible behavior (new commands, new errors, install flow changes) → `docs/user/`. Internal architecture (new packages, new patterns, newly-confirmed Plaud API endpoints) → `docs/technical/`.

## State machine

| Status | Meaning |
|---|---|
| `Draft` | Negotiating. Edit freely. |
| `Active` | Implementation in progress. |
| `Done <YYYY-MM-DD>` | Acceptance walked, all phases checked, docs updated. Immutable from here. |
| `Superseded by <id>` | Replaced. |

## Where to look for examples

- [`specs/0001-auth-and-list/`](../../specs/0001-auth-and-list/) — the v0.1.0 spec, in `Active` (released, awaiting cross-platform smoke).
- [`specs/0002-download-recordings/`](../../specs/0002-download-recordings/) — a representative `Draft` for a feature spec.
- [`specs/0005-help-and-discoverability/`](../../specs/0005-help-and-discoverability/) — a `Draft` that is more text-content than code.

If you're unsure about scope, look at how 0001 trimmed itself down from an early draft that included download / sync / prompt composition into a v0.1 that did just login + list + logout. Smaller specs ship.
