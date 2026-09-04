package commands

import (
	"os"
	"path/filepath"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
)

// agentCanonicalExt is the extension of mdm's own canonical copy of an agent
// definition. It is always ".md" regardless of what extension the harness
// wants (harness.AgentFileExt) — the canonical file is mdm's, not a
// harness's.
const agentCanonicalExt = ".md"

// agentDiskName turns a definition's frontmatter name into the one name mdm
// uses for it everywhere: the canonical file, every harness's own file, and
// the lock key. Frontmatter is third-party text and is routinely not a legal
// or stable file name ("Code Reviewer"), so it is sanitized exactly the way
// installSkillForHarness sanitizes a skill's name.
//
// The single function matters more than the sanitizing does. When the file
// name and the lock key were derived separately, an install wrote
// ".agents/agents/Code Reviewer.md" while the lock recorded "code-reviewer",
// and `mdm agents remove code-reviewer` then dropped the lock entry, found
// no file to delete, reported success, and left the definition live in the
// harness with nothing left to find it by.
func agentDiskName(rawName string) string {
	return sanitizeName(rawName)
}

// agentCanonicalPath returns mdm's canonical file for one definition. name
// must already have been through agentDiskName — it is the lock key too.
func agentCanonicalPath(name string, global bool, cwd string) string {
	return filepath.Join(harness.CanonicalAgentsDir(global, cwd), name+agentCanonicalExt)
}

// agentHarnessPath returns the file harnessName reads this definition from,
// or "" when that harness has no agents directory for this scope. name must
// already have been through agentDiskName.
func agentHarnessPath(name, harnessName string, global bool, cwd string) string {
	dir := harness.AgentsInstallDirFor(harnessName, global, cwd)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name+harness.AgentFileExt(harnessName))
}

// sameFileOnDisk reports whether two paths name the same file once symlinks
// are followed. os.Stat (not Lstat) is deliberate: a harness's symlink into
// the canonical directory has to count as the same file as its target.
// Either path failing to stat means they cannot be the same file — a missing
// destination is the normal case for a first install.
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
	// Reinstalling a definition mdm already owns is a no-op, not an error:
	// `mdm agents add .` discovers .agents/agents (a conventional agents
	// directory) and .claude/agents (whose entries are symlinks into it), so
	// the source file can BE the destination. copyFile opens the destination
	// O_TRUNC before reading the source, which in that case empties the very
	// file it is about to read and leaves a 0-byte canonical copy behind a
	// green checkmark. Comparing the files first is what makes that
	// impossible, on this route and on any other that reaches the same pair.
	if !sameFileOnDisk(a.Path, canonicalPath) {
		if err := copyFile(a.Path, canonicalPath); err != nil {
			return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
		}
	}

	if mode == InstallModeCopy {
		if err := os.MkdirAll(harnessDir, 0755); err != nil {
			return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
		}
		// Same guard as above: a harness whose agents directory IS the
		// canonical directory would otherwise copy the file onto itself.
		if !sameFileOnDisk(canonicalPath, harnessPath) {
			if err := copyFile(canonicalPath, harnessPath); err != nil {
				return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
			}
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
	if !sameFileOnDisk(canonicalPath, harnessPath) {
		if err := copyFile(canonicalPath, harnessPath); err != nil {
			return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
		}
	}
	return InstallResult{Success: true, Path: harnessPath, CanonicalPath: canonicalPath, Mode: InstallModeSymlink, SymlinkFailed: true}
}
