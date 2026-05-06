# mdm skills sync

Sync skills from `node_modules` into agent skill directories.

## Usage

```
mdm skills sync
```

Scans `node_modules` in the current directory for packages that contain a `SKILL.md` file. Discovered skills are shown and you can select which ones to install, then choose a scope and target agents.

This is the workflow for skill packages distributed through npm, yarn, or pnpm — install the package normally, then run `mdm skills sync` to make it available to your AI tools.

## Flow

1. `node_modules` is scanned for `SKILL.md` files.
2. Found skills are listed with their names, descriptions, and paths.
3. A multiselect lets you pick which skills to sync (all pre-checked by default).
4. You choose a scope (project or global) and which agents to install to.
5. Skills are copied or symlinked into agent directories and recorded in the lock file.

```
Scanning node_modules for skills...

Found 2 skill(s) in node_modules:

  vercel-react-best-practices  React patterns from Vercel
    node_modules/@vercel/skills/vercel-react-best-practices

  typescript-strict  Strict TypeScript configuration
    node_modules/@vercel/skills/typescript-strict
```

## Flags

| Flag | Description |
|---|---|
| `--yes, -y` | Skip confirmation prompts; sync all found skills |

## Examples

```bash
# Install skill package and sync
npm install @vercel/agent-skills
mdm skills sync

# Sync without prompts
mdm skills sync -y
```

## Lock file

Synced skills are recorded in `skills-lock.json` with `sourceType: "local"` and a relative path to the `node_modules` directory. Running `mdm skills sync` again after an `npm install` update will re-sync with the new version.
