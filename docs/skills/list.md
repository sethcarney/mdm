# mdm skills list

List installed skills.

## Usage

```
mdm skills list
```

Skills are grouped by scope (project then global) and show the harnesses they are installed to and the path on disk.

## Output

```
Project skills:

  my-skill  A brief description
    harnesses: Claude Code, Cursor
    .agents/skills/my-skill

  another-skill
    harnesses: Claude Code
    .claude/skills/another-skill

Global skills:

  shared-skill  Shared across all projects
    harnesses: Claude Code, Cursor, Windsurf
    ~/.agents/skills/shared-skill
```

After the list is printed, press `s` to expand a detail view showing the first few lines of content from each skill's `SKILL.md`.

## Flags

| Flag | Description |
|---|---|
| `--global, -g` | List global skills only |
| `--project, -p` | List project skills only |
| `--harness` | Filter by harness name (repeatable) |
| `--json` | Output as JSON |

## Examples

```bash
# List all installed skills (project + global)
mdm skills list

# List only global skills
mdm skills list -g

# Filter to skills installed for Claude Code
mdm skills list --harness claude-code

# Machine-readable JSON output
mdm skills list --json
```

## JSON output

With `--json`, each skill entry includes (field names are capitalized, matching Go's default JSON marshaling - the harness-name array keeps the key `Agents` for output-compatibility with earlier releases):

```json
[
  {
    "Name": "my-skill",
    "Description": "A brief description",
    "Scope": "project",
    "Path": "/home/user/project/.agents/skills/my-skill",
    "CanonicalPath": "/home/user/project/.agents/skills/my-skill",
    "Agents": ["claude-code", "cursor"]
  }
]
```
