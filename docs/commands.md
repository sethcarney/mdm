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
│   ├── remove [skills...]                  # Uninstall skills (aliases: rm, r)
│   ├── list                                # List installed skills (alias: ls)
│   ├── find [query]                        # Search skills.sh and install interactively (aliases: search, f, s)
│   ├── update [skills...]                  # Re-fetch from recorded source+ref (alias: check)
│   ├── audit [skills...]                   # Check for updates & security advisories
│   ├── init [name]                         # Scaffold a new SKILL.md
│   ├── install                             # Restore all skills from skills-lock.json
│   └── sync                                # Sync skills from node_modules
├── knowledge                               # [experimental] Manage OKF knowledge bundles
│   ├── add <source>                        # Install a bundle (alias: a)
│   ├── remove [bundles...]                 # Remove bundles (aliases: rm, r)
│   ├── list                                # List installed bundles (alias: ls)
│   ├── update [bundles...]                 # Re-fetch bundles
│   ├── validate [path]                     # Check OKF conformance & links
│   ├── init [name]                         # Scaffold a minimal bundle
│   └── install                             # Restore bundles from knowledge-lock.json
├── experimental                            # Manage experimental features
│   ├── list                                # Show features and status (alias: ls)
│   ├── enable <feature>                    # Persist an opt-in
│   └── disable <feature>                   # Remove a persisted opt-in
├── agents                                  # Manage the configured agent list
│   ├── list                                # Show configured agents (alias: ls)
│   ├── add [agents...]                     # Add agents (interactive with no args)
│   └── remove [agents...]                  # Remove agents & their unique files
├── rules                                   # Manage agent instruction files
│   ├── link                                # Symlink instruction files to one AGENTS.md
│   ├── status                              # Show which files exist/are symlinked/missing
│   └── unlink                              # Remove symlinks, restore per-agent files
└── sandbox                                 # Harden agent sandboxes (secrets & workspace confinement)
    ├── setup                               # Apply the recommended baseline (aliases: init, apply)
    └── status                              # Check current config against the baseline
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

Restore all skills from `skills-lock.json` — ideal for CI and onboarding.

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

## `mdm sandbox`

Configure Claude Code, Codex, and GitHub Copilot CLI so they cannot read
secret files and stay confined to the project working directory. Each tool is
hardened through its own native mechanism; mdm only adds or tightens settings
and never removes entries you wrote yourself.

| Command | Description |
| --- | --- |
| `sandbox setup` <small>(`init`, `apply`)</small> | Apply the recommended sandbox baseline |
| `sandbox status` | Check current configuration against the baseline |

| Flag | Applies to | Description |
| --- | --- | --- |
| `--agent`, `-a` | all | Limit to specific agents (repeatable) |
| `--dry-run` | `setup` | Show what would change without writing anything |
| `--yes`, `-y` | `setup` | Apply without prompting (all supported agents unless `--agent` is given) |
| `--json` | `status` | Output status as a JSON array |

[:octicons-arrow-right-24: Details](sandbox.md)

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

## `mdm knowledge` <small>experimental</small>

Manage Open Knowledge Format (OKF) bundles. Hidden until enabled — see
[experimental features](experimental.md).

| Command | Description |
| --- | --- |
| `knowledge add <source>` <small>(`a`)</small> | Install a bundle into `./knowledge/` and record it |
| `knowledge remove [bundles...]` <small>(`rm`, `r`)</small> | Remove bundles and their lock entries |
| `knowledge list` <small>(`ls`)</small> | List installed bundles |
| `knowledge update [bundles...]` | Re-fetch bundles from their recorded source+ref |
| `knowledge validate [path]` | Check OKF conformance and link integrity |
| `knowledge init [name]` | Scaffold a minimal conformant bundle |
| `knowledge install` | Restore all bundles from `knowledge-lock.json` |

| Flag | Applies to | Description |
| --- | --- | --- |
| `--dir` | `add` | Directory to install bundles into (relative to project root) |
| `--bundle`, `-b` | `add` | Bundle names to install (repeatable; `*` for all) |
| `--yes`, `-y` | `remove` | Skip confirmation prompts |
| `--json` | `validate` | Print the validation report as JSON |
| `--allow-hidden-chars` | `add` / `update` / `install` | Allow markdown files with hidden Unicode characters |

[:octicons-arrow-right-24: Details](specs/knowledge.md)

---

## `mdm experimental`

Toggle experimental feature gates. Features can also be enabled via the
`MDM_EXPERIMENTAL` environment variable (comma-separated names, or `all`).

| Command | Description |
| --- | --- |
| `experimental list` <small>(`ls`)</small> | Show experimental features and their status |
| `experimental enable <feature>` | Persist an opt-in |
| `experimental disable <feature>` | Remove a persisted opt-in |

Currently available features:

| Feature | Description |
| --- | --- |
| `knowledge` | Manage OKF knowledge bundles (`mdm knowledge`) |

[:octicons-arrow-right-24: Details](experimental.md)

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
