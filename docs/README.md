# Documentation

Two audiences, two folders.

## For users

If you want to install plaud-cli and back up your Plaud.ai recordings:

- [`user/install.md`](./user/install.md): install paths (release binary, build from source).
- [`user/getting-started.md`](./user/getting-started.md): first-time setup walkthrough.
- [`user/commands/`](./user/commands/): reference per CLI subcommand.
  - [`login.md`](./user/commands/login.md)
  - [`list.md`](./user/commands/list.md)
  - [`logout.md`](./user/commands/logout.md)
- [`user/troubleshooting.md`](./user/troubleshooting.md): common errors, what they mean, how to recover.

## For contributors

If you want to understand the code, add a feature, or write a new spec:

- [`technical/architecture.md`](./technical/architecture.md): code layout, design principles, package responsibilities.
- [`technical/plaud-api.md`](./technical/plaud-api.md): what we have figured out about the (undocumented) Plaud consumer API.
- [`technical/adding-a-spec.md`](./technical/adding-a-spec.md): pointer to the spec workflow.

The spec library at [`/specs/`](../specs/) is where design decisions live. The contributor guide at [`/CLAUDE.md`](../CLAUDE.md) is where conventions live (TDD, fail-fast, no em dashes, definition of done). Read both before writing code.

## Conventions for docs

- **English by default.** Norwegian translations live in their own future spec.
- **No em dashes.** Use parentheses for asides, commas for natural pauses.
- **No emojis** unless the project explicitly opts in.
- **Examples never contain real tokens or real-domain emails.** Use `<jwt>` and `you@example.com`.
- **One-screen rule for user pages.** If a topic does not fit in 50 lines, split it.
