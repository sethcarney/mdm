# mdm doctor

Check the health of installed skills and project markdown files.

`mdm doctor` runs a series of local checks and prints a report grouped by category. It covers skill installation integrity, agent symlinks, and any markdown files large enough to strain agent context windows — including instruction files, skill content, and general project docs.

## Checks performed

### Skills

For each skill recorded in the lock file:

| Check | What it catches |
|---|---|
| Directory exists | Skill was deleted from disk after install |
| SKILL.md present and valid | Missing file or frontmatter without `name`/`description` |
| Symlinks resolve | Agent-specific links (e.g. `.claude/skills/my-skill`) point to a missing target |
| Hash matches lock | Skill files were modified manually since install |
| Markdown file sizes | `.md` files inside the skill directory are too large |

### Instruction files

Checks every known agent instruction file in the project root for size:
`CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `.windsurfrules`, `.clinerules`, `.roorules`, `GEMINI.md`, `.github/copilot-instructions.md`, and others.

### Project markdown

Walks the entire project tree and flags any other `.md` file that is too large. Skips directories already covered above and common noise directories (`.git`, `node_modules`, `vendor`, `dist`, `build`, `.next`, etc.). Stops after 10,000 filesystem entries to avoid hangs on very large repositories.

### Size thresholds

| Size | Severity |
|---|---|
| ≥ 20 KB | Warning — may strain agent context windows |
| ≥ 100 KB | Error — likely too large for agent context windows |

## Output

```
Project skills:

  ✓ my-skill
    .agents/skills/my-skill

  ✗ broken-skill
    ✗ skill directory not found on disk — run `mdm skills install` to restore

  ▲ large-skill
    ▲ SKILL.md is 45KB — may strain agent context windows

Instruction files:

  ▲ CLAUDE.md is 32KB — may strain agent context windows

Project markdown:

  ▲ docs/reference.md is 28KB — may strain agent context windows

Doctor complete: 3 skill(s) checked, project markdown scanned, 1 error(s), 2 warning(s)
```

Each skill shows a `✓` (ok), `▲` (warning), or `✗` (error). The summary line always prints, even when no skills are installed, so you know the checks ran.

## Commands

```
mdm doctor       Check project and global skills, plus all project markdown
mdm doctor -g    Check global skills only (skips project markdown scan)
mdm doctor -p    Check project skills and project markdown only
```

### Flags

| Flag | Description |
|---|---|
| `--global, -g` | Check global skills only |
| `--project, -p` | Check project skills and project markdown only |

### Examples

```bash
# Full check — skills (both scopes) + all project markdown
mdm doctor

# Only check globally installed skills
mdm doctor -g

# Only check project skills and local markdown
mdm doctor -p
```

## Common issues and fixes

| Issue | Fix |
|---|---|
| Skill directory not found | Run `mdm skills install` to restore from the lock file |
| Skill content modified | Run `mdm skills update` to sync back to the source version |
| Broken symlink | Re-install the skill with `mdm skills add` |
| Instruction file too large | Split content into smaller files or remove outdated sections |
| Large project markdown | Trim the file or exclude it from agent context |
