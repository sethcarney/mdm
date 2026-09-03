package commands

import (
	"os"
	"path/filepath"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
)

// installAgentFile installs one agent-definition file into one harness: the
// canonical copy is written first at .agents/agents/<name>.md, then linked
// (or copied, on symlink failure) into the harness's own agents directory
// under <name><ext>, where ext is harness.AgentFileExt(harnessName).
//
// A harness with no agent concept (AgentsInstallDirFor returns "") is
// reported as a skip rather than a failure: installing an agent definition
// to several harnesses where some have no agent support at all is a normal,
// expected outcome, not an error.
func installAgentFile(a *agentfile.AgentFile, harnessName string, global bool, cwd string, mode InstallMode) InstallResult {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	h := harness.AllHarnesses[harnessName]
	if h == nil {
		return InstallResult{Success: false, Mode: mode, Error: "unknown harness: " + harnessName}
	}

	harnessDir := harness.AgentsInstallDirFor(harnessName, global, cwd)
	if harnessDir == "" {
		reason := h.DisplayName + " has no agent concept"
		if global {
			reason = h.DisplayName + " does not support global agent installation"
		}
		return InstallResult{Success: false, Mode: mode, Error: reason}
	}

	ext := harness.AgentFileExt(harnessName)
	fileName := a.Name + ext

	canonicalBase := harness.CanonicalAgentsDir(global, cwd)
	canonicalPath := filepath.Join(canonicalBase, a.Name+".md")
	harnessPath := filepath.Join(harnessDir, fileName)

	if !isPathSafe(canonicalBase, canonicalPath) || !isPathSafe(harnessDir, harnessPath) {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: "potential path traversal detected"}
	}

	if err := os.MkdirAll(canonicalBase, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	if err := copyFile(a.Path, canonicalPath); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}

	if mode == InstallModeCopy {
		if err := os.MkdirAll(harnessDir, 0755); err != nil {
			return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
		}
		if err := copyFile(canonicalPath, harnessPath); err != nil {
			return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
		}
		return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeCopy}
	}

	// Symlink-then-copy-on-failure, following performSymlinkInstall in
	// installer.go: link the single canonical file into the harness
	// directory, falling back to a plain copy when the platform or
	// filesystem cannot create the link.
	if createSymlink(canonicalPath, harnessPath) {
		return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeSymlink}
	}
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	if err := copyFile(canonicalPath, harnessPath); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeSymlink, SymlinkFailed: true}
}
