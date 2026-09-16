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
	// honorsCwd reports whether the harness reads a "cwd" field on a stdio
	// server. Claude Code does not: its schema is command/args/env, and
	// `claude mcp get` shows a configured cwd dropped rather than applied.
	// Writing one there would only put this machine's absolute path into a
	// file the team commits, so the field is omitted for targets that ignore
	// it, and SupportsCwd lets the caller say so.
	honorsCwd bool
}

// Targets maps harness names to their project-scope MCP config descriptors.
var Targets = map[string]MCPTarget{
	"claude-code": {HarnessName: "claude-code", ConfigPath: ".mcp.json", ServersKey: "mcpServers", style: styleTyped, honorsCwd: false},
	"cursor":      {HarnessName: "cursor", ConfigPath: ".cursor/mcp.json", ServersKey: "mcpServers", style: styleBare, honorsCwd: true},
}

// SupportsCwd reports whether this target's harness honors a stdio server's
// cwd. A plugin that declares one for a harness that does not is told, rather
// than left to wonder why its server started somewhere else.
func (t MCPTarget) SupportsCwd() bool { return t.honorsCwd }

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
