# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

**MDM** (Markdown Management) is a Go CLI tool for managing "skills" — reusable markdown-based prompt libraries for AI agents (Claude Code, Cursor, Cline, Copilot, and 40+ others). Skills are installed from GitHub repos, GitLab, URLs, or local paths and placed into each agent's skills directory.

## Commands

All commands run from `src/`:

```bash
make build    # Compile to ./src/mdm
make test     # Run all tests
make install  # go install . (installs to $GOPATH/bin)
```

Run a single test:
```bash
cd src && go test ./tests/ -run TestVersion
cd src && go test ./internal/skill/ -run TestFindSkills
```

Run with the debugger via `.vscode/launch.json` (Delve is configured).

## Architecture

```
src/
├── main.go              # Entry: builds root Cobra command, calls Execute()
├── commands/            # One file per CLI command
│   ├── root.go          # Cobra root; flag normalization; ANSI logo/styles
│   ├── skills.go        # `mdm skills` subcommand; registers add/remove/list/find/update/init/install/sync
│   ├── add.go           # Install flow: multi-agent/skill prompts, scope selection
│   ├── installer.go     # Core install logic: clone → discover → copy → lock
│   └── ...              # remove, list, find, update, sync, selfupdate, init
└── internal/
    ├── agent/           # AllAgents registry (40+ agents); skill dir paths; detection
    ├── skill/           # Skill discovery (SKILL.md parsing); frontmatter; filtering
    ├── source/          # URL/path parsing into ParsedSource (GitHub, GitLab, local, well-known)
    ├── registry/        # Well-known registry fetching (.well-known/agent-skills standard)
    ├── lock/            # skill-lock.json read/write; tracks hashes, versions, timestamps
    ├── git/             # Shallow git clone; branch/ref handling
    ├── blob/            # GitHub API tree/blob queries for skill discovery
    ├── ui/              # ANSI color constants; Bubbletea spinner
    └── version/         # Version constant (bump here for releases)
```

### Key data flow

`mdm skills add` → `installer.go` orchestrates:
1. `source/` parses the input URL/path into a `ParsedSource`
2. `git/` clones the repo (shallow) or `blob/` queries GitHub API
3. `skill/` discovers `SKILL.md` files and applies `--skill` filters
4. User is prompted for which agents to install to (or `--agent` flag)
5. Skill dirs are copied into each agent's skills directory
6. `lock/` records the installation in `.skill-lock.json`

### Adding a new agent

Add an entry to `AllAgents` in `internal/agent/` with the agent's skills dir path(s) and an optional `DetectInstalled()` function.

### Adding a new command

Create a file in `commands/`, define a `cobra.Command`, and register it either on the root command in `root.go` (for top-level commands like `upgrade`) or on the `skills` subcommand in `skills.go` (for skill management commands like `add`, `list`, etc.).

## Release Process

CI in `.github/workflows/release.yml` triggers on pushes to `main` (non-markdown files). It builds binaries for Linux/macOS/Windows (x64 + ARM64), then creates a GitHub release. Version is read from `internal/version/version.go` — bump it there before merging to main.
