# mdm rules

Manage project-level instruction files for AI harnesses.

`AGENTS.md` is the universal source of truth. It is read natively by Codex CLI, Gemini CLI, OpenCode, and Replit. `mdm rules link` symlinks every other harness's instruction file — `CLAUDE.md`, `.cursorrules`, `.windsurfrules`, `.clinerules`, etc. — to `AGENTS.md` so every tool reads the same content from one place.

## Why AGENTS.md?

Many AI tools each look for a different file name in your project root:

| Tool              | File                              |
| ----------------- | --------------------------------- |
| Claude Code       | `CLAUDE.md`                       |
| Cursor            | `.cursorrules`                    |
| Windsurf          | `.windsurfrules`                  |
| Cline / Roo Code  | `.clinerules` / `.roorules`       |
| GitHub Copilot    | `.github/copilot-instructions.md` |
| Codex CLI         | `AGENTS.md`                       |
| Gemini CLI        | `GEMINI.md`                       |
| OpenCode / Replit | `AGENTS.md`                       |

Without a shared source, you end up copying the same instructions into multiple files and keeping them in sync by hand. `AGENTS.md` is designed as the universal compatibility file - use it as the single source and symlink everything else to it.

## Commands

```
mdm rules link     Set up AGENTS.md as source of truth and symlink harness files
mdm rules status   Show the state of all harness instruction files
mdm rules unlink   Remove symlinks created by mdm rules link
```

## mdm rules link

Interactive setup. Walks you through three steps:

### Step 1 - Find your current rules

The command scans your project for any known instruction files that already contain content. If it finds some, you are asked to pick which one is your current source of truth:

```
? Which file contains your current rules?
  CLAUDE.md          Claude Code · # Project Overview...
  .cursorrules       Cursor · # Rules for this project...
  None of these - start with an empty AGENTS.md
```

- If you select a file, its content is copied into `AGENTS.md`.
- If you select "None of these", an empty `AGENTS.md` is created for you to fill in.
- If `AGENTS.md` already has content, this step is skipped automatically.

### Step 2 - Select your tools

A searchable multiselect shows harnesses that have a unique instruction file. Harnesses that read `AGENTS.md` natively (Codex, Gemini CLI, OpenCode, Replit, etc.) are shown in a locked panel on the right — they need no symlinking and are always covered. Harnesses you have previously configured or that are detected as installed are pre-checked.

```
Which AI tools are you using in this project?  │  always included:
  > filter...                                  │  ◉ Codex
  ❯ ◉ Claude Code        CLAUDE.md            │  ◉ Gemini CLI
    ◉ Cursor             .cursorrules          │  ◉ OpenCode
    ○ Cline              .clinerules           │  ◉ Replit
    ○ Windsurf           .windsurfrules
  type to filter · space to toggle · enter to confirm
```

Selecting harnesses here also updates `configuredHarnesses` in `mdm.lock`, so subsequent `mdm skills add` commands default to the same set.

### Step 3 - Create symlinks

Each selected tool's instruction file is replaced with a symlink pointing to `AGENTS.md`. The file that was promoted in Step 1 is also replaced with a symlink (its content now lives in `AGENTS.md`).

```
  ✓ CLAUDE.md                          → AGENTS.md
  ✓ .cursorrules                        → AGENTS.md
  ✓ .windsurfrules                      → AGENTS.md

Linked 3 file(s) → AGENTS.md
```

Existing real files are replaced with symlinks only after per-file confirmation. Pass `-y` / `--yes` to skip all prompts.

### Flags

| Flag        | Description                                                                  |
| ----------- | -------------------------------------------------------------------------------- |
| `--harness` | Skip the tool-selection prompt and link specific harnesses only (repeatable) |
| `--yes, -y` | Replace real files without prompting                                        |

### Examples

```bash
# Interactive - scan, pick source, select tools, symlink
mdm rules link

# Link only Claude Code and Cursor (no prompt)
mdm rules link --harness claude-code cursor

# Replace any existing real files without asking
mdm rules link -y
```

## mdm rules status

Shows the current state of every known instruction file in the project.

```
  File                                   State        Details
  ────────────────────────────────────────────────────────────────────────
  .cursorrules                           linked        → AGENTS.md
  harnesses: Cursor

  .windsurfrules                         linked        → AGENTS.md
  harnesses: Windsurf

  AGENTS.md                              real file
  harnesses: Codex, OpenCode, Replit

  CLAUDE.md                              real file
  harnesses: Claude Code

  GEMINI.md                              missing
  harnesses: Gemini CLI
```

States:

| State       | Meaning                                |
| ----------- | -------------------------------------- |
| `linked`    | Symlink pointing to a file that exists |
| `real file` | A regular file (not a symlink)         |
| `missing`   | The file does not exist                |
| `broken`    | Symlink whose target is missing        |

### Flags

| Flag        | Description                                                                    |
| ----------- | ----------------------------------------------------------------------------------- |
| `--harness` | Limit the status report to the named harnesses (repeatable)                     |
| `--json`    | Output the status table as a JSON array (file, state, target, agents) for scripting |

```bash
# Only show status for Claude Code and Cursor
mdm rules status --harness claude-code cursor

# Machine-readable
mdm rules status --json
```

!!! note "The JSON field is still called `agents`"
    `mdm rules status --json` keeps the harness-name array under the key
    `"agents"` for output-compatibility with scripts written against earlier
    releases — even though the human-readable table above says `harnesses:`.
    This is a deliberate, narrow exception; it does not affect anything else
    documented on this page.

## mdm rules unlink

Removes symlinks created by `mdm rules link`. Real files are never touched.

With no arguments, shows a picker listing each symlinked instruction file (with its symlink target as a hint). Check the ones you want to remove, then confirm:

```
Which symlinks would you like to remove?
  > filter...
  ❯ ○ .cursorrules          → AGENTS.md
    ○ .windsurfrules        → AGENTS.md
    ○ CLAUDE.md             → AGENTS.md
  type to filter · space to toggle · enter to confirm

Remove 2 symlink(s)? [y/N]
```

Pass `--harness` to skip the picker and target specific harnesses directly, or `-y` to skip the confirmation prompt.

```bash
mdm rules unlink                        # interactive — pick then confirm
mdm rules unlink --harness cursor       # only remove cursor's symlink (no picker)
mdm rules unlink -y                     # skip confirmation
```
