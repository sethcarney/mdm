# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

**MDM** (Markdown Management) is a Go CLI tool for managing "skills" — reusable markdown-based prompt libraries for AI agents (Claude Code, Cursor, Cline, Copilot, and 40+ others). Skills are installed from GitHub repos, GitLab, URLs, or local paths and placed into each agent's skills directory.

## Commands

All commands run from the repo root:

```bash
make build    # Compile to ./mdm
make test     # Run all tests
make install  # go install . (installs to $GOPATH/bin)
```

Run a single test:
```bash
go test ./tests/ -run TestVersion
go test ./internal/skill/ -run TestFindSkills
```

Run with the debugger via `.vscode/launch.json` (Delve is configured).

## CLI reference

```
mdm
├── upgrade                          # Self-update the mdm binary from GitHub releases (aliases: update-cli, self-update)
├── doctor                           # Check installed skills and project markdown for health issues
├── completion [bash|zsh|fish|ps1]   # Generate shell completion script
│   └── install                      # Write completion into shell rc file
├── skills                           # Manage skills for AI agents
│   ├── add <package>                # Install a skill from GitHub, GitLab, URL, or local path (aliases: a, install, i)
│   ├── remove [skills...]           # Uninstall skills (aliases: rm, r)
│   ├── list                         # List installed skills (aliases: ls)
│   ├── find [query]                 # Search the skills.sh registry and install interactively (aliases: search, f, s)
│   ├── update [skills...]           # Re-fetch skills from their recorded source+ref (aliases: check)
│   ├── audit [skills...]            # Check installed skills for updates and security advisories
│   ├── init [name]                  # Scaffold a new SKILL.md in the current directory
│   ├── install                      # Restore all skills from skills-lock.json (CI/onboarding)
│   └── sync                         # Sync skills from node_modules into agent skill directories
└── rules                            # Manage agent instruction files (CLAUDE.md, AGENTS.md, .cursorrules, etc.)
    ├── link                         # Symlink all agent instruction files to a single AGENTS.md source of truth
    ├── status                       # Show which instruction files exist, are symlinked, or are missing
    └── unlink                       # Remove symlinks and restore per-agent instruction files
```

## Architecture

```
├── main.go              # Entry: builds root Cobra command, calls Execute()
├── commands/            # One file per CLI command
│   ├── root.go          # Cobra root; flag normalization; ANSI logo/styles; completion command
│   ├── skills.go        # `mdm skills` group; registers all skills subcommands
│   ├── add.go           # `mdm skills add`: install flow; multi-agent/skill prompts, scope selection
│   ├── installer.go     # Shared install logic: clone → discover → copy → lock; sanitizeName, isPathSafe, skillNameMatches
│   ├── remove.go        # `mdm skills remove`
│   ├── list.go          # `mdm skills list`
│   ├── find.go          # `mdm skills find`: queries skills.sh search API
│   ├── update.go        # `mdm skills update`: re-installs from recorded source+ref in lock file
│   ├── audit.go         # `mdm skills audit`: checks skills.sh API for updates and OSV security advisories
│   ├── init.go          # `mdm skills init`: scaffolds a new SKILL.md
│   ├── install.go       # `mdm skills install`: restores skills from skills-lock.json
│   ├── sync.go          # `mdm skills sync`: syncs from node_modules
│   ├── rules.go         # `mdm rules` group: link/status/unlink agent instruction files
│   ├── selfupdate.go    # `mdm upgrade`: downloads and replaces the mdm binary from GitHub releases
│   └── doctor.go        # `mdm doctor`: checks skill health, symlinks, hashes, README presence, and markdown sizes
├── internal/
    ├── agent/           # AllAgents registry (45+ agents); skill dir paths; detection
    ├── skill/           # Skill discovery (SKILL.md parsing); frontmatter; filtering
    ├── source/          # URL/path parsing into ParsedSource (GitHub, GitLab, local, well-known)
    ├── registry/        # Well-known registry fetching (.well-known/agent-skills standard)
    ├── lock/            # skills-lock.json read/write; tracks hashes, versions, timestamps
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
6. `lock/` records the installation in `skills-lock.json`

### Adding a new agent

Add an entry to `AllAgents` in `internal/agent/` with the agent's skills dir path(s) and an optional `DetectInstalled()` function.

### Adding a new command

Create a file in `commands/`, define a `cobra.Command`, and register it either on the root command in `root.go` (for top-level commands like `upgrade`) or on the `skills` subcommand in `skills.go` (for skill management commands like `add`, `list`, etc.).

## Pre-PR Checklist

Before opening a pull request, run all CI checks locally and fix any failures:

```bash
# 1. Tests
go test ./...

# 2. Vulnerability scan
go install golang.org/x/vuln/cmd/govulncheck@v1.1.4
govulncheck ./...

# 3. Formatting — must produce no output
gofmt -s -l .
# Auto-fix with: gofmt -s -w .

# 4. Cyclomatic complexity — no function may exceed 16
go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0
gocyclo -over 16 .
```

All four checks must pass with no errors before a PR is opened. CI will run the same checks and block merge on failure.

## Windows Executable Icon

The Windows binary embeds an icon and version metadata via a `.syso` file that the Go linker picks up automatically when `GOOS=windows`.

### How it works

1. **`assets/mdm.svg`** — the canonical icon source (block-M + downward arrow, black on white).
2. **`assets/mdm.ico`** — a committed multi-resolution ICO (16 / 32 / 48 / 64 / 128 / 256 px). Because it's committed, CI needs no external image tools.
3. **`assets/versioninfo.json`** — goversioninfo config: file description, product name, icon path.
4. **`resource_windows.syso`** — generated at build time by `make syso`; git-ignored. The `_windows` suffix is a Go build constraint so it's only linked into Windows targets.
5. **`tools/gen-icon/`** — pure-Go program that renders the SVG shapes at all six sizes (4× supersampled for anti-aliasing) and writes `assets/mdm.ico`. No external tools required.

### Updating the icon

Edit `assets/mdm.svg`, regenerate the ICO, then commit it:

```bash
make icon   # runs go run ./tools/gen-icon/ → writes assets/mdm.ico
```

### Building the Windows exe locally (any platform)

```bash
make syso                                              # writes resource_windows.syso
GOOS=windows GOARCH=amd64 go build -o mdm.exe .       # links syso into the exe
```

On Windows (no GOOS override needed): `make syso && go build -o mdm.exe .`

Right-click `mdm.exe` → Properties → Details to verify the icon and version fields.

### goversioninfo is a declared tool dependency

`goversioninfo` is pinned in `go.mod` under the `tool` directive (Go 1.24+). `make syso` invokes it via `go tool goversioninfo` — no manual install required.

## Release Process

CI in `.github/workflows/release.yml` triggers on pushes to `main` (non-markdown files). It builds binaries for Linux/macOS/Windows (x64 + ARM64), then creates a GitHub release. Version is read from `internal/version/version.go` — bump it there before merging to main.