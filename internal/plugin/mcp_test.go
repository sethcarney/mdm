package plugin

import (
	"errors"
	"fmt"
	"testing"
)

func mcpJSON(servers string) string {
	return fmt.Sprintf(`{"$schema": %q, "mcpServers": {%s}}`, MCPSchema, servers)
}

func loadMCP(t *testing.T, contents string) (*MCPConfig, []Issue, error) {
	t.Helper()
	root := writePlugin(t, map[string]string{MCPFile: contents})
	return LoadMCPConfig(root)
}

func TestLoadMCPConfigMissing(t *testing.T) {
	cfg, issues, err := LoadMCPConfig(t.TempDir())
	if cfg != nil || issues != nil || err != nil {
		t.Errorf("missing mcp.json should be a no-op, got %v %v %v", cfg, issues, err)
	}
}

func TestLoadMCPConfigTopLevelDisables(t *testing.T) {
	cases := map[string]string{
		"invalid-json":       `{not json`,
		"unknown-top-level":  fmt.Sprintf(`{"$schema": %q, "mcpServers": {}, "extra": 1}`, MCPSchema),
		"missing-schema":     `{"mcpServers": {}}`,
		"unsupported-schema": `{"$schema": "https://agent-plugins.org/schemas/9.0.0/mcp.schema.json", "mcpServers": {}}`,
		"missing-servers":    fmt.Sprintf(`{"$schema": %q}`, MCPSchema),
		"servers-not-object": fmt.Sprintf(`{"$schema": %q, "mcpServers": []}`, MCPSchema),
	}
	for label, contents := range cases {
		cfg, issues, err := loadMCP(t, contents)
		if !errors.Is(err, ErrMCPDisabled) || cfg != nil {
			t.Errorf("%s: want ErrMCPDisabled and nil config, got %v %v", label, cfg, err)
		}
		if !HasErrors(issues) {
			t.Errorf("%s: want an error issue, got %v", label, issues)
		}
	}
}

func TestLoadMCPConfigEmptyServersValid(t *testing.T) {
	cfg, issues, err := loadMCP(t, mcpJSON(""))
	if err != nil || len(issues) != 0 || cfg == nil || len(cfg.Servers) != 0 {
		t.Errorf("empty mcpServers is valid, got %v %v %v", cfg, issues, err)
	}
}

func TestParseStdioServer(t *testing.T) {
	cfg, issues, err := loadMCP(t, mcpJSON(
		`"good": {"type": "stdio", "command": "./bin/server", "args": ["--data", "${PLUGIN_DATA}"],
		          "env": {"MODE": "fast"}, "cwd": "${PLUGIN_ROOT}/work"}`))
	if err != nil || len(issues) != 0 {
		t.Fatalf("unexpected: %v %v", issues, err)
	}
	s := cfg.Servers[0]
	if s.ID != "good" || s.Type != ServerStdio || s.Command != "./bin/server" || s.Cwd != "${PLUGIN_ROOT}/work" {
		t.Errorf("unexpected server: %+v", s)
	}
}

func TestInvalidServersSkippedNotFatal(t *testing.T) {
	cfg, issues, err := loadMCP(t, mcpJSON(
		`"ok":            {"type": "stdio", "command": "server"},
		 "no-type":       {"command": "x"},
		 "bad-type":      {"type": "websocket", "url": "wss://x"},
		 "no-command":    {"type": "stdio"},
		 "multi-token":   {"type": "stdio", "command": "python server.py"},
		 "placeholder":   {"type": "stdio", "command": "${PLUGIN_ROOT}/bin/x"},
		 "bare-path":     {"type": "stdio", "command": "bin/server"},
		 "abs-path":      {"type": "stdio", "command": "/usr/bin/x"},
		 "escape-cmd":    {"type": "stdio", "command": "./../evil"},
		 "reserved-env":  {"type": "stdio", "command": "x", "env": {"PLUGIN_ROOT": "/tmp"}},
		 "bad-cwd":       {"type": "stdio", "command": "x", "cwd": "/abs"},
		 "escape-cwd":    {"type": "stdio", "command": "x", "cwd": "./../out"},
		 "unknown-field": {"type": "stdio", "command": "x", "shell": true}`))
	if err != nil {
		t.Fatalf("individual bad servers must not disable MCP: %v", err)
	}
	if len(cfg.Servers) != 1 || cfg.Servers[0].ID != "ok" {
		t.Errorf("want only server %q to survive, got %+v", "ok", cfg.Servers)
	}
	if len(issues) != 12 {
		t.Errorf("want 12 skip issues, got %d: %v", len(issues), issues)
	}
}

func TestParseHTTPServers(t *testing.T) {
	cfg, issues, err := loadMCP(t, mcpJSON(
		`"remote":   {"type": "streamable-http", "url": "https://api.example.com/mcp", "headers": {"X-Api-Version": "1"}},
		 "local":    {"type": "streamable-http", "url": "http://localhost:3000/mcp"},
		 "loopback": {"type": "streamable-http", "url": "http://127.0.0.1:8080/"},
		 "legacy":   {"type": "sse", "url": "https://sse.example.com/events"}`))
	if err != nil || len(issues) != 0 {
		t.Fatalf("unexpected: %v %v", issues, err)
	}
	if len(cfg.Servers) != 4 {
		t.Errorf("want 4 servers, got %+v", cfg.Servers)
	}
}

func TestInvalidHTTPServersSkipped(t *testing.T) {
	_, issues, err := loadMCP(t, mcpJSON(
		`"plain-http":  {"type": "streamable-http", "url": "http://api.example.com/mcp"},
		 "userinfo":    {"type": "streamable-http", "url": "https://user:pw@example.com/"},
		 "fragment":    {"type": "streamable-http", "url": "https://example.com/mcp#frag"},
		 "not-a-url":   {"type": "streamable-http", "url": "not a url"},
		 "no-url":      {"type": "streamable-http"},
		 "dup-headers": {"type": "streamable-http", "url": "https://x.example.com/", "headers": {"X-A": "1", "x-a": "2"}}`))
	if err != nil {
		t.Fatalf("individual bad servers must not disable MCP: %v", err)
	}
	if len(issues) != 6 {
		t.Errorf("want 6 skip issues, got %d: %v", len(issues), issues)
	}
}
