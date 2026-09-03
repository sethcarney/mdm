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
5. Markdown files are scanned for hidden Unicode characters.
6. Skill directories are copied into each agent's skills directory.
7. The installation is recorded in `mdm.lock`.

## Flags

| Flag            | Description                                          |
| --------------- | ---------------------------------------------------- |
| `--global, -g`  | Install globally (user-level, `~/.agents/skills/`)   |
| `--project, -p` | Force project-scope install                          |
| `--agent, -a`   | Agents to install to (repeatable; use `*` for all)   |
| `--skill, -s`   | Skill names to install (repeatable; use `*` for all) |
| `--list, -l`    | List available skills without installing             |
| `--yes, -y`     | Skip all confirmation prompts                        |
| `--copy`        | Copy files instead of symlinking; switches the scope to copy mode |
| `--symlink`     | Symlink files from `.agents/skills` (the default); switches a scope back from copy mode |
| `--all`         | Shorthand for `--skill '*' --agent '*' -y`           |
| `--full-depth`  | Search all subdirectories for SKILL.md files         |
| `--skip-audit`  | Skip the security audit check                        |
| `--fail-on-audit` | Exit non-zero when security findings are detected instead of prompting (CI-friendly) |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

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

The install mode is a scope-wide switch, not a per-skill one. Passing `--copy` records `installMode: copy` in the scope's lock, so later installs, updates, and restores in that scope copy without repeating the flag. Passing `--symlink` switches the scope back: it records symlink mode and turns the copied installs back into links, so there is no need to edit the lock by hand. Symlink is the default, so `--symlink` on a scope that has never been switched changes nothing. The two flags cannot be combined. Switching a scope that already has installs re-materializes them into the new mode instead of leaving a mix, and reports how many it converted; the flag itself is the consent, so there is no extra confirmation. The switch is applied only once everything that could still stop the install has passed: the agent selection and the security-audit confirmation. Backing out at either of them leaves the install mode and the skills untouched, though an interactive agent picker already saves your selection to `configuredAgents` before the audit gate, so that part of the record can persist even when you decline it.

The conversion covers every agent directory the scope supports, not just the agents recorded in `configuredAgents`. That list holds only what you last picked in the interactive agent picker, so an agent you installed to with `-a <agent> -y` is converted along with the rest rather than being left behind on symlinks.

Only what mdm installed is converted. Switching to copy mode converts the symlinks pointing into the scope's `.agents/skills` directory; a symlink you put in an agent's skills directory yourself, pointing somewhere else, is left exactly as it is. The `.agents/skills` copy each converted link pointed at also stays: agents that read that shared directory install into it in copy mode too, so it keeps being refreshed. Switching back to symlink mode converts the real directories that hold a `SKILL.md`, creating the `.agents/skills` copy first when a copy install never wrote one; a directory without a `SKILL.md` is not an mdm install and is left alone.

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

## Installing vs forking

`add` keeps a skill in sync with its author: `mdm skills update` re-fetches it
from the recorded source and ref, replacing whatever is on disk. That makes it
the wrong command for a skill you intend to *change* — your edits are gone at
the next update.

To take a third-party skill and build on it, use
[`mdm skills cherry-pick`](cherry-pick.md) instead. It copies the skill into
`./skills` as part of your own repository, records where it came from and under
what license, and is deliberately left alone by `mdm skills update`.

## Security audit

When installing public skills from GitHub, mdm checks the skills.sh registry for any known security advisories. If an advisory is found you are shown the details and asked to confirm before proceeding. Pass `--skip-audit` to disable this check.

## Hidden character scan

Before installing, mdm scans all markdown files in the selected skill for hidden Unicode characters used in prompt-smuggling attacks, including Unicode tags, bidirectional controls, zero-width characters, variation selectors, and soft hyphens. Findings block installation even with `--yes`. Pass `--allow-hidden-chars` to continue intentionally.
