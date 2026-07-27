# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Project Is

**MDM** (Markdown Management) is a Go CLI tool for managing "skills" — reusable markdown-based prompt libraries for AI agents (Claude Code, Cursor, Cline, Copilot, and 40+ others). Skills are installed from GitHub repos, GitLab, URLs, or local paths and placed into each agent's skills directory.

## Git Conventions

### Commit messages

Use semantic (Conventional Commits) format:

```
<type>(<scope>): <short description>

[optional body]
```

Types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`

Examples:
```
feat(skills): add --dry-run flag to mdm skills add
fix(audit): handle nil response from OSV API
chore: bump Go toolchain to 1.26.3
docs(agents): add pre-PR checklist guidance
```

### Branch naming

Use a `<type>/<short-description>` prefix matching the commit type:

```
feat/dry-run-flag
fix/osv-nil-panic
chore/go1.26.3-toolchain
docs/pre-pr-checklist
```

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

## Dev container

`.devcontainer/` provides a reproducible environment with the exact toolchain CI
uses — no local Go install required. Open the repo in VS Code and choose
**Reopen in Container**, or run it headless:

```bash
npx @devcontainers/cli up --workspace-folder .
```

What it pins:

| Tool | Version | Why |
| --- | --- | --- |
| Go | derived from `go.mod` | The `go` directive is the only place the version is written |
| git | Debian `os-provided` | `internal/git` shells out to the git binary, so `mdm skills add` needs it |
| Claude Code | latest (feature-managed) | Runs against the same toolchain as the rest of the project |
| golangci-lint | v2.12.2 | Same version and `.golangci.yml` config as the CI lint step |
| govulncheck | v1.5.0 | Same version as the CI vulnerability step |
| goreleaser | v2.15.4 | Matches `.github/workflows/release.yml` for local release dry-runs |

`postCreateCommand` runs `.devcontainer/post-create.sh`, which does `go mod
download` and installs the three tools above. The tools are built with
`go install` under a pinned `GOTOOLCHAIN` rather than downloaded as prebuilt
binaries — golangci-lint has to be compiled with the project's Go to parse its
go1.26 sources, the same reason `.github/workflows/ci.yml` sets
`GOTOOLCHAIN=go1.26.5` before installing it. The script is idempotent, so
re-running it on a rebuild skips tools already at the pinned version.

### Why the Go version isn't in `devcontainer.json`

Bumping `go.mod` is enough to move the container. `post-create.sh` reads the
`go` directive and runs `go env -w GOTOOLCHAIN=go<version>`, which every `go`
command honours in any shell. The dev container feature only installs a
bootstrap Go, so the first `go` command in a fresh container may download the
pinned toolchain — a one-time cost that buys an exact match with CI.

The explicit pin matters: under the default `GOTOOLCHAIN=auto` the `go.mod`
version is only a *minimum*, so a newer toolchain in the base image would
silently win.

### Claude Code

The container installs the Claude Code CLI, plus the VS Code extension when
opened in VS Code or Codespaces. Run `claude` in the container terminal and
follow the sign-in prompt.

Settings, session history, and the OAuth session are otherwise thrown away on
every rebuild. A named volume plus `CLAUDE_CONFIG_DIR` keeps them:

```jsonc
"mounts": [
  "source=claude-code-config-${devcontainerId},target=/home/vscode/.claude,type=volume"
],
"containerEnv": {
  "CLAUDE_CONFIG_DIR": "/home/vscode/.claude"
}
```

Both halves are required. Claude Code splits its state between the `~/.claude`
directory and a separate `~/.claude.json` file that sits *beside* it, and
`~/.claude.json` is the one holding the OAuth session. Mounting a volume on the
directory alone leaves that file on the container's throwaway filesystem, so
the login is gone after every rebuild. The giveaway is:

```
Claude configuration file not found at: /home/vscode/.claude.json
A backup file exists at: /home/vscode/.claude/backups/.claude.json.backup.<ts>
```

— the backup survived because `backups/` is inside the volume; the file it was
copied from was not. `CLAUDE_CONFIG_DIR` moves the whole config directory,
`.claude.json` included, into the volume.

`${devcontainerId}` scopes the volume to this project, so mdm does not share a
session with every other repo on your machine. The target has to be
`remoteUser`'s home directory — `tests/devcontainer_test.go` fails the build if
those two stop agreeing, because the symptom otherwise is just being asked to
sign in again after every rebuild, with no error to trace.

The volume is a *named volume*, not a bind mount: no credential file from the
host is exposed to the container. Note that anything running in the container
can still read the token in `~/.claude`, so treat it the way you would any
other credential in your dev environment.

The feature tag (`:1.0`) pins the feature's install script, not the CLI — the
feature installs the latest Claude Code, which then auto-updates itself. To pin
a specific release instead, install it from a Dockerfile with
`npm install -g @anthropic-ai/claude-code@X.Y.Z` and set `DISABLE_AUTOUPDATER=1`
in `containerEnv`.

### Drift protection

`tests/devcontainer_test.go` fails the build when any pin disagrees with its
source of truth — the `go` directive for the toolchain, and the `*_VERSION`
assignments in `post-create.sh` for the tools. It covers the workflows, the
`Makefile`, this file, and the dev container.

Dependabot cannot do this job: it never rewrites go.mod's `go` directive, it
does not parse `GOTOOLCHAIN` strings or `go install ...@v` lines inside workflow
steps, and a `groups` block never spans package ecosystems — so the bumps could
not land in one PR even in principle.

## CLI reference

```
mdm
├── upgrade                                 # Self-update the mdm binary from GitHub releases (aliases: update-cli, self-update)
├── uninstall                               # Remove the mdm binary from your system (aliases: remove-cli)
├── doctor                                  # Check installed skills and project markdown for health issues
├── completion [bash|zsh|fish|powershell]   # Generate shell completion script
│   └── install                             # Write completion into shell rc file
├── skills                                  # Manage skills for AI agents
│   ├── add <package>                       # Install a skill from GitHub, GitLab, URL, or local path (alias: a)
│   ├── remove [skills...]                  # Uninstall skills (aliases: rm, r)
│   ├── list                                # List installed skills (alias: ls)
│   ├── find [query]                        # Search the skills.sh registry and install interactively (aliases: search, f, s)
│   ├── update [skills...]                  # Re-fetch skills from their recorded source+ref (alias: check)
│   ├── audit [skills...]                   # Check installed skills for updates and security advisories
│   ├── init [name]                         # Scaffold a new SKILL.md in the current directory
│   ├── install                             # Restore all skills from skills-lock.json (CI/onboarding)
│   └── sync                                # Sync skills from node_modules into agent skill directories
├── knowledge                               # [EXPERIMENTAL] Manage OKF knowledge bundles (hidden until enabled; see docs/experimental.md)
│   ├── add <source>                        # Install an OKF bundle into ./knowledge/ and record it in knowledge-lock.json (alias: a)
│   ├── remove [bundles...]                 # Remove bundles and their lock entries (aliases: rm, r)
│   ├── list                                # List installed bundles (alias: ls)
│   ├── update [bundles...]                 # Re-fetch bundles from their recorded source+ref
│   ├── validate [path]                     # Check OKF conformance and link integrity (--json)
│   ├── init [name]                         # Scaffold a minimal conformant bundle
│   └── install                             # Restore all bundles from knowledge-lock.json (CI/onboarding)
├── experimental                            # Manage experimental features (also: MDM_EXPERIMENTAL env var)
│   ├── list                                # Show experimental features and their status (alias: ls)
│   ├── enable <feature>                    # Persist an opt-in
│   └── disable <feature>                   # Remove a persisted opt-in
├── agents                                  # Manage the configured agent list used as default install targets
│   ├── list                                # Show configured agents for the current scope (alias: ls)
│   ├── add [agents...]                     # Add agents to the configured list (interactive picker with no args)
│   └── remove [agents...]                  # Remove agents and their unique skill/instruction files
└── rules                                   # Manage agent instruction files (CLAUDE.md, AGENTS.md, .cursorrules, etc.)
    ├── link                                # Symlink all agent instruction files to a single AGENTS.md source of truth
    ├── status                              # Show which instruction files exist, are symlinked, or are missing
    └── unlink                              # Remove symlinks and restore per-agent instruction files
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
│   ├── agents.go        # `mdm agents` group: list/add/remove configured agents (project + global scope)
│   ├── rules.go         # `mdm rules` group: link/status/unlink agent instruction files
│   ├── selfupdate.go    # `mdm upgrade`: downloads and replaces the mdm binary from GitHub releases
│   ├── uninstall.go     # `mdm uninstall`: removes the mdm binary from the system
│   ├── hidden_scan.go   # Hidden-character pre-install scan shared by add/update
│   ├── experimental.go  # `mdm experimental` group: list/enable/disable feature gates
│   ├── knowledge*.go    # `mdm knowledge` group [EXPERIMENTAL]: OKF bundle add/list/remove/update/validate/init/install + doctor section
│   └── doctor.go        # `mdm doctor`: checks skill health, symlinks, hashes, README presence, and markdown sizes
├── internal/
    ├── agent/           # AllAgents registry (45+ agents); skill dir paths; detection
    ├── skill/           # Skill discovery (SKILL.md parsing); frontmatter; filtering
    ├── okf/             # [EXPERIMENTAL] OKF bundle parsing, discovery, validation, content hashing
    ├── experimental/    # Named feature gates (MDM_EXPERIMENTAL env var + persisted opt-ins)
    ├── source/          # URL/path parsing into ParsedSource (GitHub, GitLab, local, well-known)
    ├── registry/        # Well-known registry fetching (.well-known/agent-skills standard)
    ├── lock/            # skills-lock.json + knowledge-lock.json read/write; tracks hashes, versions, timestamps, configuredAgents
    ├── git/             # Shallow git clone; branch/ref handling
    ├── blob/            # GitHub API tree/blob queries for skill discovery
    ├── security/        # markdownscan: hidden-character / prompt-smuggling detection
    ├── update/          # Shared helpers for self-update and skill update flows
    ├── ui/              # ANSI color constants; Bubbletea spinner
    └── version/         # App name + dev fallback version (release tags override via ldflags)
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

Before opening a pull request, run these three checks — they mirror what CI runs
and block merge on failure:

```bash
# 1. Tests
go test ./...

# 2. Vulnerability scan
# GOTOOLCHAIN pins the build of govulncheck to the project's Go (go.mod) so it
# can parse go1.26 sources even when your base toolchain is older.
GOTOOLCHAIN=go1.26.5 go install golang.org/x/vuln/cmd/govulncheck@v1.5.0
govulncheck ./...

# 3. Lint (formatting + cyclomatic complexity + vet)
# golangci-lint replaces the retired Go Report Card service and the previous
# standalone gofmt + gocyclo checks. Enabled linters live in .golangci.yml.
GOTOOLCHAIN=go1.26.5 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
golangci-lint run ./...
# Auto-fix formatting with: golangci-lint fmt
```

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

CI in `.github/workflows/release.yml` triggers on **tag pushes** matching `v*`. To release:

```bash
git tag v1.5.8
git push origin v1.5.8
```

GoReleaser builds binaries for Linux/macOS/Windows (x64 + ARM64), creates a GitHub release, and injects the tag as the version via ldflags. `internal/version/version.go` holds a `"dev"` fallback for `go install` users — do not bump it for releases, the tag is the source of truth.

Pre-releases work the same way: push a tag like `v1.6.0-rc.1` and GoReleaser marks the GitHub release as a prerelease automatically. `mdm upgrade` skips prereleases because GitHub's `/releases/latest` API excludes them.
