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
// agent's native MCP config and returns agent → namespaced server ids.
// Agents without an MCP config descriptor are skipped silently - skills
// still install for them, matching the spec's incremental-adoption rule.
func wirePluginMCP(c pluginCandidate, destDir, dataDir string, agents []string, opts PluginsAddOptions, cwd string) map[string][]string {
	if opts.SkipMCP {
		return nil
	}
	cfg, _, err := plugin.LoadMCPConfig(destDir)
	if errors.Is(err, plugin.ErrMCPDisabled) {
		ui.LogWarn(fmt.Sprintf("%s: mcp.json is invalid - MCP disabled, skills still installed (run 'mdm plugins validate')", c.Name))
		return nil
	}
	if cfg == nil || len(cfg.Servers) == 0 {
		return nil
	}

	// Re-wiring an update must not leave ids from a removed server behind.
	if prev, ok := lock.ReadPluginsLock(cwd).Plugins[c.Name]; ok {
		unwirePluginMCP(c.Name, prev, cwd)
	}

	result := map[string][]string{}
	for _, agentName := range agents {
		target, ok := mcpwire.Targets[agentName]
		if !ok {
			continue
		}
		entries := map[string]map[string]any{}
		for _, s := range cfg.Servers {
			rendered, err := target.RenderServer(s, destDir, dataDir)
			if err != nil {
				ui.LogWarn(fmt.Sprintf("%s: server %q skipped for %s: %v", c.Name, s.ID, agentName, err))
				continue
			}
			entries[mcpwire.NamespacedID(c.Name, s.ID)] = rendered
		}
		ids, err := target.Install(cwd, entries)
		if err != nil {
			ui.LogWarn(fmt.Sprintf("%s: could not write %s: %v", c.Name, target.ConfigPath, err))
			continue
		}
		if len(ids) > 0 {
			result[agentName] = ids
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// unwirePluginMCP removes the plugin's server entries from every agent MCP
// config recorded in the lock entry.
func unwirePluginMCP(name string, entry lock.PluginLockEntry, cwd string) {
	for agentName, ids := range entry.MCP {
		target, ok := mcpwire.Targets[agentName]
		if !ok {
			continue
		}
		if err := target.Remove(cwd, ids); err != nil {
			ui.LogWarn(fmt.Sprintf("%s: could not clean %s: %v", name, target.ConfigPath, err))
		}
	}
}
