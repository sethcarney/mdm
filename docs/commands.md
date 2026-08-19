# Command reference

Every `mdm` command, subcommand, alias, and flag. For a narrative walkthrough of
any command, follow the "Details" links to the dedicated guide pages.

!!! info "Global flags"
    These persistent flags are available on **every** command:

    | Flag | Description |
    | --- | --- |
    | `--verbose`, `-v` | Print diagnostic steps to stderr |
    | `--help`, `-h` | Show help for the command |
    | `--version` | Print the `mdm` version (root command only) |

## Command tree

```text
mdm
├── upgrade                                 # Self-update the binary (aliases: update-cli, self-update)
├── uninstall                               # Remove the binary (alias: remove-cli)
├── doctor                                  # Health-check installed skills & project markdown
├── completion [bash|zsh|fish|powershell]   # Generate shell completion
│   └── install                             # Write completion into your shell rc
├── skills                                  # Manage skills for AI agents
│   ├── add <package>                       # Install a skill (alias: a)
│   ├── cherry-pick <source>                # Fork skills into ./skills as your own (aliases: fork, cp)
│   ├── remove [skills...]                  # Uninstall skills (aliases: rm, r)
│   ├── list                                # List installed skills (alias: ls)
│   ├── find [query]                        # Search skills.sh and install interactively (aliases: search, f, s)
│   ├── update [skills...]                  # Re-fetch from recorded source+ref (alias: check)
│   ├── audit [skills...]                   # Check for updates & security advisories
│   ├── init [name]                         # Scaffold a new SKILL.md
│   ├── install                             # Restore all skills from mdm-lock.json
│   └── sync                                # Sync skills from node_modules
├── knowledge                               # Manage OKF knowledge bundles
│   ├── add <source>                        # Install a bundle (alias: a)
│   ├── remove [bundles...]                 # Remove bundles (aliases: rm, r)
│   ├── list                                # List installed bundles (alias: ls)
│   ├── update [bundles...]                 # Re-fetch bundles
│   ├── validate [path]                     # Check OKF conformance & links
│   ├── init [name]                         # Scaffold a minimal bundle
│   └── install                             # Restore bundles from mdm-lock.json
├── plugins                                 # Manage Agent Plugins
│   ├── add <source>                        # Install a plugin, link skills, wire MCP (alias: a)
│   ├── remove [plugins...]                 # Remove plugins (aliases: rm, r)
│   ├── list                                # List installed plugins (alias: ls)
│   ├── update [plugins...]                 # Re-fetch plugins
│   ├── validate [path]                     # Check Agent Plugins conformance
│   ├── init [name]                         # Scaffold a minimal plugin
│   └── install                             # Restore plugins from mdm-lock.json
├── migrate                                 # Fold v1 lock files into mdm-lock.json / mdm-state.json
├── experimental                            # Manage experimental features
│   ├── list                                # Show features and status (alias: ls)
│   ├── enable <feature>                    # Persist an opt-in
│   └── disable <feature>                   # Remove a persisted opt-in
├── agents                                  # Manage the configured agent list
│   ├── list                                # Show configured agents (alias: ls)
│   ├── add [agents...]                     # Add agents (interactive with no args)
│   └── remove [agents...]                  # Remove agents & their unique files
└── rules                                   # Manage agent instruction files
    ├── link                                # Symlink instruction files to one AGENTS.md
    ├── status                              # Show which files exist/are symlinked/missing
    └── unlink                              # Remove symlinks, restore per-agent files
```

---

## `mdm skills`

Manage skills — reusable markdown prompt libraries — for your AI agents.

### `skills add` <small>alias: `a`</small>

Install a skill from GitHub, GitLab, a URL, or a local path.

```bash
mdm skills add <package>
```

`<package>` accepts GitHub shorthand (`owner/repo`), full GitHub/GitLab URLs, a
git URL with a `#ref`, a local path, or a well-known alias (`vercel`,
`anthropic`).

| Flag | Description |
| --- | --- |
| `--global`, `-g` | Install globally (user-level, `~/.agents/skills/`) |
| `--project`, `-p` | Force project-scope install |
| `--agent`, `-a` | Agents to install to (repeatable; `*` for all) |
| `--skill`, `-s` | Skill names to install (repeatable; `*` for all) |
| `--list`, `-l` | List available skills without installing |
| `--yes`, `-y` | Skip confirmation prompts |
| `--copy` | Copy files instead of symlinking |
| `--all` | Shorthand for `--skill '*' --agent '*' -y` |
| `--full-depth` | Search all subdirectories for skills |
| `--skip-audit` | Skip the security audit check for public skills |
| `--fail-on-audit` | Exit non-zero on security findings instead of prompting |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](skills/add.md)

### `skills cherry-pick` <small>aliases: `fork`, `cp`</small>

Fork third-party skills into `./skills` so you can edit them and ship them as
your own. Unlike `skills add`, nothing updates them afterwards — the copy is
yours, with its provenance and license recorded inside it.

```bash
mdm skills cherry-pick <source>
```

| Flag | Description |
| --- | --- |
| `--dir`, `-d` | Directory to fork into (default `skills`) |
| `--skill`, `-s` | Skill names to fork (repeatable; `*` for all) |
| `--as` | Rename the forked skill (single skill only) |
| `--install`, `-i` | Also install the forks into your agents |
| `--agent`, `-a` | Agents to install the forks to (implies `--install`) |
| `--force` | Replace an existing fork, discarding local edits |
| `--dry-run` | Show what would be forked without writing anything |
| `--list`, `-l` | List the skills available at the source without forking |
| `--status` | Show this project's forks and whether they have been edited |
| `--no-attribution` | Do not write `ATTRIBUTION.md` |

[:octicons-arrow-right-24: Details](skills/cherry-pick.md)

### `skills remove` <small>aliases: `rm`, `r`</small>

Uninstall one or more skills.

```bash
mdm skills remove [skills...]
```

| Flag | Description |
| --- | --- |
| `--global`, `-g` | Remove from global scope |
| `--agent`, `-a` | Remove from specific agents (repeatable) |
| `--skill`, `-s` | Skill names to remove (repeatable) |
| `--yes`, `-y` | Skip confirmation prompts |
| `--all` | Shorthand for `--skill '*' --agent '*' -y` |

[:octicons-arrow-right-24: Details](skills/remove.md)

### `skills list` <small>alias: `ls`</small>

List installed skills.

```bash
mdm skills list
```

| Flag | Description |
| --- | --- |
| `--global`, `-g` | List global skills |
| `--project`, `-p` | List project skills |
| `--json` | Output as JSON |

[:octicons-arrow-right-24: Details](skills/list.md)

### `skills find` <small>aliases: `search`, `f`, `s`</small>

Search the [skills.sh](https://skills.sh) registry and install interactively.

```bash
mdm skills find [query]
```

| Flag | Description |
| --- | --- |
| `--json` | Output results as JSON without installing |
| `--source` | List skills available at a remote source without installing |

[:octicons-arrow-right-24: Details](skills/find.md)

### `skills update` <small>alias: `check`</small>

Re-fetch skills from their recorded source and ref in the lock file.

```bash
mdm skills update [skills...]
```

| Flag | Description |
| --- | --- |
| `--global`, `-g` | Update global skills only |
| `--project`, `-p` | Update project skills only |
| `--yes`, `-y` | Skip the scope prompt |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](skills/update.md)

### `skills audit`

Check installed skills for available updates and OSV security advisories.

```bash
mdm skills audit [skills...]
```

| Flag | Description |
| --- | --- |
| `--global`, `-g` | Audit global skills only |
| `--project`, `-p` | Audit project skills only |
| `--json` | Output as JSON |
| `--source`, `-r` | Pre-install audit: `owner/repo` or URL to audit before installing |
| `--skill`, `-s` | Skill name to audit (use with `--source`) |

[:octicons-arrow-right-24: Details](skills/audit.md)

### `skills init`

Scaffold a new `SKILL.md` in the current directory.

```bash
mdm skills init [name]
```

[:octicons-arrow-right-24: Details](skills/init.md)

### `skills install`

Restore all skills from `mdm-lock.json` — ideal for CI and onboarding.

```bash
mdm skills install
```

| Flag | Description |
| --- | --- |
| `--yes`, `-y` | Skip confirmation prompts |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](skills/install.md)

### `skills sync`

Sync skills from `node_modules` into each agent's skill directories.

```bash
mdm skills sync
```

| Flag | Description |
| --- | --- |
| `--yes`, `-y` | Skip confirmation prompts |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](skills/sync.md)

---

## `mdm rules`

Manage agent instruction files (`CLAUDE.md`, `AGENTS.md`, `.cursorrules`, and
friends) by pointing them all at a single source of truth.

| Command | Description |
| --- | --- |
| `rules link` | Symlink all agent instruction files to one `AGENTS.md` |
| `rules status` | Show which instruction files exist, are symlinked, or missing |
| `rules unlink` | Remove symlinks and restore per-agent instruction files |

| Flag (on `link` / `status` / `unlink`) | Description |
| --- | --- |
| `--agent`, `-a` | Limit to specific agents (repeatable) |
| `--json` | Output status as a JSON array (`status`) |

[:octicons-arrow-right-24: Details](rules.md)

---

## `mdm agents`

Manage the configured agent list used as default install targets. Works at both
project and global scope.

| Command | Description |
| --- | --- |
| `agents list` <small>(`ls`)</small> | Show configured agents for the current scope |
| `agents add [agents...]` | Add agents (interactive picker with no args) |
| `agents remove [agents...]` | Remove agents and their unique skill/instruction files |

| Flag | Applies to | Description |
| --- | --- | --- |
| `--global`, `-g` | all | Operate on the global configured-agent list |
| `--json` | `list` | Output as JSON |
| `--available` | `list` | List all agents known to mdm, not just configured ones |
| `--yes`, `-y` | `remove` | Skip confirmation prompts |

[:octicons-arrow-right-24: Details](agents.md)

---

## `mdm knowledge`

Manage Open Knowledge Format (OKF) bundles.

| Command | Description |
| --- | --- |
| `knowledge add <source>` <small>(`a`)</small> | Install a bundle into `./knowledge/` and record it |
| `knowledge remove [bundles...]` <small>(`rm`, `r`)</small> | Remove bundles and their lock entries |
| `knowledge list` <small>(`ls`)</small> | List installed bundles |
| `knowledge update [bundles...]` | Re-fetch bundles from their recorded source+ref |
| `knowledge validate [path]` | Check OKF conformance and link integrity |
| `knowledge init [name]` | Scaffold a minimal conformant bundle |
| `knowledge install` | Restore all bundles from `mdm-lock.json` |

| Flag | Applies to | Description |
| --- | --- | --- |
| `--dir` | `add` | Directory to install bundles into (relative to project root) |
| `--bundle`, `-b` | `add` | Bundle names to install (repeatable; `*` for all) |
| `--yes`, `-y` | `remove` | Skip confirmation prompts |
| `--json` | `validate` | Print the validation report as JSON |
| `--allow-hidden-chars` | `add` / `update` / `install` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](specs/knowledge.md)

---

## `mdm plugins`

Manage Agent Plugins — portable packages of skills and MCP servers following
the vendor-neutral [agent-plugins.org](https://agent-plugins.org) standard.

| Command | Description |
| --- | --- |
| `plugins add <source>` <small>(`a`)</small> | Install a plugin into `.agents/plugins/`, link its skills, wire its MCP servers |
| `plugins remove [plugins...]` <small>(`rm`, `r`)</small> | Unwire MCP, unlink skills, delete the plugin and its lock entry |
| `plugins list` <small>(`ls`)</small> | List installed plugins |
| `plugins update [plugins...]` | Re-fetch plugins from their recorded source+ref (preserves the data dir) |
| `plugins validate [path]` | Check Agent Plugins spec conformance |
| `plugins init [name]` | Scaffold a minimal conformant plugin |
| `plugins install` | Restore all plugins from `mdm-lock.json` |

| Flag | Applies to | Description |
| --- | --- | --- |
| `--plugin`, `-p` | `add` | Plugin names to install (repeatable; `*` for all) |
| `--agent`, `-a` | `add` | Agents to install for (repeatable) |
| `--skip-mcp` | `add` / `update` / `install` | Install skills only; do not write MCP config |
| `--dry-run` | `add` | Show what would be installed without writing anything |
| `--purge-data` | `remove` | Also delete the plugin's persistent data directory |
| `--with-mcp` | `init` | Also scaffold an example `mcp.json` |
| `--yes`, `-y` | `add` / `remove` | Skip confirmation prompts |
| `--json` | `validate` | Print the validation report as JSON |
| `--allow-hidden-chars` | `add` / `update` / `install` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](specs/plugins.md)

---

## `mdm experimental`

Toggle experimental feature gates. Features can also be enabled via the
`MDM_EXPERIMENTAL` environment variable (comma-separated names, or `all`).

| Command | Description |
| --- | --- |
| `experimental list` <small>(`ls`)</small> | Show experimental features and their status |
| `experimental enable <feature>` | Persist an opt-in |
| `experimental disable <feature>` | Remove a persisted opt-in |

This release ships no experimental features — `knowledge` and `plugins`
graduated to full support in v2.

[:octicons-arrow-right-24: Details](experimental.md)

---

## `mdm migrate`

Fold the v1 lock files into the v2 layout: `skills-lock.json`,
`knowledge-lock.json`, and `plugins-lock.json` become one `mdm-lock.json`
at the project root, and the global `~/.agents/skills-lock.json` becomes
`~/.agents/mdm-state.json`.

```bash
mdm migrate --dry-run   # show the plan
mdm migrate -y          # migrate without prompting
```

`skills-lock.json` is replaced with a tombstone that points v1 users at
`mdm-lock.json`; commit the new lock and the removals together. v2 reads
the v1 files transparently until you migrate, but only ever writes the new
ones, and `mdm doctor` flags projects that still carry v1 files.

| Flag | Description |
| --- | --- |
| `--dry-run` | Show what would be migrated without changing anything |
| `--yes`, `-y` | Skip the confirmation prompt |
| `--no-tombstone` | Delete `skills-lock.json` instead of leaving a tombstone |
| `--force` | Discard legacy entries missing from an existing `mdm-lock.json` (they are listed first) |

---

## `mdm doctor`

Check installed skills and project markdown for health issues — broken symlinks,
hash mismatches, missing READMEs, and oversized markdown files.

```bash
mdm doctor
```

| Flag | Description |
| --- | --- |
| `--global`, `-g` | Check global skills only |
| `--project`, `-p` | Check project skills only |

[:octicons-arrow-right-24: Details](doctor.md)

---

## `mdm upgrade` <small>aliases: `update-cli`, `self-update`</small>

Download and replace the `mdm` binary from GitHub releases.

```bash
mdm upgrade
```

| Flag | Description |
| --- | --- |
| `--beta` | Upgrade to the latest beta/prerelease version |
| `--stable` | Upgrade to the latest stable version (default) |

[:octicons-arrow-right-24: Details](upgrade.md)

---

## `mdm uninstall` <small>alias: `remove-cli`</small>

Remove the `mdm` binary from your system.

```bash
mdm uninstall
```

| Flag | Description |
| --- | --- |
| `--yes`, `-y` | Skip the confirmation prompt |

[:octicons-arrow-right-24: Details](uninstall.md)

---

## `mdm completion`

Generate a shell completion script.

```bash
mdm completion [bash|zsh|fish|powershell]
mdm completion install     # write completion into your shell rc file
```
