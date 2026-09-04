# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`CLAUDE.md` is a symlink to this file. Edit `AGENTS.md`; never replace the
symlink with a copy. The filename `AGENTS.md` is an ecosystem contract — several
harnesses look for exactly that name — and does not change, even though this
project now calls the tools themselves "harnesses" rather than "agents".

## What This Project Is

**MDM** (Markdown Management) is a Go CLI tool for managing "skills" — reusable markdown-based prompt libraries for AI coding tools (Claude Code, Cursor, Cline, Copilot, and 40+ others). Skills are installed from GitHub repos, GitLab, URLs, or local paths and placed into each tool's skills directory.

### "Harness" and "agent" mean two different things

The word "agent" used to mean the AI tool mdm installs into. It no longer does,
and the distinction runs through every name in the codebase:

| Term | Means | Command | Lives in |
| --- | --- | --- | --- |
| **harness** | an AI coding tool mdm installs into (Claude Code, Cursor, Codex, …) | `mdm harnesses` | `internal/harness/`, `commands/harnesses.go` |
| **agent definition** | one markdown file with `name` + `description` frontmatter that gives a harness a named subagent persona | `mdm agents` | `internal/agentfile/`, `commands/agent_artifacts.go` |

So `mdm agents add` installs *into* a harness; `mdm harnesses add` configures
*which* harnesses are the default install targets. A skill is a directory with a
`SKILL.md`; an agent definition is a single file. Both are installed from the
same sources, recorded in the same lock, and obey the same per-scope install
mode.

Three names deliberately keep the old word and must not be renamed: `AGENTS.md`
itself, the shared `.agents/` directory (`.agents/skills`, `.agents/agents`), and
the `configuredAgents` JSON key that v1 lock readers still parse (the Go field
behind it is `ConfiguredHarnesses`).

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
docs(harness): add pre-PR checklist guidance
```

### Branch naming

Use a `<type>/<short-description>` prefix matching the commit type:

```
feat/dry-run-flag
fix/osv-nil-panic
chore/go1.26.3-toolchain
docs/pre-pr-checklist
```

This applies to AI agents too, and overrides whatever branch a coding-agent
harness assigns. Claude Code on the web, for example, opens each session on a
generated `claude/<description>-<id>` branch; move the work to a branch named by
the convention above before pushing, rather than pushing the generated name.

### Commit authorship

Commits are authored by the person running the tool, not by the tool. An agent
that finds a bot identity in `git config user.name` / `user.email` - some
hosted environments preset one - should commit under the repository owner's
identity instead, matching what `git log` already shows:

```
Seth <48496865+sethcarney@users.noreply.github.com>
```

Agent attribution is disabled for this repository: do not add
`Co-Authored-By:` trailers, `Generated with` footers, or session links to
commits or pull requests. `.claude/settings.json` turns off Claude Code's
automatic attribution to match. Authorship carries the accountability, and
the repository owner reviews everything an agent produces before it lands.

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
uses - no local Go install required. Open the repo in VS Code and choose
**Reopen in Container**, or run it headless:

```bash
npm ci   # installs the devcontainer CLI pinned in package-lock.json
npx devcontainer up --workspace-folder .
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
binaries - golangci-lint has to be compiled with the project's Go to parse its
go1.26 sources, the same reason `.github/workflows/ci.yml` sets
`GOTOOLCHAIN=go1.26.6` before installing it. The script is idempotent, so
re-running it on a rebuild skips tools already at the pinned version.

### Why the Go version isn't in `devcontainer.json`

Bumping `go.mod` is enough to move the container. `post-create.sh` reads the
`go` directive and runs `go env -w GOTOOLCHAIN=go<version>`, which every `go`
command honours in any shell. The dev container feature only installs a
bootstrap Go, so the first `go` command in a fresh container may download the
pinned toolchain - a one-time cost that buys an exact match with CI.

The explicit pin matters: under the default `GOTOOLCHAIN=auto` the `go.mod`
version is only a *minimum*, so a newer toolchain in the base image would
silently win.

### Podman compatibility

The container builds under docker and podman, but podman needs three
accommodations for a buildah bug
([containers/buildah#6503](https://github.com/containers/buildah/issues/6503)):
every feature installs in a build step that bind-mounts under `/tmp`, and the
layer that step commits loses `/tmp`'s `1777` mode. The first feature step sees
a healthy `/tmp`; in any later one, `apt-get update` fails with *"Couldn't
create temporary file /tmp/apt.conf.* for passing config to apt-key"* and every
repository reports as unsigned.

1. `.devcontainer/Dockerfile` pre-installs the packages the features would
   apt-get, so their install scripts skip apt entirely.
2. `overrideFeatureInstallOrder` puts claude-code - the one feature that still
   needs apt, for the nodesource repo - in the first feature step.
3. `runArgs` mounts a tmpfs over `/tmp`, because the final image inherits the
   broken mode from the last feature step, and VS Code's attach sequence needs
   a writable `/tmp` (`mkdir -p /tmp/.X11-unix`) before any lifecycle hook
   could repair it. `post-create.sh` keeps a `chmod 1777` fallback for hosts
   that ignore `runArgs`.

All three are no-ops under docker. If a feature update starts failing this way
again under podman, its install script has begun apt-getting something new -
add the package to the Dockerfile list.

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

- the backup survived because `backups/` is inside the volume; the file it was
copied from was not. `CLAUDE_CONFIG_DIR` moves the whole config directory,
`.claude.json` included, into the volume.

`${devcontainerId}` scopes the volume to this project, so mdm does not share a
session with every other repo on your machine. The target has to be
`remoteUser`'s home directory - `tests/devcontainer_test.go` fails the build if
those two stop agreeing, because the symptom otherwise is just being asked to
sign in again after every rebuild, with no error to trace.

The volume is a *named volume*, not a bind mount: no credential file from the
host is exposed to the container. Note that anything running in the container
can still read the token in `~/.claude`, so treat it the way you would any
other credential in your dev environment.

The feature tag (`:1.0`) pins the feature's install script, not the CLI - the
feature installs the latest Claude Code, which then auto-updates itself. To pin
a specific release instead, install it from a Dockerfile with
`npm install -g @anthropic-ai/claude-code@X.Y.Z` and set `DISABLE_AUTOUPDATER=1`
in `containerEnv`.

### Drift protection

`tests/devcontainer_test.go` fails the build when any pin disagrees with its
source of truth - the `go` directive for the toolchain, and the `*_VERSION`
assignments in `post-create.sh` for the tools. It covers the workflows, the
`Makefile`, this file, and the dev container.

Dependabot cannot do this job: it never rewrites go.mod's `go` directive, it
does not parse `GOTOOLCHAIN` strings or `go install ...@v` lines inside workflow
steps, and a `groups` block never spans package ecosystems - so the bumps could
not land in one PR even in principle.

## Dev Container Feature

Separate from `.devcontainer/` (which configures *this* repo's container), `src/`
and `test/` publish mdm itself as a
[Dev Container Feature](https://containers.dev/implementors/features/), so other
repos can install the CLI with one entry in their `devcontainer.json`:

```jsonc
"features": {
  "ghcr.io/sethcarney/mdm/mdm:1": {}
}
```

| Path | Role |
| --- | --- |
| `src/mdm/devcontainer-feature.json` | Feature metadata: id, version, the `version` option |
| `src/mdm/install.sh` | Arch detection → download → checksum → `/usr/local/bin/mdm` |
| `src/mdm/README.md` | Published feature docs (hand-written, not generated) |
| `test/mdm/test.sh` | Default test, run against every base image in the matrix |
| `test/mdm/scenarios.json` + `test/mdm/*.sh` | Option, non-root, and slim-image scenarios |
| `.github/workflows/devcontainer-feature.yml` | Builds the feature into real containers on PRs |
| `.github/workflows/devcontainer-feature-release.yml` | Publishes to GHCR on merge to `main` |

The layout is fixed by the devcontainer CLI: it addresses a feature by its
directory name under `src/`, looks for the tests in `test/<same name>/`, and
runs `<scenario key>.sh` for each key in `scenarios.json`.

### Versioning

The version in `devcontainer-feature.json` is the **feature's** version, not
mdm's - it tracks changes to `install.sh` and the options, and moves
independently of the release tags. Bump it there and the release workflow
publishes `:1`, `:1.0`, `:1.0.0`, and `:latest`; the action skips a version
that is already in the registry, so a run that changes nothing is a no-op.

Repo tagging is deliberately disabled in that workflow. `release.yml` fires on
`v*` tags and runs GoReleaser, so a publishing action that creates tags in this
repo is a foot-gun waiting for the first version collision.

**A newly published GHCR package is private.** After the first publish, set it
to public under *Packages → mdm → Package settings → Change visibility*, or
consumers get a 401 from the registry when their container builds.

### Why the feature only installs a binary

Feature install scripts run during **image build**, before the workspace is
mounted, so nothing that reads or writes the repo can work there - `mdm skills
install` and `mdm rules link` belong in a `postCreateCommand`, which runs after
the workspace exists. That is why there is no "run mdm on create" option: the
option would have to run in a phase where there is no repo to run against.

It also installs to `/usr/local/bin` rather than `~/.local/bin` (where
`install.sh` in the repo root puts it): features install as root at build time,
and only a shared path is on `PATH` for whichever `remoteUser` the image ends up
running as.

### Why /usr/local/bin holds a symlink

The binary itself goes to `/usr/local/lib/mdm/mdm`, chowned to `_REMOTE_USER`,
and `/usr/local/bin/mdm` is a symlink to it. That is what makes `mdm upgrade`
work inside the container: replacing a running executable means unlinking and
recreating it, which is a permission on the *directory*, not on the file - so a
binary sitting directly in a root-owned `/usr/local/bin` cannot be upgraded by
the container's user at all, leaving an image rebuild as the only route to a
new release. Chowning `/usr/local/bin` itself would hand that user every other
feature's binaries too, hence the private directory.

`tests/devcontainer_feature_test.go` pins both halves, and
`test/mdm/non_root_user.sh` performs the unlink-and-replace as the non-root
user, because nothing else fails when this regresses - the container still
builds, and only upgrades break, in someone else's container.

### Drift protection

`tests/devcontainer_feature_test.go` ties the feature to the release it
installs. `install.sh` hardcodes the repository, the asset names, and the
checksum file name, none of which the compiler can see - so the test reads them
back out of `.goreleaser.yaml` and fails the build when they disagree. It also
checks that every feature has tests, that every scenario has a script (and every
script a scenario), that pinned scenarios assert the version they pin, that the
scripts are executable, and that the options table in `src/mdm/README.md`
matches `devcontainer-feature.json`.

### Running the feature tests locally

They need Docker and the devcontainer CLI:

```bash
npm ci   # the CLI version CI runs, from package-lock.json

# Default test against one base image
npx devcontainer features test -f mdm -i mcr.microsoft.com/devcontainers/base:ubuntu --skip-scenarios .

# The scenarios in test/mdm/scenarios.json
npx devcontainer features test -f mdm --skip-autogenerated --skip-duplicated .
```

## Docs site dependencies

The MkDocs build installs Python packages, so its dependencies are pinned by
hash the same way the Actions are pinned by SHA:

| File | Role |
| --- | --- |
| `docs/requirements.in` | Direct dependencies - the file you edit |
| `docs/requirements.txt` | Generated lock: every transitive dependency, `==` pinned, with artifact hashes |
| `.github/workflows/docs.yml` | Installs the lock with `pip install --require-hashes` |

Regenerate the lock after editing `requirements.in`:

```bash
make docs-lock   # uv pip compile --generate-hashes, resolved for the Python the workflow uses
```

`--require-hashes` is what makes OpenSSF Scorecard count the pip command as
pinned, and it is all-or-nothing: pip fails the whole install if any requirement
lacks a hash or an `==` pin, so `requirements.txt` cannot be edited by hand.
`tests/docs_requirements_test.go` enforces all of that - the flag in the
workflow, a fully hashed lock, the lock agreeing with `requirements.in`, and the
`--python-version` in `make docs-lock` matching `setup-python` in the workflow.
Dependabot watches `/docs` and recompiles the pair together.

## Dependency pinning

Everything CI executes is pinned to an artifact, not to a name - that is what
OpenSSF Scorecard's Pinned-Dependencies check measures, and what keeps a
republished tag from changing what runs.

| Ecosystem | Pinned by | Kept fresh by |
| --- | --- | --- |
| GitHub Actions | commit SHA, with the tag in a trailing comment | Dependabot `github-actions`, `/` |
| Container images | `FROM image:tag@sha256:…` in `.devcontainer/Dockerfile` | Dependabot `docker`, `/.devcontainer` |
| npm (devcontainer CLI) | `npm ci` against `package-lock.json` integrity hashes | Dependabot `npm`, `/` |
| pip (docs site) | `pip install --require-hashes` against `docs/requirements.txt` | Dependabot `pip`, `/docs` |
| Go tools | `go install <module>@<version>` under a pinned `GOTOOLCHAIN` | `tests/devcontainer_test.go` |

The root `package.json` is build tooling and nothing else - it pins the
devcontainer CLI so `.github/workflows/devcontainer-feature.yml` can install it
with `npm ci` instead of `npm install -g @devcontainers/cli@<version>`. The
distinction matters even though both name an exact version: a version is not an
artifact, and Scorecard scores it accordingly. `devcontainer-feature-release.yml`
reads the same field out of `package.json` at run time rather than repeating it,
so the CLI that validated a feature is the one that publishes it.
`tests/devcontainer_feature_test.go` enforces the manifest/lock agreement, the
`npm ci` + `npx devcontainer` form, and the docs above.

### The one thing that stays unpinned

`.github/workflows/release.yml` references
`slsa-framework/slsa-github-generator` by tag, and Scorecard reports it as an
unpinned third-party action. That is a known false positive, not an oversight:
the generator reads `github.action_ref` at run time to decide which builder
binary to download, so a SHA reference breaks the lookup outright. Pinning it is
[documented as unsupported upstream](https://github.com/slsa-framework/slsa-github-generator#referencing-slsa-builders-and-generators).

The alternative that would clear the warning - dropping the generator for
`actions/attest-build-provenance` - trades SLSA Build Level 3 for Level 2 and
invalidates the `slsa-verifier` instructions in `SECURITY.md`. Losing a
provenance level to gain a Scorecard point is the wrong direction, so the tag
reference stays. Scorecard has no suppression syntax; the comment above the
`uses:` line is the record.

## CLI reference

```
mdm
├── upgrade                                 # Self-update the mdm binary from GitHub releases (aliases: update-cli, self-update)
├── uninstall                               # Remove the mdm binary from your system (aliases: remove-cli)
├── doctor                                  # Check installed skills and project markdown for health issues
├── migrate                                 # Fold v1 lock files into mdm.lock / mdm-state.json (--dry-run, --no-tombstone, --force)
├── bug                                     # Open a prefilled GitHub issue form (--print, --command); no network I/O
├── completion [bash|zsh|fish|powershell]   # Generate shell completion script
│   └── install                             # Write completion into shell rc file
├── skills                                  # Manage skills for AI harnesses
│   ├── add <package>                       # Install a skill from GitHub, GitLab, URL, or local path (alias: a; --copy/--symlink)
│   ├── cherry-pick <source>                # Fork skills into ./skills as your own, with provenance (aliases: fork, cp)
│   ├── remove [skills...]                  # Uninstall skills (aliases: rm, r)
│   ├── list                                # List installed skills (alias: ls)
│   ├── find [query]                        # Search the skills.sh registry and install interactively (aliases: search, f, s)
│   ├── update [skills...]                  # Re-fetch skills from their recorded source+ref (alias: check)
│   ├── audit [skills...]                   # Check installed skills for updates and security advisories
│   ├── init [name]                         # Scaffold a new SKILL.md in the current directory
│   ├── install                             # Restore skills, then agent definitions, from mdm.lock (CI/onboarding)
│   └── sync                                # Sync skills from node_modules into harness skill directories
├── knowledge                               # Manage OKF knowledge bundles
│   ├── add <source>                        # Install an OKF bundle into ./knowledge/ and record it in mdm.lock (alias: a)
│   ├── remove [bundles...]                 # Remove bundles and their lock entries (aliases: rm, r)
│   ├── list                                # List installed bundles (alias: ls)
│   ├── update [bundles...]                 # Re-fetch bundles from their recorded source+ref
│   ├── validate [path]                     # Check OKF conformance and link integrity (--json)
│   ├── init [name]                         # Scaffold a minimal conformant bundle
│   └── install                             # Restore all bundles from mdm.lock (CI/onboarding)
├── plugins                                 # Manage Agent Plugins (agent-plugins.org)
│   ├── add <source>                        # Install a plugin into .agents/plugins/, link its skills, wire MCP config (alias: a)
│   ├── remove [plugins...]                 # Unwire MCP, unlink skills, delete plugin + lock entry (aliases: rm, r; --purge-data)
│   ├── list                                # List installed plugins (alias: ls)
│   ├── update [plugins...]                 # Re-fetch plugins from their recorded source+ref (preserves data dir)
│   ├── validate [path]                     # Check Agent Plugins spec conformance (--json)
│   ├── init [name]                         # Scaffold a minimal conformant plugin (--with-mcp)
│   └── install                             # Restore all plugins from mdm.lock (CI/onboarding)
├── experimental                            # Manage experimental features (also: MDM_EXPERIMENTAL env var)
│   ├── list                                # Show experimental features and their status (alias: ls)
│   ├── enable <feature>                    # Persist an opt-in
│   └── disable <feature>                   # Remove a persisted opt-in
├── harnesses                               # Manage the configured harness list used as default install targets
│   ├── list                                # Show configured harnesses for the current scope (alias: ls)
│   ├── add [harnesses...]                  # Add harnesses to the configured list (interactive picker with no args)
│   └── remove [harnesses...]               # Remove harnesses and their unique skill/instruction files
├── agents                                  # Manage agent definitions — subagent persona files installed into a harness
│   ├── add <source>                        # Install agent definitions from GitHub, a URL, or a local path (alias: a)
│   ├── list                                # List installed agent definitions (alias: ls)
│   ├── remove [names...]                   # Remove installed agent definitions (aliases: rm, r)
│   ├── update [names...]                   # Re-fetch definitions from their recorded source+ref
│   └── install                             # Restore all agent definitions from mdm.lock (CI/onboarding)
└── rules                                   # Manage harness instruction files (CLAUDE.md, AGENTS.md, .cursorrules, etc.)
    ├── link                                # Symlink all harness instruction files to a single AGENTS.md source of truth
    ├── status                              # Show which instruction files exist, are symlinked, or are missing
    └── unlink                              # Remove symlinks and restore per-harness instruction files
```

## Architecture

```
├── main.go              # Entry: builds root Cobra command, calls Execute()
├── commands/            # One file per CLI command
│   ├── root.go          # Cobra root; flag normalization; ANSI logo/styles; completion command
│   ├── skills.go        # `mdm skills` group; registers all skills subcommands
│   ├── add.go           # `mdm skills add`: install flow; multi-harness/skill prompts, scope selection
│   ├── installer.go     # Shared install logic: clone → discover → copy → lock; sanitizeName, isPathSafe, skillNameMatches
│   ├── install_mode.go  # Per-scope symlink/copy switch; converts every existing skill AND agent definition when the mode changes
│   ├── cherrypick.go    # `mdm skills cherry-pick`: vendor third-party skills into ./skills; license resolution, --status
│   ├── remove.go        # `mdm skills remove`
│   ├── list.go          # `mdm skills list`
│   ├── find.go          # `mdm skills find`: queries skills.sh search API
│   ├── update.go        # `mdm skills update`: re-installs from recorded source+ref in lock file
│   ├── audit.go         # `mdm skills audit`: checks skills.sh API for updates and OSV security advisories
│   ├── init.go          # `mdm skills init`: scaffolds a new SKILL.md
│   ├── install.go       # `mdm skills install`: restores skills from mdm.lock
│   ├── sync.go          # `mdm skills sync`: syncs from node_modules
│   ├── harnesses.go     # `mdm harnesses` group: list/add/remove configured harnesses (project + global scope)
│   ├── agent_artifacts.go # `mdm agents` group: add/list/remove/update/install AGENT DEFINITIONS (not harnesses)
│   ├── agent_installer.go # Installs one definition into one harness; agentDiskName, the canonical/harness path pair
│   ├── rules.go         # `mdm rules` group: link/status/unlink harness instruction files
│   ├── selfupdate.go    # `mdm upgrade`: downloads and replaces the mdm binary from GitHub releases
│   ├── uninstall.go     # `mdm uninstall`: removes the mdm binary from the system
│   ├── hidden_scan.go   # Hidden-character pre-install scan shared by every install path (skills, agent definitions, knowledge, plugins)
│   ├── experimental.go  # `mdm experimental` group: list/enable/disable feature gates
│   ├── knowledge*.go    # `mdm knowledge` group: OKF bundle add/list/remove/update/validate/init/install + doctor section
│   ├── plugins*.go      # `mdm plugins` group: Agent Plugins add/list/remove/update/validate/init/install + MCP wiring + doctor section
│   ├── bug.go           # `mdm bug`: prefilled issue-form URL + panic hook (HandlePanic, deferred from main)
│   └── doctor.go        # `mdm doctor`: checks skill health, symlinks, hashes, README presence, and markdown sizes
├── internal/
    ├── harness/         # AllHarnesses registry (45+ harnesses); skills / agents dir paths; detection
    ├── skill/           # Skill discovery (SKILL.md parsing); frontmatter; filtering
    ├── agentfile/       # Agent-definition discovery and parsing (single .md files with name + description frontmatter)
    ├── fork/            # Cherry-pick provenance: .mdm-origin.json, ATTRIBUTION.md, content hashing, license detection
    ├── okf/             # OKF bundle parsing, discovery, validation, content hashing
    ├── plugin/          # Agent Plugins spec conformance: plugin.json + mcp.json parsing, discovery, path containment, hashing
    ├── mcpwire/         # Per-harness MCP config targets; renders plugin servers into .mcp.json / .cursor/mcp.json
    ├── experimental/    # Named feature gates (MDM_EXPERIMENTAL env var + persisted opt-ins)
    ├── pathsafe/        # The one copy of the install path guard: IsSafeRelDir (lexical) + ResolvedContains (on-disk containment), applied by skill/ and agentfile/ to directories a source declares for itself
    ├── source/          # URL/path parsing into ParsedSource (GitHub, GitLab, local, well-known)
    ├── registry/        # Well-known registry fetching (.well-known/agent-skills standard)
    ├── lock/            # mdm.lock read/write (skills, agents, knowledge, plugins sections; reads legacy v1 lock files as a fallback); tracks hashes, versions, timestamps, the per-scope installMode, and configuredHarnesses (still written under the `configuredAgents` JSON key for v1 readers)
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
4. `hidden_scan.go` scans the selected markdown before anything is written; a
   finding blocks the install unless `--allow-hidden-chars` is passed
5. User is prompted for which harnesses to install to (or `--harness` flag)
6. Skill dirs are written to `.agents/skills/<name>` and symlinked (or copied,
   per the scope's `installMode`) into each harness's skills directory. A skill
   whose source directory IS its destination — `mdm skills add .` rediscovering
   mdm's own canonical copies — is skipped rather than copied. The check is
   `os.Stat` + `os.SameFile` at the level where both paths are known, immediately
   before the destination is emptied; a same-file guard inside `copyFile` would
   be too late, because in symlink mode the directory it would read is deleted a
   layer above it.
7. `lock/` records the installation in the skills section of `mdm.lock`

Directories a source declares for itself — `skillDirs` in `skill/`, `agentsDirs`
in `agentfile/`, both read out of `.claude-plugin/marketplace.json` — are
third-party input and go through `pathsafe/` before they are opened: the lexical
`IsSafeRelDir` first, then `ResolvedContains` against the resolved search root, so
that a directory which only looks local but is really a symlink out of the tree
is refused. A rejected entry is dropped silently. Note the asymmetry: the
`agentfile/` guard is live, reached by `mdm agents add` and `mdm agents update`
through `DiscoverAgentFiles`, while the `skill/` guard sits in `DiscoverSkills`,
`GetPluginSkillPaths` and `GetPluginGroupings`, none of which any command in
`commands/` calls today — the discovery the CLI runs is
`DiscoverNodeModuleSkills`. The skills-side guard hardens a latent hazard rather
than closing a reachable hole.

`mdm skills remove` (`remove.go`) is scoped by `--harness`. It computes, BEFORE
deleting anything, which harnesses outside the filter still hold the skill, and
keeps the canonical directory and the lock entry when any do. That question
cannot be answered by probing each harness's directory the way the
agent-definition side does, because for a `SharedSkillsDir` harness that
directory IS the canonical one being decided about. With no filter every harness
is swept, not only the detected ones. Failed deletions are collected and reported
against the skill, and the lock entry survives them: the lock is the record of
what is on disk.

`mdm agents add` → `agent_artifacts.go` runs the same seven steps with
`agentfile/` in place of `skill/`, `agent_installer.go` in place of
`installer.go`, and the agents section of the lock. Two rules hold there
specifically:

- The name is sanitized ONCE, by `agentDiskName`, because it becomes both a
  file name and the lock key. If those two ever disagree, `mdm agents remove`
  drops the lock entry, finds nothing to delete, and leaves the definition live
  in the harness with no record of it.
- A harness with no agent concept is a **skip** with a printed reason, not a
  failure — but a run that installed nothing anywhere prints no success line and
  exits non-zero.

`mdm skills cherry-pick` → `cherrypick.go` reuses steps 1–3, then diverges:

4. The skill directory is copied into `./skills/<name>` — the project's own tree,
   not a harness's
5. `fork/` writes `.mdm-origin.json` (source, ref, commit, license, content hash)
   and `ATTRIBUTION.md`, and the upstream license text is copied in when the
   skill directory did not carry its own
6. Nothing is recorded upstream-wards: with `--install` the lock entry points at
   `./skills`, a *local* source, and `planUpdates` skips local sources - which is
   what keeps `mdm skills update` from overwriting a fork

That last point is the whole design. A fork is a file in the user's repository;
anything that re-fetches it would defeat the purpose of having forked it.

### Adding a new harness

Add an entry to `AllHarnesses` in `internal/harness/` with the harness's skills
dir path(s) and an optional `DetectInstalled()` function. If the harness also
loads subagent persona files, set `AgentsInstallDir` (and
`GlobalAgentsInstallDir`), plus `AgentFileSuffix` when it wants something other
than `.md` — GitHub Copilot CLI wants `.agent.md`, and writing a plain `.md`
there installs a file the harness silently never loads. Leaving those fields
empty is how a harness declares it has no agent concept, which every
agent-definition path treats as a skip.

Resolve paths through the helpers (`SkillsInstallDir`, `AgentsInstallDirFor`,
`CanonicalSkillsDir`, `CanonicalAgentsDir`, `UsesSharedSkillsDir`), not by
string-comparing `SkillsDir`. The TODO on `SkillsInstallDir` lists the older
callers that still build paths by hand — check a new layout against those too.

### Adding a new command

Create a file in `commands/`, define a `cobra.Command`, and register it either on the root command in `root.go` (for top-level commands like `upgrade`) or on the `skills` subcommand in `skills.go` (for skill management commands like `add`, `list`, etc.).

### Adding a new install path

Anything that writes third-party markdown into a place a model will read must
call the `hidden_scan.go` gate first and expose `--allow-hidden-chars`. The
README promises this for *every* install, and an install path that skips it
makes that claim false — most sharply for agent definitions, which exist to
become a persona the model adopts.

## Pre-PR Checklist

Before opening a pull request, run these three checks - they mirror what CI runs
and block merge on failure:

```bash
# 1. Tests
go test ./...

# 2. Vulnerability scan
# GOTOOLCHAIN pins the build of govulncheck to the project's Go (go.mod) so it
# can parse go1.26 sources even when your base toolchain is older.
GOTOOLCHAIN=go1.26.6 go install golang.org/x/vuln/cmd/govulncheck@v1.5.0
govulncheck ./...

# 3. Lint (formatting + cyclomatic complexity + vet)
# golangci-lint replaces the retired Go Report Card service and the previous
# standalone gofmt + gocyclo checks. Enabled linters live in .golangci.yml.
GOTOOLCHAIN=go1.26.6 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
golangci-lint run ./...
# Auto-fix formatting with: golangci-lint fmt
```

## Windows Executable Icon

The Windows binary embeds an icon and version metadata via a `.syso` file that the Go linker picks up automatically when `GOOS=windows`.

### How it works

1. **`assets/mdm.svg`** - the canonical icon source (block-M + downward arrow, black on white).
2. **`assets/mdm.ico`** - a committed multi-resolution ICO (16 / 32 / 48 / 64 / 128 / 256 px). Because it's committed, CI needs no external image tools.
3. **`assets/versioninfo.json`** - goversioninfo config: file description, product name, icon path.
4. **`resource_windows.syso`** - generated at build time by `make syso`; git-ignored. The `_windows` suffix is a Go build constraint so it's only linked into Windows targets.
5. **`tools/gen-icon/`** - pure-Go program that renders the SVG shapes at all six sizes (4× supersampled for anti-aliasing) and writes `assets/mdm.ico`. No external tools required.

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

`goversioninfo` is pinned in `go.mod` under the `tool` directive (Go 1.24+). `make syso` invokes it via `go tool goversioninfo` - no manual install required.

## Release Process

CI in `.github/workflows/release.yml` triggers on **tag pushes** matching `v*`. To release:

```bash
git tag v1.5.8
git push origin v1.5.8
```

GoReleaser builds binaries for Linux/macOS/Windows (x64 + ARM64), creates a GitHub release, and injects the tag as the version via ldflags. `internal/version/version.go` holds a `"dev"` fallback for `go install` users - do not bump it for releases, the tag is the source of truth.

Pre-releases work the same way: push a tag like `v1.6.0-rc.1` and GoReleaser marks the GitHub release as a prerelease automatically. `mdm upgrade` skips prereleases because GitHub's `/releases/latest` API excludes them.
