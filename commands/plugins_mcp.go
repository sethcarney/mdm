package commands

import (
	"errors"
	"fmt"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/mcpwire"
	"github.com/sethcarney/mdm/internal/plugin"
	"github.com/sethcarney/mdm/internal/ui"
)

// wirePluginMCP translates the plugin's mcp.json servers into each target
// harness's native MCP config and returns harness → namespaced server ids.
// Harnesses without an MCP config descriptor are skipped silently - skills
// still install for them, matching the spec's incremental-adoption rule.
func wirePluginMCP(c pluginCandidate, destDir, dataDir string, harnesses []string, opts PluginsAddOptions, cwd string) map[string][]string {
	prev, hadPrev := lock.ReadPluginsLock(cwd).Plugins[c.Name]

	// --skip-mcp leaves the MCP config alone, so whatever a previous install
	// wired stays both on disk and in the lock entry. Returning nil here would
	// strand it: `mdm plugins remove` cleans the ids the lock names, and it
	// would name none.
	if opts.SkipMCP {
		if hadPrev {
			return prev.MCP
		}
		return nil
	}

	// Every path below replaces this plugin's wiring, so the previous entries
	// come out first - before the early returns, not after them. An update
	// whose new mcp.json is gone or unreadable still has to take the old
	// servers out of the user's config, because the entry that recorded them is
	// about to be overwritten and nothing could clean them afterwards.
	if hadPrev {
		unwirePluginMCP(c.Name, prev, cwd)
	}

	cfg, _, err := plugin.LoadMCPConfig(destDir)
	if errors.Is(err, plugin.ErrMCPDisabled) {
		ui.LogWarn(fmt.Sprintf("%s: mcp.json is invalid - MCP disabled, skills still installed (run 'mdm plugins validate')", c.Name))
		return nil
	}
	if cfg == nil || len(cfg.Servers) == 0 {
		return nil
	}

	result := map[string][]string{}
	for _, harnessName := range harnesses {
		target, ok := mcpwire.Targets[harnessName]
		if !ok {
			continue
		}
		entries := map[string]map[string]any{}
		for _, s := range cfg.Servers {
			rendered, err := target.RenderServer(s, destDir, dataDir)
			if err != nil {
				ui.LogWarn(fmt.Sprintf("%s: server %q skipped for %s: %v", c.Name, s.ID, harnessName, err))
				continue
			}
			// A declared cwd that the harness does not read is dropped. Saying
			// so beats letting the author wonder why the server started in the
			// project root instead.
			if s.Cwd != "" && !target.SupportsCwd() {
				ui.LogWarn(fmt.Sprintf("%s: %s ignores a server's cwd, so %q starts in the project root", c.Name, harnessName, s.ID))
			}
			entries[mcpwire.NamespacedID(c.Name, s.ID)] = rendered
		}
		ids, err := target.Install(cwd, entries)
		if err != nil {
			ui.LogWarn(fmt.Sprintf("%s: could not write %s: %v", c.Name, target.ConfigPath, err))
			continue
		}
		if len(ids) > 0 {
			result[harnessName] = ids
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// unwirePluginMCP removes the plugin's server entries from every harness MCP
// config recorded in the lock entry.
func unwirePluginMCP(name string, entry lock.PluginLockEntry, cwd string) {
	for harnessName, ids := range entry.MCP {
		target, ok := mcpwire.Targets[harnessName]
		if !ok {
			continue
		}
		if err := target.Remove(cwd, ids); err != nil {
			ui.LogWarn(fmt.Sprintf("%s: could not clean %s: %v", name, target.ConfigPath, err))
		}
	}
}
