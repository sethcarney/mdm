# mdm

[![CI](https://github.com/sethcarney/mdm/actions/workflows/ci.yml/badge.svg)](https://github.com/sethcarney/mdm/actions/workflows/ci.yml)
[![CodeQL](https://github.com/sethcarney/mdm/actions/workflows/codeql.yml/badge.svg)](https://github.com/sethcarney/mdm/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/sethcarney/mdm/badge)](https://securityscorecards.dev/viewer/?uri=github.com/sethcarney/mdm)
[![Go Report Card](https://goreportcard.com/badge/github.com/sethcarney/mdm)](https://goreportcard.com/report/github.com/sethcarney/mdm)

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
mdm rules link             Set up AGENTS.md as source of truth and symlink agent files
mdm rules status           Show the state of all agent instruction files
mdm rules unlink           Remove symlinks created by mdm rules link

mdm skills add <package>   Add a skill from GitHub or URL
mdm skills remove          Remove installed skills
mdm skills list            List installed skills
mdm skills find [query]    Search the registry
mdm skills update          Update installed skills
mdm skills audit           Check installed skills for updates and security advisories
mdm skills doctor          Check installed skills and project markdown for health issues
mdm skills init <name>     Scaffold a new skill
mdm skills install         Restore skills from skills-lock.json
mdm skills sync            Sync skills from node_modules
mdm upgrade                Upgrade the mdm CLI binary
```

Run `mdm --help` for the full command reference. See [docs/rules.md](docs/rules.md) for a detailed walkthrough of the `mdm rules` flow.

## Development

See [src/README.md](src/README.md) for how to build, test, and debug locally.
