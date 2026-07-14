# mdm

[![CI](https://github.com/sethcarney/mdm/actions/workflows/ci.yml/badge.svg)](https://github.com/sethcarney/mdm/actions/workflows/ci.yml)
[![CodeQL](https://github.com/sethcarney/mdm/actions/workflows/codeql.yml/badge.svg)](https://github.com/sethcarney/mdm/actions/workflows/codeql.yml)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/sethcarney/mdm/badge)](https://securityscorecards.dev/viewer/?uri=github.com/sethcarney/mdm)
[![SLSA 3](https://slsa.dev/images/gh-badge-level3.svg)](https://slsa.dev)
[![golangci-lint](https://img.shields.io/badge/lint-golangci--lint-00ADD8?logo=go)](https://golangci-lint.run)
[![GitHub Release](https://img.shields.io/github/v/release/sethcarney/mdm)](https://github.com/sethcarney/mdm/releases/latest)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/sethcarney/mdm)

The markdown management CLI. No telemetry · Fully open source.

📖 **Full documentation: [sethcarney.github.io/mdm](https://sethcarney.github.io/mdm/)** — install guide, complete [command reference](https://sethcarney.github.io/mdm/commands/), and per-command guides.

## Why mdm?

Managing markdown across multiple agentic coding tools is more painful than it should be. Keeping `CLAUDE.md`, `copilot-instructions.md`, `.cursorrules`, and friends consistent and up to date is tedious enough on its own. Add in installing first- and third-party skills and context files, versioning them, pulling updates, and auditing them for prompt injection risks and hidden characters, and it becomes a real maintenance burden.

`mdm` is a fast, security-focused markdown management CLI written in Go, designed to solve exactly these problems:

- **45 agents supported** out of the box, including Claude Code, Cursor, Cline, GitHub Copilot, Gemini CLI, Codex, and 39 more.
- **One source of truth for instruction files.** `mdm rules link` makes `AGENTS.md` the canonical file and symlinks each agent's expected filename to it.
- **Skills from anywhere.** Install from GitHub, GitLab, arbitrary URLs, local paths, or the [skills.sh](https://skills.sh) registry.
- **Reproducible installs.** Repos can commit a `skills-lock.json` with their recommended skills so new teammates run `mdm skills install` once and onboard with whatever agent they prefer.
- **Security-focused by default.** Every install runs a deterministic local scan for hidden characters and prompt-smuggling patterns, and `mdm skills audit` checks for updates and OSV security advisories.
- **Knowledge bundles _(experimental)_.** `mdm knowledge` installs, validates, and updates [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) bundles for AI agents. It's gated behind an experimental flag — see [docs/experimental.md](docs/experimental.md) to enable it.
- **No telemetry, fully open source.**

Prefer a UI? There's also a [VS Code extension](https://marketplace.visualstudio.com/items?itemName=SethsSoftware.mdm-sidebar).

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
mdm skills init [name]     Scaffold a new skill
mdm skills install         Restore skills from skills-lock.json
mdm skills sync            Sync skills from node_modules

mdm agents list            Show the configured agents for the current scope
mdm agents add             Add agents to the configured default install list
mdm agents remove          Remove agents (and their unique skill / instruction files)

mdm doctor                 Check installed skills and project markdown for health issues
mdm upgrade                Upgrade the mdm CLI binary
```

Run `mdm --help` for the full command reference. See [docs/rules.md](docs/rules.md) for a detailed walkthrough of the `mdm rules` flow.

Skill installs run a deterministic local hidden-character scan over markdown files before copying or symlinking content. See [docs/security/hidden-character-scan.md](docs/security/hidden-character-scan.md) for the exact checks and bypass policy.

When installing from a git source, mdm restricts git to the **https** and **ssh** transports. This blocks git's `ext::`/`fd::` local-command transports, which would otherwise let a repository source string — including one replayed from a checked-in `skills-lock.json` — execute arbitrary commands. See [docs/security/git-transport-restrictions.md](docs/security/git-transport-restrictions.md) for the rationale and the full allow/deny list.

## Development

```bash
make build    # compile to ./mdm
make test     # run all tests
make install  # install to $GOPATH/bin
go run . --help
```

See [.vscode/launch.json](.vscode/launch.json) for VS Code debug configurations.
