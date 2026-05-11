# mdm skills update

Update installed skills to their latest versions.

## Usage

```
mdm skills update [skills...]
```

Re-fetches each skill from its recorded source and ref in the lock file. Updated skills are scanned for hidden Unicode characters before files are copied or symlinked.

Alias: `check`

## Up-to-date detection

| Source | Method |
|---|---|
| GitHub / GitLab / other git | Semver tag comparison via `git ls-remote --tags` — no clone needed |
| Local path | Always skipped — local skills stay in sync with the source code they live alongside |

Remote skills must be pinned to a semver tag (e.g. `#v1.2.0`) to use automatic update detection. mdm fetches all tags from the remote, finds the highest stable release, and upgrades if a newer one exists. Skills installed from a branch (e.g. `main`) are skipped with a prompt to pin to a tag instead.

Pre-release tags (e.g. `v2.0.0-beta.1`) are ignored by the update check. You can install a pre-release explicitly with `mdm skills add <source>#v2.0.0-beta.1`, but the update command only promotes to stable releases.

## Pinning to a version

Install a specific version by appending a `#tag` fragment to the source:

```bash
mdm skills add owner/repo#v1.2.0
```

When a newer stable tag is available, `mdm skills update` prints:

```
→ upgrading v1.2.0 → v1.3.0
```

and re-installs from the new tag automatically.

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
