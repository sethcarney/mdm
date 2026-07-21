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
//   - permissions.deny Read() rules block secret reads at the tool layer
//   - permissions.disableBypassPermissionsMode blocks --dangerously-skip-permissions
//   - sandbox.enabled turns on OS-level (Seatbelt/bubblewrap) enforcement for
//     Bash commands, which confines writes to the working directory
//   - sandbox.allowUnsandboxedCommands=false forbids the escape hatch that
//     retries failed commands outside the sandbox
//   - sandbox.filesystem.denyRead is what actually blocks secret *reads* at
//     the OS layer: enabling the sandbox alone still lets commands read the
//     whole disk, so we deny the home directory (re-allowing the project via
//     allowRead ".") plus in-project secret globs
//
// Project settings are committable, so a team shares one hardened baseline.

func claudeAgent() Agent {
	return Agent{
		Name:        "claude-code",
		DisplayName: "Claude Code",
		Notes: []string{
			"Settings are project-scoped (.claude/settings.json) and safe to commit.",
			"The OS sandbox needs bubblewrap+socat on Linux/WSL2; macOS needs nothing extra.",
			"sandbox.filesystem denies reads of your home directory (allowing the project back in) plus in-project secret files — widen with allowWrite/allowRead if a build needs a path in $HOME.",
		},
		Plan:   func(dir string) ([]Change, error) { return claudeSetup(dir, false) },
		Apply:  func(dir string) ([]Change, error) { return claudeSetup(dir, true) },
		Status: claudeStatus,
		HasProjectConfig: func(dir string) bool {
			settings, exists, err := readJSONMap(claudeSettingsPath(dir))
			if err != nil || !exists {
				return false
			}
			_, ok := settings["sandbox"].(map[string]any)
			return ok
		},
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

	var descs []string

	// permissions: Read() deny rules (deduped by gitignore-equivalence so
	// Read(.env) and Read(**/.env) never both appear) + bypass lockdown.
	perms := ensureMap(settings, "permissions")
	if deny, added := mergeDenyRules(stringSlice(perms["deny"]), secretReadDenyRules()); added > 0 {
		perms["deny"] = toAnySlice(deny)
		descs = append(descs, fmt.Sprintf("add %d Read deny rule(s) for secret paths", added))
	}
	if perms["disableBypassPermissionsMode"] != "disable" {
		perms["disableBypassPermissionsMode"] = "disable"
		descs = append(descs, "disable bypass-permissions mode")
	}

	// sandbox: OS-level enforcement + filesystem read blocking.
	sb := ensureMap(settings, "sandbox")
	if sb["enabled"] != true {
		sb["enabled"] = true
		descs = append(descs, "enable the OS sandbox for Bash commands")
	}
	if sb["allowUnsandboxedCommands"] != false {
		sb["allowUnsandboxedCommands"] = false
		descs = append(descs, "forbid the unsandboxed-command escape hatch")
	}
	fsm := ensureMap(sb, "filesystem")
	if allowRead, added := mergeStringSet(stringSlice(fsm["allowRead"]), []string{"."}); added > 0 {
		fsm["allowRead"] = toAnySlice(allowRead)
		descs = append(descs, "allow sandbox reads within the project")
	}
	if denyRead, added := mergeStringSet(stringSlice(fsm["denyRead"]), sandboxDenyReadPaths()); added > 0 {
		fsm["denyRead"] = toAnySlice(denyRead)
		descs = append(descs, "block sandbox reads of home and in-project secrets")
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
	haveKeys := map[string]bool{}
	for _, r := range stringSlice(perms["deny"]) {
		haveKeys[denyRuleKey(r)] = true
	}
	baseline := secretReadDenyRules()
	missing := 0
	for _, rule := range baseline {
		if !haveKeys[denyRuleKey(rule)] {
			missing++
		}
	}
	checks := []Check{
		boolCheck("Secret read deny rules", missing == 0,
			"all baseline rules present",
			fmt.Sprintf("%d of %d baseline rules missing", missing, len(baseline)), path),
		boolCheck("Bypass mode disabled", perms != nil && perms["disableBypassPermissionsMode"] == "disable",
			"permissions.disableBypassPermissionsMode = disable",
			"--dangerously-skip-permissions is not blocked", path),
	}

	sb, _ := settings["sandbox"].(map[string]any)
	checks = append(checks, boolCheck("OS sandbox enabled", sb != nil && sb["enabled"] == true,
		"sandbox.enabled = true",
		"Bash commands are not OS-sandboxed", path))
	checks = append(checks, boolCheck("Strict sandbox", sb != nil && sb["allowUnsandboxedCommands"] == false,
		"allowUnsandboxedCommands = false",
		"commands may fall back to running unsandboxed", path))

	var fsm map[string]any
	if sb != nil {
		fsm, _ = sb["filesystem"].(map[string]any)
	}
	readBlocked := fsm != nil && containsString(stringSlice(fsm["denyRead"]), "~/") && containsString(stringSlice(fsm["allowRead"]), ".")
	checks = append(checks, boolCheck("OS-level secret read blocking", readBlocked,
		"sandbox.filesystem denies home reads, allows the project",
		"sandbox.filesystem.denyRead does not block home — enabling the sandbox alone still allows secret reads", path))

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
