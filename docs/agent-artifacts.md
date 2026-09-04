# mdm agents

Manage agent definitions — single markdown files that give a harness a named
subagent persona (e.g. Claude Code subagents). An agent definition is
distinct from a [harness](harnesses.md), which is the AI coding tool itself,
and distinct from a skill, which is a reusable prompt library rather than a
persona.

An agent definition file has YAML frontmatter with (at minimum) `name` and
`description`:

```markdown
---
name: code-reviewer
description: Reviews pull requests for correctness and style issues.
---

You are a meticulous code reviewer...
```

A file missing either field is not treated as an agent definition — it is
silently skipped rather than reported as an error, since a plain markdown
file with no such frontmatter is a normal thing to find in a source tree.

## Discovery

`mdm agents add <source>` searches the fetched source for agent-definition
files in this order, and the **first occurrence of a name wins**:

1. Any directories the source declares itself, via an `agentsDirs` array in
   `.claude-plugin/marketplace.json` at the root of the source. This lets a
   source say where its agents live instead of relying on convention.
2. The conventional directories, always searched in this order:
   `agents`, `subagents`, `.claude/agents`, `.github/agents`, `.agents/agents`.

Only files directly inside one of these directories are scanned (not
subdirectories), and only files ending in `.md` are considered — this is
independent of the file extension a *target* harness expects on install (see
[GitHub Copilot's `.agent.md` requirement](#github-copilot-loads-only-agentmd) below).

A directory named in `agentsDirs` is declared by the source, and the source is
third-party, so each one goes through two checks before it is opened. The first
is lexical: the declared string must be a genuine relative subdirectory, which
rules out an empty value, `.`, a parent escape, and a rooted path (including a
driveless `/x`, which Windows does not treat as absolute). The second resolves
the candidate on disk and requires the result to still sit inside the resolved
search root, which is what catches a directory that only *looks* local but is
really a symlink pointing somewhere else on the victim's disk. A resolution
error, including a dangling or looping symlink, counts as unsafe. The
`.claude-plugin/marketplace.json` file itself goes through the containment check
too, because it is opened before anything has looked at where it points.

A rejected entry is dropped silently, the same way a file missing its
frontmatter is. This is untrusted input being filtered, not a mistake by the
person running mdm, and there is nothing for them to act on.

## Commands

```
mdm agents add <source>     Add agent definitions from GitHub, a URL, or a local path
mdm agents list             List installed agent definitions
mdm agents remove [names...] Remove installed agent definitions
mdm agents update [names...] Update installed agent definitions
mdm agents install          Restore agent definitions from mdm.lock
```

### mdm agents add

```bash
mdm agents add owner/repo
mdm agents add owner/repo --agent code-reviewer
mdm agents add owner/repo --harness claude-code cursor
mdm agents add ./my-agents
```

With no `--agent` filter, every discovered definition is offered in an
interactive multiselect (or installed automatically when there is exactly
one, or `--yes` is passed). `--harness` and `--agent` both accept multiple
values, space-separated after the flag or repeated:

```bash
mdm agents add owner/repo --harness claude-code cursor
mdm agents add owner/repo --agent code-reviewer --agent test-writer
```

| Flag | Description |
| --- | --- |
| `--global, -g` | Install globally (user-level) |
| `--project, -p` | Force project-scope install |
| `--harness` | Harnesses to install to (repeatable, use `*` for all) |
| `--agent, -a` | Agent definition names to install (repeatable, use `*` for all) |
| `--yes, -y` | Skip confirmation prompts |

### mdm agents list

```bash
mdm agents list
mdm agents list -g
```

Shows each installed definition's source, and which harnesses it is
currently installed in. A definition whose canonical file is missing, or
that is not installed in any harness, is flagged.

| Flag | Description |
| --- | --- |
| `--global, -g` | List global agent definitions |
| `--project, -p` | List project agent definitions |

### mdm agents remove [names...]

```bash
mdm agents remove
mdm agents remove code-reviewer
mdm agents remove code-reviewer -y
```

With no names, an interactive multiselect lets you choose which definitions
to remove. `--harness` scopes the removal to specific harnesses only — the
canonical file and the lock entry are kept as long as any harness (including
one outside the filter) still has a copy.

| Flag | Description |
| --- | --- |
| `--global, -g` | Remove from global scope |
| `--project, -p` | Remove from project scope |
| `--harness` | Remove from specific harnesses only (repeatable) |
| `--agent, -a` | Agent definition names to remove (repeatable) |
| `--yes, -y` | Skip confirmation prompts |

### mdm agents update [names...]

```bash
mdm agents update
mdm agents update code-reviewer
mdm agents update -g
```

Re-fetches from the recorded source and ref. Definitions sharing a source
repository and ref are re-fetched together, one clone per update run rather
than one per definition. Every harness a definition is *currently* installed
to is refreshed — not just the canonical copy — so a copy-mode harness
install is kept in sync too, instead of going stale.

| Flag | Description |
| --- | --- |
| `--global, -g` | Update global agent definitions only |
| `--project, -p` | Update project agent definitions only |
| `--yes, -y` | Skip the scope prompt |

### mdm agents install

```bash
mdm agents install
mdm agents install -y
```

Restores every agent definition recorded in the project and/or global lock,
the same "which lock file" resolution `mdm skills install` uses. A project
with no agent definitions produces no output, since that is a normal
outcome, not something to report.

`mdm skills install` calls this automatically after restoring skills, so a
plain `mdm skills install` (or the `postCreateCommand: mdm skills install`
pattern from a dev container) restores both.

## Install layout

Installing an agent definition writes it twice, mirroring how skills are
installed:

1. A canonical copy at `.agents/agents/<name>.md` (project scope) or
   `~/.agents/agents/<name>.md` (global scope) — the one place the content
   actually lives.
2. A symlink (or, on install-mode `copy`, a real copy) from each target
   harness's own agent directory, named `<name>` plus that harness's
   expected extension (`.md` for every harness except GitHub Copilot, which
   needs `.agent.md`) — see the [harness support table](#harness-support)
   below for the exact directories.

If a symlink cannot be created (Windows without Developer Mode or the
symlink privilege, typically), mdm copies instead, per install, without
recording anything — exactly the fallback `mdm skills add` uses.

## Install mode

Agent definitions do not carry their own `--copy` / `--symlink` flags.
Instead they obey whatever install mode is already committed for the scope —
the same scope-wide switch skills use, set with `mdm skills add --copy`,
`mdm skills add --symlink`, or the equivalent flags on `mdm skills install`.
A scope in copy mode installs agent definitions as real files from the start;
a scope in the default symlink mode links them, falling back to a copy only
when the link itself cannot be created.

## Doctor integration

`mdm doctor` reports on locked agent definitions in their own "Agent
definitions" section, distinguishing two different problems:

- the canonical file is missing entirely (`run mdm agents install to
  restore`), and
- a harness's own copy is a broken symlink whose target is gone (`run mdm
  agents update <name> to repair`), as opposed to simply not being installed
  in that harness at all, which is not itself an error.

See [mdm doctor](doctor.md) for the full check list.

## Harness support

Not every harness has an agent-definition concept. The five that do are
listed below, each checked against its own vendor documentation. A harness
that is not listed here has no agent concept at all — `mdm agents add`
installs to it are skipped with a notice rather than failing, the same way a
harness with no skills directory would be.

| Harness | Project directory | User directory | File extension | Source |
| --- | --- | --- | --- | --- |
| Claude Code | `.claude/agents` | `~/.claude/agents` | `.md` | [code.claude.com/docs/en/sub-agents](https://code.claude.com/docs/en/sub-agents) — checked 2026-09-03 |
| GitHub Copilot | `.github/agents` | `~/.copilot/agents` | `.agent.md` | [docs.github.com/.../create-custom-agents-for-cli](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli) — checked 2026-09-03 |
| OpenCode | `.opencode/agents` | `$XDG_CONFIG_HOME/opencode/agents` (default `~/.config/opencode/agents`) | `.md` | [opencode.ai/docs/agents](https://opencode.ai/docs/agents/) — checked 2026-09-03 |
| Cursor | `.cursor/agents` | `~/.cursor/agents` | `.md` | [cursor.com/docs/subagents](https://cursor.com/docs/subagents) — checked 2026-09-03 |
| Gemini CLI | `.gemini/agents` | `~/.gemini/agents` | `.md` | [geminicli.com/docs/core/subagents](https://geminicli.com/docs/core/subagents/) — checked 2026-09-03 |

Both directories, project and user, hold plain markdown files with `name`
and `description` frontmatter for every harness above except GitHub Copilot.

### GitHub Copilot loads only `.agent.md`

Copilot CLI reads agent definitions **only** from files ending in
`.agent.md`. A plain `.md` file placed in `.github/agents` or
`~/.copilot/agents` is silently never read — there is no error, no warning,
it just never loads. `mdm` accounts for this automatically (see
[Install layout](#install-layout) above), but if you are placing a file by
hand rather than through `mdm agents add`, name it accordingly.

### GitHub Copilot's precedence is the opposite of everyone else's

For Copilot CLI specifically, a same-named definition in the **user**
directory (`~/.copilot/agents`) takes precedence over the **project**
directory (`.github/agents`) one. This is confirmed from Copilot's own
documentation, not a guess, and it is the **opposite** of Claude Code (where
the project definition wins) and the opposite of how mdm's own skills
scoping behaves. It is deliberate and specific to Copilot CLI — do not
expect it to generalize.

For OpenCode, Cursor, and Gemini CLI, no such precedence rule is confirmed
either way in the vendor documentation checked above. Treat installing the
same name to both scopes on those harnesses as undefined rather than
assuming either side wins.
