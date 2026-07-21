package sandbox

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Gemini CLI hardening has two halves:
//
//   - User settings (~/.gemini/settings.json): security.folderTrust.enabled
//     turns on the trust gate so an untrusted workspace runs in "safe mode"
//     (project settings, .env files, and MCP servers are not loaded).
//
//   - Project settings (.gemini/settings.json, committable): tools.sandbox
//     isolates every shell tool call, limiting filesystem access to the
//     project directory.
//
// Like Codex, Gemini has no per-file read-deny mechanism: it confines reads
// through sandbox workspace isolation rather than path rules, so secrets that
// live outside the workspace become unreadable while in-workspace files do
// not. That limitation is surfaced as an explicit status check.

func geminiAgent() Agent {
	return Agent{
		Name:        "gemini-cli",
		DisplayName: "Gemini CLI",
		Notes: []string{
			"folderTrust is enabled in user settings (~/.gemini/settings.json); the sandbox is set per-project in .gemini/settings.json (committable).",
			"The sandbox needs a container runtime (Docker or Podman) on Linux; macOS can use the built-in sandbox-exec. Without one, tool execution is not isolated.",
			"Gemini has no per-file read-deny; the sandbox limits filesystem access to the workspace, so keep secrets outside it or protect them with OS permissions.",
		},
		Plan:   func(dir string) ([]Change, error) { return geminiSetup(dir, false) },
		Apply:  func(dir string) ([]Change, error) { return geminiSetup(dir, true) },
		Status: geminiStatus,
		HasProjectConfig: func(dir string) bool {
			// Only claim ownership when tools.sandbox is present, since
			// .gemini/settings.json commonly exists for unrelated settings.
			settings, exists, err := readJSONMap(geminiProjectSettingsPath(dir))
			if err != nil || !exists {
				return false
			}
			tools, ok := settings["tools"].(map[string]any)
			if !ok {
				return false
			}
			_, ok = tools["sandbox"]
			return ok
		},
	}
}

func geminiUserSettingsPath() string {
	return filepath.Join(envOrDefaultDir("GEMINI_HOME", ".gemini"), "settings.json")
}

func geminiProjectSettingsPath(projectDir string) string {
	return filepath.Join(projectDir, ".gemini", "settings.json")
}

// geminiSandboxEnabled reports whether a tools.sandbox value already turns the
// sandbox on (true, or a non-empty mechanism string like "docker"/"podman").
func geminiSandboxEnabled(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false"
	}
	return false
}

func geminiSetup(projectDir string, write bool) ([]Change, error) {
	var changes []Change

	// User settings: folder trust gate.
	userPath := geminiUserSettingsPath()
	userSettings, userExists, err := readJSONMap(userPath)
	if err != nil {
		return nil, err
	}
	ft := ensureMap(ensureMap(userSettings, "security"), "folderTrust")
	if ft["enabled"] != true {
		ft["enabled"] = true
		changes = append(changes, Change{Path: userPath, Description: "enable folder trust (untrusted workspaces run in safe mode)", Create: !userExists})
		if write {
			if err := writeJSONMap(userPath, userSettings); err != nil {
				return nil, err
			}
		}
	}

	// Project settings: sandbox isolation.
	projPath := geminiProjectSettingsPath(projectDir)
	projSettings, projExists, err := readJSONMap(projPath)
	if err != nil {
		return nil, err
	}
	tools := ensureMap(projSettings, "tools")
	if !geminiSandboxEnabled(tools["sandbox"]) {
		tools["sandbox"] = true
		changes = append(changes, Change{Path: projPath, Description: "enable the tool-execution sandbox (confines shell tools to the workspace)", Create: !projExists})
		if write {
			if err := writeJSONMap(projPath, projSettings); err != nil {
				return nil, err
			}
		}
	}

	return changes, nil
}

func geminiStatus(projectDir string) ([]Check, error) {
	userPath := geminiUserSettingsPath()
	userSettings, userExists, err := readJSONMap(userPath)
	if err != nil {
		return nil, err
	}
	var checks []Check

	ftEnabled := false
	if userExists {
		if sec, ok := userSettings["security"].(map[string]any); ok {
			if ft, ok := sec["folderTrust"].(map[string]any); ok {
				ftEnabled = ft["enabled"] == true
			}
		}
	}
	checks = append(checks, boolCheck("Folder trust", ftEnabled,
		"security.folderTrust.enabled = true",
		"untrusted workspaces are not gated — run 'mdm sandbox setup'", userPath))

	projPath := geminiProjectSettingsPath(projectDir)
	projSettings, projExists, err := readJSONMap(projPath)
	if err != nil {
		return nil, err
	}
	sandboxOn := false
	if projExists {
		if tools, ok := projSettings["tools"].(map[string]any); ok {
			sandboxOn = geminiSandboxEnabled(tools["sandbox"])
		}
	}
	detail := "tools.sandbox is not enabled — shell tools run unisolated"
	if sandboxOn {
		v, _ := projSettings["tools"].(map[string]any)
		detail = fmt.Sprintf("tools.sandbox = %v", v["sandbox"])
	}
	checks = append(checks, boolCheck("Tool sandbox", sandboxOn, detail, strings.TrimSpace(detail), projPath))

	checks = append(checks, Check{Name: "Secret read blocking", State: StateUnsupported,
		Detail: "Gemini has no per-file read-deny; the sandbox confines reads to the workspace", Path: projPath})
	return checks, nil
}
