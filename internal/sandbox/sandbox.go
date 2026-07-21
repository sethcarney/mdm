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

	// HasProjectConfig reports whether an mdm-written sandbox artifact already
	// exists for this agent in the project. `mdm doctor` uses it to surface
	// drift for agents the user has already hardened, without nagging every
	// project where a tool merely happens to be installed. Nil (e.g. Codex,
	// which is global-only) means "no project artifact".
	HasProjectConfig func(projectDir string) bool
}

// Agents lists the supported tools in display order.
func Agents() []Agent {
	return []Agent{claudeAgent(), codexAgent(), copilotAgent(), cursorAgent(), geminiAgent()}
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

// projectSecretGlobs name secret-bearing files that live *inside* a project,
// as gitignore-style globs relative to the project root. They are reused in
// two forms:
//
//   - Read() permission deny rules, as Read(**/<glob>) (Claude Code, Cursor)
//   - OS-sandbox filesystem denyRead entries, as ./**/<glob> (Claude Code)
//
// so a project's own secrets are blocked both at the tool layer and, where
// available, at the OS layer.
var projectSecretGlobs = []string{
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"id_rsa",
	"id_ed25519",
	"secrets/**",
	"*credentials*",
	"appsettings.*.json",
	".npmrc",
	"nuget.config",
	"NuGet.Config",
}

// homeSecretReadRules are Read() permission deny rules for credential stores
// outside the project, anchored at the home directory with ~/.
var homeSecretReadRules = []string{
	"Read(~/.ssh/**)",
	"Read(~/.aws/**)",
	"Read(~/.gnupg/**)",
	"Read(~/.kube/**)",
	"Read(~/.netrc)",
	"Read(~/.npmrc)",
	"Read(~/.config/gh/**)",
	"Read(~/.docker/config.json)",
	"Read(~/.microsoft/usersecrets/**)",
	"Read(~/.nuget/NuGet/NuGet.Config)",
}

// secretReadDenyRules returns the canonical Read() deny rules: project
// secrets as Read(**/<glob>) plus the home credential stores.
func secretReadDenyRules() []string {
	rules := make([]string, 0, len(projectSecretGlobs)+len(homeSecretReadRules))
	for _, g := range projectSecretGlobs {
		rules = append(rules, "Read(**/"+g+")")
	}
	return append(rules, homeSecretReadRules...)
}

// sandboxDenyReadPaths returns OS-sandbox filesystem denyRead entries: the
// whole home directory (re-allowing the project via allowRead ".") plus each
// project secret glob so in-project secrets stay blocked even though the
// project is otherwise readable.
func sandboxDenyReadPaths() []string {
	paths := make([]string, 0, len(projectSecretGlobs)+1)
	paths = append(paths, "~/")
	for _, g := range projectSecretGlobs {
		paths = append(paths, "./**/"+g)
	}
	return paths
}

// denyRuleKey normalizes a Read() rule so gitignore-equivalent spellings
// collapse to one key: Read(**/.env) and Read(.env) both match any .env at
// any depth, so they share a key and are never both written. Non-Read rules
// key on themselves.
func denyRuleKey(rule string) string {
	if !strings.HasPrefix(rule, "Read(") || !strings.HasSuffix(rule, ")") {
		return rule
	}
	inner := rule[len("Read(") : len(rule)-1]
	inner = strings.TrimPrefix(inner, "**/")
	return "Read(" + inner + ")"
}

// mergeDenyRules appends desired rules to existing, skipping any whose
// normalized key is already present, and reports how many were added.
func mergeDenyRules(existing, desired []string) (merged []string, added int) {
	keys := make(map[string]bool, len(existing))
	for _, r := range existing {
		keys[denyRuleKey(r)] = true
	}
	merged = append([]string(nil), existing...)
	for _, r := range desired {
		k := denyRuleKey(r)
		if keys[k] {
			continue
		}
		keys[k] = true
		merged = append(merged, r)
		added++
	}
	return merged, added
}

// mergeStringSet appends desired entries to existing with exact-string
// deduplication, reporting how many were added.
func mergeStringSet(existing, desired []string) (merged []string, added int) {
	seen := make(map[string]bool, len(existing))
	for _, s := range existing {
		seen[s] = true
	}
	merged = append([]string(nil), existing...)
	for _, s := range desired {
		if seen[s] {
			continue
		}
		seen[s] = true
		merged = append(merged, s)
		added++
	}
	return merged, added
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
