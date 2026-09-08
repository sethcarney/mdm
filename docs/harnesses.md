# mdm harnesses

Manage the list of AI harnesses mdm should support by default.

A harness is the AI coding tool itself - Claude Code, Cursor, Windsurf, and so
on - as distinct from an [agent definition](agent-artifacts.md), which is a
single file (markdown, or TOML for Codex) installed *into* a harness. The configured harness list is the
single source of truth for which harnesses skills are installed to. It is
read whenever `mdm skills add` needs to know which harnesses to target and is
updated automatically when you pick harnesses interactively.

!!! note "Upgrading from an earlier v2 build"
    This command was `mdm agents`, and the flag that names a harness on the
    skills, plugins, rules and cherry-pick commands was `--agent` / `-a`. Both
    are now `--harness` and `mdm harnesses`; `mdm agents` installs agent
    definitions instead. There is no alias: `mdm agents add cursor` stops with
    a message pointing here rather than fetching a repository called `cursor`.

## Harness categories

Harnesses fall into three categories that determine whether they need explicit configuration:

| Category               | Description                                             | Needs tracking?                                   |
| ---------------------- | --------------------------------------------------------- | ------------------------------------------------- |
| **Shared skills dir**  | Uses `.agents/skills` - skills are auto-installed        | Only if they also have a unique instructions file |
| **Uses AGENTS.md**     | Reads `AGENTS.md` natively for instructions               | Only if they also have a unique skills dir         |
| **Both (no tracking)** | Shared skills dir + AGENTS.md (or no instructions file)   | Never - always supported automatically             |

Harnesses in the "both" category (Codex, Gemini CLI, Warp, Replit, etc.) appear as **always included** in every picker and are never added to `configuredHarnesses`. Harnesses with a unique skills directory or a non-AGENTS.md instructions file (Claude Code, Cursor, GitHub Copilot, etc.) must be explicitly configured.

## Why configure harnesses?

Without a configured list, `mdm skills add` prompts you to pick harnesses every time. Once you run `mdm harnesses add`, your preferred harnesses are pre-selected in every future install prompt - and `mdm skills add --yes` installs to exactly that list without prompting at all.

`mdm rules link` also updates `configuredHarnesses` automatically when you select harnesses interactively.

## Scopes

Harness lists are stored per scope alongside the skill lock file:

| Scope   | Storage                                |
| ------- | --------------------------------------- |
| Project | `mdm.lock` in the project root          |
| Global  | `~/.agents/mdm-state.json`              |

Use `--global` / `-g` to read and write the global list. The default is project scope.

## Commands

```
mdm harnesses list              Show configured harnesses for project scope
mdm harnesses list -g           Show configured harnesses for global scope
mdm harnesses add               Interactively pick harnesses to configure
mdm harnesses add <harnesses...> Add specific harnesses by name
mdm harnesses remove            Interactively remove harnesses
mdm harnesses remove <harnesses> Remove specific harnesses by name
```

## mdm harnesses list

Shows the configured harnesses for the chosen scope. Harnesses that are detected as installed on your machine are marked with a `✓`.

```
Project scope harnesses:

  Claude Code               ✓ installed
  Cursor                    ✓ installed
  Windsurf
```

If no harnesses are configured yet, the command tells you how to set them up.

### Flags

| Flag           | Description                                                                    |
| -------------- | -------------------------------------------------------------------------------- |
| `--global, -g` | List global configured harnesses                                                |
| `--available`  | List every harness mdm knows about, not just the ones in the configured list    |
| `--json`       | Output as a JSON array (name, displayName, scope, installed)                    |

## mdm harnesses add

With no arguments, opens a searchable multiselect. Harnesses that are always supported automatically (shared skills dir + AGENTS.md) are shown in a locked panel to the right of the prompt - they require no configuration and cannot be deselected. Your current configured list is pre-checked in the left panel. Confirming replaces the entire list with your selection.

```
Which harnesses do you want to configure?  │  always included:
  > filter...                              │  ◉ Codex
  ❯ ◉ Claude Code                         │  ◉ Gemini CLI
    ◉ Cursor                              │  ◉ Warp
    ○ Windsurf                            │  ...
    ○ Cline
  type to filter · space to toggle · enter to confirm
```

When called with harness names, those harnesses are appended to the existing list (duplicates are ignored).

```bash
# Interactive picker - replaces the current list
mdm harnesses add

# Append specific harnesses
mdm harnesses add claude-code cursor

# Configure global harnesses
mdm harnesses add --global claude-code
```

### Flags

| Flag           | Description                       |
| -------------- | ------------------------------------ |
| `--global, -g` | Add to global configured harnesses  |

## mdm harnesses remove

With no arguments, shows a multiselect of your currently configured harnesses with nothing pre-selected. Check the harnesses you want to remove, then confirm before any changes are made.

```
Which harnesses would you like to remove?
  > filter...
  ❯ ○ Claude Code
    ○ Cursor
    ○ Windsurf
  type to filter · space to toggle · enter to confirm

Remove 1 harness(es): Windsurf? [y/N]
```

After removing harnesses from the configured list, mdm also cleans up the files that belong exclusively to each removed harness:

- **Skills directory** - the harness's own skills folder (e.g. `.claude/skills/`, `.roo/skills/`) is removed if it exists. The shared `.agents/skills/` directory is never touched.

    !!! warning "OpenClaw's skills directory is `./skills/`"
        Removing OpenClaw deletes `./skills/` - the same directory many projects
        use for hand-written skills. mdm cannot tell your own skills from an
        OpenClaw install, so anything in there that is not a
        [cherry-picked fork](skills/cherry-pick.md) goes with it. Commit the
        directory first, or keep your skills elsewhere. See
        [Troubleshooting](troubleshooting.md#mdm-harnesses-remove-deleted-skills-i-wrote-by-hand).
- **Instructions file** - the harness's instructions file (e.g. `CLAUDE.md`, `.cursorrules`, `.github/copilot-instructions.md`) is removed. The shared `AGENTS.md` is never touched.

```bash
# Interactive removal
mdm harnesses remove

# Remove specific harnesses
mdm harnesses remove cursor

# Remove from global list
mdm harnesses remove --global cursor
```

### Flags

| Flag           | Description                             |
| -------------- | ------------------------------------------ |
| `--global, -g` | Remove from global configured harnesses   |
| `--yes, -y`    | Skip the confirmation prompt before removing files (use in CI / scripts) |

## Integration with mdm skills add

When `mdm skills add` needs to determine which harnesses to install to:

1. If `--harness` is passed explicitly, those harnesses are used.
2. If configured harnesses exist for the scope, they are used as the default selection (pre-checked in the picker, or used directly with `--yes`).
3. If no configured harnesses exist, the picker falls back to detected installed harnesses.

```bash
# Configure once
mdm harnesses add claude-code cursor

# Every subsequent install targets claude-code + cursor by default
mdm skills add vercel-labs/agent-skills
mdm skills add anthropics/skills --yes     # no prompt needed
```

## Harness names

Harness names are the machine names used with `--harness` flags across all commands. Run `mdm skills add --help` and look at `--harness` completions, or browse the list with `mdm harnesses add` (interactive picker shows all supported harnesses).

Common names: `claude-code`, `cursor`, `windsurf`, `cline`, `roo`, `github-copilot`, `gemini-cli`, `codex`, `opencode`.
