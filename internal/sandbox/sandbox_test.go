package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points every agent's home-based config at fresh temp dirs so tests
// never read or write the developer's real configuration.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("COPILOT_HOME", filepath.Join(home, ".copilot"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	return home
}

// ─── Claude Code ──────────────────────────────────────────────────────────────

func TestClaudeSetupFreshProject(t *testing.T) {
	isolate(t)
	project := t.TempDir()

	changes, err := claudeSetup(project, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Create {
		t.Fatalf("expected one create change, got %+v", changes)
	}

	settings, exists, err := readJSONMap(claudeSettingsPath(project))
	if err != nil || !exists {
		t.Fatalf("settings not written: exists=%v err=%v", exists, err)
	}
	perms := settings["permissions"].(map[string]any)
	deny := stringSlice(perms["deny"])
	for _, rule := range secretDenyReadRules {
		if !containsString(deny, rule) {
			t.Errorf("missing deny rule %q", rule)
		}
	}
	if perms["disableBypassPermissionsMode"] != "disable" {
		t.Error("bypass mode not disabled")
	}
	if sb := settings["sandbox"].(map[string]any); sb["enabled"] != true {
		t.Error("sandbox not enabled")
	}

	// Second run is a no-op.
	changes, err = claudeSetup(project, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected idempotent setup, got %+v", changes)
	}
}

func TestClaudeSetupPreservesExistingSettings(t *testing.T) {
	isolate(t)
	project := t.TempDir()
	path := claudeSettingsPath(project)
	existing := map[string]any{
		"model": "opus",
		"permissions": map[string]any{
			"allow": []any{"Bash(npm test *)"},
			"deny":  []any{"Read(.env)", "WebFetch"},
		},
	}
	if err := writeJSONMap(path, existing); err != nil {
		t.Fatal(err)
	}

	if _, err := claudeSetup(project, true); err != nil {
		t.Fatal(err)
	}

	settings, _, _ := readJSONMap(path)
	if settings["model"] != "opus" {
		t.Error("unrelated key was dropped")
	}
	perms := settings["permissions"].(map[string]any)
	if allow := stringSlice(perms["allow"]); len(allow) != 1 || allow[0] != "Bash(npm test *)" {
		t.Errorf("allow rules changed: %v", allow)
	}
	deny := stringSlice(perms["deny"])
	if !containsString(deny, "WebFetch") {
		t.Error("user deny rule was dropped")
	}
	count := 0
	for _, r := range deny {
		if r == "Read(.env)" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Read(.env) duplicated: %d occurrences", count)
	}
}

func TestClaudeSetupRejectsInvalidJSON(t *testing.T) {
	isolate(t)
	project := t.TempDir()
	path := claudeSettingsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := claudeSetup(project, true); err == nil {
		t.Fatal("expected error on invalid JSON, got none")
	}
	if data, _ := os.ReadFile(path); string(data) != "{not json" {
		t.Error("invalid file was overwritten")
	}
}

func TestClaudeStatus(t *testing.T) {
	isolate(t)
	project := t.TempDir()

	checks, err := claudeStatus(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 1 || checks[0].State != StateMissing {
		t.Fatalf("expected single missing check for fresh project, got %+v", checks)
	}

	if _, err := claudeSetup(project, true); err != nil {
		t.Fatal(err)
	}
	checks, err = claudeStatus(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.State != StateOK {
			t.Errorf("check %q = %s (%s), want ok", c.Name, c.State, c.Detail)
		}
	}
}

func TestClaudeStatusWarnsOnAdditionalDirectories(t *testing.T) {
	isolate(t)
	project := t.TempDir()
	if _, err := claudeSetup(project, true); err != nil {
		t.Fatal(err)
	}
	settings, _, _ := readJSONMap(claudeSettingsPath(project))
	settings["permissions"].(map[string]any)["additionalDirectories"] = []any{"/tmp"}
	if err := writeJSONMap(claudeSettingsPath(project), settings); err != nil {
		t.Fatal(err)
	}

	checks, err := claudeStatus(project)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range checks {
		if c.Name == "Additional directories" && c.State == StateWarn {
			found = true
		}
	}
	if !found {
		t.Errorf("expected additionalDirectories warning, got %+v", checks)
	}
}

// ─── Codex ────────────────────────────────────────────────────────────────────

func TestCodexSetupFreshConfig(t *testing.T) {
	isolate(t)

	changes, err := codexSetup(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !changes[0].Create {
		t.Fatalf("expected one create change, got %+v", changes)
	}

	lines, exists, _ := readLines(codexConfigPath())
	if !exists {
		t.Fatal("config.toml not written")
	}
	assertToml(t, lines, "", "sandbox_mode", "workspace-write")
	assertToml(t, lines, "", "approval_policy", "on-request")
	assertToml(t, lines, "sandbox_workspace_write", "network_access", "false")

	if changes, _ := codexSetup(false); len(changes) != 0 {
		t.Fatalf("expected idempotent setup, got %+v", changes)
	}
}

func TestCodexSetupPreservesCommentsAndKeys(t *testing.T) {
	isolate(t)
	path := codexConfigPath()
	original := `# my codex config
model = "gpt-5.2"  # pinned

sandbox_mode = "danger-full-access"

[mcp_servers.github]
command = "gh-mcp"

[sandbox_workspace_write]
network_access = true
writable_roots = ["/opt/cache"]
`
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := codexSetup(true); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	for _, want := range []string{"# my codex config", `model = "gpt-5.2"  # pinned`, "[mcp_servers.github]", `command = "gh-mcp"`, `writable_roots = ["/opt/cache"]`} {
		if !strings.Contains(content, want) {
			t.Errorf("lost existing content %q in:\n%s", want, content)
		}
	}
	lines := strings.Split(content, "\n")
	assertToml(t, lines, "", "sandbox_mode", "workspace-write")
	assertToml(t, lines, "", "approval_policy", "on-request")
	assertToml(t, lines, "sandbox_workspace_write", "network_access", "false")
}

func TestCodexSetupKeepsStricterSettings(t *testing.T) {
	isolate(t)
	path := codexConfigPath()
	original := `sandbox_mode = "read-only"
approval_policy = "untrusted"

[sandbox_workspace_write]
network_access = false
`
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	changes, err := codexSetup(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("stricter settings should be untouched, got %+v", changes)
	}
	lines, _, _ := readLines(path)
	assertToml(t, lines, "", "sandbox_mode", "read-only")
	assertToml(t, lines, "", "approval_policy", "untrusted")
}

func TestCodexStatusWarnsOnDangerousSettings(t *testing.T) {
	isolate(t)
	path := codexConfigPath()
	original := `sandbox_mode = "danger-full-access"
approval_policy = "never"
`
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	checks, err := codexStatus()
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]CheckState{}
	for _, c := range checks {
		states[c.Name] = c.State
	}
	if states["Write confinement"] != StateWarn {
		t.Errorf("danger-full-access should warn, got %s", states["Write confinement"])
	}
	if states["Escape approvals"] != StateWarn {
		t.Errorf("approval_policy=never should warn, got %s", states["Escape approvals"])
	}
	if states["Secret read blocking"] != StateUnsupported {
		t.Errorf("secret read blocking should be unsupported for codex, got %s", states["Secret read blocking"])
	}
}

func assertToml(t *testing.T, lines []string, table, key, want string) {
	t.Helper()
	got, found := tomlGet(lines, table, key)
	if !found {
		t.Errorf("[%s] %s not found", table, key)
		return
	}
	if got != want {
		t.Errorf("[%s] %s = %q, want %q", table, key, got, want)
	}
}

// ─── GitHub Copilot ───────────────────────────────────────────────────────────

func TestCopilotSetupWritesHookAndSettings(t *testing.T) {
	isolate(t)
	project := t.TempDir()

	changes, err := copilotSetup(project, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes (settings + hook json + script), got %+v", changes)
	}

	settings, _, err := readJSONMap(filepath.Join(copilotHome(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	perms := settings["permissions"].(map[string]any)
	if perms["disableBypassPermissionsMode"] != "disable" {
		t.Error("bypass flags not blocked")
	}

	var hook map[string]any
	data, err := os.ReadFile(copilotHookJSONPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &hook); err != nil {
		t.Fatalf("hook json invalid: %v", err)
	}
	info, err := os.Stat(copilotHookScriptPath(project))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0100 == 0 {
		t.Error("hook script is not executable")
	}

	if changes, _ := copilotSetup(project, false); len(changes) != 0 {
		t.Fatalf("expected idempotent setup, got %+v", changes)
	}
}

func TestCopilotSetupPreservesUserSettings(t *testing.T) {
	isolate(t)
	project := t.TempDir()
	path := filepath.Join(copilotHome(), "settings.json")
	if err := writeJSONMap(path, map[string]any{"theme": "dim", "allowedUrls": []any{"github.com"}}); err != nil {
		t.Fatal(err)
	}

	if _, err := copilotSetup(project, true); err != nil {
		t.Fatal(err)
	}
	settings, _, _ := readJSONMap(path)
	if settings["theme"] != "dim" {
		t.Error("unrelated setting dropped")
	}
	if urls := stringSlice(settings["allowedUrls"]); len(urls) != 1 || urls[0] != "github.com" {
		t.Errorf("allowedUrls changed: %v", urls)
	}
}

func TestCopilotStatusAfterSetup(t *testing.T) {
	isolate(t)
	project := t.TempDir()

	if _, err := copilotSetup(project, true); err != nil {
		t.Fatal(err)
	}
	checks, err := copilotStatus(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.State != StateOK {
			t.Errorf("check %q = %s (%s), want ok", c.Name, c.State, c.Detail)
		}
	}
}

func TestCopilotStatusWarnsOnBroadTrustedFolders(t *testing.T) {
	home := isolate(t)
	project := t.TempDir()
	if err := writeJSONMap(filepath.Join(copilotHome(), "config.json"), map[string]any{
		"trustedFolders": []any{home, filepath.Join(home, "code")},
	}); err != nil {
		t.Fatal(err)
	}

	checks, err := copilotStatus(project)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range checks {
		if c.Name == "Trusted folders" && c.State == StateWarn {
			found = true
			if strings.Contains(c.Detail, filepath.Join(home, "code")) {
				t.Error("scoped folder flagged as broad")
			}
		}
	}
	if !found {
		t.Errorf("expected trusted-folder warning, got %+v", checks)
	}
}

// ─── shared ───────────────────────────────────────────────────────────────────

func TestAgentsRegistry(t *testing.T) {
	agents := Agents()
	if len(agents) != 3 {
		t.Fatalf("expected 3 supported agents, got %d", len(agents))
	}
	for _, name := range []string{"claude-code", "codex", "github-copilot"} {
		if !Supported(name) {
			t.Errorf("%s should be supported", name)
		}
	}
	if Supported("cursor") {
		t.Error("cursor should not be supported")
	}
}
