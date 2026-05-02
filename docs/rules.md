# mdm rules

Manage project-level instruction files for AI agents.

`AGENTS.md` is the universal source of truth. It is read natively by Codex CLI, Gemini CLI, OpenCode, and Replit. `mdm rules link` symlinks every other agent's instruction file — `CLAUDE.md`, `.cursorrules`, `.windsurfrules`, `.clinerules`, etc. — to `AGENTS.md` so every tool reads the same content from one place.

## Why AGENTS.md?

Many AI tools each look for a different file name in your project root:

| Tool | File |
|---|---|
| Claude Code | `CLAUDE.md` |
| Cursor | `.cursorrules` |
| Windsurf | `.windsurfrules` |
| Cline / Roo Code | `.clinerules` / `.roorules` |
| GitHub Copilot | `.github/copilot-instructions.md` |
| Codex CLI | `AGENTS.md` |
| Gemini CLI | `GEMINI.md` |
| OpenCode / Replit | `AGENTS.md` |

Without a shared source, you end up copying the same instructions into multiple files and keeping them in sync by hand. `AGENTS.md` is designed as the universal compatibility file — use it as the single source and symlink everything else to it.

## Commands

```
mdm rules link     Set up AGENTS.md as source of truth and symlink agent files
mdm rules status   Show the state of all agent instruction files
mdm rules unlink   Remove symlinks created by mdm rules link
```

## mdm rules link

Interactive setup. Walks you through three steps:

### Step 1 — Find your current rules

The command scans your project for any known instruction files that already contain content. If it finds some, you are asked to pick which one is your current source of truth:

```
? Which file contains your current rules?
  CLAUDE.md          Claude Code · # Project Overview...
  .cursorrules       Cursor · # Rules for this project...
  None of these — start with an empty AGENTS.md
```

- If you select a file, its content is copied into `AGENTS.md`.
- If you select "None of these", an empty `AGENTS.md` is created for you to fill in.
- If `AGENTS.md` already has content, this step is skipped automatically.

### Step 2 — Select your tools

A searchable multiselect shows every supported agent. Tools that already read `AGENTS.md` natively are shown locked at the top — they need no symlinking. Tools you have installed are pre-checked.

```
? Which AI tools are you using in this project?
  ◉ Codex           (locked — reads AGENTS.md natively)
  ◉ Gemini CLI      (locked — reads AGENTS.md natively)
  ◉ OpenCode        (locked — reads AGENTS.md natively)
  ◉ Replit          (locked — reads AGENTS.md natively)
  ────
  ◉ Claude Code     CLAUDE.md          (detected)
  ◉ Cursor          .cursorrules       (detected)
  ○ Cline           .clinerules
  ○ Windsurf        .windsurfrules
  ...
```

### Step 3 — Create symlinks

Each selected tool's instruction file is replaced with a symlink pointing to `AGENTS.md`. The file that was promoted in Step 1 is also replaced with a symlink (its content now lives in `AGENTS.md`).

```
  ✓ CLAUDE.md                          → AGENTS.md
  ✓ .cursorrules                        → AGENTS.md
  ✓ .windsurfrules                      → AGENTS.md

Linked 3 file(s) → AGENTS.md
```

Existing real files are replaced with symlinks only after per-file confirmation. Pass `-y` / `--yes` to skip all prompts.

### Flags

| Flag | Description |
|---|---|
| `--agent, -a` | Skip the tool-selection prompt and link specific agents only (repeatable) |
| `--yes, -y` | Replace real files without prompting |

### Examples

```bash
# Interactive — scan, pick source, select tools, symlink
mdm rules link

# Link only Claude Code and Cursor (no prompt)
mdm rules link --agent claude-code cursor

# Replace any existing real files without asking
mdm rules link -y
```

## mdm rules status

Shows the current state of every known instruction file in the project.

```
  File                                   State        Details
  ────────────────────────────────────────────────────────────────────────
  .cursorrules                           linked        → AGENTS.md
  agents: Cursor

  .windsurfrules                         linked        → AGENTS.md
  agents: Windsurf

  AGENTS.md                              real file
  agents: Codex, OpenCode, Replit

  CLAUDE.md                              real file
  agents: Claude Code

  GEMINI.md                              missing
  agents: Gemini CLI
```

States:

| State | Meaning |
|---|---|
| `linked` | Symlink pointing to a file that exists |
| `real file` | A regular file (not a symlink) |
| `missing` | The file does not exist |
| `broken` | Symlink whose target is missing |

## mdm rules unlink

Removes symlinks created by `mdm rules link`. Real files are never touched.

```bash
mdm rules unlink                        # interactive confirmation
mdm rules unlink --agent cursor         # only remove cursor's symlink
mdm rules unlink -y                     # skip confirmation
```
