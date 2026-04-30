# plaud-cli

> ## ⚠ Unofficial community tool
>
> **plaud-cli is NOT affiliated with, endorsed by, sponsored by, or connected to PLAUD LLC.**
>
> "PLAUD" and "Plaud.ai" are trademarks of their respective owners. This project uses these names solely to identify the third-party service it interoperates with. No claim of ownership, partnership, or endorsement is made or implied.
>
> See [`NOTICE`](./NOTICE) for the full statement.

A small, single-binary CLI for archiving recordings, transcripts, and summaries from your Plaud.ai account into local storage.

## Status

**v0.1.0 released (2026-05-01).** Current scope is `login`, `list`, and `logout`. Download, sync, and prompt composition come in later versions. See [`specs/`](./specs/) for the roadmap and active spec.

Releases: https://github.com/simensollie/plaud-cli/releases

## Why

Plaud.ai stores your recordings, transcripts, and summaries in their cloud only. There is no sanctioned way to bulk-export or maintain a local archive. This tool fills that gap by talking to the Plaud web/mobile API with your own credentials and writing your data to disk in a format you control.

The official partner Developer Platform (`docs.plaud.ai`) is for B2B integrations and does not currently expose end-user data; the companion OAuth API is in private beta. Until those land, this tool uses the same endpoints the consumer web app uses. Expect occasional breakage when Plaud changes their API.

## Install

Download the archive for your platform from the [Releases](https://github.com/simensollie/plaud-cli/releases) page, extract, and put `plaud` on your `PATH`.

For example, on Linux amd64:

```bash
curl -fsSL -o plaud.tar.gz https://github.com/simensollie/plaud-cli/releases/download/v0.1.0/plaud-cli_0.1.0_linux_amd64.tar.gz
tar -xzf plaud.tar.gz plaud
mv plaud ~/.local/bin/
plaud --version
```

Or build from source:

```bash
git clone https://github.com/simensollie/plaud-cli
cd plaud-cli
go build ./cmd/plaud
./plaud --version
```

## Usage

```bash
plaud login                      # interactive: region, email, OTP code
plaud login --token <jwt> \      # alternative: paste a JWT from web.plaud.ai's localStorage
            --region eu \        # (use this if your account uses Apple/Google SSO
            --email <addr>       #  and OTP login is unavailable)
plaud list                       # show all recordings on your account, newest first
plaud logout                     # delete stored credentials
```

Where credentials are kept:

- POSIX: `${XDG_CONFIG_HOME:-~/.config}/plaud/credentials.json` (mode `0600`)
- Windows: `%APPDATA%\plaud\credentials.json`

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
