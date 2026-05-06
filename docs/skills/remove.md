# mdm skills remove

Remove installed skills.

## Usage

```
mdm skills remove [skills...]
```

Without arguments, an interactive multiselect lets you choose which skills to remove. With skill names provided, those skills are removed directly (with a confirmation prompt unless `--yes` is passed).

## Flags

| Flag           | Description                                   |
| -------------- | --------------------------------------------- |
| `--global, -g` | Remove from global scope                      |
| `--agent, -a`  | Remove from specific agents only (repeatable) |
| `--skill, -s`  | Skill names to remove (repeatable)            |
| `--yes, -y`    | Skip confirmation prompts                     |
| `--all`        | Shorthand for `--skill '*' --agent '*' -y`    |

## Examples

```bash
# Interactive — pick scope, then pick skills to remove
mdm skills remove

# Remove a specific skill (prompts for scope)
mdm skills remove my-skill

# Remove multiple skills without prompting
mdm skills remove skill1 skill2 -y

# Remove a globally installed skill
mdm skills remove --global my-skill

# Remove all installed skills
mdm skills remove --all
```

## What gets removed

For each skill, mdm removes:

- The canonical skill directory (`.agents/skills/<skill>` for project, `~/.agents/skills/<skill>` for global).
- Any agent-specific symlinks or copies (e.g. `.claude/skills/<skill>`, `~/.cursor/skills/<skill>`).
- The entry in `skills-lock.json`.

If `--agent` is provided, only that agent's symlink or copy is removed — the canonical directory and other agent links remain.

## Orphan cleanup

When `mdm skills remove` finds no installed skills to remove, it automatically scans the lock file for entries whose directories no longer exist on disk and cleans them up. This handles cases where skill files were deleted manually.
