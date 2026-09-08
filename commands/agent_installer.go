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
//
// The copy replaces dst rather than being written into it. In symlink mode the
// canonical file is not a spare copy - it is what every harness symlink
// resolves to - so truncating it in place is a window in which every harness
// reads a zero-length definition, and a crash inside that window leaves it that
// way with nothing to restore it from.
func copyAgentFileUnlessSame(src, dst string) error {
	if sameFileOnDisk(src, dst) {
		return nil
	}
	return replaceFileFrom(src, dst)
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
// nil when the canonical bytes are what the harness reads, whether it takes
// them as a link, a copy, or a materialized byte copy. Encode refuses
// a definition carrying a TOML local date or time, and this runs before
// anything is written so that refusal costs nothing on disk. Encode's error is
// passed through unwrapped: the summary prints it under a line that already
// names the definition and every harness it failed for, and a reason carrying
// neither is one string however many harnesses share it.
func encodeForHarness(a *agentfile.AgentFile, harnessName string) ([]byte, error) {
	// Checked before the materialization test because a TOML source reaches
	// Codex as a symlink, which encodes nothing and would skip the guard.
	if harness.AgentFormat(harnessName) == agentfile.FormatTOML && strings.TrimSpace(a.Instructions) == "" {
		return nil, fmt.Errorf("it has an empty body, and a TOML agent definition requires developer_instructions")
	}
	// Same format: nothing to convert, whatever the materialization rule
	// says. Copilot's committed directory gets the canonical bytes verbatim,
	// not a re-encoding that re-sorts keys and drops comments.
	if harness.AgentFormat(harnessName) == agentCanonicalFormat(a) {
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
	return fmt.Errorf("%s already has a canonical file at %s, and %s is %s - remove the existing definition with `mdm agents remove %s` before adding the same name in the other format",
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

// replaceFilePath has build write the new content of path under a reserved
// sibling name, then renames that over path. The rename replaces the name
// itself, so whatever path held - a symlink included - is discarded rather than
// written through, and path never holds a partial file: until the rename it
// holds its previous content, and after it holds all of the new one. Writing
// into path can do neither, because both os.WriteFile and copyFile open the
// destination O_CREATE|O_WRONLY|O_TRUNC, which follows a symlink onto its target
// and empties a real file before the first new byte arrives. The sibling
// location keeps the rename on one filesystem.
func replaceFilePath(path string, build func(temp string) error) error {
	temp, err := reserveSiblingName(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return err
	}
	if err := build(temp); err != nil {
		_ = removeFileFn(temp)
		return err
	}
	if err := renameFn(temp, path); err != nil {
		_ = removeFileFn(temp)
		return err
	}
	return nil
}

// replaceFileWith replaces path with data.
func replaceFileWith(path string, data []byte) error {
	return replaceFilePath(path, func(temp string) error { return os.WriteFile(temp, data, 0644) })
}

// replaceFileFrom replaces dst with a copy of src. copyFileFn gives the result
// the source's mode, exactly as a copy straight into dst would.
func replaceFileFrom(src, dst string) error {
	return replaceFilePath(dst, func(temp string) error { return copyFileFn(src, temp) })
}

// writeMaterializedAgent writes already-encoded bytes into the harness's own
// directory as a real file. The path is replaced rather than written to: a
// symlink can already be there - GitHub documents committing .github/agents, so
// a teammate on a platform with symlinks can commit one - and writing through it
// would put this harness's re-encoding in the canonical file every other harness
// links at, while leaving the install a link where the harness needs a real file.
func writeMaterializedAgent(data []byte, harnessDir, harnessPath, canonicalPath string, mode InstallMode) InstallResult {
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	if err := replaceFileWith(harnessPath, data); err != nil {
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

// copyMaterializedAgent gives a harness that must hold a real file a byte copy
// of the canonical file, replacing whatever the path held, exactly as
// writeMaterializedAgent does with converted bytes. replaceFileFrom, not
// copyAgentFileUnlessSame: a symlink to the canonical file counts as the same
// file, and here it is precisely the thing that has to go.
func copyMaterializedAgent(harnessDir, harnessPath, canonicalPath string, mode InstallMode) InstallResult {
	if err := os.MkdirAll(harnessDir, 0755); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
	if err := replaceFileFrom(canonicalPath, harnessPath); err != nil {
		return InstallResult{Success: false, Path: harnessPath, Mode: mode, Error: err.Error()}
	}
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
	if materializes(agentCanonicalFormat(a), harnessName) {
		return copyMaterializedAgent(harnessDir, harnessPath, canonicalPath, mode)
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
