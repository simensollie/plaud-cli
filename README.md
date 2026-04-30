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

**v0.1: in development.** Current scope is login + list. Download, sync, and prompt composition come in later versions. See [`specs/`](./specs/) for the roadmap and active spec.

## Why

Plaud.ai stores your recordings, transcripts, and summaries in their cloud only. There is no sanctioned way to bulk-export or maintain a local archive. This tool fills that gap by talking to the Plaud web/mobile API with your own credentials and writing your data to disk in a format you control.

The official partner Developer Platform (`docs.plaud.ai`) is for B2B integrations and does not currently expose end-user data; the companion OAuth API is in private beta. Until those land, this tool uses the same endpoints the consumer web app uses. Expect occasional breakage when Plaud changes their API.

## Install

Not yet released. Once v0.1 ships, binaries will be available on the [Releases](https://github.com/simensollie/plaud-cli/releases) page. For now:

```bash
git clone https://github.com/simensollie/plaud-cli
cd plaud-cli
go build ./cmd/plaud
./plaud --version
```

## Usage (planned for v0.1)

```bash
plaud login          # interactive: region, email, OTP code
plaud list           # show all recordings on your account
plaud logout         # delete stored credentials
```

## Contributing

This repo is spec-driven, test-driven, and "fail fast, fail often". See [`CLAUDE.md`](./CLAUDE.md) for principles, conventions, and the development workflow. See [`specs/README.md`](./specs/README.md) for how to read or write a spec.

In short: no code without a spec, every functional requirement has a test that cites its FR ID, and tests come before implementation.

## License

[MIT](./LICENSE).

## Disclaimer

Use this tool only with your own Plaud.ai account. Using it against another person's account without authorization is unlawful. Running this tool against your own account is data portability under GDPR Article 20.
