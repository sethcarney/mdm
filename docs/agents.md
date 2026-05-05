# mdm agents

Manage the list of AI agents mdm should support by default.

The configured agent list is the single source of truth for which agents skills are installed to. It is read whenever `mdm skills add` needs to know which agents to target and is updated automatically when you pick agents interactively.

## Agent categories

Agents fall into three categories that determine whether they need explicit configuration:

| Category               | Description                                             | Needs tracking?                                   |
| ---------------------- | ------------------------------------------------------- | ------------------------------------------------- |
| **Shared skills dir**  | Uses `.agents/skills` — skills are auto-installed       | Only if they also have a unique instructions file |
| **Uses AGENTS.md**     | Reads `AGENTS.md` natively for instructions             | Only if they also have a unique skills dir        |
| **Both (no tracking)** | Shared skills dir + AGENTS.md (or no instructions file) | Never — always supported automatically            |

Agents in the "both" category (Codex, Gemini CLI, Warp, Replit, etc.) appear as **always included** in every picker and are never added to `configuredAgents`. Agents with a unique skills directory or a non-AGENTS.md instructions file (Claude Code, Cursor, GitHub Copilot, etc.) must be explicitly configured.

## Why configure agents?

Without a configured list, `mdm skills add` prompts you to pick agents every time. Once you run `mdm agents add`, your preferred agents are pre-selected in every future install prompt — and `mdm skills add --yes` installs to exactly that list without prompting at all.

`mdm rules link` also updates `configuredAgents` automatically when you select agents interactively.

## Scopes

Agent lists are stored per scope alongside the skill lock file:

| Scope   | Storage                                |
| ------- | -------------------------------------- |
| Project | `skills-lock.json` in the project root |
| Global  | `~/.agents/skills-lock.json`           |

Use `--global` / `-g` to read and write the global list. The default is project scope.

## Commands

```
mdm agents list            Show configured agents for project scope
mdm agents list -g         Show configured agents for global scope
mdm agents add             Interactively pick agents to configure
mdm agents add <agents...> Add specific agents by name
mdm agents remove          Interactively remove agents
mdm agents remove <agents> Remove specific agents by name
```

## mdm agents list

Shows the configured agents for the chosen scope. Agents that are detected as installed on your machine are marked with a `✓`.

```
Project scope agents:

  Claude Code               ✓ installed
  Cursor                    ✓ installed
  Windsurf
```

If no agents are configured yet, the command tells you how to set them up.

### Flags

| Flag           | Description                   |
| -------------- | ----------------------------- |
| `--global, -g` | List global configured agents |

## mdm agents add

With no arguments, opens a searchable multiselect. Agents that are always supported automatically (shared skills dir + AGENTS.md) are shown in a locked panel to the right of the prompt — they require no configuration and cannot be deselected. Your current configured list is pre-checked in the left panel. Confirming replaces the entire list with your selection.

```
Which agents do you want to configure?  │  always included:
  > filter...                           │  ◉ Codex
  ❯ ◉ Claude Code                      │  ◉ Gemini CLI
    ◉ Cursor                           │  ◉ Warp
    ○ Windsurf                         │  ...
    ○ Cline
  type to filter · space to toggle · enter to confirm
```

When called with agent names, those agents are appended to the existing list (duplicates are ignored).

```bash
# Interactive picker — replaces the current list
mdm agents add

# Append specific agents
mdm agents add claude-code cursor

# Configure global agents
mdm agents add --global claude-code
```

### Flags

| Flag           | Description                     |
| -------------- | ------------------------------- |
| `--global, -g` | Add to global configured agents |

## mdm agents remove

With no arguments, shows a multiselect of your currently configured agents with nothing pre-selected. Check the agents you want to remove, then confirm before any changes are made.

```
Which agents would you like to remove?
  > filter...
  ❯ ○ Claude Code
    ○ Cursor
    ○ Windsurf
  type to filter · space to toggle · enter to confirm

Remove 1 agent(s): Windsurf? [y/N]
```

After removing agents from the configured list, mdm also cleans up the files that belong exclusively to each removed agent:

- **Skills directory** — the agent's own skills folder (e.g. `.claude/skills/`, `.cursor/skills/`) is removed if it exists. The shared `.agents/skills/` directory is never touched.
- **Instructions file** — the agent's instructions file (e.g. `CLAUDE.md`, `.cursorrules`, `.github/copilot-instructions.md`) is removed. The shared `AGENTS.md` is never touched.

```bash
# Interactive removal
mdm agents remove

# Remove specific agents
mdm agents remove cursor

# Remove from global list
mdm agents remove --global cursor
```

### Flags

| Flag           | Description                          |
| -------------- | ------------------------------------ |
| `--global, -g` | Remove from global configured agents |

## Integration with mdm skills add

When `mdm skills add` needs to determine which agents to install to:

1. If `--agent` is passed explicitly, those agents are used.
2. If configured agents exist for the scope, they are used as the default selection (pre-checked in the picker, or used directly with `--yes`).
3. If no configured agents exist, the picker falls back to detected installed agents.

```bash
# Configure once
mdm agents add claude-code cursor

# Every subsequent install targets claude-code + cursor by default
mdm skills add vercel-labs/agent-skills
mdm skills add anthropics/skills --yes     # no prompt needed
```

## Agent names

Agent names are the machine names used with `--agent` flags across all commands. Run `mdm skills add --help` and look at `--agent` completions, or browse the list with `mdm agents add` (interactive picker shows all supported agents).

Common names: `claude-code`, `cursor`, `windsurf`, `cline`, `roo`, `github-copilot`, `gemini-cli`, `codex`, `opencode`.
