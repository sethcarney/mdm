# mdm

The markdown management CLI. No telemetry · Fully open source.

## Install

**macOS / Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/sethcarney/mdm/main/install.sh | bash
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/sethcarney/mdm/main/install.ps1 | iex
```

Both installers place the binary in `~/.local/bin/mdm` and will warn if that directory isn't in your `PATH`.

To install to a different directory, set `INSTALL_DIR` before running:

```bash
INSTALL_DIR=/usr/local/bin curl -fsSL https://raw.githubusercontent.com/sethcarney/mdm/main/install.sh | bash
```

## Usage

```
mdm add <package>     Add a skill from GitHub or URL
mdm remove            Remove installed skills
mdm list              List installed skills
mdm find [query]      Search the registry
mdm update            Update installed skills
mdm upgrade           Upgrade the mdm CLI binary
```

Run `mdm --help` for the full command reference.

## Development

See [src/README.md](src/README.md) for how to build, test, and debug locally.
