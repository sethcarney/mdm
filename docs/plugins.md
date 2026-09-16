# Agent Plugins

`mdm plugins` installs, validates, and updates [Agent
Plugins](https://agent-plugins.org) - portable packages that bundle skills and
MCP servers. Installing a plugin links its skills into your harnesses' skills
directories and wires its MCP servers into each harness's MCP config
(`.mcp.json`, `.cursor/mcp.json`, …). Plugins live in `.agents/plugins/` and are
recorded in the `plugins` section of `mdm.lock`.

`mdm plugins` is a stable, first-class command group. It was introduced behind an
experimental gate in an earlier build; that gate was removed in v2, and the
separate `plugins-lock.json` is now the `plugins` section of `mdm.lock` (run `mdm
migrate` to fold an old file in). See [the design spec](specs/plugins.md) for the
history and the conformance rules.

## Commands

| Command | What it does |
| --- | --- |
| `mdm plugins add <source>` | Install a plugin, link its skills, and wire its MCP config. |
| `mdm plugins list` | List installed plugins (alias: `ls`; `--json` for machine output). |
| `mdm plugins update [plugins...]` | Re-fetch plugins from their recorded source and ref (preserves the data dir). |
| `mdm plugins remove [plugins...]` | Unwire MCP, unlink skills, delete the plugin and its lock entry (aliases: `rm`, `r`; `--purge-data` also removes its data dir). |
| `mdm plugins validate [path]` | Check Agent Plugins spec conformance (`--json` for machine output). |
| `mdm plugins init [name]` | Scaffold a minimal conformant plugin (`--with-mcp` adds an MCP server). |
| `mdm plugins install` | Restore every plugin from `mdm.lock` - the CI / onboarding path. |

## Typical flow

```bash
# Author or install a plugin
mdm plugins init my-plugin --with-mcp   # scaffold one with an MCP server
mdm plugins add owner/repo              # or install an existing one

# Check it, then commit mdm.lock
mdm plugins validate ./my-plugin
git add mdm.lock .agents/plugins/ .mcp.json

# A teammate restores everything on a fresh clone
mdm plugins install
```

Every install runs the same hidden-character scan as skills; pass
`--allow-hidden-chars` to override it deliberately. Removing a plugin unwires its
MCP servers and unlinks its skills, but keeps its data directory unless you pass
`--purge-data`.

## JSON output

`--json` prints a top-level array and nothing else - no prompts, no progress,
no ANSI escapes - and prints `[]` with exit 0 when nothing is installed, so a
caller can tell "nothing installed" apart from "the command failed". The field
names are a public contract: a rename keeps the old key.

```bash
mdm plugins list --json
```

```json
[
  {
    "name": "acme-pack",
    "version": "1.2.0",
    "source": "acme/plugins",
    "ref": "main",
    "specVersion": "1.0.0",
    "installDir": ".agents/plugins/acme-pack",
    "skills": ["lint-rules"],
    "harnesses": ["claude-code"],
    "mcpServers": 2,
    "valid": true
  }
]
```

`harnesses` is the harnesses the plugin's skills were installed for, under the
name the text output has always used for it - the lock's own key for the same
list is `skillAgents`, from before the harness rename. `mcpServers` counts the
servers wired into harness MCP configs, and `valid` is whether the manifest
still loads from `installDir` - the text output prints "missing or invalid on
disk" when it does not.
