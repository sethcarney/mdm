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
- **Reproducible installs.** Repos can commit an `mdm.lock` with their recommended skills, knowledge bundles, and plugins so new teammates run `mdm skills install` once and onboard with whatever agent they prefer.
- **Security-focused by default.** Every install runs a deterministic local scan for hidden characters and prompt-smuggling patterns, and `mdm skills audit` checks for updates and OSV security advisories.
- **Knowledge bundles.** `mdm knowledge` installs, validates, and updates [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) bundles for AI agents.
- **Agent Plugins.** `mdm plugins` installs, validates, and updates [Agent Plugins](https://agent-plugins.org) — portable packages of skills and MCP servers — and wires their MCP servers into each agent's config.
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

**Dev container**

mdm is published as a [Dev Container Feature](https://containers.dev/implementors/features/), so a repo can declare it instead of scripting an install:

```jsonc
"features": {
  "ghcr.io/sethcarney/mdm/mdm:1": {}
}
```

The feature installs the release binary for the container's architecture to `/usr/local/bin/mdm`, verified against the release checksums — no Go toolchain needed in the image. Pin a release with `{"version": "1.9.1"}`, and pair it with `"postCreateCommand": "mdm skills install"` to restore the skills in `mdm.lock` on create. See [src/mdm/README.md](src/mdm/README.md) for the full options.

## Usage

```
mdm rules link             Set up AGENTS.md as source of truth and symlink agent files
mdm rules status           Show the state of all agent instruction files
mdm rules unlink           Remove symlinks created by mdm rules link

mdm skills add <package>   Add a skill from GitHub or URL
mdm skills cherry-pick     Fork third-party skills into ./skills as your own
mdm skills remove          Remove installed skills
mdm skills list            List installed skills
mdm skills find [query]    Search the registry
mdm skills update          Update installed skills
mdm skills audit           Check installed skills for updates and security advisories
mdm skills init [name]     Scaffold a new skill
mdm skills install         Restore skills from mdm.lock
mdm skills sync            Sync skills from node_modules

mdm knowledge add <source>  Install an OKF knowledge bundle into ./knowledge/
mdm knowledge list          List installed bundles (also: remove · update · validate · init · install)

mdm plugins add <source>   Install an Agent Plugin: link skills, wire MCP servers
mdm plugins list           List installed plugins (also: remove · update · validate · init · install)

mdm agents list            Show the configured agents for the current scope
mdm agents add             Add agents to the configured default install list
mdm agents remove          Remove agents (and their unique skill / instruction files)

mdm doctor                 Check installed skills and project markdown for health issues
mdm migrate                Fold v1 lock files into mdm.lock / mdm-state.json
mdm bug                    Open a prefilled bug-report form (nothing is sent automatically)
mdm upgrade                Upgrade the mdm CLI binary
```

Run `mdm --help` for the full command reference. See [docs/rules.md](docs/rules.md) for a detailed walkthrough of the `mdm rules` flow.

> [!WARNING]
> **`mdm agents remove openclaw` deletes `./skills/`.** Removing an agent cleans
> up the skills directory that belongs to it, and OpenClaw's project skills
> directory is `skills/` — the same place many projects keep hand-written
> skills. mdm cannot tell your own work from an OpenClaw install, so anything in
> there that is not a [cherry-picked fork](https://sethcarney.github.io/mdm/skills/cherry-pick/)
> (those carry an `.mdm-origin.json` marker and are preserved) is deleted along
> with it. Commit `./skills/` before removing agents, or keep hand-written skills
> in a directory no agent claims. Every other agent uses a dot-prefixed or shared
> directory. See [Troubleshooting](https://sethcarney.github.io/mdm/troubleshooting/).

Skill installs run a deterministic local hidden-character scan over markdown files before copying or symlinking content. See [docs/security/hidden-character-scan.md](docs/security/hidden-character-scan.md) for the exact checks and bypass policy.

When installing from a git source, mdm restricts git to the **https** and **ssh** transports. This blocks git's `ext::`/`fd::` local-command transports, which would otherwise let a repository source string — including one replayed from a checked-in `mdm.lock` — execute arbitrary commands. See [docs/security/git-transport-restrictions.md](docs/security/git-transport-restrictions.md) for the rationale and the full allow/deny list.

## How skills land in your repo, and what to commit

`mdm skills add` writes one canonical copy of each skill to `.agents/skills/<name>`
and gives every agent you install to a relative symlink from its own skills
directory (`.claude/skills/<name>`, `.cursor/skills/<name>`, and so on) back to
that copy. Agents that read `.agents/skills` natively get no link at all.

**Symlink is the default** because the canonical directory is the only place
the content lives. Installing to five agents does not mean five copies of the
same files, an update touches one directory and every agent sees it at once, and
there is never a question of which agent's copy is the current one.

**Copy mode** exists for tools that do not follow symlinks, such as sandboxed
agents that refuse to traverse links, or a machine where symlinks cannot be
created. Pass `--copy` once:

```bash
mdm skills add owner/repo --copy
```

The mode is a switch on the whole scope, not a property of one skill. `--copy`
records `installMode: copy` in `mdm.lock` (or in the global state file with
`-g`), converts the installs already in that scope to real directories, and
every later `add`, `update`, and `install` in the scope copies without the flag.
`--symlink` switches back the same way: it turns the copied installs into links
again and records symlink mode, so the lock never needs editing by hand.

**If a symlink cannot be created**, mdm copies that install instead of failing,
on the spot and per install. The usual cause is Windows without Developer Mode
or the symlink privilege. Nothing is recorded: the scope stays in symlink mode,
and the install summary still says so. If that is the permanent state of the
machine, pass `--copy` so the mode is recorded and the lock says what is on
disk; `mdm doctor` reports a scope whose files are copies while its lock does
not say so.

**Do not commit the skills mdm manages.** Commit `mdm.lock` and let
`mdm skills install` regenerate the rest, the way a package lock is committed
and the package directory is not:

- **The content is duplicated.** One skill is the canonical directory plus a
  link or a full copy per agent. Committing it puts the same files into your
  repository at two or more paths, and in copy mode that is a complete copy per
  agent, all of which have to agree.
- **Remote skills drift.** A skill from a GitHub, GitLab, or URL source is
  pinned by source and ref in `mdm.lock`. Once its files are committed, the
  repository has its own copy that nothing keeps in step with that ref: an
  update rewrites the files under a reviewer who cannot tell a local edit from
  an upstream change, and a local edit is silently lost on the next update.
  The lock is the record of what was installed; the files are its output.
- **Symlinks do not travel well.** Git stores a symlink as a path, and a
  checkout on a machine that cannot create symlinks leaves a text file where
  the link was. Regenerating from the lock on each machine avoids the problem.

Ignore the canonical directory and each agent's skills directory. This
repository's own `.gitignore` is the shape to follow:

```gitignore
.agents
.claude/*
!.claude/settings.json
```

Add a line per agent you install to; `mdm agents list` shows the configured
ones. Two things do belong in the repository: skills you write yourself, and
forks made with `mdm skills cherry-pick`, which land in `./skills/` precisely so
they can be committed and edited as your own. See
[docs/skills/add.md](docs/skills/add.md) for the full install-mode rules and
[docs/skills/cherry-pick.md](docs/skills/cherry-pick.md) for forks.

## Development

```bash
make build    # compile to ./mdm
make test     # run all tests
make install  # install to $GOPATH/bin
go run . --help
```

See [.vscode/launch.json](.vscode/launch.json) for VS Code debug configurations.
