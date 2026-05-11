# mdm skills update

Update installed skills to their latest versions.

## Usage

```
mdm skills update [skills...]
```

Re-fetches each skill from its recorded source and ref in the lock file. Skills that are already up to date are skipped. Local skills compare the `version` field in the source `SKILL.md` against the value recorded at install time.
Updated skills are scanned for hidden Unicode characters before files are copied or symlinked.

Alias: `check`

## Up-to-date detection

| Source | Method |
|---|---|
| GitHub / GitLab / other git | Semver tag comparison via `git ls-remote --tags` — no clone needed. Must be pinned to a version tag (e.g. `#v1.2.0`). |
| Local path | `version` field in `SKILL.md` — bump it to trigger an update |

Remote skills use semver tag comparison: mdm fetches all tags from the remote and finds the latest stable release. If the installed tag is behind, the skill is upgraded automatically. Skills pinned to a branch (e.g. `main`) are skipped with a prompt to pin to a tag instead. Local skills have no remote to query, so version comparison is used: mdm reads the `version` field from the source `SKILL.md` and compares it to the value recorded at install time. If no version is present, the skill is skipped with a warning.

## Scope

Without `--global` or `--project`, a prompt asks which scope to update:

```
? Update which scope?
  ● Both (project and global)
    Project
    Global
```

With `--yes`, both scopes are updated without prompting.

## Flags

| Flag | Description |
|---|---|
| `--global, -g` | Update global skills only |
| `--project, -p` | Update project skills only |
| `--yes, -y` | Skip scope prompt, update both scopes |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

## Examples

```bash
# Update all skills (both scopes, with prompt)
mdm skills update

# Update a specific skill
mdm skills update my-skill

# Update only global skills
mdm skills update -g

# Update both scopes without prompting
mdm skills update -y

# Update even if a skill intentionally contains hidden characters
mdm skills update -y --allow-hidden-chars
```
