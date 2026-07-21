package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Codex reads its sandbox policy from $CODEX_HOME/config.toml (global only —
// Codex has no per-project config file):
//
//   - sandbox_mode = "workspace-write" confines writes to the workspace
//   - [sandbox_workspace_write] network_access = false keeps commands offline
//   - approval_policy = "on-request" makes escapes outside the sandbox prompt
//
// Codex's sandbox cannot restrict file *reads*: read-only and workspace-write
// both allow reading any file on disk. That limitation is surfaced as an
// explicit "unsupported" status check instead of being papered over.
//
// The config is edited line-by-line so user comments and unrelated keys are
// preserved exactly.

func codexAgent() Agent {
	return Agent{
		Name:        "codex",
		DisplayName: "Codex",
		Notes: []string{
			"Codex config is global ($CODEX_HOME/config.toml) — it has no per-project config file.",
			"Codex's sandbox confines writes and network, but cannot block file reads: keep secrets out of reachable paths or use OS file permissions.",
			"Stricter existing settings (read-only mode, untrusted approvals) are kept as-is.",
		},
		Plan:   func(dir string) ([]Change, error) { return codexSetup(false) },
		Apply:  func(dir string) ([]Change, error) { return codexSetup(true) },
		Status: func(dir string) ([]Check, error) { return codexStatus() },
	}
}

func codexConfigPath() string {
	return filepath.Join(envOrDefaultDir("CODEX_HOME", ".codex"), "config.toml")
}

// codexSetup tightens config.toml. With write set to false it only reports
// what would change.
func codexSetup(write bool) ([]Change, error) {
	path := codexConfigPath()
	lines, exists, err := readLines(path)
	if err != nil {
		return nil, err
	}

	var descs []string

	mode, _ := tomlGet(lines, "", "sandbox_mode")
	if mode != "read-only" && mode != "workspace-write" {
		lines = tomlSet(lines, "", "sandbox_mode", `"workspace-write"`)
		if mode == "danger-full-access" {
			descs = append(descs, "replace danger-full-access with workspace-write sandbox")
		} else {
			descs = append(descs, "confine writes to the workspace (sandbox_mode)")
		}
	}

	policy, _ := tomlGet(lines, "", "approval_policy")
	if policy != "untrusted" && policy != "on-failure" && policy != "on-request" {
		lines = tomlSet(lines, "", "approval_policy", `"on-request"`)
		descs = append(descs, "require approval to leave the sandbox (approval_policy)")
	}

	if net, _ := tomlGet(lines, "sandbox_workspace_write", "network_access"); net != "false" {
		lines = tomlSet(lines, "sandbox_workspace_write", "network_access", "false")
		descs = append(descs, "turn off network access for sandboxed commands")
	}

	if len(descs) == 0 {
		return nil, nil
	}
	change := Change{Path: path, Description: strings.Join(descs, "; "), Create: !exists}
	if write {
		if err := writeLines(path, lines); err != nil {
			return nil, err
		}
	}
	return []Change{change}, nil
}

func codexStatus() ([]Check, error) {
	path := codexConfigPath()
	lines, exists, err := readLines(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Check{{Name: "Global config", State: StateMissing, Detail: "no config.toml — Codex defaults apply; run 'mdm sandbox setup' to pin the policy", Path: path}}, nil
	}

	var checks []Check

	mode, _ := tomlGet(lines, "", "sandbox_mode")
	switch mode {
	case "read-only", "workspace-write":
		checks = append(checks, Check{Name: "Write confinement", State: StateOK, Detail: "sandbox_mode = " + mode, Path: path})
	case "danger-full-access":
		checks = append(checks, Check{Name: "Write confinement", State: StateWarn, Detail: "sandbox_mode = danger-full-access disables the sandbox", Path: path})
	default:
		checks = append(checks, Check{Name: "Write confinement", State: StateMissing, Detail: "sandbox_mode not pinned (Codex defaults to read-only)", Path: path})
	}

	policy, _ := tomlGet(lines, "", "approval_policy")
	switch policy {
	case "untrusted", "on-failure", "on-request":
		checks = append(checks, Check{Name: "Escape approvals", State: StateOK, Detail: "approval_policy = " + policy, Path: path})
	case "never":
		checks = append(checks, Check{Name: "Escape approvals", State: StateWarn, Detail: "approval_policy = never skips all approval prompts", Path: path})
	default:
		checks = append(checks, Check{Name: "Escape approvals", State: StateMissing, Detail: "approval_policy not pinned", Path: path})
	}

	net, netFound := tomlGet(lines, "sandbox_workspace_write", "network_access")
	switch {
	case net == "false":
		checks = append(checks, Check{Name: "Network access", State: StateOK, Detail: "network_access = false for sandboxed commands", Path: path})
	case netFound:
		checks = append(checks, Check{Name: "Network access", State: StateWarn, Detail: "network_access = " + net + " lets sandboxed commands reach the network", Path: path})
	default:
		checks = append(checks, Check{Name: "Network access", State: StateMissing, Detail: "network_access not pinned (defaults to off)", Path: path})
	}

	checks = append(checks, Check{Name: "Secret read blocking", State: StateUnsupported, Detail: "Codex cannot deny file reads; keep secrets outside reachable paths", Path: path})
	return checks, nil
}

// ─── minimal TOML line editing ────────────────────────────────────────────────
//
// These helpers handle only what codexSetup needs: scalar keys at the top
// level or directly inside a named [table]. Lines are never re-formatted, so
// comments and unrelated configuration survive untouched.

func readLines(path string) (lines []string, exists bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return strings.Split(string(data), "\n"), true, nil
}

func writeLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// tomlTableName returns the table a header line opens, or ok=false for
// non-header lines. Array-of-table headers ([[...]]) report ok=true with a
// name that never matches a plain table.
func tomlTableName(line string) (name string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") || !strings.Contains(trimmed, "]") {
		return "", false
	}
	return strings.TrimSpace(trimmed[1:strings.Index(trimmed, "]")]), true
}

// tomlKeyLine reports whether line assigns key, returning the raw value with
// surrounding whitespace, quotes, and any trailing comment removed.
func tomlKeyLine(line, key string) (value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, key) {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[len(key):])
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	value = strings.TrimSpace(rest[1:])
	if strings.HasPrefix(value, `"`) {
		if end := strings.Index(value[1:], `"`); end >= 0 {
			return value[1 : end+1], true
		}
	}
	if idx := strings.Index(value, "#"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	return value, true
}

// tomlGet returns the value of key inside table ("" = top level).
func tomlGet(lines []string, table, key string) (value string, found bool) {
	current := ""
	for _, line := range lines {
		if name, ok := tomlTableName(line); ok {
			current = name
			continue
		}
		if current != table {
			continue
		}
		if v, ok := tomlKeyLine(line, key); ok {
			return v, true
		}
	}
	return "", false
}

// tomlSet replaces key's line inside table ("" = top level), or inserts it:
// top-level keys go before the first table header, table keys at the end of
// their table. A missing table is appended.
func tomlSet(lines []string, table, key, rawValue string) []string {
	assignment := fmt.Sprintf("%s = %s", key, rawValue)
	current := ""
	insertAt := -1
	for i, line := range lines {
		if name, ok := tomlTableName(line); ok {
			if current == table {
				insertAt = i
				break
			}
			current = name
			continue
		}
		if current != table {
			continue
		}
		if _, ok := tomlKeyLine(line, key); ok {
			lines[i] = assignment
			return lines
		}
	}
	if insertAt < 0 && current == table {
		insertAt = len(lines)
	}
	if insertAt >= 0 {
		return insertLine(lines, insertAt, assignment)
	}
	// Table not present: append a fresh one.
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
		lines = append(lines, "")
	}
	return append(lines, "["+table+"]", assignment)
}

func insertLine(lines []string, at int, line string) []string {
	// Skip back over blank lines so the assignment lands with its section.
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, line)
	return append(out, lines[at:]...)
}
