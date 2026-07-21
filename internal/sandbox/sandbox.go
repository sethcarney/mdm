// Package sandbox writes hardened, vendor-native sandbox configuration for
// the major agentic coding tools so they cannot read secrets and stay inside
// the project working directory.
//
// Each supported agent exposes the same three operations:
//
//   - Plan:   report the config changes setup would make, without writing
//   - Apply:  make the changes, merging non-destructively into existing config
//   - Status: check the current config against the recommended baseline
//
// mdm only ever adds or tightens settings. It never removes user-authored
// entries, and it preserves unknown keys in every file it touches.
package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// Change describes one config-file mutation that setup makes (or would make).
type Change struct {
	Path        string `json:"path"`
	Description string `json:"description"`
	Create      bool   `json:"create"` // file does not exist yet
}

// CheckState classifies one baseline check in `mdm sandbox status`.
type CheckState string

const (
	StateOK          CheckState = "ok"          // baseline satisfied
	StateMissing     CheckState = "missing"     // baseline item absent
	StateWarn        CheckState = "warn"        // configured in a way that weakens the baseline
	StateUnsupported CheckState = "unsupported" // the tool has no mechanism for this protection
)

// Check is one status line item for an agent.
type Check struct {
	Name   string     `json:"name"`
	State  CheckState `json:"state"`
	Detail string     `json:"detail,omitempty"`
	Path   string     `json:"path,omitempty"`
}

// Agent is one sandbox-capable tool mdm knows how to configure.
type Agent struct {
	Name        string
	DisplayName string
	// Notes are honest caveats about what this tool's sandbox can and
	// cannot enforce; printed after setup and status.
	Notes []string

	Plan   func(projectDir string) ([]Change, error)
	Apply  func(projectDir string) ([]Change, error)
	Status func(projectDir string) ([]Check, error)
}

// Agents lists the supported tools in display order.
func Agents() []Agent {
	return []Agent{claudeAgent(), codexAgent(), copilotAgent()}
}

// Supported reports whether name is a sandbox-capable agent.
func Supported(name string) bool {
	for _, a := range Agents() {
		if a.Name == name {
			return true
		}
	}
	return false
}

// secretDenyReadRules are Claude Code permission deny rules (gitignore-style
// path patterns) covering common credential locations. Bare filenames match
// at any depth under the project, and ~/ anchors at the home directory, so
// the same rules work in project and user settings.
var secretDenyReadRules = []string{
	"Read(.env)",
	"Read(.env.*)",
	"Read(**/*.pem)",
	"Read(**/*.key)",
	"Read(**/id_rsa)",
	"Read(**/id_ed25519)",
	"Read(**/secrets/**)",
	"Read(~/.ssh/**)",
	"Read(~/.aws/**)",
	"Read(~/.gnupg/**)",
	"Read(~/.kube/**)",
	"Read(~/.netrc)",
	"Read(~/.npmrc)",
	"Read(~/.config/gh/**)",
	"Read(~/.docker/config.json)",
}

// secretHookPattern is the extended regex (POSIX ERE, matched
// case-insensitively) the Copilot pre-tool-use hook greps for inside tool
// arguments. Filename patterns require a path-ish leading boundary so code
// idioms like `process.env.FOO` never match. Kept in one place so the shell
// script and its tests stay in sync; must not contain single quotes, since
// the script embeds it in a single-quoted string.
const secretHookPattern = `(^|[/\\ "=(,:])\.env(\.[A-Za-z0-9_.-]+)?([^A-Za-z0-9_.-]|$)` +
	`|\.(pem|key)([^A-Za-z0-9]|$)` +
	`|(^|[/\\ "=(,:~])(id_rsa|id_ed25519|\.ssh/|\.aws/|\.gnupg/|\.kube/|\.netrc|\.npmrc|secrets/|credentials\.json)`

// homeDir returns the user home directory, or "" when unavailable.
func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// envOrDefaultDir resolves a tool config home: the env var when set,
// otherwise <home>/<fallback>.
func envOrDefaultDir(envVar, fallback string) string {
	if dir := strings.TrimSpace(os.Getenv(envVar)); dir != "" {
		return dir
	}
	return filepath.Join(homeDir(), fallback)
}
