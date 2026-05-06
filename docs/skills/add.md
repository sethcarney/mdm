# mdm skills add

Install a skill from GitHub, GitLab, a URL, or a local path.

## Usage

```
mdm skills add <package>
```

`<package>` can be any of:

| Format           | Example                              |
| ---------------- | ------------------------------------ |
| GitHub shorthand | `owner/repo`                         |
| Full GitHub URL  | `https://github.com/owner/repo`      |
| GitLab URL       | `https://gitlab.com/owner/repo`      |
| Git URL with ref | `https://github.com/owner/repo#main` |
| Local path       | `./my-local-skill`                   |
| Well-known alias | `vercel`, `anthropic`                |

## Install flow

1. The source is fetched (shallow clone or GitHub API tree query).
2. `SKILL.md` files inside the repo are discovered.
3. If the repo contains multiple skills, a picker lets you choose which ones to install.
4. You are prompted for scope (project or global) and which agents to install to — unless flags are provided.
5. Skill directories are copied into each agent's skills directory.
6. The installation is recorded in `skills-lock.json`.

## Flags

| Flag            | Description                                          |
| --------------- | ---------------------------------------------------- |
| `--global, -g`  | Install globally (user-level, `~/.agents/skills/`)   |
| `--project, -p` | Force project-scope install                          |
| `--agent, -a`   | Agents to install to (repeatable; use `*` for all)   |
| `--skill, -s`   | Skill names to install (repeatable; use `*` for all) |
| `--list, -l`    | List available skills without installing             |
| `--yes, -y`     | Skip all confirmation prompts                        |
| `--copy`        | Copy files instead of symlinking                     |
| `--all`         | Shorthand for `--skill '*' --agent '*' -y`           |
| `--full-depth`  | Search all subdirectories for SKILL.md files         |
| `--skip-audit`  | Skip the security audit check                        |

The `--agent` and `--skill` flags accept multiple space-separated values after a single flag or can be repeated:

```bash
mdm skills add owner/repo -a claude-code cursor
mdm skills add owner/repo -a claude-code -a cursor   # equivalent
```

## Agent selection

The agent picker shows agents with unique skills directories in the left panel. Agents that are always auto-covered (shared `.agents/skills` directory) appear in a locked panel to the right — they are always installed to and cannot be deselected.

```
Which agents would you like to install to?  │  always included:
  > filter...                               │  ◉ Codex
  ❯ ● Claude Code                          │  ◉ Gemini CLI
    ○ Cursor                               │  ◉ Warp
    ○ Windsurf                             │  ...
  type to filter · space to toggle · enter to confirm
```

If you have a configured agent list (set via `mdm agents add` or `mdm rules link`), those agents are pre-checked. Otherwise agents detected as installed are pre-checked. Your selection is saved back to `configuredAgents` for future installs.

Agents that use the shared `.agents/skills` directory but also have a unique instruction file (such as GitHub Copilot, which uses `.github/copilot-instructions.md`) do not appear in the left panel — they are always included via the locked panel. If such an agent was previously configured via `mdm rules link`, it is preserved in `configuredAgents` even though it is not shown as a selectable option.

**Project scope** (default): skills are installed under `.agents/skills/` in the current directory. Each agent that has its own skills directory gets a symlink pointing to the shared location.

**Global scope** (`-g`): skills are installed under `~/.agents/skills/`. Agents with a global skills directory get a symlink to that shared location.

**Copy mode** (`--copy`): instead of symlinking from agent directories to `.agents/skills/`, files are copied directly. Use this if your tools don't follow symlinks.

## Examples

```bash
# Install interactively — prompts for scope, agents, and skill selection
mdm skills add vercel-labs/agent-skills

# Install a specific skill, skip prompts
mdm skills add vercel-labs/agent-skills --skill vercel-react-best-practices -y

# Install all skills globally to all agents
mdm skills add anthropics/skills --all -g

# Install from a specific branch
mdm skills add owner/repo#feat/my-branch

# Install from a local directory
mdm skills add ./my-skill

# List skills in a package without installing
mdm skills add vercel-labs/agent-skills --list

# Install to specific agents only
mdm skills add owner/repo -a claude-code cursor
```

## Security audit

When installing public skills from GitHub, mdm checks the skills.sh registry for any known security advisories. If an advisory is found you are shown the details and asked to confirm before proceeding. Pass `--skip-audit` to disable this check.
