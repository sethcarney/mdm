# mdm doctor

Check the health of everything mdm installs, plus your project markdown.

`mdm doctor` runs a series of local checks and prints a report grouped by category. It covers skill, agent-definition, knowledge-bundle, and plugin installation integrity, harness symlinks, leftover v1 lock files, and any markdown files large enough to strain harness context windows - including instruction files, skill content, and general project docs.

Sections appear only when they have something to report, so a project with no bundles or plugins sees the same output it always did.

## Checks performed

### Skills

For each skill recorded in the lock file:

| Check                      | What it catches                                                                 |
| -------------------------- | ------------------------------------------------------------------------------- |
| Directory exists           | Skill was deleted from disk after install                                       |
| SKILL.md present and valid | Missing file or frontmatter without `name`/`description`                        |
| Symlinks resolve           | Harness-specific links (e.g. `.claude/skills/my-skill`) point to a missing target |
| Hash matches lock          | Skill files were modified manually since install                                |
| Markdown file sizes        | `.md` files inside the skill directory are too large                            |

### Agent definitions

For each [agent definition](agent-artifacts.md) recorded in the lock file: whether its canonical file (`.agents/agents/<name>.md`, or `.toml` for a TOML source) still exists, and whether every harness it is installed in has a healthy copy - distinguishing a broken symlink in a harness (target missing; run `mdm agents update <name>`) from the definition not being installed in any harness at all (run `mdm agents install`).

### Knowledge bundles

For each [knowledge bundle](knowledge.md) recorded in the `knowledge` section of `mdm.lock`:

| Check | What it catches |
| --- | --- |
| Bundle directory exists | Bundle was deleted from disk after install - run `mdm knowledge install` |
| Content hash matches lock | Bundle files were modified since install - run `mdm knowledge update <name>` |
| OKF conformance | The bundle no longer validates - run `mdm knowledge validate ./knowledge/<name>` |

### Plugins

For each [plugin](plugins.md) recorded in the `plugins` section of `mdm.lock`:

| Check | What it catches |
| --- | --- |
| Plugin directory exists | Plugin was deleted from disk after install - run `mdm plugins install` |
| `plugin.json` loads | A malformed or missing manifest - run `mdm plugins validate ./.agents/plugins/<name>` |
| Content hash matches lock | Plugin files were modified since install - run `mdm plugins update <name>` |
| Skill links intact | A linked skill is missing, or is now owned by a different plugin |
| MCP config in sync | A recorded server is missing from a harness's MCP config, or an mdm-managed server is in the config but not in the lock |
| Gitignore hint | A plugin data directory holding machine-local state that isn't gitignored |
| MCP portability hint | Wired MCP config carrying absolute paths that only resolve on this machine |

### Migration

Flags project and global state that still uses the v1 layout: a `skills-lock.json`, `knowledge-lock.json`, or `plugins-lock.json` that should be folded into `mdm.lock`, a global `~/.agents/skills-lock.json` that should become `mdm-state.json`, and a scope that is using an install mode it has not recorded. All of them are fixed by [`mdm migrate`](commands.md#mdm-migrate).

### Instruction files

Checks every known harness instruction file in the project root for size:
`CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `.windsurfrules`, `.clinerules`, `.roorules`, `GEMINI.md`, `.github/copilot-instructions.md`, and others.

### Rules linking

For each harness recorded in `configuredHarnesses` that has a unique instruction file (e.g. `CLAUDE.md`, `.windsurfrules`), checks whether that file exists and is symlinked to `AGENTS.md`. If not, warns you to run `mdm rules link`.

### Skill coverage

For each configured harness whose rules file is already linked, checks that every installed project skill has a corresponding entry in that harness's skills directory. Catches the case where you add a new harness via `mdm rules link` but haven't re-run `mdm skills add` to distribute existing skills to it.

### Project markdown

Walks the entire project tree and flags any other `.md` file that is too large. Skips directories already covered above and common noise directories (`.git`, `node_modules`, `vendor`, `dist`, `build`, `.next`, etc.). Stops after 10,000 filesystem entries to avoid hangs on very large repositories.

### Size thresholds

| Size      | Severity                                             |
| --------- | ------------------------------------------------------ |
| ≥ 64 KB   | Warning - may strain harness context windows         |
| ≥ 256 KB  | Error - likely too large for harness context windows |

Markdown is plain text at roughly 4 bytes per token, so these map to about
16K and 64K tokens - a nudge at ~12% of a 128K-token context window, an error
at roughly half of it.

## Output

```
Project skills:

  ✓ my-skill
    .agents/skills/my-skill

  ✗ broken-skill
    ✗ skill directory not found on disk - run `mdm skills install` to restore

  ▲ large-skill
    ▲ SKILL.md is 45KB - may strain harness context windows

Instruction files:

  ▲ CLAUDE.md is 32KB - may strain harness context windows

Rules linking:

  ▲ Windsurf (windsurf) is configured but .windsurfrules is missing - run `mdm rules link` to create it

Skill coverage:

  ▲ Cursor (cursor) is configured but skill "my-skill" is not installed for it - run `mdm skills add` to include it

Knowledge bundles:

  ▲ platform-docs: modified since install (content hash mismatch) - run `mdm knowledge update platform-docs` to re-fetch

Plugins:

  ✗ my-plugin: plugin directory ./.agents/plugins/my-plugin not found - run `mdm plugins install` to restore

Agent definitions:

  ▲ agent "code-reviewer" is not installed in any harness - run `mdm agents install` to restore
  ✗ agent "test-writer": broken symlink in Cursor - target missing, run `mdm agents update test-writer` to repair

Migration:

  ▲ skills-lock.json is a v1 lock file - fold it into mdm.lock with `mdm migrate`

Project markdown:

  ▲ docs/reference.md is 28KB - may strain harness context windows

Doctor complete: 3 skill(s) checked, project markdown scanned, 1 error(s), 4 warning(s)
```

Each skill shows a `✓` (ok), `▲` (warning), or `✗` (error). The summary line always prints, even when no skills are installed, so you know the checks ran.

## Commands

```
mdm doctor       Check project and global skills, plus all project markdown
mdm doctor -g    Check global skills only (skips project markdown scan)
mdm doctor -p    Check project skills and project markdown only
```

### Flags

| Flag            | Description                                    |
| --------------- | ---------------------------------------------- |
| `--global, -g`  | Check global skills only                       |
| `--project, -p` | Check project skills and project markdown only |

### Examples

```bash
# Full check - skills (both scopes) + all project markdown
mdm doctor

# Only check globally installed skills
mdm doctor -g

# Only check project skills and local markdown
mdm doctor -p
```

## Common issues and fixes

| Issue                                | Fix                                                                           |
| ------------------------------------ | ----------------------------------------------------------------------------- |
| Skill directory not found            | Run `mdm skills install` to restore from the lock file                        |
| Skill content modified               | Run `mdm skills update` to sync back to the source version                    |
| Broken symlink                       | Re-install the skill with `mdm skills add`                                    |
| Instruction file too large           | Split content into smaller files or remove outdated sections                  |
| Large project markdown                 | Trim the file or exclude it from harness context                                |
| Rules file missing or not linked       | Run `mdm rules link` to symlink the harness's instruction file to `AGENTS.md`   |
| Skill missing for a configured harness | Run `mdm skills add` and select the harness to distribute existing skills to it |
| Agent definition canonical file missing | Run `mdm agents install` to restore it                                        |
| Agent definition broken symlink in a harness | Run `mdm agents update <name>` to repair it                             |

## Exit code

`mdm doctor` exits `1` when any error-level issue is found, so it can gate
CI. Warnings alone exit `0`.
