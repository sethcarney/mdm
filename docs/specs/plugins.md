# Spec: Experimental Agent Plugins Support (`mdm plugins`)

| | |
|---|---|
| **Status** | Draft |
| **Stability** | Experimental — gated behind `MDM_EXPERIMENTAL=plugins` / `mdm experimental enable plugins` |
| **Author** | Dakota Kim |
| **Created** | 2026-08-06 |
| **Tracking issue** | TBD |
| **External spec** | [Agent Plugins v1.0.0](https://agent-plugins.org) |

## Summary

Add an experimental `mdm plugins` command group that installs, validates, and
manages **Agent Plugins** — the vendor-neutral agent-plugins.org format for
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
skill grouping — that is a marketplace index; agent-plugins.org defines the
plugin package itself.

## Goals

1. **Install and manage plugins** from the same source types skills support,
   pinned in a lock file, restorable in CI.
2. **Full spec conformance as a client**: closed-schema manifest validation
   without fetching schemas, fixed-location component discovery, path
   containment, and the resilience rules (a broken `mcp.json` disables MCP
   only; a broken server or skill is skipped individually).
3. **MCP wiring**: translate `mcp.json` into each agent's native MCP config
   (Claude Code's `.mcp.json`, Cursor's `.cursor/mcp.json`), performing the
   launcher duties the spec assigns to clients — `${PLUGIN_ROOT}` /
   `${PLUGIN_DATA}` expansion, env injection, command resolution — at install
   time, since the agent (not mdm) launches the servers.
4. **Author tooling**: `mdm plugins init` scaffolds a conformant plugin;
   `mdm plugins validate` checks one against the spec.

## Non-goals (v1)

- **Global scope.** MCP wiring targets project config files; Claude Code's
  global MCP config lives inside `~/.claude.json` alongside unrelated state.
- **Well-known registry sources** for plugins.
- **Extension-directory semantics.** mdm ignores reverse-domain extension
  dirs, as the spec instructs for unimplemented namespaces.
- An umbrella `mdm install` that restores skills + knowledge + plugins.

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

- `.agents/plugins/<name>/` — the plugin, copied verbatim. This directory *is*
  the spec's `PLUGIN_ROOT`.
- `.agents/plugins-data/<name>/` — the spec's `PLUGIN_DATA`: created on
  install, preserved across updates, deleted only by `remove --purge-data`.
  Doctor suggests gitignoring `plugins-data/` once it holds anything.
- `.agents/skills/<skill>` — a **symlink into the plugin directory**, and each
  agent's skills dir links to the canonical entry as usual. One copy on disk,
  atomic updates, and ownership is self-evident from the link target.

### Lock file: separate by design

`plugins-lock.json` (project scope), for the same reason as
`knowledge-lock.json`: the skill locks are read into fixed structs and
rewritten wholesale, so an older mdm binary touching skills would silently
drop unknown keys. Entries record source/ref, install and data dirs, spec and
plugin versions, content hash, installed skills and agents, and the namespaced
MCP server ids written per agent.

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
  exists

### MCP wiring (`internal/mcpwire`)

A per-agent target registry, deliberately separate from the stable
`internal/agent` registry: `{AgentName, ConfigPath, ServersKey, style}`.
Claude Code (`.mcp.json`, typed entries with streamable HTTP spelled `http`)
and Cursor (`.cursor/mcp.json`, bare entries) ship in v1; another agent is one
map entry.

Server ids are namespaced `<plugin>--<server>` — the spec forbids `--` inside
plugin names, so the split is unambiguous, and it avoids the `:` and `__`
sequences agents use for MCP tool-name mangling.

Because mdm writes config rather than launching servers, everything the spec
requires of the launcher is baked in at install time: `${PLUGIN_ROOT}` and
`${PLUGIN_DATA}` become absolute paths, both variables are injected into the
server's `env`, `./`-prefixed commands resolve inside the plugin root (with
containment re-checked), and an omitted `cwd` is written as the plugin root.
Config merges preserve every key mdm does not own; removal deletes exactly the
recorded ids and never the file.

### Package boundary

Per the spec's package-boundary rule, every file mdm reads out of a plugin
must resolve inside the filesystem-resolved plugin root. An escaping
`plugin.json` symlink rejects the plugin, an escaping `mcp.json` disables
MCP, an escaping `SKILL.md` skips that skill, and the install copy skips
(with a warning) any symlink that resolves outside the root — so a
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
re-owned skill links, MCP ids missing from agent config, orphaned mdm-managed
ids, and the `plugins-data/` gitignore hint.

## Package layout

| Path | Role |
|---|---|
| `internal/plugin/` | Spec conformance: manifest, name rules, mcp.json, path containment, discovery, hashing |
| `internal/mcpwire/` | Per-agent MCP config targets, rendering, read-merge-write |
| `internal/lock/plugins.go` | `plugins-lock.json` read/write |
| `commands/plugins*.go` | The command group, one file per subcommand |

## Testing strategy

- **Unit (`internal/plugin`)**: table-driven spec conformance — every name
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
- MCP targets cover the majority of MCP-capable agents in `AllAgents`.
- Command surface survives a release cycle without changes.

## Exit criteria (removal)

If the standard stalls or is superseded, remove the gate and command group in
a minor release; `plugins-lock.json` and installed plugin directories are
inert files a user can delete.
