# mdm agents

Manage agent definitions - single files that give a harness a named
subagent persona (e.g. Claude Code subagents). An agent definition is
distinct from a [harness](harnesses.md), which is the AI coding tool itself,
and distinct from a skill, which is a reusable prompt library rather than a
persona.

An agent definition is a markdown file with YAML frontmatter holding (at
minimum) `name` and `description`:

```markdown
---
name: code-reviewer
description: Reviews pull requests for correctness and style issues.
---

You are a meticulous code reviewer...
```

or, for Codex, a TOML file with `name`, `description`, and
`developer_instructions`:

```toml
name = "code-reviewer"
description = "Reviews pull requests for correctness and style issues."
developer_instructions = "You are a meticulous code reviewer..."
```

The markdown body and `developer_instructions` hold the same content -
`mdm` converts between the two shapes as needed on install, so a definition
authored in either format still reaches every harness regardless of which
format that harness reads. See [Formats: markdown and
TOML](#formats-markdown-and-toml) below.

A markdown file missing `name` or `description` frontmatter, or a TOML file
missing `name` or `description`, is not treated as an agent definition - it
is silently skipped rather than reported as an error, since a plain file
with no such structure is a normal thing to find in a source tree.

## Discovery

`mdm agents add <source>` searches the fetched source for agent-definition
files in this order, and the **first occurrence of a name wins**:

1. Any directories the source declares itself, via an `agentsDirs` array in
   `.claude-plugin/marketplace.json` at the root of the source. This lets a
   source say where its agents live instead of relying on convention.
2. The conventional directories, always searched in this order:
   `agents`, `subagents`, `.claude/agents`, `.github/agents`, `.agents/agents`.

Only files directly inside one of these directories are scanned (not
subdirectories), and only files ending in `.md` or `.toml` are considered -
this is independent of the file extension a *target* harness expects on
install (see [GitHub Copilot's `.agent.md`
requirement](#github-copilot-loads-only-agentmd) and [Formats: markdown and
TOML](#formats-markdown-and-toml) below).

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
| `--copy` | Copy files instead of symlinking (switches the scope to copy mode) |
| `--symlink` | Symlink files from `.agents/agents` (the default; switches a scope back from copy mode) |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |
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
to remove. `--harness` scopes the removal to specific harnesses only - the
canonical file and the lock entry are kept as long as any harness (including
one outside the filter) still has a copy.

A file inside the local directory a definition was added from is yours, not an
install, and is never deleted. After `mdm agents add .` adopted a hand-written
`.claude/agents/critic.md`, `mdm agents remove critic` turns that link back into
the real file, drops the lock entry, and says what it kept.

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
to is refreshed - not just the canonical copy - so a copy-mode harness
install is kept in sync too, instead of going stale.

| Flag | Description |
| --- | --- |
| `--global, -g` | Update global agent definitions only |
| `--project, -p` | Update project agent definitions only |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |
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

| Flag | Description |
| --- | --- |
| `--copy` | Copy files instead of symlinking (switches the scope to copy mode) |
| `--symlink` | Symlink files from `.agents/agents` (the default; switches a scope back from copy mode) |
| `--allow-hidden-chars` | Allow markdown files with hidden Unicode characters |
| `--yes, -y` | Skip confirmation prompts |

## Install layout

Installing an agent definition writes it twice, mirroring how skills are
installed:

1. A canonical copy at `.agents/agents/<name>` (project scope) or
   `~/.agents/agents/<name>` (global scope) - the one place the content
   actually lives. The extension mirrors the *source*: a markdown source
   produces `<name>.md`; a Codex TOML source produces `<name>.toml`.
2. Something under each target harness's own agent directory, named
   `<name>` plus that harness's expected extension (`.md` for most
   harnesses, `.agent.md` for GitHub Copilot, `.toml` for Codex) - see the
   [harness support table](#harness-support) below for the exact
   directories.

That second file is a symlink back to the canonical copy only when the
harness reads the same format the canonical file is already in *and* the
harness's directory is generated output nobody commits. Otherwise it is a
real file: either a plain copy (install-mode `copy`, or the symlink fallback
below) or, when the harness reads the other format, a converted copy. See
[Formats: markdown and TOML](#formats-markdown-and-toml) for exactly when
each case applies - notably, GitHub Copilot and Codex are real files
**regardless of the scope's install mode**, because a symlink is unsafe or
impossible for both of them respectively.

If a symlink cannot be created (Windows without Developer Mode or the
symlink privilege, typically), mdm copies instead, per install, without
recording anything - exactly the fallback `mdm skills add` uses.

## Install mode

`mdm agents add` and `mdm agents install` accept their own `--copy` /
`--symlink` flags, but a flag on either one does the same thing a flag on
`mdm skills add` does: it switches the install mode for the whole scope,
not just for agent definitions. Agent definitions and skills share one
scope-wide mode - set it from either side, with `mdm agents add --copy`,
`mdm skills add --copy`, or the equivalent flag on `mdm agents install` /
`mdm skills install` - and the change applies to both. `mdm agents update`
has no mode flags of its own; it obeys whatever mode the scope is already
in. A scope in copy mode installs agent definitions as real files from the
start; a scope in the default symlink mode links them, falling back to a
copy only when the link itself cannot be created.

**This no longer describes every file in the scope.** Whatever mode a scope
is in, GitHub Copilot's copy and every install that crosses formats (a
markdown definition installed to Codex, or a Codex definition installed to
a markdown harness) are real files regardless - `--copy` and `--symlink`
only decide the harnesses left over: the ones that read the canonical
format natively out of a directory that is safe to symlink into. Switching
a scope's mode converts those; it leaves Copilot's copy and every
cross-format copy exactly as they were. See [Formats: markdown and
TOML](#formats-markdown-and-toml) for why.

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

These six harnesses are the ones mdm can install agent definitions to, each
checked against its own vendor documentation. A harness that is not listed
here has no agent-definition directory recorded in mdm - `mdm agents add`
installs to it are skipped with a notice rather than failing, the same way a
harness with no skills directory would be. That is a statement about what
mdm has configured, not a claim about whether the harness itself supports
agent definitions; most other harnesses have not been checked either way.

| Harness | Project directory | User directory | File extension | Format | Source |
| --- | --- | --- | --- | --- | --- |
| Claude Code | `.claude/agents` | `~/.claude/agents` | `.md` | markdown | [code.claude.com/docs/en/sub-agents](https://code.claude.com/docs/en/sub-agents) - checked 2026-09-03 |
| GitHub Copilot | `.github/agents` | `~/.copilot/agents` | `.agent.md` | markdown | [docs.github.com/.../create-custom-agents-for-cli](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/create-custom-agents-for-cli) - checked 2026-09-03 |
| OpenCode | `.opencode/agents` | `$XDG_CONFIG_HOME/opencode/agents` (default `~/.config/opencode/agents`) | `.md` | markdown | [opencode.ai/docs/agents](https://opencode.ai/docs/agents/) - checked 2026-09-03 |
| Cursor | `.cursor/agents` | `~/.cursor/agents` | `.md` | markdown | [cursor.com/docs/subagents](https://cursor.com/docs/subagents) - checked 2026-09-03 |
| Gemini CLI | `.gemini/agents` | `~/.gemini/agents` | `.md` | markdown | [geminicli.com/docs/core/subagents](https://geminicli.com/docs/core/subagents/) - checked 2026-09-03 |
| Codex | `.codex/agents` | `$CODEX_HOME/agents` (default `~/.codex/agents`) | `.toml` | **TOML** | [learn.chatgpt.com/docs/agent-configuration/subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents) - checked 2026-09-05 |

Both directories, project and user, hold plain markdown files with `name`
and `description` frontmatter for every harness above except GitHub Copilot
(whose files additionally need the `.agent.md` extension, covered below) and
Codex, which reads TOML instead of markdown - see [Formats: markdown and
TOML](#formats-markdown-and-toml) below.

### GitHub Copilot loads only `.agent.md`

Copilot CLI reads agent definitions **only** from files ending in
`.agent.md`. A plain `.md` file placed in `.github/agents` or
`~/.copilot/agents` is silently never read - there is no error, no warning,
it just never loads. `mdm` accounts for this automatically (see
[Install layout](#install-layout) above), but if you are placing a file by
hand rather than through `mdm agents add`, name it accordingly.

### GitHub Copilot's precedence is the opposite of everyone else's

For Copilot CLI specifically, a same-named definition in the **user**
directory (`~/.copilot/agents`) takes precedence over the **project**
directory (`.github/agents`) one. This is confirmed from Copilot's own
documentation, not a guess, and it is the **opposite** of Claude Code (where
the project definition wins) and the opposite of how mdm's own skills
scoping behaves. It is deliberate and specific to Copilot CLI - do not
expect it to generalize.

For OpenCode, Cursor, and Gemini CLI, no such precedence rule is confirmed
either way in the vendor documentation checked above. Treat installing the
same name to both scopes on those harnesses as undefined rather than
assuming either side wins.

### Codex reads TOML, not markdown

Codex supports custom agents as standalone `.toml` files (`.codex/agents`
for project scope, `$CODEX_HOME/agents` - `~/.codex/agents` by default -
for user scope), with required fields `name`, `description`, and
`developer_instructions`. Source:
[learn.chatgpt.com/docs/agent-configuration/subagents](https://learn.chatgpt.com/docs/agent-configuration/subagents)
- checked 2026-09-05.

`mdm agents add` installs to Codex like any other harness: point it at a
source holding markdown definitions, TOML definitions, or a mix of both, and
add `codex` to `--harness` (or accept it from the interactive picker, or
`*`). A markdown source is converted to TOML on the way in - no more risk of
handing Codex a `.md` file it silently never reads. See [Formats: markdown
and TOML](#formats-markdown-and-toml) below for exactly how the conversion
works and what its edges are.

## Formats: markdown and TOML

Codex is the only supported harness that does not read markdown, so an
agent definition now comes in two shapes:

| Markdown | Codex TOML |
| --- | --- |
| frontmatter `name` | `name` |
| frontmatter `description` | `description` |
| body | `developer_instructions` |
| any other frontmatter key | the corresponding top-level key |

Frontmatter is YAML, which holds nested maps, so a passthrough key like
`mcp_servers` or `skills.config` survives a round trip in either direction.
TOML comments and key order do not.

`developer_instructions` is required, so a markdown definition whose body is
empty cannot be converted for Codex. That harness is refused by name, with
the reason, and the definition still installs to every markdown harness you
targeted. Substituting the description would invent content the author did
not write.

### The canonical file mirrors the source

mdm's own canonical copy takes its extension from whichever format the
source was in: a markdown source produces `.agents/agents/<name>.md`; a
Codex TOML source produces `.agents/agents/<name>.toml`. `mdm agents list`
and `mdm doctor` resolve the canonical path through the lock, which records
which one it is, rather than assuming `.md`.

### When an install is a link, and when it is a real file

An install into a harness's own directory is a symlink back to the
canonical file only when *both* are true: the harness reads the same
format the canonical file is already in, and the harness's directory is
generated output nobody is expected to commit. Otherwise it is a real file
- a plain copy, or, when the formats differ, a converted copy:

| Canonical format | Harness | Result |
| --- | --- | --- |
| markdown | Claude Code, OpenCode, Cursor, Gemini CLI | symlink |
| markdown | Codex | converted TOML, real file |
| markdown | GitHub Copilot | real copy |
| TOML | Codex | symlink |
| TOML | GitHub Copilot | converted markdown, real file |
| TOML | any other harness | converted markdown, real file |

Codex meets the first clause when the source is already TOML, and fails it
whenever the source is markdown. GitHub Copilot fails the second clause
regardless of format: `.github/agents` sits inside `.github`, which holds
workflows and CODEOWNERS and is never gitignored wholesale, and GitHub
documents repo-scoped agents as the way to share them through a
repository - a symlink committed there arrives on a teammate's Windows
checkout as a text file containing a path, not the agent definition.

This is what **Install mode** above means by "no longer describes every
file in the scope": `--copy` and `--symlink` govern the rows that read the
canonical format out of a directory that is safe to link into. GitHub
Copilot's row and every cross-format row are real files no matter which
mode the scope records, and switching modes does not touch them.

### mdm does not rewrite a definition's name

Claude Code and Gemini CLI require the frontmatter `name` to match a
specific pattern (lowercase with hyphens, for example). mdm does not
normalize a definition's name to fit - the source owns it, and rewriting it
per target harness would force every install of that definition to be a
real file, since a symlink hands the harness the source's bytes verbatim.
When a definition's name will not satisfy a target harness's documented
pattern, `mdm agents add` warns, naming the definition and the harness, and
installs it anyway - the alternative is an install that reports success and
is then silently ignored by that harness.

The name is still sanitized once, to build the file name mdm writes to disk
and the lock key it is tracked under (`code-reviewer` for a definition named
`Code Reviewer`, for instance). That sanitized form, not the raw
frontmatter, is what has to be unique: if a markdown definition and a TOML
definition sanitize to the same name, the second one to install is refused
and the error names both files - accepting it would silently change which
format that name's canonical file is in, and re-convert every harness that
already has it.
