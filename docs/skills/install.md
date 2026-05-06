# mdm skills install

Restore skills from `skills-lock.json`.

## Usage

```
mdm skills install
```

Reads the lock file and re-installs every recorded skill from its original source. Intended for CI pipelines and onboarding — run it after cloning a repo to get all skills back without having to remember each package source.

## How it works

mdm looks for skills in both the project lock (`skills-lock.json`) and the global lock (`~/.agents/skills-lock.json`):

| Situation | Behaviour |
|---|---|
| Only project lock has skills | Restores project skills silently |
| Only global lock has skills | Explains the situation and asks to confirm before restoring |
| Both locks have skills | Prompts you to choose which lock to restore from |
| Neither lock has skills | Prints a message and exits |

With `--yes`, the project lock is preferred and no prompts are shown.

Skills are re-installed by calling `mdm skills add` for each recorded source, grouped by origin so repos are only fetched once.

## Flags

| Flag | Description |
|---|---|
| `--yes, -y` | Skip prompts; default to project lock when both exist |

## Examples

```bash
# Restore all project skills after cloning a repo
mdm skills install

# CI — restore without any prompts
mdm skills install -y
```

## CI usage

Add `skills-lock.json` to version control, then restore in your CI setup:

```yaml
# GitHub Actions example
- name: Restore skills
  run: mdm skills install -y
```
