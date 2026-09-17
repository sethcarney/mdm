# mdm install

Restore everything `mdm.lock` records - skills, [agent definitions](agent-artifacts.md), [knowledge bundles](knowledge.md) and [plugins](plugins.md) - in one command.

## Usage

```
mdm install
```

Intended for CI pipelines and onboarding: run it after cloning a repo to get the whole project back without remembering each package source.

Each section is restored by the same code as its own command, so `mdm install` is equivalent to running all four in order, but with one scope decision and one exit code:

```bash
mdm skills install
mdm agents install
mdm knowledge install
mdm plugins install
```

To restore less than everything, use the per-section commands directly - [`mdm skills install`](skills/install.md) covers skills and agent definitions together. `mdm install` has no per-section flags.

## Scope

A `mdm.lock` in the working directory settles it: `mdm install` in a checkout restores that checkout, with no prompt.

| Situation | Behavior |
|---|---|
| `mdm.lock` in the working directory | Restores that project, no prompt |
| No `mdm.lock`, nothing recorded globally | Prints that there is nothing to restore, exits 0 |
| No `mdm.lock`, global state has entries | Explains, and asks before restoring globally |
| ...and `--yes` was passed | Refuses, and names `--global` as the flag that would allow it |

`--project` and `--global` choose explicitly and skip that resolution.

`--yes` never reaches global scope on its own. A CI step that meant to restore a checkout and found no lock would otherwise install into the runner's home directory and report success for it, so the implicit path stops and says what to pass.

Knowledge bundles and plugins are recorded in the project lock only. Under `--global` they are not restored, and a run standing in a project that has them says so rather than finishing silently.

## Order

Sections restore in this order, and the last step is load-bearing:

1. **Skills**
2. **Agent definitions**
3. **Knowledge bundles**
4. **Plugins**

Plugins go last because of how a name collision is reported. A plugin skill whose name matches a standalone one is refused by the plugin install with a warning naming both. In the reverse order the *skills* entry is refused instead, and that leaves a canonical directory on disk - the plugin's own symlink - which the restore's completeness check would count as a successful restore. Both orders are safe; only this one reports the collision honestly.

A section the lock does not record is skipped silently, so a skills-only project does not get three paragraphs explaining that it has no plugins.

## Exit code

A restore that could not install everything the lock describes exits non-zero, after finishing every entry it could install. Every section feeds the same flag, so one failed bundle fails the run even when all the skills restored.

This is what keeps CI from treating a half-provisioned checkout as a good one. See [local sources and team setup](skills/install.md#local-sources-and-team-setup) for the case that most often triggers it.

## Flags

| Flag | Description |
|---|---|
| `--yes, -y` | Skip confirmation prompts |
| `--project` | Restore the project's `mdm.lock` (the default when one is present) |
| `--global` | Restore the globally recorded skills and agent definitions |
| `--copy` | Copy files instead of symlinking; switches the scope to copy mode |
| `--symlink` | Symlink files from `.agents` (the default); switches a scope back from copy mode |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |
| `--skip-mcp` | Do not wire MCP server config for restored plugins |

`--copy`/`--symlink` and `--project`/`--global` are each one switch with two settings, so passing both of a pair is rejected.

## Examples

```bash
# Restore the whole project after cloning a repo
mdm install

# CI - restore without any prompts
mdm install -y

# Restore plugins without touching any harness's MCP config
mdm install -y --skip-mcp

# Convert the project to copy mode as part of the restore
mdm install -y --copy
```

## CI usage

Commit `mdm.lock`, then restore in your CI setup:

```yaml
# GitHub Actions example
- name: Restore skills, agent definitions, knowledge and plugins
  run: mdm install -y
```

In a dev container, pair the [mdm feature](https://github.com/sethcarney/mdm/tree/main/src/mdm) with a `postCreateCommand`, which runs after the workspace is mounted:

```jsonc
"features": {
  "ghcr.io/sethcarney/mdm/mdm:1": {}
},
"postCreateCommand": "mdm install -y"
```
