package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

// agentCanonicalExt is the extension of mdm's own canonical copy of a
// definition. The canonical file mirrors its source, so the extension follows
// the format, not whatever extension the harness wants (harness.AgentFileExt).
func agentCanonicalExt(format agentfile.Format) string {
	if format == agentfile.FormatTOML {
		return ".toml"
	}
	return ".md"
}

// lockedAgentFormat reads the canonical format out of a lock entry. An absent
// format means markdown: every canonical file written before the lock recorded
// one is a .md.
func lockedAgentFormat(entry lock.AgentLockEntry) agentfile.Format {
	if entry.Format == string(agentfile.FormatTOML) {
		return agentfile.FormatTOML
	}
	return agentfile.FormatMarkdown
}

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
func agentCanonicalPath(name string, format agentfile.Format, global bool, cwd string) string {
	return filepath.Join(harness.CanonicalAgentsDir(global, cwd), name+agentCanonicalExt(format))
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

// agentCanonicalFormat is the shape of the bytes mdm keeps in its canonical
// directory, which is whatever shape the source was in. A definition assembled
// in code can leave Format unset, so the source extension is the fallback.
func agentCanonicalFormat(a *agentfile.AgentFile) agentfile.Format {
	if a.Format != "" {
		return a.Format
	}
	return agentfile.FormatForExt(filepath.Ext(a.Path))
}

// materializes reports whether harnessName needs a real file rather than a
// symlink. A symlink hands the harness the canonical bytes verbatim, so it
// only works when those bytes are already what the harness reads and the
// directory is generated output nobody commits.
func materializes(canonicalFormat agentfile.Format, harnessName string) bool {
	if h, ok := harness.AllHarnesses[harnessName]; ok && h.AgentAlwaysMaterialize {
		return true
	}
	return harness.AgentFormat(harnessName) != canonicalFormat
}

// encodeForHarness renders the definition in harnessName's format, or returns
// nil when a link or a plain copy of the canonical file will do. Encode refuses
// a definition carrying a TOML local date or time, and this runs before
// anything is written so that refusal costs nothing on disk. Encode's error is
// passed through unwrapped: the summary prints it under a line that already
// names the definition and every harness it failed for, and a reason carrying
// neither is one string however many harnesses share it.
func encodeForHarness(a *agentfile.AgentFile, harnessName string) ([]byte, error) {
	if !materializes(agentCanonicalFormat(a), harnessName) {
		return nil, nil
	}
	return agentfile.Encode(a, harness.AgentFormat(harnessName))
}

// otherAgentFormat returns the format a definition is not in.
func otherAgentFormat(f agentfile.Format) agentfile.Format {
	if f == agentfile.FormatTOML {
		return agentfile.FormatMarkdown
	}
	return agentfile.FormatTOML
}

// checkAgentFormatCollision refuses a definition whose name already has a
// canonical file in the other format. Accepting it would leave one name with
// two canonical files, and would change the format every harness already
// holding that definition is handed, as a side effect of an ordinary add.
func checkAgentFormatCollision(name string, format agentfile.Format, sourcePath string, global bool, cwd string) error {
	existing := agentCanonicalPath(name, otherAgentFormat(format), global, cwd)
	if _, err := os.Stat(existing); err != nil {
		return nil
	}
	return fmt.Errorf("%s already has a canonical file at %s, and %s is %s — remove the existing definition with `mdm agents remove %s` before adding the same name in the other format",
		name, existing, sourcePath, format, name)
}

// prepareAgentWrite runs everything that can refuse an install before the first
// byte is written: the collision check against a canonical file in the other
// format, and the harness encoding. It returns nil bytes when the harness takes
// the canonical file as it stands.
func prepareAgentWrite(a *agentfile.AgentFile, harnessName, name string, global bool, cwd string) ([]byte, error) {
	if err := checkAgentFormatCollision(name, agentCanonicalFormat(a), a.Path, global, cwd); err != nil {
		return nil, err
	}
	return encodeForHarness(a, harnessName)
}

// agentFailures collects the harnesses one definition failed for in a run,
// grouped by reason. Every refusal on this path is computed to be read: Encode
// names the offending key, and the format-collision check names both files. A
// summary that prints only harness names throws all of that away and leaves the
// user with nothing to act on.
type agentFailures struct {
	reasons   []string            // distinct reasons, in first-seen order
	harnesses map[string][]string // reason -> the harnesses it applies to
	all       []string            // every failed harness, in the order given
}

// note records one install. Results that succeeded or were skipped are ignored,
// so every install loop can call it unconditionally.
func (f *agentFailures) note(harnessName string, r InstallResult) {
	if r.Success || r.Skipped {
		return
	}
	if f.harnesses == nil {
		f.harnesses = map[string][]string{}
	}
	if _, seen := f.harnesses[r.Error]; !seen {
		f.reasons = append(f.reasons, r.Error)
	}
	f.harnesses[r.Error] = append(f.harnesses[r.Error], harnessName)
	f.all = append(f.all, harnessName)
}

// any reports whether at least one install failed.
func (f *agentFailures) any() bool {
	return f != nil && len(f.reasons) > 0
}

// explain prints each reason once, indented under the failure line. A reason
// shared by several harnesses is not repeated per harness; the harnesses are
// named only when the definition failed for more than one reason, since a
// single reason applies to every harness the line above already lists.
func (f *agentFailures) explain() {
	for _, reason := range f.reasons {
		line := reason
		if len(f.reasons) > 1 {
			line = strings.Join(f.harnesses[reason], ", ") + ": " + reason
		}
		fmt.Printf("%s      %s%s\n", ansiDim, line, ansiReset)
	}
}

// reportAgentFailure prints one definition's failure line and the reasons under
// it. It prints nothing when nothing failed.
func reportAgentFailure(displayName string, f *agentFailures) {
	if !f.any() {
		return
	}
	ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", displayName, strings.Join(f.all, ", ")))
	f.explain()
}

// writeMaterializedAgent writes already-encoded bytes into the harness's own
// directory as a real file.
func writeMaterializedAgent(data []byte, harnessDir, harnessPath, canonicalPath string, mode InstallMode) InstallResult {
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	if err := os.WriteFile(harnessPath, data, 0644); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	// The scope keeps its recorded mode. Materialized, not SymlinkFailed: no
	// link was attempted, so the run must not tell the user a symlink failed
	// and offer --copy, which would change nothing.
	return InstallResult{
		Success:       true,
		Path:          harnessPath,
		CanonicalPath: canonicalPath,
		Mode:          mode,
		Materialized:  true,
	}
}

// installAgentFile installs one definition into one harness: a canonical copy
// at .agents/agents/<name><sourceExt>, and under it a link, a copy, or a
// converted real file at <name><harnessExt>. Every refusal is decided in memory
// before the first write, so a definition mdm cannot take leaves no canonical
// file stranded where discovery would find it. No agents directory is a skip.
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
	canonicalPath := agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd)
	harnessPath := agentHarnessPath(name, harnessName, global, cwd)

	if !isPathSafe(canonicalBase, canonicalPath) || !isPathSafe(harnessDir, harnessPath) {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: "potential path traversal detected"}
	}

	// Before any write: a refusal here must not leave a canonical file behind
	// that no lock entry names and that list, remove and doctor cannot see.
	encoded, err := prepareAgentWrite(a, harnessName, name, global, cwd)
	if err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}

	if err := os.MkdirAll(canonicalBase, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	// The source file can be the canonical file itself, so reinstalling a
	// definition mdm already owns is a no-op.
	if err := copyAgentFileUnlessSame(a.Path, canonicalPath); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}

	if encoded != nil {
		return writeMaterializedAgent(encoded, harnessDir, harnessPath, canonicalPath, mode)
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
