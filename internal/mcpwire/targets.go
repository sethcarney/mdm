// Package mcpwire translates a plugin's validated mcp.json servers into
// the MCP config files of specific harnesses. It lives apart from the stable
// harness registry (internal/harness) so the experimental plugins feature
// never touches stable state.
package mcpwire

import (
	"strings"
)

// renderStyle selects the JSON dialect a harness's MCP config expects.
type renderStyle int

const (
	// styleTyped writes an explicit "type" field per server, with
	// streamable HTTP spelled "http" (Claude Code's .mcp.json).
	styleTyped renderStyle = iota
	// styleBare omits the type field - the presence of "command" vs
	// "url" implies the transport (Cursor's mcp.json).
	styleBare
)

// MCPTarget describes one harness's MCP config file. Adding a harness is one
// map entry in Targets.
type MCPTarget struct {
	HarnessName string // key into harness.AllHarnesses
	ConfigPath  string // project-relative config file path
	ServersKey  string // top-level key holding the server map
	style       renderStyle
}

// Targets maps harness names to their project-scope MCP config descriptors.
var Targets = map[string]MCPTarget{
	"claude-code": {HarnessName: "claude-code", ConfigPath: ".mcp.json", ServersKey: "mcpServers", style: styleTyped},
	"cursor":      {HarnessName: "cursor", ConfigPath: ".cursor/mcp.json", ServersKey: "mcpServers", style: styleBare},
}

// idSeparator joins plugin and server names. The spec forbids consecutive
// hyphens inside plugin names, so the split is unambiguous, and it avoids
// the ':' and '__' sequences harnesses use in MCP tool-name mangling.
const idSeparator = "--"

// NamespacedID is the server id written into harness config, namespaced by
// plugin so two plugins can both ship a server called "api".
func NamespacedID(pluginName, serverID string) string {
	return pluginName + idSeparator + serverID
}

// ParseNamespacedID splits a namespaced id back into plugin and server.
func ParseNamespacedID(id string) (pluginName, serverID string, ok bool) {
	i := strings.Index(id, idSeparator)
	if i <= 0 || i+len(idSeparator) >= len(id) {
		return "", "", false
	}
	return id[:i], id[i+len(idSeparator):], true
}
