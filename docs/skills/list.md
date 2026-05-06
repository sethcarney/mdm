# mdm skills list

List installed skills.

## Usage

```
mdm skills list
```

Skills are grouped by scope (project then global) and show the agents they are installed to and the path on disk.

## Output

```
Project skills:

  my-skill  A brief description
    agents: Claude Code, Cursor
    .agents/skills/my-skill

  another-skill
    agents: Claude Code
    .claude/skills/another-skill

Global skills:

  shared-skill  Shared across all projects
    agents: Claude Code, Cursor, Windsurf
    ~/.agents/skills/shared-skill
```

After the list is printed, press `s` to expand a detail view showing the first few lines of content from each skill's `SKILL.md`.

## Flags

| Flag | Description |
|---|---|
| `--global, -g` | List global skills only |
| `--project, -p` | List project skills only |
| `--agent, -a` | Filter by agent name (repeatable) |
| `--json` | Output as JSON |

## Examples

```bash
# List all installed skills (project + global)
mdm skills list

# List only global skills
mdm skills list -g

# Filter to skills installed for Claude Code
mdm skills list -a claude-code

# Machine-readable JSON output
mdm skills list --json
```

## JSON output

With `--json`, each skill entry includes:

```json
[
  {
    "name": "my-skill",
    "description": "A brief description",
    "scope": "project",
    "path": "/home/user/project/.agents/skills/my-skill",
    "agents": ["claude-code", "cursor"]
  }
]
```
