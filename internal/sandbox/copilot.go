package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitHub Copilot CLI hardening has two halves:
//
//   - User settings ($COPILOT_HOME/settings.json, default ~/.copilot):
//     permissions.disableBypassPermissionsMode = "disable" suppresses every
//     allow-all flag (--yolo, --allow-all, --allow-all-tools, ...) so the
//     permission prompts cannot be turned off wholesale.
//
//   - A repo-level preToolUse hook (.github/hooks/): Copilot has no
//     persistent deny rules (permissions-config.json only stores approvals,
//     and --deny-tool is per-session), so the durable way to block secret
//     reads is a pre-tool-use hook that denies any file or shell tool call
//     referencing a secret path. Command hooks are fail-closed: a crashing
//     hook denies the call.
//
// Working-directory confinement is Copilot's default: paths outside trusted
// folders prompt. Status warns when the trusted-folder list looks too broad.

func copilotAgent() Agent {
	return Agent{
		Name:        "github-copilot",
		DisplayName: "GitHub Copilot CLI",
		Notes: []string{
			"The secret guard is a repo-level pre-tool-use hook (.github/hooks/) — commit it to share with the team.",
			"The hook script is POSIX sh, so it protects macOS/Linux sessions; on native Windows it does not run.",
			"Copilot's OS-level sandbox is experimental; enable it per-session with --sandbox or /sandbox enable.",
		},
		Plan:   func(dir string) ([]Change, error) { return copilotSetup(dir, false) },
		Apply:  func(dir string) ([]Change, error) { return copilotSetup(dir, true) },
		Status: copilotStatus,
	}
}

func copilotHome() string {
	return envOrDefaultDir("COPILOT_HOME", ".copilot")
}

func copilotHookJSONPath(projectDir string) string {
	return filepath.Join(projectDir, ".github", "hooks", "mdm-sandbox.json")
}

func copilotHookScriptPath(projectDir string) string {
	return filepath.Join(projectDir, ".github", "hooks", "mdm-sandbox.sh")
}

const copilotHookJSON = `{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "type": "command",
        "bash": "sh .github/hooks/mdm-sandbox.sh",
        "timeoutSec": 10
      }
    ]
  }
}
`

// copilotHookScript builds the pre-tool-use guard script around
// secretHookPattern.
func copilotHookScript() string {
	return `#!/bin/sh
# Secret-path guard installed by 'mdm sandbox setup'.
# Denies GitHub Copilot CLI tool calls whose arguments reference common
# secret locations (.env files, private keys, cloud credential dirs).
# False positive? Adjust the pattern below, or remove this file together
# with .github/hooks/mdm-sandbox.json to drop the guard.

payload=$(cat)

# Only file- and shell-facing tools are guarded.
printf '%s' "$payload" | grep -Eq '"toolName"[[:space:]]*:[[:space:]]*"(bash|powershell|view|create|edit|str_replace_editor|apply_patch|grep|rg|glob)"' || exit 0

# Ignore harmless committed templates before scanning.
scrubbed=$(printf '%s' "$payload" | sed -E 's/\.env\.(example|sample|template)//g')

if printf '%s' "$scrubbed" | grep -Eiq '` + secretHookPattern + `'; then
  printf '{"permissionDecision":"deny","permissionDecisionReason":"mdm sandbox guard: this tool call references a potential secret path (.env, key, or credential file). If this is a false positive, adjust .github/hooks/mdm-sandbox.sh."}'
fi
exit 0
`
}

// copilotSetup writes the user-settings hardening and the repo hook. With
// write set to false it only reports what would change.
func copilotSetup(projectDir string, write bool) ([]Change, error) {
	var changes []Change

	settingsChange, err := copilotSettingsSetup(write)
	if err != nil {
		return nil, err
	}
	changes = append(changes, settingsChange...)

	hookChanges, err := copilotHookSetup(projectDir, write)
	if err != nil {
		return nil, err
	}
	return append(changes, hookChanges...), nil
}

func copilotSettingsSetup(write bool) ([]Change, error) {
	path := filepath.Join(copilotHome(), "settings.json")
	settings, exists, err := readJSONMap(path)
	if err != nil {
		return nil, fmt.Errorf("%w (Copilot settings support JSON with comments, which mdm cannot rewrite safely — add \"permissions\": {\"disableBypassPermissionsMode\": \"disable\"} manually)", err)
	}
	perms := ensureMap(settings, "permissions")
	if perms["disableBypassPermissionsMode"] == "disable" {
		return nil, nil
	}
	perms["disableBypassPermissionsMode"] = "disable"
	if write {
		if err := writeJSONMap(path, settings); err != nil {
			return nil, err
		}
	}
	return []Change{{Path: path, Description: "block --yolo / --allow-all flags", Create: !exists}}, nil
}

func copilotHookSetup(projectDir string, write bool) ([]Change, error) {
	var changes []Change
	files := []struct {
		path, content, desc string
		mode                os.FileMode
	}{
		{copilotHookJSONPath(projectDir), copilotHookJSON, "register the pre-tool-use secret guard hook", 0644},
		{copilotHookScriptPath(projectDir), copilotHookScript(), "install the secret guard script", 0755},
	}
	for _, f := range files {
		existing, err := os.ReadFile(f.path)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if exists && string(existing) == f.content {
			continue
		}
		desc := f.desc
		if exists {
			desc = "update " + filepath.Base(f.path) + " to the current guard"
		}
		changes = append(changes, Change{Path: f.path, Description: desc, Create: !exists})
		if write {
			if err := os.MkdirAll(filepath.Dir(f.path), 0755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(f.path, []byte(f.content), f.mode); err != nil {
				return nil, err
			}
		}
	}
	return changes, nil
}

func copilotStatus(projectDir string) ([]Check, error) {
	var checks []Check

	settingsPath := filepath.Join(copilotHome(), "settings.json")
	settings, exists, err := readJSONMap(settingsPath)
	switch {
	case err != nil:
		checks = append(checks, Check{Name: "Bypass flags blocked", State: StateWarn, Detail: "settings.json could not be parsed: " + err.Error(), Path: settingsPath})
	case !exists:
		checks = append(checks, Check{Name: "Bypass flags blocked", State: StateMissing, Detail: "no user settings — --yolo / --allow-all are not blocked", Path: settingsPath})
	default:
		perms, _ := settings["permissions"].(map[string]any)
		checks = append(checks, boolCheck("Bypass flags blocked", perms != nil && perms["disableBypassPermissionsMode"] == "disable",
			"permissions.disableBypassPermissionsMode = disable",
			"--yolo / --allow-all are not blocked", settingsPath))
	}

	hookOK := fileHasContent(copilotHookJSONPath(projectDir), copilotHookJSON) &&
		fileHasContent(copilotHookScriptPath(projectDir), copilotHookScript())
	checks = append(checks, boolCheck("Secret guard hook", hookOK,
		"pre-tool-use guard installed in .github/hooks/",
		"no pre-tool-use secret guard — run 'mdm sandbox setup'", copilotHookJSONPath(projectDir)))

	if broad := broadTrustedFolders(); len(broad) > 0 {
		checks = append(checks, Check{
			Name:   "Trusted folders",
			State:  StateWarn,
			Detail: "very broad trusted folder(s): " + strings.Join(broad, ", "),
			Path:   filepath.Join(copilotHome(), "config.json"),
		})
	}
	return checks, nil
}

func fileHasContent(path, want string) bool {
	data, err := os.ReadFile(path)
	return err == nil && string(data) == want
}

// broadTrustedFolders flags trusted-folder entries that grant Copilot the
// whole filesystem or home directory. config.json is CLI-managed, so this is
// a read-only advisory check.
func broadTrustedFolders() []string {
	config, exists, err := readJSONMap(filepath.Join(copilotHome(), "config.json"))
	if err != nil || !exists {
		return nil
	}
	var broad []string
	for _, key := range []string{"trustedFolders", "trusted_folders"} {
		for _, folder := range stringSlice(config[key]) {
			clean := filepath.Clean(folder)
			if clean == "/" || clean == homeDir() {
				broad = append(broad, folder)
			}
		}
	}
	return broad
}
