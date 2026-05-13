# plaud-cli

> ## 🛑 Superseded by official Plaud tooling
>
> Plaud now ships both an **official CLI** and an **official MCP server** that cover almost everything this project does. **New users should start there.** This repo stays publicly available, but is in maintenance mode (see below).
>
> **Official CLI** ([docs](https://docs.plaud.ai/documentation/plaud_app/cli)):
>
> ```bash
> npm install -g @plaud-ai/cli   # requires Node.js >= 20
> ```
>
> Commands: `plaud login` / `logout` / `me`, `plaud files` / `recent` / `today` / `search`, `plaud file <id>`, `plaud audio <id>`, `plaud transcript <id> -o`, `plaud summary <id> -o`.
>
> **Official MCP server** ([docs](https://docs.plaud.ai/documentation/plaud_app/mcp)) for Claude Code, Claude Desktop, Cursor, VS Code, Zed, ChatGPT, etc.:
>
> ```bash
> npx -y @plaud-ai/mcp@latest install
> ```
>
> **⚠ Binary name collision.** Plaud's official CLI installs a binary called `plaud`, the same name this project uses. Do not install both on the same `PATH`. If you are migrating, `rm` (or rename) this project's binary first.
>
> ### Command mapping
>
> | This project | Official replacement |
> |---|---|
> | `plaud login` | `plaud login` (official) |
> | `plaud logout` | `plaud logout` (official) |
> | `plaud list` | `plaud files` (or `recent`, `today`, `search`) |
> | `plaud download <id>` | `plaud transcript <id> -o`, `plaud summary <id> -o`, `plaud audio <id>` |
> | `plaud sync` | **No official equivalent yet.** |
>
> ### The one niche this tool still covers
>
> The official CLI deliberately omits bulk operations: no `sync`, no `--watch`, no prune, no on-disk archive layout, and `search` is capped at the most recent ~500 recordings. Audio is served via 24-hour presigned URLs, so building a long-lived offline archive against the official CLI means scripting your own downloader on top of it.
>
> If you need a **scheduled, file-on-disk archive** of every recording (audio + transcript + summary) in a stable `YYYY/MM/<slug>/` layout (e.g. for GDPR data portability, compliance retention, or feeding a non-MCP pipeline), `plaud sync` is still the simplest path. For everything else, switch to the official tools.
>
> ### Maintenance posture
>
> - The repo stays public and the binaries stay downloadable.
> - **No new features.** v0.3.0 acceptance will land; no new specs will be opened.
> - Security fixes and Plaud-API breakage fixes will be considered on a best-effort basis.
> - Issues and PRs may be triaged slowly. If your problem is something the [official CLI](https://docs.plaud.ai/documentation/plaud_app/cli) or [MCP](https://docs.plaud.ai/documentation/plaud_app/mcp) can solve, please use those instead.

> ## ⚠ Unofficial community tool
>
> **plaud-cli is NOT affiliated with, endorsed by, sponsored by, or connected to PLAUD LLC.**
>
> "PLAUD" and "Plaud.ai" are trademarks of their respective owners. This project uses these names solely to identify the third-party service it interoperates with. No claim of ownership, partnership, or endorsement is made or implied.
>
> See [`NOTICE`](./NOTICE) for the full statement.

A small, single-binary CLI for archiving recordings, transcripts, and summaries from your Plaud.ai account into local storage.

## Status

| Version | Scope | State |
|---|---|---|
| **v0.1.0** | `login`, `list`, `logout` | Released (2026-05-01) |
| **v0.2.0** | `download` (transcripts, summaries, audio per recording) | Released |
| **v0.3.0** | `sync` (mirror your whole account, watch mode, prune) | Implemented in `main`; pending acceptance walk |
| **Project** | Further development | **Maintenance mode.** Superseded by the [official Plaud CLI](https://docs.plaud.ai/documentation/plaud_app/cli) and [MCP server](https://docs.plaud.ai/documentation/plaud_app/mcp). |

Releases: https://github.com/simensollie/plaud-cli/releases

The roadmap and active specs live in [`specs/`](./specs/). No new specs will be opened.

## Why (historical)

Plaud.ai used to store your recordings, transcripts, and summaries in their cloud only. There was no sanctioned way to bulk-export or maintain a local archive. This tool filled that gap by talking to the Plaud web/mobile API with your own credentials and writing your data to disk in a format you control.

**That gap is now largely closed.** Plaud's [official MCP server](https://docs.plaud.ai/documentation/plaud_app/mcp) exposes recordings, transcripts, AI-notes, and presigned audio URLs to any MCP-compatible client. If you want conversational access to your Plaud data from Claude Code, Cursor, ChatGPT, etc., use that.

This tool remains useful only if you need a **file-on-disk archive** of every recording (audio + transcript + summary) in a deterministic directory layout, refreshed on a schedule, without a running AI client. Plaud's MCP does not currently offer that.

## Get started

Two ways to install. Pick the one that fits.

### Option 1: Download the release binary (recommended)

`plaud` is a single static binary. No runtime dependencies, no installer, no admin rights needed. Pick your platform below; replace `v0.2.0` with the tag from the [Releases page](https://github.com/simensollie/plaud-cli/releases) if a newer one is available.

**macOS** (Apple Silicon):

```bash
curl -fsSL -o plaud.tar.gz \
  https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_darwin_arm64.tar.gz
tar -xzf plaud.tar.gz plaud
mkdir -p ~/.local/bin && mv plaud ~/.local/bin/
plaud --version
```

For Intel Macs, swap `darwin_arm64` for `darwin_amd64`. If `~/.local/bin` is not on your `PATH`, add it to `~/.zshrc` or `~/.bashrc`.

**Linux** (x86_64):

```bash
curl -fsSL -o plaud.tar.gz \
  https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_linux_amd64.tar.gz
tar -xzf plaud.tar.gz plaud
mkdir -p ~/.local/bin && mv plaud ~/.local/bin/
plaud --version
```

For ARM (Raspberry Pi etc.), swap `linux_amd64` for `linux_arm64`.

**Windows** (PowerShell):

```powershell
$tag = "v0.2.0"
$arch = "amd64"   # or "arm64"
$url  = "https://github.com/simensollie/plaud-cli/releases/download/$tag/plaud-cli_0.2.0_windows_$arch.zip"
Invoke-WebRequest -Uri $url -OutFile plaud.zip
Expand-Archive plaud.zip -DestinationPath .
.\plaud.exe --version
```

Move `plaud.exe` somewhere on your `PATH`. The simplest spot for a per-user install is `%LOCALAPPDATA%\Programs\plaud\`.

**Verify the download** (optional but recommended):

```bash
curl -fsSL -O https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

`OK` next to your archive's filename means the bytes match what the release pipeline produced.

### Option 2: Build from source

You need [Go 1.23 or newer](https://go.dev/dl/).

```bash
git clone https://github.com/simensollie/plaud-cli
cd plaud-cli
go build -o plaud ./cmd/plaud
./plaud --version
```

A source build prints `plaud version 0.x.y-dev` (the development string) instead of the release version. Functionally identical.

For local development, the same toolchain runs the test suite:

```bash
go test -race ./...
go vet ./...
gofmt -l .
```

See [`CLAUDE.md`](./CLAUDE.md) for contributor conventions and the spec-driven workflow.

## First commands

Once `plaud --version` works, log in and try a few commands:

```bash
plaud login                        # interactive: region (us|eu|jp), email, OTP code
plaud list                         # show all recordings on your account, newest first
plaud download <id>                # fetch one recording into ~/PlaudArchive (text by default)
plaud sync                         # mirror every recording on the account
plaud sync --watch --interval 15m  # poll every 15 minutes (foreground; one terminal session)
plaud logout                       # delete stored credentials
```

For Apple/Google SSO accounts (no password set), use the token-paste path:

```bash
plaud login --token <jwt> --region eu --email you@example.com
```

The full walkthrough — including how to grab the JWT from `web.plaud.ai`'s `localStorage` — is in [`docs/user/getting-started.md`](./docs/user/getting-started.md).

## Where things live

| Item | Path |
|---|---|
| Credentials | `${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json` (POSIX, mode `0600`) <br> `%APPDATA%\plaud\credentials.json` (Windows) |
| Archive root | `~/PlaudArchive/YYYY/MM/<slug>/` (POSIX) <br> `%USERPROFILE%\PlaudArchive\...` (Windows) |
| Sync state file | `<archive_root>/.plaud-sync.state` |

Override the archive root with `--out DIR` per invocation, or `PLAUD_ARCHIVE_DIR` in your shell rc.

## Documentation

- [`docs/user/`](./docs/user/) — install, getting started, command reference, troubleshooting.
- [`docs/technical/`](./docs/technical/) — architecture, the (undocumented) Plaud API knowledge, contributor pointers.
- [`docs/README.md`](./docs/README.md) — the docs index.

## Contributing

This repo is spec-driven, test-driven, and "fail fast, fail often". See [`CLAUDE.md`](./CLAUDE.md) for principles, conventions, and the development workflow. See [`specs/README.md`](./specs/README.md) for how to read or write a spec.

In short: no code without a spec, every functional requirement has a test that cites its FR ID, tests come before implementation, and documentation in `docs/` ships in the same change as the code.

## License

[MIT](./LICENSE).

## Disclaimer

Use this tool only with your own Plaud.ai account. Using it against another person's account without authorization is unlawful. Running this tool against your own account is data portability under GDPR Article 20.
