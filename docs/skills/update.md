# mdm skills update

Update installed skills to their latest versions.

## Usage

```
mdm skills update [skills...]
```

Re-fetches each skill from its recorded source and ref in the lock file. Skills that are already up to date are skipped. Local skills (installed from a path rather than a git remote) are always skipped.
Updated skills are scanned for hidden Unicode characters before files are copied or symlinked.

Alias: `check`

## Up-to-date detection

mdm uses two methods to check whether a skill needs updating, in order:

| Method | When used |
|---|---|
| Skill folder hash | GitHub skills with a recorded `skillFolderHash` — only counts a change if the skill's own directory changed, not the rest of the repo |
| Commit SHA | All other git sources — compares the current remote HEAD (or ref) against the stored commit SHA |

If neither hash is recorded (e.g. an older install), the skill is always re-fetched to populate the hashes going forward.

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
