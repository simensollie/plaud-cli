# Plan: Spec 0005: Help and discoverability

Tracer-bullet sequencing. Phases below are an outline; concrete failing-test names and code paths fill in once the spec moves to `Status: Active`.

For coding rules, TDD discipline, and "fail fast" stance, see `/CLAUDE.md`. This spec has no dependencies on other drafts; it can land alongside 0002.

---

## Phase 0: Improve existing command Long descriptions

**Outcome:** `plaud login --help`, `plaud list --help`, `plaud logout --help` each have purpose / elaboration / Examples / See-also.

**Failing test stub:**
- `cmd/plaud/help_text_test.go::TestLogin_F01_HelpHasExamplesSection`
- `cmd/plaud/help_text_test.go::TestList_F01_HelpHasExamplesSection`
- `cmd/plaud/help_text_test.go::TestLogout_F01_HelpHasExamplesSection`
- `cmd/plaud/help_text_test.go::TestF08_NoRealJWTsInHelp`
- `cmd/plaud/help_text_test.go::TestF09_NoRealEmailsInHelp`

**Code stub:**
- Update `cmd/plaud/login.go`, `list.go`, `logout.go` `Long` fields.

---

## Phase 1: Topic-help infrastructure

**Outcome:** `plaud help <topic>` works for a single placeholder topic ("auth"), exits 0, prints content. Lays groundwork for the rest.

**Failing test stub:**
- `cmd/plaud/help_test.go::TestHelp_F02_AuthTopicPrintsContent`
- `cmd/plaud/help_test.go::TestHelp_F02_UnknownTopicListsAvailable`

**Code stub:**
- `cmd/plaud/help/auth.md` (placeholder content; real content in Phase 2)
- `cmd/plaud/help/embed.go`: `//go:embed *.md` + `topics() map[string]string`
- `cmd/plaud/help.go`: Cobra subcommand override that handles topics; falls through to Cobra's default for command names.

---

## Phase 2: Write topic content

**Outcome:** All six topics (`auth`, `install`, `troubleshooting`, `privacy`, `unofficial`, `completion`) have prose content.

**Failing test stub:**
- `cmd/plaud/help_test.go::TestHelp_F02_AllTopicsAreNonEmpty`
- `cmd/plaud/help_test.go::TestHelp_F08_F09_NoSecretsOrRealEmails`
- `cmd/plaud/help_test.go::TestHelp_F13_UnofficialTopicMatchesNOTICE`

**Code stub:**
- `cmd/plaud/help/{auth,install,troubleshooting,privacy,unofficial,completion}.md`

---

## Phase 3: Topics in `plaud --help` listing

**Outcome:** Topics show up under a "Topics:" section in the root command's `--help` output.

**Failing test stub:**
- `cmd/plaud/main_test.go::TestRoot_F03_HelpListsTopicsSection`

**Code stub:**
- Custom Cobra help template on the root command that appends a Topics section.

---

## Phase 4: Shell completion command

**Outcome:** `plaud completion <shell>` prints a working completion script.

**Failing test stub:**
- `cmd/plaud/completion_test.go::TestCompletion_F04_BashScriptHeader`
- `cmd/plaud/completion_test.go::TestCompletion_F04_ZshScriptHeader`
- `cmd/plaud/completion_test.go::TestCompletion_F04_FishScriptHeader`
- `cmd/plaud/completion_test.go::TestCompletion_F04_PowershellScriptHeader`

**Code stub:**
- `cmd/plaud/completion.go`: Cobra builtin via `cmd.GenBashCompletion(stdout)` etc.

---

## Phase 5: Contextual error hints

**Outcome:** Errors from `plaud list` (and any future authenticated command) point to the relevant topic.

**Failing test stub:**
- `cmd/plaud/list_test.go::TestList_F05_NotLoggedInPointsToHelpAuth` (extends existing test)
- `cmd/plaud/list_test.go::TestList_F06_TokenInvalidPointsToHelpTroubleshooting` (extends existing test)
- `cmd/plaud/login_test.go::TestLogin_F07_PasswordNotSetPointsToHelpAuth` (extends existing test)

**Code stub:**
- Adjust the error strings in `cmd/plaud/list.go` and `login.go`. No new files.

---

## Phase 6: First-run tip in `plaud help`

**Outcome:** When credentials are absent, `plaud help` (no args) appends "Tip: new here? Run `plaud help auth` to set up authentication." Suppressed when credentials exist.

**Failing test stub:**
- `cmd/plaud/help_test.go::TestHelp_F06_FirstRunTipShownWhenLoggedOut`
- `cmd/plaud/help_test.go::TestHelp_F06_TipSuppressedWhenLoggedIn`

**Code stub:**
- `cmd/plaud/help.go`: Cobra `HelpFunc` override on root.

---

## Acceptance walk-through (final sign-off)

Reproduces `spec.md` §8.

1. `plaud --help` shows commands + topics, fits 80×24.
2. `plaud help auth` prints in under 50 lines, contains both interactive and `--token` paths, no real JWTs / emails.
3. `plaud help <each topic>` exits 0 and prints non-empty content.
4. `plaud help nonexistent` exits non-zero, lists the available topics.
5. `plaud completion bash | head -1` shows a recognizable bash completion header. Same for zsh / fish / powershell.
6. `rm -rf ~/.config/plaud && plaud list` prints an error containing both `plaud login` and `plaud help auth`.
7. With a manually-corrupted credentials file (token replaced with garbage), `plaud list` prints an error containing `plaud help troubleshooting`.
8. `grep` checks for tokens and real-domain emails in `cmd/plaud/help/` (F-08 / F-09) come back clean.
9. Repeat 1–8 on macOS, Linux, Windows.

When all pass, set `Status: Done <YYYY-MM-DD>` in `spec.md`.
