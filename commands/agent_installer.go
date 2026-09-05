package commands

import (
	"os"
	"path/filepath"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
)

// agentCanonicalExt is the extension of mdm's own canonical copy of an agent
// definition. It is always ".md" whatever extension the harness wants
// (harness.AgentFileExt).
const agentCanonicalExt = ".md"

// agentDiskName turns a definition's frontmatter name into the one name mdm
// uses everywhere: the canonical file, every harness's own file, and the lock
// key. Frontmatter is third-party text and often not a legal file name ("Code
// Reviewer"), so it is sanitized the way installSkillForHarness sanitizes a
// skill name. One function for all three keeps the file and the lock key in step.
func agentDiskName(rawName string) string {
	return sanitizeName(rawName)
}

// agentCanonicalPath returns mdm's canonical file for one definition. name must
// already have been through agentDiskName, which also produces the lock key.
func agentCanonicalPath(name string, global bool, cwd string) string {
	return filepath.Join(harness.CanonicalAgentsDir(global, cwd), name+agentCanonicalExt)
}

// agentHarnessPath returns the file harnessName reads this definition from, or
// "" when that harness has no agents directory for this scope.
func agentHarnessPath(name, harnessName string, global bool, cwd string) string {
	dir := harness.AgentsInstallDirFor(harnessName, global, cwd)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name+harness.AgentFileExt(harnessName))
}

// sameFileOnDisk reports whether two paths name the same file once symlinks are
// followed. os.Stat, not Lstat: a harness's symlink into the canonical directory
// counts as the same file as its target. A failed stat means not the same file.
func sameFileOnDisk(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// copyAgentFileUnlessSame copies src over dst unless the two are already the
// same file on disk. `mdm agents add .` discovers both .agents/agents and
// .claude/agents, whose entries link into it, so src can be dst. copyFile opens
// the destination O_TRUNC first, which would empty the file it then reads.
func copyAgentFileUnlessSame(src, dst string) error {
	if sameFileOnDisk(src, dst) {
		return nil
	}
	return copyFile(src, dst)
}

// copyAgentIntoHarness copies the canonical definition into the harness's own
// agents directory, creating that directory first.
func copyAgentIntoHarness(canonicalPath, harnessDir, harnessPath string) error {
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return err
	}
	return copyAgentFileUnlessSame(canonicalPath, harnessPath)
}

// installAgentFile installs one agent-definition file into one harness. The
// canonical copy is written first at .agents/agents/<name>.md, then linked (or
// copied, on symlink failure) into the harness's own agents directory under
// <name><ext>, where ext is harness.AgentFileExt(harnessName). A harness with no
// agent-definition directory recorded is a skip, not a failure.
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
		reason := "no agent-definition directory is recorded for " + h.DisplayName
		if global {
			reason = "no global agent-definition directory is recorded for " + h.DisplayName
		}
		return InstallResult{Success: false, Skipped: true, Mode: mode, Error: reason}
	}

	name := agentDiskName(a.Name)
	canonicalBase := harness.CanonicalAgentsDir(global, cwd)
	canonicalPath := agentCanonicalPath(name, global, cwd)
	harnessPath := agentHarnessPath(name, harnessName, global, cwd)

	if !isPathSafe(canonicalBase, canonicalPath) || !isPathSafe(harnessDir, harnessPath) {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: "potential path traversal detected"}
	}

	if err := os.MkdirAll(canonicalBase, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	// The source file can be the canonical file itself, so reinstalling a
	// definition mdm already owns is a no-op.
	if err := copyAgentFileUnlessSame(a.Path, canonicalPath); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}

	if mode == InstallModeCopy {
		if err := copyAgentIntoHarness(canonicalPath, harnessDir, harnessPath); err != nil {
			return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
		}
		return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeCopy}
	}

	// Symlink first, plain copy when the platform or filesystem cannot
	// create the link, following performSymlinkInstall in installer.go.
	if createSymlink(canonicalPath, harnessPath) {
		return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeSymlink}
	}
	if err := copyAgentIntoHarness(canonicalPath, harnessDir, harnessPath); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeSymlink, SymlinkFailed: true}
}
