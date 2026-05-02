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
mdm skills add <package>   Add a skill from GitHub or URL
mdm skills remove          Remove installed skills
mdm skills list            List installed skills
mdm skills find [query]    Search the registry
mdm skills update          Update installed skills
mdm skills init <name>     Scaffold a new skill
mdm skills install         Restore skills from skills-lock.json
mdm skills sync            Sync skills from node_modules
mdm upgrade                Upgrade the mdm CLI binary
```

Run `mdm --help` or `mdm skills --help` for the full command reference.

## Development

See [src/README.md](src/README.md) for how to build, test, and debug locally.
