# Notes: Spec 0005: Help and discoverability

Append-only journal. Newest entry on top.

For the convention, see `specs/README.md`.

---

## 2026-05-01: Spec opened (Draft)

Trigger for this spec: v0.1.0 shipped with Cobra's default `--help` output, which is functional but not great for non-developer users. The Norwegian QMS coordinators in the persona list will not Google their way through "Token expired or invalid". They want the CLI itself to tell them what to do.

**Key design decisions baked in:**

- **Topics live in `cmd/plaud/help/*.md` and are embedded via `//go:embed`.** Two reasons. First: editing help is a code change with a test, which keeps drift in check. Second: shipping topic content as separate files makes them trivially discoverable from a code reader's perspective. They are also a good seed for an i18n spec later (0006 or whatever): translators copy the file, swap text, register the new locale.
- **No ANSI colors, no emojis, no box-drawing.** This aligns with the project's "fail fast, fail often" stance: a help system that looks broken on Windows cmd.exe or in `less -R` is worse than a plain one. We can revisit colors in v1.x once we have signal that users want it.
- **The first-run tip is gated on credentials presence**, not on a "first invocation" flag-file. State is the credentials file; we already check it. Adding a separate "have you ever run this before" flag is over-engineering.
- **Error messages get more verbose, on purpose.** "Not logged in. Run `plaud login` first." → "Not logged in. Run `plaud login` to log in via OTP, or `plaud login --token <jwt>` if you use SSO. See `plaud help auth` for details." That is more text than a typical Unix tool emits, but the audience is not typical Unix users. Worth the extra line.

**Things to watch during implementation:**

- Cobra's help template language is finicky. If overriding the root help template gets ugly, consider just a `HelpFunc` that defers to `RunE` of a custom `help` subcommand. We end up with two ways to render help (`--help` flag vs `help` arg); both should land at the same content.
- `--help` flag goes through Cobra's default; `plaud help <topic>` goes through our override. Make sure they don't diverge for command names: `plaud help login` should produce the same output as `plaud login --help`.
- The `unofficial` topic should be one source of truth with `NOTICE`. If they diverge, the `--help` content will outvote the `NOTICE` for users who never read the file. Keep them in sync via test (F-13).
- Shell completion testing on Windows: PowerShell completion has its own shape that differs from bash/zsh/fish. We don't need to validate the script semantically, just assert the recognizable header lines exist for each shell (Cobra's generators are battle-tested).

**Soft tradeoff: scope vs feel.**

This spec is small in code (mostly text + a Cobra subcommand). It has a disproportionate impact on the felt quality of the CLI for non-developer users. Worth doing fully rather than splitting across versions, even though it's tempting to slip individual phases (0, 4, 5) into v0.1.x patch releases.

**Out-of-spec by design:**

- No `plaud doctor` health-check command. Tempting, but most "doctor"-style commands end up being cargo-culted from kubectl / brew without a clear target. If we add one, it should answer a specific question users actually ask, not be a kitchen-sink diagnostic.
- No interactive setup wizard. The audience persona is non-developer, but a TUI introduces a maintenance surface (terminal probing, signal handling, Windows weirdness) that does not pay back on its own.
