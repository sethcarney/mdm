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

| Field | Required | Description |
|---|---|---|
| `name` | Yes | Machine name used to reference the skill |
| `description` | Yes | Short description shown in listings and search results |

## Examples

```bash
# Create SKILL.md in the current directory
mdm skills init

# Create my-skill/SKILL.md
mdm skills init my-skill
```

## Publishing

Once you have written your skill:

1. Push the directory to a public GitHub repository.
2. Share it with `mdm skills add <owner>/<repo>`.
3. Optionally submit to the [skills.sh](https://skills.sh) registry so others can find it with `mdm skills find`.

A single repository can contain multiple skills — each in its own subdirectory with its own `SKILL.md`.
