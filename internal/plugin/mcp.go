package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ErrMCPDisabled is returned by LoadMCPConfig when mcp.json exists but its
// top-level structure is invalid or targets an unsupported schema. Per the
// spec this disables the MCP component only — the plugin's skills still
// load.
var ErrMCPDisabled = errors.New("mcp configuration disabled")

type ServerType string

const (
	ServerStdio          ServerType = "stdio"
	ServerStreamableHTTP ServerType = "streamable-http"
	ServerSSE            ServerType = "sse"
)

// Server is one validated mcp.json server entry.
type Server struct {
	ID   string
	Type ServerType
	// stdio
	Command string
	Args    []string
	Env     map[string]string
	Cwd     string
	// streamable-http / sse
	URL     string
	Headers map[string]string
}

type MCPConfig struct {
	Schema  string
	Servers []Server
}

// LoadMCPConfig parses <root>/mcp.json with the spec's resilience rules:
//   - missing file → (nil, nil, nil)
//   - invalid JSON, top-level schema violation, or unsupported $schema →
//     (nil, issues, ErrMCPDisabled)
//   - invalid individual server → dropped, reported in issues
func LoadMCPConfig(root string) (*MCPConfig, []Issue, error) {
	mcpPath := filepath.Join(root, MCPFile)
	if EscapesRoot(root, mcpPath) {
		return nil, []Issue{mcpIssue(SeverityError, "escapes-root", MCPFile+" resolves outside the plugin root — MCP disabled")}, ErrMCPDisabled
	}
	data, err := os.ReadFile(mcpPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, []Issue{mcpIssue(SeverityError, "unreadable", err.Error())}, ErrMCPDisabled
	}
	rawServers, issues, err := parseMCPTopLevel(data)
	if err != nil {
		return nil, issues, err
	}
	cfg := &MCPConfig{Schema: MCPSchema}
	for _, id := range sortedKeys(rawServers) {
		server, serverIssues := parseServer(id, rawServers[id])
		issues = append(issues, serverIssues...)
		if server != nil {
			cfg.Servers = append(cfg.Servers, *server)
		}
	}
	return cfg, issues, nil
}

func mcpIssue(sev Severity, rule, message string) Issue {
	return Issue{File: MCPFile, Severity: sev, Rule: "mcp-" + rule, Message: message}
}

// parseMCPTopLevel enforces the closed top level: exactly $schema (matching
// this build's supported version) and mcpServers (an object). Any violation
// disables MCP entirely.
func parseMCPTopLevel(data []byte) (map[string]json.RawMessage, []Issue, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, []Issue{mcpIssue(SeverityError, "invalid-json", err.Error())}, ErrMCPDisabled
	}
	for _, key := range sortedKeys(raw) {
		if key != "$schema" && key != "mcpServers" {
			return nil, []Issue{mcpIssue(SeverityError, "unknown-field", fmt.Sprintf("unknown top-level field %q", key))}, ErrMCPDisabled
		}
	}
	var schema string
	if raw["$schema"] == nil || json.Unmarshal(raw["$schema"], &schema) != nil {
		return nil, []Issue{mcpIssue(SeverityError, "missing-schema", "$schema is missing or not a string")}, ErrMCPDisabled
	}
	if schema != MCPSchema {
		return nil, []Issue{mcpIssue(SeverityError, "unsupported-schema", fmt.Sprintf("unsupported $schema %q — this build supports %s", schema, MCPSchema))}, ErrMCPDisabled
	}
	var servers map[string]json.RawMessage
	if raw["mcpServers"] == nil || json.Unmarshal(raw["mcpServers"], &servers) != nil {
		return nil, []Issue{mcpIssue(SeverityError, "invalid-servers", "mcpServers is missing or not an object")}, ErrMCPDisabled
	}
	return servers, nil, nil
}

// parseServer validates one server entry against the closed transport
// union. An invalid entry returns a nil server plus issues — the spec says
// to skip it and continue loading the rest.
func parseServer(id string, value json.RawMessage) (*Server, []Issue) {
	invalid := func(format string, args ...interface{}) (*Server, []Issue) {
		return nil, []Issue{mcpIssue(SeverityError, "invalid-server", fmt.Sprintf("server %q skipped: %s", id, fmt.Sprintf(format, args...)))}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(value, &raw); err != nil {
		return invalid("not an object")
	}
	var serverType string
	if raw["type"] == nil || json.Unmarshal(raw["type"], &serverType) != nil {
		return invalid("type is missing or not a string")
	}
	server := &Server{ID: id, Type: ServerType(serverType)}
	var err error
	switch server.Type {
	case ServerStdio:
		err = parseStdioServer(server, raw)
	case ServerStreamableHTTP, ServerSSE:
		err = parseHTTPServer(server, raw)
	default:
		return invalid("unknown type %q", serverType)
	}
	if err != nil {
		return invalid("%s", err)
	}
	return server, nil
}

func parseStdioServer(server *Server, raw map[string]json.RawMessage) error {
	for _, key := range sortedKeys(raw) {
		var err error
		switch key {
		case "type":
		case "command":
			err = json.Unmarshal(raw[key], &server.Command)
		case "args":
			err = json.Unmarshal(raw[key], &server.Args)
		case "env":
			err = json.Unmarshal(raw[key], &server.Env)
		case "cwd":
			err = json.Unmarshal(raw[key], &server.Cwd)
		default:
			return fmt.Errorf("unknown field %q", key)
		}
		if err != nil {
			return fmt.Errorf("field %s has the wrong type", key)
		}
	}
	if err := validateCommand(server.Command); err != nil {
		return err
	}
	for key := range server.Env {
		if key == "PLUGIN_ROOT" || key == "PLUGIN_DATA" {
			return fmt.Errorf("env must not contain %s — the client sets it", key)
		}
	}
	return ValidateCwd(server.Cwd)
}

// validateCommand enforces the single-executable-token rule: a bare
// program name resolved via platform search rules, or a "./"-prefixed
// plugin-relative path. No placeholder expansion applies to command.
func validateCommand(command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	if strings.ContainsAny(command, " \t") {
		return fmt.Errorf("command must be a single executable token")
	}
	if strings.Contains(command, "${") {
		return fmt.Errorf("command must not contain placeholders")
	}
	if strings.ContainsAny(command, `/\`) && !strings.HasPrefix(command, "./") {
		return fmt.Errorf("command paths must be ./-prefixed and plugin-relative")
	}
	if strings.HasPrefix(command, "./") && escapes(strings.TrimPrefix(command, "./")) {
		return fmt.Errorf("command path escapes the plugin root")
	}
	return nil
}

func parseHTTPServer(server *Server, raw map[string]json.RawMessage) error {
	for _, key := range sortedKeys(raw) {
		var err error
		switch key {
		case "type":
		case "url":
			err = json.Unmarshal(raw[key], &server.URL)
		case "headers":
			err = json.Unmarshal(raw[key], &server.Headers)
		default:
			return fmt.Errorf("unknown field %q", key)
		}
		if err != nil {
			return fmt.Errorf("field %s has the wrong type", key)
		}
	}
	if err := validateServerURL(server.URL); err != nil {
		return err
	}
	return validateHeaders(server.Headers)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// validateServerURL enforces an absolute HTTP(S) URL without user info or
// fragment, HTTPS except on loopback hosts.
func validateServerURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("url is not valid: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must be absolute http or https")
	}
	if u.User != nil || u.Fragment != "" || u.Host == "" {
		return fmt.Errorf("url must not contain user info or a fragment")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("http is only allowed for loopback hosts")
	}
	return nil
}

// validateHeaders rejects duplicate header names differing only by case.
func validateHeaders(headers map[string]string) error {
	seen := make(map[string]string, len(headers))
	for name := range headers {
		lower := strings.ToLower(name)
		if other, ok := seen[lower]; ok {
			return fmt.Errorf("duplicate header %q / %q", other, name)
		}
		seen[lower] = name
	}
	return nil
}
