package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Claude Code enforcement lives in the project's .claude/settings.json:
//
//   - permissions.deny Read() rules block secret reads at the app level
//     (they also feed the OS sandbox boundary when it is enabled)
//   - permissions.disableBypassPermissionsMode blocks --dangerously-skip-permissions
//   - sandbox.enabled turns on OS-level (Seatbelt/bubblewrap) enforcement for
//     Bash commands, which confines writes to the working directory and gates
//     network access behind per-domain approval
//
// Project settings are committable, so a team shares one hardened baseline.

func claudeAgent() Agent {
	return Agent{
		Name:        "claude-code",
		DisplayName: "Claude Code",
		Notes: []string{
			"Settings are project-scoped (.claude/settings.json) and safe to commit.",
			"The OS sandbox needs bubblewrap+socat on Linux/WSL2; macOS needs nothing extra.",
			"Deny rules cover Claude's file tools and recognized shell commands; the OS sandbox extends enforcement to all Bash subprocesses.",
		},
		Plan:   func(dir string) ([]Change, error) { return claudeSetup(dir, false) },
		Apply:  func(dir string) ([]Change, error) { return claudeSetup(dir, true) },
		Status: claudeStatus,
	}
}

func claudeSettingsPath(projectDir string) string {
	return filepath.Join(projectDir, ".claude", "settings.json")
}

// claudeSetup merges the baseline into .claude/settings.json. With write set
// to false it only reports what would change.
func claudeSetup(projectDir string, write bool) ([]Change, error) {
	path := claudeSettingsPath(projectDir)
	settings, exists, err := readJSONMap(path)
	if err != nil {
		return nil, err
	}

	var added []string

	perms := ensureMap(settings, "permissions")
	deny := stringSlice(perms["deny"])
	for _, rule := range secretDenyReadRules {
		if !containsString(deny, rule) {
			deny = append(deny, rule)
			added = append(added, rule)
		}
	}
	perms["deny"] = toAnySlice(deny)

	var descs []string
	if len(added) > 0 {
		descs = append(descs, fmt.Sprintf("add %d Read deny rule(s) for secret paths", len(added)))
	}
	if perms["disableBypassPermissionsMode"] != "disable" {
		perms["disableBypassPermissionsMode"] = "disable"
		descs = append(descs, "disable bypass-permissions mode")
	}

	sb := ensureMap(settings, "sandbox")
	if sb["enabled"] != true {
		sb["enabled"] = true
		descs = append(descs, "enable the OS sandbox for Bash commands")
	}

	if len(descs) == 0 {
		return nil, nil
	}
	change := Change{Path: path, Description: strings.Join(descs, "; "), Create: !exists}
	if write {
		if err := writeJSONMap(path, settings); err != nil {
			return nil, err
		}
	}
	return []Change{change}, nil
}

func claudeStatus(projectDir string) ([]Check, error) {
	path := claudeSettingsPath(projectDir)
	settings, exists, err := readJSONMap(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Check{{Name: "Project settings", State: StateMissing, Detail: "no .claude/settings.json — run 'mdm sandbox setup'", Path: path}}, nil
	}

	perms, _ := settings["permissions"].(map[string]any)
	deny := stringSlice(perms["deny"])
	missing := 0
	for _, rule := range secretDenyReadRules {
		if !containsString(deny, rule) {
			missing++
		}
	}
	checks := []Check{
		boolCheck("Secret read deny rules", missing == 0,
			"all baseline rules present",
			fmt.Sprintf("%d of %d baseline rules missing", missing, len(secretDenyReadRules)), path),
		boolCheck("Bypass mode disabled", perms != nil && perms["disableBypassPermissionsMode"] == "disable",
			"permissions.disableBypassPermissionsMode = disable",
			"--dangerously-skip-permissions is not blocked", path),
	}

	sb, _ := settings["sandbox"].(map[string]any)
	checks = append(checks, boolCheck("OS sandbox enabled", sb != nil && sb["enabled"] == true,
		"sandbox.enabled = true",
		"Bash commands are not OS-sandboxed", path))

	if extra := stringSlice(perms["additionalDirectories"]); len(extra) > 0 {
		checks = append(checks, Check{
			Name:   "Additional directories",
			State:  StateWarn,
			Detail: fmt.Sprintf("access extends beyond the project: %s", strings.Join(extra, ", ")),
			Path:   path,
		})
	}
	return checks, nil
}

func boolCheck(name string, ok bool, okDetail, missingDetail, path string) Check {
	if ok {
		return Check{Name: name, State: StateOK, Detail: okDetail, Path: path}
	}
	return Check{Name: name, State: StateMissing, Detail: missingDetail, Path: path}
}

// ─── JSON helpers ─────────────────────────────────────────────────────────────

// readJSONMap loads a JSON object file. A missing file yields an empty map
// with exists=false. Invalid JSON is an error rather than data loss.
func readJSONMap(path string) (m map[string]any, exists bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, true, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, true, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	return m, true, nil
}

func writeJSONMap(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if m, ok := parent[key].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	parent[key] = m
	return m
}

func stringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func toAnySlice(items []string) []any {
	out := make([]any, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func containsString(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}
