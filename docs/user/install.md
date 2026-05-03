# Install

plaud-cli is a single static binary. No runtime dependencies. Install in one of two ways.

## From a release binary (recommended)

Download the archive for your platform from the [Releases page](https://github.com/simensollie/plaud-cli/releases), extract, and put the `plaud` binary on your `PATH`.

### macOS

```bash
# Apple Silicon (M1/M2/M3/M4)
curl -fsSL -o plaud.tar.gz \
  https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_darwin_arm64.tar.gz

# Intel
# curl -fsSL -o plaud.tar.gz \
#   https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_darwin_amd64.tar.gz

tar -xzf plaud.tar.gz plaud
mkdir -p ~/.local/bin && mv plaud ~/.local/bin/
plaud --version
```

If `~/.local/bin` is not on your `PATH`, add it to your shell profile (`~/.zshrc` or `~/.bashrc`).

### Linux

```bash
# x86_64
curl -fsSL -o plaud.tar.gz \
  https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_linux_amd64.tar.gz

# ARM (Raspberry Pi etc.)
# curl -fsSL -o plaud.tar.gz \
#   https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/plaud-cli_0.2.0_linux_arm64.tar.gz

tar -xzf plaud.tar.gz plaud
mkdir -p ~/.local/bin && mv plaud ~/.local/bin/
plaud --version
```

### Windows

In PowerShell:

```powershell
$tag = "v0.2.0"
$arch = "amd64"   # or "arm64"
$url  = "https://github.com/simensollie/plaud-cli/releases/download/$tag/plaud-cli_0.2.0_windows_$arch.zip"

Invoke-WebRequest -Uri $url -OutFile plaud.zip
Expand-Archive plaud.zip -DestinationPath .
.\plaud.exe --version
```

Move `plaud.exe` somewhere on your `PATH`. The simplest spot for a per-user install is `%LOCALAPPDATA%\Programs\plaud\`.

## Verifying the download (optional but recommended)

Each release ships a `checksums.txt` listing the SHA-256 of every archive.

```bash
curl -fsSL -O https://github.com/simensollie/plaud-cli/releases/download/v0.2.0/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

`OK` next to your archive's filename means the bytes you downloaded match what GoReleaser produced in the release pipeline.

## From source

You need [Go 1.23 or newer](https://go.dev/dl/).

```bash
git clone https://github.com/simensollie/plaud-cli
cd plaud-cli
go build -o plaud ./cmd/plaud
./plaud --version
```

Building from source produces a binary that prints `plaud version 0.x.y-dev` (the development version string) unless you build with the same `-ldflags` GoReleaser uses.

## What's next

Once `plaud --version` works, head to [`getting-started.md`](./getting-started.md).
