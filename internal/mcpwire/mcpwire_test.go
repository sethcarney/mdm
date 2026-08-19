package mcpwire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/plugin"
)

func TestNamespacedIDRoundTrip(t *testing.T) {
	id := NamespacedID("my-plugin", "api")
	if id != "my-plugin--api" {
		t.Fatalf("NamespacedID = %q", id)
	}
	pluginName, serverID, ok := ParseNamespacedID(id)
	if !ok || pluginName != "my-plugin" || serverID != "api" {
		t.Errorf("ParseNamespacedID = %q %q %v", pluginName, serverID, ok)
	}
	for _, bad := range []string{"", "--x", "x--", "plain"} {
		if _, _, ok := ParseNamespacedID(bad); ok {
			t.Errorf("ParseNamespacedID(%q) should fail", bad)
		}
	}
}

func TestRenderStdio(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "server"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	s := plugin.Server{
		ID:      "api",
		Type:    plugin.ServerStdio,
		Command: "./bin/server",
		Args:    []string{"--data", "${PLUGIN_DATA}/cache"},
		Env:     map[string]string{"MODE": "fast", "ROOTED": "${PLUGIN_ROOT}/etc"},
	}

	out, err := Targets["claude-code"].RenderServer(s, root, data)
	if err != nil {
		t.Fatal(err)
	}
	if out["type"] != "stdio" {
		t.Errorf("claude-code style should include type, got %v", out["type"])
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if cmd := out["command"].(string); !filepath.IsAbs(cmd) || !strings.HasPrefix(cmd, resolvedRoot) {
		t.Errorf("./-prefixed command should resolve inside the plugin root, got %q", cmd)
	}
	if args := out["args"].([]string); args[1] != data+"/cache" {
		t.Errorf("args should expand PLUGIN_DATA, got %v", args)
	}
	env := out["env"].(map[string]string)
	if env["PLUGIN_ROOT"] != root || env["PLUGIN_DATA"] != data {
		t.Errorf("env must inject PLUGIN_ROOT/PLUGIN_DATA, got %v", env)
	}
	if env["ROOTED"] != root+"/etc" || env["MODE"] != "fast" {
		t.Errorf("env values should expand placeholders, got %v", env)
	}
	if out["cwd"] != root {
		t.Errorf("omitted cwd should default to the plugin root, got %v", out["cwd"])
	}
}

func TestRenderStdioBareStyle(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	// Bare commands pass through for PATH lookup; cursor style omits type.
	bare := plugin.Server{ID: "x", Type: plugin.ServerStdio, Command: "python3", Cwd: "${PLUGIN_DATA}/work"}
	out, err := Targets["cursor"].RenderServer(bare, root, data)
	if err != nil {
		t.Fatal(err)
	}
	if _, hasType := out["type"]; hasType {
		t.Error("cursor style should omit the type field")
	}
	if out["command"] != "python3" {
		t.Errorf("bare command must pass through, got %v", out["command"])
	}
	if out["cwd"] != filepath.Join(data, "work") {
		t.Errorf("cwd should expand PLUGIN_DATA, got %v", out["cwd"])
	}
}

func TestRenderHTTP(t *testing.T) {
	s := plugin.Server{ID: "r", Type: plugin.ServerStreamableHTTP, URL: "https://api.example.com/mcp", Headers: map[string]string{"X-V": "1"}}
	out, err := Targets["claude-code"].RenderServer(s, "/root", "/data")
	if err != nil {
		t.Fatal(err)
	}
	if out["type"] != "http" || out["url"] != "https://api.example.com/mcp" {
		t.Errorf("unexpected render: %v", out)
	}
	sse := plugin.Server{ID: "l", Type: plugin.ServerSSE, URL: "https://sse.example.com/"}
	out, err = Targets["claude-code"].RenderServer(sse, "/root", "/data")
	if err != nil || out["type"] != "sse" {
		t.Errorf("legacy sse should keep its type, got %v (%v)", out, err)
	}
}

func TestInstallPreservesForeignEntriesAndKeys(t *testing.T) {
	projectRoot := t.TempDir()
	target := Targets["claude-code"]
	existing := `{
  "mcpServers": {"user-server": {"command": "keep-me"}},
  "unrelatedTopLevel": {"keep": true}
}
`
	if err := os.WriteFile(filepath.Join(projectRoot, ".mcp.json"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	ids, err := target.Install(projectRoot, map[string]map[string]any{
		"my-plugin--api": {"type": "stdio", "command": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "my-plugin--api" {
		t.Fatalf("ids = %v", ids)
	}

	raw, err := os.ReadFile(filepath.Join(projectRoot, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["unrelatedTopLevel"]; !ok {
		t.Error("unknown top-level keys must survive")
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(parsed["mcpServers"], &servers); err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["user-server"]; !ok {
		t.Error("pre-existing user server must survive")
	}
	if _, ok := servers["my-plugin--api"]; !ok {
		t.Error("plugin server must be written")
	}

	// Remove deletes only owned ids and never the file.
	if err := target.Remove(projectRoot, []string{"my-plugin--api", "not-there"}); err != nil {
		t.Fatal(err)
	}
	_, servers, err = target.readConfig(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["user-server"]; !ok {
		t.Error("remove must not touch user entries")
	}
	if _, ok := servers["my-plugin--api"]; ok {
		t.Error("remove should delete the plugin entry")
	}
}

func TestInstallCreatesFileAndListManaged(t *testing.T) {
	projectRoot := t.TempDir()
	target := Targets["cursor"]
	if _, err := target.Install(projectRoot, map[string]map[string]any{
		"my-plugin--a": {"command": "x"},
		"my-plugin--b": {"url": "https://x.example.com/"},
		"other--c":     {"command": "y"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".cursor", "mcp.json")); err != nil {
		t.Fatalf("config file should be created with parents: %v", err)
	}
	ids, err := target.ListManaged(projectRoot, "my-plugin")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "my-plugin--a" || ids[1] != "my-plugin--b" {
		t.Errorf("ListManaged = %v", ids)
	}
}
