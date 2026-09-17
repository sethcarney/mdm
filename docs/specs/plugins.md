# Design spec: Agent Plugins support (`mdm plugins`)

!!! info "Historical document - `mdm plugins` is stable"
    This is the **original design spec**, written when the feature was still
    being built, and it is kept unchanged as a record of that design. It
    describes an experimental gate and a separate `plugins-lock.json`;
    **neither exists any more.** In v2 the gate was removed, the command group
    is always available with no opt-in, and its lock entries live in the
    `plugins` section of `mdm.lock` (`mdm migrate` folds an old file in).

    For how `mdm plugins` works today, read the
    [Agent Plugins guide](../plugins.md) and the
    [command reference](../commands.md#mdm-plugins).

| | |
|---|---|
| **Status** | Implemented - graduated to full support in v2 |
| **Stability** | Stable. Everything below describes the original experimental phase and is retained for history only. |
| **Author** | Dakota Kim |
| **Created** | 2026-08-06 |
| **Tracking issue** | TBD |
| **External spec** | [Agent Plugins v1.0.0](https://agent-plugins.org) |

## Summary

Add an experimental `mdm plugins` command group that installs, validates, and
manages **Agent Plugins** - the vendor-neutral agent-plugins.org format for
packaging skills and MCP servers as portable plugin directories. The spec
deliberately defines no lockfile, registry, or install mechanism; that is the
gap a package manager fills. mdm reuses its existing acquisition, locking, and
security pipeline, ships the feature behind a named experimental gate, and
makes no stability promises while adoption of the standard is still forming.

A plugin directory contains a `plugin.json` manifest, skills under
`skills/<name>/SKILL.md` (the Agent Skills format mdm already parses), an
optional `mcp.json` describing MCP servers, and reverse-domain extension
directories for client-specific content.

Note this is **not** the same thing as Claude Code's
`.claude-plugin/marketplace.json` format, which `internal/skill` reads for
skill grouping - that is a marketplace index; agent-plugins.org defines the
plugin package itself.

## Goals

1. **Install and manage plugins** from the same source types skills support,
   pinned in a lock file, restorable in CI.
2. **Full spec conformance as a client**: closed-schema manifest validation
   without fetching schemas, fixed-location component discovery, path
   containment, and the resilience rules (a broken `mcp.json` disables MCP
   only; a broken server or skill is skipped individually).
3. **MCP wiring**: translate `mcp.json` into each harness's native MCP config
   (Claude Code's `.mcp.json`, Cursor's `.cursor/mcp.json`), performing the
   launcher duties the spec assigns to clients - `${PLUGIN_ROOT}` /
   `${PLUGIN_DATA}` expansion, env injection, command resolution - at install
   time, since the harness (not mdm) launches the servers.
4. **Author tooling**: `mdm plugins init` scaffolds a conformant plugin;
   `mdm plugins validate` checks one against the spec.

## Non-goals (v1)

- **Global scope.** MCP wiring targets project config files; Claude Code's
  global MCP config lives inside `~/.claude.json` alongside unrelated state.
- **Well-known registry sources** for plugins.
- **Extension-directory semantics.** mdm ignores reverse-domain extension
  dirs, as the spec instructs for unimplemented namespaces.
- An umbrella `mdm install` that restores skills + knowledge + plugins.
  *(Shipped after v2: knowledge and plugins graduated from the experimental
  gate this non-goal was written under. See [mdm install](../install.md).)*

## Design

### The experimental gate

Identical to `knowledge`: `experimental.Plugins`, hidden from `--help` until
enabled, `PersistentPreRunE` refusal with an actionable message, stderr banner
on every invocation.

### Command surface

```
mdm plugins
├── add <source>          # Install into .agents/plugins/, link skills, wire MCP
├── remove [plugins...]   # Unwire MCP, unlink skills, delete; --purge-data
├── list                  # Installed plugins from plugins-lock.json
├── update [plugins...]   # Re-fetch from recorded source+ref; preserves data dir
├── validate [path]       # Spec-conformance report (--json)
├── init [name]           # Scaffold plugin.json + example skill; --with-mcp
└── install               # Restore everything from plugins-lock.json (CI)
```

### On-disk layout

- `.agents/plugins/<name>/` - the plugin, copied verbatim. This directory *is*
  the spec's `PLUGIN_ROOT`.
- `.agents/plugins-data/<name>/` - the spec's `PLUGIN_DATA`: created on
  install, preserved across updates, deleted only by `remove --purge-data`.
  Doctor suggests gitignoring `plugins-data/` once it holds anything.
- `.agents/skills/<skill>` - a **symlink into the plugin directory**, and each
  harness's skills dir links to the canonical entry as usual. One copy on disk,
  atomic updates, and ownership is self-evident from the link target.

### Lock file: separate by design

`plugins-lock.json` (project scope), for the same reason as
`knowledge-lock.json`: the skill locks are read into fixed structs and
rewritten wholesale, so an older mdm binary touching skills would silently
drop unknown keys. Entries record source/ref, install and data dirs, spec and
plugin versions, content hash, installed skills and harnesses, and the namespaced
MCP server ids written per harness.

### Coexistence with skills-lock.json

**plugins-lock.json exclusively owns plugin-delivered skills; the stable
skills commands recognize them but never manage them.** Nothing plugin-related
is ever written to `skills-lock.json`. When the gate is on:

- `mdm skills list` labels plugin skills "(from plugin X)"
- `mdm skills remove` / a standalone install over a plugin-owned skill refuses
  and points at `mdm plugins`
- `mdm skills update <name>` on a plugin-owned skill warns and points at
  `mdm plugins update <plugin>` (plugin skills are never in the skills lock,
  so the filter would otherwise silently match nothing)
- name collisions are first-come-first-served with a warning, in both
  directions
- `mdm skills install` prints a one-line hint when a `plugins-lock.json`
  exists *(now generalized: it names the knowledge bundles and plugins it did
  not restore and points at [`mdm install`](../install.md))*

### MCP wiring (`internal/mcpwire`)

A per-harness target registry, deliberately separate from the stable
`internal/harness` registry: `{HarnessName, ConfigPath, ServersKey, style}`.
Claude Code (`.mcp.json`, typed entries with streamable HTTP spelled `http`)
and Cursor (`.cursor/mcp.json`, bare entries) ship in v1; another harness is one
map entry.

Server ids are namespaced `<plugin>--<server>` - the spec forbids `--` inside
plugin names, so the split is unambiguous, and it avoids the `:` and `__`
sequences harnesses use for MCP tool-name mangling.

Because mdm writes config rather than launching servers, everything the spec
requires of the launcher is baked in at install time: `${PLUGIN_ROOT}` and
`${PLUGIN_DATA}` become absolute paths, both variables are injected into the
server's `env`, and `./`-prefixed commands resolve inside the plugin root (with
containment re-checked).

`cwd` is written only for a harness that reads one. Claude Code does not: its
stdio schema is `command`, `args` and `env`, and a configured `cwd` is dropped
rather than applied, so writing one put this machine's absolute path into a
committed file and changed nothing. A plugin that declares a `cwd` for such a
harness is told its server will start in the project root.

**The written config is machine-local.** The absolute paths above cannot be
made portable: Claude Code expands `${VAR}` in `.mcp.json` only from variables
it already holds, and `CLAUDE_PROJECT_DIR` is set in the *server's*
environment rather than its own, so `${CLAUDE_PROJECT_DIR}` in the config reads
as a missing variable; Cursor expands nothing. A stdio server living inside the
repository therefore has to be named by an absolute path. Treat the MCP config
the way `.agents/` is already treated - generated output regenerated from
`mdm.lock` by `mdm plugins install` - rather than a file whose contents travel
between machines. `mdm doctor` says so once a plugin has wired servers into
it.
Config merges preserve every key mdm does not own; removal deletes exactly the
recorded ids and never the file.

### Package boundary

Per the spec's package-boundary rule, every file mdm reads out of a plugin
must resolve inside the filesystem-resolved plugin root. An escaping
`plugin.json` symlink rejects the plugin, an escaping `mcp.json` disables
MCP, an escaping `SKILL.md` skips that skill, and the install copy skips
(with a warning) any symlink that resolves outside the root - so a
malicious plugin cannot pull files from elsewhere on disk into the
project. Directory symlinks are never followed during the copy; file
symlinks that resolve within the root are copied as regular files.

### Security scan

Every selected plugin runs the mandatory hidden-character scan
(`internal/security/markdownscan`) over its whole directory before install,
exactly like skills and knowledge bundles. `--allow-hidden-chars` overrides
with a warning.

### Doctor integration

Gated section: missing/invalid plugin dirs, content-hash drift, broken or
re-owned skill links, MCP ids missing from harness config, orphaned mdm-managed
ids, and the `plugins-data/` gitignore hint.

## Package layout

| Path | Role |
|---|---|
| `internal/plugin/` | Spec conformance: manifest, name rules, mcp.json, path containment, discovery, hashing |
| `internal/mcpwire/` | Per-harness MCP config targets, rendering, read-merge-write |
| `internal/lock/plugins.go` | `plugins-lock.json` read/write |
| `commands/plugins*.go` | The command group, one file per subcommand |

## Testing strategy

- **Unit (`internal/plugin`)**: table-driven spec conformance - every name
  rule, the fatal/non-fatal manifest matrix, the closed server union,
  command/cwd/URL constraints, symlink-escape rejection, non-recursive
  placeholder expansion.
- **Unit (`internal/mcpwire`)**: merge preserves foreign entries and unknown
  top-level keys, removal deletes only owned ids, render bakes absolute paths
  and injects env.
- **CLI (`tests/plugins_test.go`)**: gate on/off, init→validate round trip,
  add→list→remove→update→install round trips, hidden-char blocking, dry run,
  `--skip-mcp`, broken-mcp resilience, lock isolation, ownership refusals,
  collision skips, doctor gating.

## Graduation criteria

- The upstream spec sees real multi-client adoption without breaking changes.
- Global scope lands with a safe answer for shared global config files.
- MCP targets cover the majority of MCP-capable harnesses in `AllHarnesses`.
- Command surface survives a release cycle without changes.

## Exit criteria (removal)

If the standard stalls or is superseded, remove the gate and command group in
a minor release; `plugins-lock.json` and installed plugin directories are
inert files a user can delete.
