# mdm skills init

Scaffold a new skill in the current directory.

## Usage

```
mdm skills init [name]
```

Creates a `SKILL.md` file with the required frontmatter and a starter template. If a name is provided, the file is created inside a new `<name>/` subdirectory. Without a name, `SKILL.md` is created in the current directory using the directory name as the skill name.

## Generated file

```markdown
---
name: my-skill
description: A brief description of what this skill does
# compatibility:
#   some-library: ">=1.0.0"
---

# my-skill

Instructions for the agent to follow when this skill is activated.

## When to use

Describe when this skill should be used.

## Instructions

1. First step
2. Second step
3. Additional steps as needed
```

## Frontmatter fields

The `name` and `description` fields are required. See the [Anthropic SKILL.md docs](https://code.claude.com/docs/en/skills) for the full list of Claude Code frontmatter options (e.g. `allowed-tools`, `disable-model-invocation`, `model`).

| Field | Required | Description |
|---|---|---|
| `name` | Yes | Machine name used as the `/skill-name` slash command. Lowercase letters, numbers, and hyphens only (max 64 characters). |
| `description` | Yes | Short description shown in listings. Claude uses this to decide when to load the skill automatically — put the key use case first. |
| `compatibility` | No | Map of library/framework version constraints (e.g. `react: "^18"`). Display-only — mdm shows this in `mdm skills list`. Not used by Claude Code. |

## Examples

```bash
# Create SKILL.md in the current directory
mdm skills init

# Create my-skill/SKILL.md
mdm skills init my-skill
```

## Publishing and version tagging

Once you have written your skill:

1. Push the directory to a public GitHub repository.
2. Share it with `mdm skills add <owner>/<repo>`.
3. Optionally submit to the [skills.sh](https://skills.sh) registry so others can find it with `mdm skills find`.

A single repository can contain multiple skills — each in its own subdirectory with its own `SKILL.md`.

### Version tagging

Tag your repository with a semver tag (e.g. `v1.0.0`) so users can pin to a specific version and receive automatic update notifications:

```bash
git tag v1.0.0
git push origin v1.0.0
```

Users install pinned versions with:

```bash
mdm skills add owner/repo#v1.0.0
```

When a newer tag is published, `mdm skills update` detects the change and offers to upgrade.

### Declaring compatibility

Use the `compatibility:` frontmatter field to document which library or runtime versions your skill is designed for:

```yaml
---
name: my-react-skill
description: React component patterns for this project
compatibility:
  react: "^18"
  node: ">=20"
  typescript: ">=5.0"
---
```

This is informational only — mdm displays it in `mdm skills list` so developers know what environment the skill targets. Claude Code does not read or enforce this field.
