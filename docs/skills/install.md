# mdm skills install

Restore skills from `mdm.lock`.

## Usage

```
mdm skills install
```

Reads the lock file and re-installs every recorded skill from its original source, then restores any [agent definitions](../agent-artifacts.md) recorded in the same lock. Intended for CI pipelines and onboarding - run it after cloning a repo to get everything back without having to remember each package source.

## How it works

mdm looks for skills in both the project lock (`mdm.lock`) and the global state file (`~/.agents/mdm-state.json`):

| Situation | Behavior |
|---|---|
| Only project lock has skills | Restores project skills silently |
| Only global lock has skills | Explains the situation and asks to confirm before restoring |
| Both locks have skills | Prompts you to choose which lock to restore from |
| Neither lock has skills | Prints a message and exits |

With `--yes`, the project lock is preferred and no prompts are shown.

Skills are re-installed by calling `mdm skills add` for each recorded source, grouped by origin so repos are only fetched once.
Each restored skill is scanned for hidden Unicode characters before files are copied or symlinked.

Restores happen in whatever mode the scope's lock records. A project installed with `--copy` restores as real files, not symlinks. Pass `--copy` or `--symlink` to switch the scope's mode as part of the restore, the same as on [`mdm skills add`](add.md).

Agent definitions restore after skills, using the same scope and lock resolution - a plain `mdm skills install` (or the `postCreateCommand: mdm skills install` pattern in a dev container) is enough to get both back. A project with no agent definitions produces no extra output for that step. To restore agent definitions only, use [`mdm agents install`](../agent-artifacts.md#mdm-agents-install) directly.

## Flags

| Flag | Description |
|---|---|
| `--yes, -y` | Skip prompts; default to project lock when both exist |
| `--copy` | Copy files instead of symlinking; switches the scope to copy mode |
| `--symlink` | Symlink files from `.agents/skills` (the default); switches a scope back from copy mode |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |

## Examples

```bash
# Restore all project skills after cloning a repo
mdm skills install

# CI - restore without any prompts
mdm skills install -y

# Restore even if a skill intentionally contains hidden characters
mdm skills install -y --allow-hidden-chars
```

## CI usage

Add `mdm.lock` to version control, then restore in your CI setup:

```yaml
# GitHub Actions example
- name: Restore skills
  run: mdm skills install -y
```
