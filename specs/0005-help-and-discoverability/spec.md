# Spec 0005: Help and discoverability

**Status:** Draft
**Created:** 2026-05-01
**Updated:** 2026-05-01
**Owner:** @simensollie
**Target version:** v0.2

Make the CLI self-explanatory for non-developer users, especially first-timers and Norwegian QMS coordinators on locked-down Windows machines who will not Google their way through cryptic error text.

The command surface stays small. The wins are in `--help` content, topic-level help, contextual error hints, and shell completion. None of this is novel; the spec exists so the help content is treated as a product surface with acceptance criteria, not as drive-by edits.

---

## 1. Goal

A new user, with the binary downloaded but no other context, can:

1. Run `plaud --help` and figure out what to do next.
2. Hit any error condition and see exactly which subcommand or topic to consult to recover.
3. Install shell completion in one command on bash, zsh, fish, or PowerShell.

A confused user can run `plaud help <topic>` for any of: `auth`, `install`, `troubleshooting`, `privacy`, `unofficial`, `completion`. Each topic is one screenful of plain prose, no marketing.

## 2. Commands / interfaces

| Command | Behavior |
|---|---|
| `plaud --help`, `plaud -h`, `plaud help` | Cobra default; lists commands plus topic-help under a "Topics" section. |
| `plaud help <command>` | Cobra default; long help for one command. |
| `plaud help <topic>` | New. Topic help: `auth`, `install`, `troubleshooting`, `privacy`, `unofficial`, `completion`. |
| `plaud completion bash\|zsh\|fish\|powershell` | Cobra builtin. Prints the completion script to stdout for the chosen shell. |

No new top-level commands beyond `help` (already implicit) and `completion`. The command surface stays at: `login`, `list`, `logout`, `help`, `completion`.

## 3. Functional requirements

| ID | Requirement | Priority |
|---|---|---|
| F-01 | Every command (`login`, `list`, `logout`) has a `Long` description containing: a one-line purpose, a 2-3 line elaboration, an "Examples:" block with at least two real invocations, and a "See also:" line pointing at related topics or commands. | Must |
| F-02 | `plaud help <topic>` works for these topics, each one-screen of plain prose: `auth`, `install`, `troubleshooting`, `privacy`, `unofficial`, `completion`. Topic content is part of this spec's acceptance test fixture. | Must |
| F-03 | Topics appear in `plaud --help` output under a "Topics:" section so users discover them. They are not hidden. | Must |
| F-04 | `plaud completion bash\|zsh\|fish\|powershell` prints the corresponding completion script to stdout. Uses Cobra's built-in generators. | Must |
| F-05 | Contextual error hints: when `plaud list` (or any future authenticated command) errors with `ErrNotLoggedIn`, the error message points to `plaud help auth` *and* shows the two paths (interactive OTP vs `--token` paste) in one sentence each. | Must |
| F-06 | Contextual error hints: when `plaud list` errors with `ErrUnauthorized`, the error message points to `plaud help troubleshooting`. | Must |
| F-07 | Contextual error hints: when `plaud login` fails with `ErrPasswordNotSet`, the message includes the canonical "set a password at https://web.plaud.ai" line *and* points to `plaud help auth` for the `--token` workaround. | Must |
| F-08 | Examples in any help text NEVER contain real-looking tokens. Use literal placeholders `<jwt>` or `eyJhbGc...REPLACE_WITH_YOUR_JWT...QwUk` so users do not accidentally paste an example into a shell. | Must |
| F-09 | Examples in any help text NEVER contain real customer-facing email addresses. Use `you@example.com` or, for Norwegian context, `<your-email>@example.no`. | Must |
| F-10 | Help output is ASCII by default. No emojis, no box-drawing, no ANSI color in `--help`. (Color may come in v1.x; out of scope here.) | Must |
| F-11 | Help text is English in v0.2. Norwegian translations (`--lang nb` or `LANG=nb_NO`) are scoped to a future spec. The text is structured so a translator can swap blocks without restructuring. | Should |
| F-12 | `plaud help completion` mirrors the content of the `plaud completion --help` page so users do not need to know the exact command name. | Should |
| F-13 | `plaud help unofficial` reproduces the contents of `NOTICE` verbatim (or close enough that a `diff` between them is one block, not many). | Should |

## 4. Storage / data model

None. Help content is compiled into the binary as static strings. Topic content lives under `cmd/plaud/help/` as one `*.md` file per topic, embedded via `//go:embed`.

```
cmd/plaud/help/
├── auth.md
├── completion.md
├── install.md
├── privacy.md
├── troubleshooting.md
└── unofficial.md
```

`//go:embed` makes these part of the binary at build time, so updating help text is a code change (with a test) and ships in the next release.

## 5. Tech stack

Unchanged from spec 0001. Cobra has built-in completion generation (`cmd.GenBashCompletion(out)` etc.) and a customizable help template; no new dependencies.

## 6. Out of scope

- **Internationalization.** Norwegian and other languages live in a future spec. v0.2 is English-only.
- **ANSI colors.** Help output is plain ASCII. Some users pipe to `less`, some run on Windows cmd.exe, some use terminals where color is unreadable. Skipping color avoids the entire class of "looks wrong on my machine" issues.
- **Interactive wizards / TUIs.** No `plaud setup` first-run TUI. The CLI stays Unix-pipe-friendly.
- **Man pages.** `cobra/doc.GenManTree` is one call away, but nobody installing v0.2 from a tarball will look at `man plaud`. Defer to v1.0 if traditional Unix users ask.
- **Web-hosted documentation site.** READMEs and embedded help are the source of truth.
- **Automatically opening a browser to docs URLs.** Surprises users; security-sensitive users hate it.
- **Tutorial-style "did you mean?" suggestions.** Cobra has a basic version (`SuggestionsMinimumDistance`) that we can leave at the default; building a smarter one is out of scope.

## 7. Open questions

| # | Question | Recommendation |
|---|---|---|
| 1 | Should `plaud --help` list topic helpers in their own section, or interleave with commands? | Separate "Topics:" section. Cobra supports custom help templates; the cost is one template override. Keeps "what can I run" visually distinct from "what can I read about". |
| 2 | Which topics are in v0.2? Six are proposed (auth, install, troubleshooting, privacy, unofficial, completion). More are tempting (`api-quirks`, `region`, `archive-layout`). | Six is the right floor. Add others when a real user need surfaces. Topics that read like FAQ-padding age badly. |
| 3 | First-run hint: should it print on `plaud list`, or on every non-login/help command? Currently `list` is the only authenticated command; v0.2 (download) adds another. | Print the hint on any authenticated command that hits `ErrNotLoggedIn`. Keeps the rule mechanical: no creds → hint → exit. |
| 4 | `plaud help auth` content: should it walk the Apple/Google SSO localStorage path step-by-step, including DevTools instructions? | Yes for v0.2. The non-developer audience does not know what DevTools is; we name the menu items. Counterargument: this content can drift if Plaud changes their web app. Mitigation: a screen-grab-free, prose-only walkthrough ages slower than one with screenshots. |
| 5 | Norwegian translations: ship in v0.2 or defer? | Defer. Translating six topic files is a meaningful chunk of work and benefits from a native reviewer. v0.2 ships English; translation is its own future spec (call it 0006-i18n). |
| 6 | Should `plaud help` (no topic) suggest the most common next command for a first-time user? | Yes, a one-line tip at the bottom of the default help: `Tip: new here? Run 'plaud help auth' to set up authentication.` Suppress when credentials exist. |

## 8. Acceptance criteria

1. `plaud --help` lists `login`, `list`, `logout`, plus a "Topics:" section listing the six topics. The output fits in 24 lines on an 80-column terminal.
2. `plaud help auth` prints the auth topic in plain ASCII, under one screenful (50 lines), and includes both interactive-OTP and `--token` paths.
3. `plaud help <each-of-the-six-topics>` exits 0 and prints non-empty content. Verified by golden-file tests under `cmd/plaud/help/testdata/`.
4. `plaud help nonexistent-topic` exits non-zero and lists the available topics.
5. `plaud completion bash | head -20` produces output beginning with the bash completion script header. Same for `zsh`, `fish`, `powershell`.
6. With no credentials present, `plaud list` prints an error that contains both the literal text `plaud login` AND `plaud help auth`. Verified by an automated test.
7. With an invalid token, `plaud list` prints an error that contains `plaud help troubleshooting`.
8. `grep -r "eyJ" cmd/plaud/help/` returns no matches: no real-shaped JWT bytes are baked into help content.
9. `grep -rE "@[a-z]+\.(com|no)" cmd/plaud/help/ | grep -v example` returns no matches: no real domains in help content.
10. Acceptance walk-through completes on macOS, Linux, and Windows with the v0.2 release binary.
