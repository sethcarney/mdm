package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

// applyScopeInstallMode reconciles the requested mode with the one the scope
// records. The mode is scope-wide: a change re-materializes every existing
// install first and records the mode only once that succeeds. A partial failure
// reports how far it got, records nothing, and refuses to proceed. It never
// prompts, and returns the mode to install with and whether to proceed.
func applyScopeInstallMode(requested InstallMode, global bool, cwd string) (InstallMode, bool) {
	current := lock.GetInstallMode(global, cwd)

	// The default into a scope that never set the switch writes nothing, so
	// a project does not gain a lock file just to say "symlink".
	if current == "" && requested == InstallModeSymlink {
		return InstallModeSymlink, true
	}
	if current == string(requested) {
		return requested, true
	}

	// The mode is changing. Gate on that, not on a recorded string: a scope that
	// predates the switch has symlinked installs and no recorded mode.
	groups := scopeConversionGroups(global, cwd)
	if conversionGroupsCount(groups) > 0 {
		from := current
		if from == "" {
			from = string(InstallModeSymlink)
		}
		fmt.Printf("\n%sThis scope installs in %s mode. Switching it to %s mode re-materializes every skill and agent definition already installed here.%s\n",
			ansiDim, from, requested, ansiReset)

		// Convert before recording, so a partial failure leaves the recorded
		// mode exactly as it was.
		n, err := rematerializeGroups(requested, groups)
		if err != nil {
			if n > 0 {
				fmt.Printf("%sCould not re-materialize existing installs after converting %d of them: %v%s\n", ansiYellow, n, err, ansiReset)
			} else {
				fmt.Printf("%sCould not re-materialize existing installs: %v%s\n", ansiYellow, err, ansiReset)
			}
			return "", false
		}
		if n > 0 {
			fmt.Printf("%sRe-materialized %d existing install(s) as %s.%s\n", ansiDim, n, requested, ansiReset)
		}
	}

	if err := lock.SetInstallMode(string(requested), global, cwd); err != nil {
		fmt.Printf("%sCould not record the install mode: %v%s\n", ansiYellow, err, ansiReset)
		return "", false
	}
	return requested, true
}

// conversionPath is one install path, paired with the name mdm's canonical copy
// carries inside the group's canonical directory. The two differ when a harness
// reads its own extension (harness.AgentFileExt): Copilot installs
// `.github/agents/critic.agent.md` from `.agents/agents/critic.md`. Deriving the
// canonical name from the target basename invents a second canonical file.
type conversionPath struct {
	target        string
	canonicalName string
}

// selfNamedConversionPath pairs a target with a canonical name equal to its own
// basename. That is the rule for skills, which carry no per-harness suffix.
func selfNamedConversionPath(target string) conversionPath {
	return conversionPath{target: target, canonicalName: filepath.Base(target)}
}

// conversionGroup is one set of install paths plus the canonical root they were
// installed from. Skills and agent definitions use different roots
// (.agents/skills and .agents/agents), and the converters read the root.
type conversionGroup struct {
	canonicalDir string
	paths        []conversionPath
}

// scopeConversionGroups lists everything in the scope that a mode change has to
// re-materialize. The install mode is a property of the scope, so the sweep
// covers agent definitions as well as skills. Skills come first.
func scopeConversionGroups(global bool, cwd string) []conversionGroup {
	return []conversionGroup{
		{canonicalDir: getCanonicalSkillsDir(global, cwd), paths: scopeInstallPaths(global, cwd)},
		{canonicalDir: harness.CanonicalAgentsDir(global, cwd), paths: scopeAgentInstallPaths(global, cwd)},
	}
}

func conversionGroupsCount(groups []conversionGroup) int {
	n := 0
	for _, g := range groups {
		n += len(g.paths)
	}
	return n
}

// rematerializeGroups converts every group in order, returning the running
// total so a partial failure can still say how far it got.
func rematerializeGroups(to InstallMode, groups []conversionGroup) (int, error) {
	total := 0
	for _, g := range groups {
		n, err := rematerializeScope(to, g.canonicalDir, g.paths)
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// scopeAgentInstallPaths is scopeInstallPaths for agent definitions: the
// on-disk file of every definition the scope's lock records, per harness with
// an agent-definition directory recorded, deduplicated. Harnesses are walked
// in sorted order, since the caller reports "converted N of them". Each path
// carries the canonical file name, name+agentCanonicalExt, not the target's
// basename.
func scopeAgentInstallPaths(global bool, cwd string) []conversionPath {
	names, _ := agentLockEntries(global, cwd)

	harnesses := make([]string, 0, len(harness.AllHarnesses))
	for name := range harness.AllHarnesses {
		harnesses = append(harnesses, name)
	}
	sort.Strings(harnesses)

	seen := map[string]bool{}
	var paths []conversionPath
	for _, name := range names {
		for _, harnessName := range harnesses {
			target := agentHarnessPath(name, harnessName, global, cwd)
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			if _, err := os.Lstat(target); err != nil {
				continue
			}
			paths = append(paths, conversionPath{target: target, canonicalName: name + agentCanonicalExt})
		}
	}
	return paths
}

// scopeSkillNames lists the skills the given scope's lock records, sorted.
func scopeSkillNames(global bool, cwd string) []string {
	var skills []string
	if global {
		for name := range lock.ReadGlobalState().Skills {
			skills = append(skills, name)
		}
	} else {
		for name := range lock.ReadLocalLock(cwd).Skills {
			skills = append(skills, name)
		}
	}
	sort.Strings(skills)
	return skills
}

// scopeInstallPaths lists the on-disk install path of every skill the scope
// records, per supported harness, deduplicated. It sweeps every harness, not
// configuredHarnesses: that list holds only what the interactive picker saved,
// so it would skip harnesses installed with `--harness <harness> -y`.
func scopeInstallPaths(global bool, cwd string) []conversionPath {
	skills := scopeSkillNames(global, cwd)
	harnesses := allHarnessesForScope(global)

	seen := map[string]bool{}
	var paths []conversionPath
	for _, skillName := range skills {
		for _, harnessName := range harnesses {
			// A shared-dir harness's install path is the canonical directory,
			// real in both modes and never convertible.
			if harness.UsesSharedSkillsDir(harnessName) {
				continue
			}
			base := getHarnessBaseDir(harnessName, global, cwd)
			if base == "" {
				continue
			}
			target := filepath.Join(base, sanitizeName(skillName))
			if seen[target] {
				continue
			}
			seen[target] = true
			if _, err := os.Lstat(target); err != nil {
				continue
			}
			// A skill's install path basename is its canonical directory name,
			// per filepath.Join(base, sanitizeName(skillName)) above.
			paths = append(paths, selfNamedConversionPath(target))
		}
	}
	return paths
}

// copyDirFn, copyFileFn, and renameFn are the conversion steps tests swap for
// failing versions. Shared mutable state, so those tests must not run in
// parallel. removeFileFn in agent_artifacts.go covers the file-shaped steps.
var (
	copyDirFn  = copyDirectory
	copyFileFn = copyFile
	renameFn   = os.Rename
)

// resolvedDir returns dir with symlinks resolved (on Windows also short names
// and case), falling back to the absolute path when it does not exist.
func resolvedDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

// rematerializeScope converts the given install paths into the requested mode,
// so a mode change never leaves a scope half symlinked and half copied. It
// returns how many installs it converted, also on error. Only what mdm installed
// is touched, per isMdmOwnedCopyInstall; anything else is skipped and not
// counted. The canonical directory is never removed.
func rematerializeScope(to InstallMode, canonicalDir string, installPaths []conversionPath) (int, error) {
	canonical := resolvedDir(canonicalDir)
	converted := 0
	for _, p := range installPaths {
		var did bool
		var err error
		if to == InstallModeCopy {
			// linkToCopy needs no canonical name: it materializes whatever
			// the link already resolves to.
			did, err = linkToCopy(canonical, p.target)
		} else {
			did, err = copyToLink(canonical, p.target, p.canonicalName)
		}
		if err != nil {
			return converted, err
		}
		if did {
			converted++
		}
	}
	return converted, nil
}

// reserveSiblingName reserves an unused name next to an install path and
// returns it free for a rename to take. MkdirTemp is the only race-free way to
// claim a unique name, so the directory it creates is removed again at once.
// The sibling location keeps the later rename on one filesystem. The error is
// bare so each caller wraps it in its own wording.
func reserveSiblingName(dir, name string) (string, error) {
	reserved, err := os.MkdirTemp(dir, name+".mdm-tmp-")
	if err != nil {
		return "", err
	}
	if err := os.Remove(reserved); err != nil {
		return "", err
	}
	return reserved, nil
}

// materializeLinkReplacement builds, beside the install path, the real content
// about to replace the symlink at it: a directory copy for a skill, a file copy
// for an agent definition. It returns the temp path and a cleanup that removes
// it. Nothing here touches the install path, so a failure at this stage leaves
// the install exactly as it was.
func materializeLinkReplacement(installDir, name, resolved string, srcInfo os.FileInfo) (temp string, removeTemp func(), err error) {
	if srcInfo.IsDir() {
		// MkdirTemp creates it 0700; take the source's mode instead, so the
		// result matches a fresh copy install.
		temp, err = os.MkdirTemp(installDir, name+".mdm-tmp-")
		if err != nil {
			return "", nil, fmt.Errorf("preparing temp dir for %s: %w", name, err)
		}
		removeTemp = func() { _ = os.RemoveAll(temp) }
		if err := os.Chmod(temp, srcInfo.Mode().Perm()); err != nil {
			removeTemp()
			return "", nil, fmt.Errorf("preparing temp dir for %s: %w", name, err)
		}
		if err := copyDirFn(resolved, temp); err != nil {
			removeTemp()
			return "", nil, fmt.Errorf("copying %s: %w", name, err)
		}
		return temp, removeTemp, nil
	}

	// Reserve a unique sibling name the way copyToLink reserves its backup
	// name: copyFileFn creates the file, so it gives the result the source's
	// mode.
	temp, err = reserveSiblingName(installDir, name)
	if err != nil {
		return "", nil, fmt.Errorf("preparing temp file for %s: %w", name, err)
	}
	removeTemp = func() { _ = removeFileFn(temp) }
	if err := copyFileFn(resolved, temp); err != nil {
		removeTemp()
		return "", nil, fmt.Errorf("copying %s: %w", name, err)
	}
	return temp, removeTemp, nil
}

// linkToCopy replaces one mdm symlink at target with a real copy of what it
// points at, reporting whether it converted anything. The copy is built beside
// the target and renamed into place, so a failed copy leaves the link untouched
// and a failed rename puts the link back.
func linkToCopy(canonical, target string) (bool, error) {
	installDir := filepath.Dir(target)
	name := filepath.Base(target)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false, fmt.Errorf("resolving %s: %w", target, err)
	}
	// Not one of mdm's links: leave it alone.
	if resolved == canonical || !isInsideOrEqual(resolved, canonical) {
		return false, nil
	}
	// Decide the shape from what the link points at, not the target name:
	// a skill is a directory, an agent definition is a single file.
	srcInfo, err := os.Stat(resolved)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", resolved, err)
	}

	// The temp entry sits in installDir so the final rename stays on one
	// filesystem.
	temp, removeTemp, err := materializeLinkReplacement(installDir, name, resolved, srcInfo)
	if err != nil {
		return false, err
	}

	// Remove the link only, never what it points at.
	if err := os.Remove(target); err != nil {
		removeTemp()
		return false, err
	}
	if err := renameFn(temp, target); err != nil {
		// Put the link back through createSymlink, which writes it relative
		// like every other mdm link. If that fails, keep temp: it is the only
		// thing left holding the content.
		if !createSymlink(resolved, target) {
			return false, fmt.Errorf("finalizing %s: rename to %s failed (%v), and restoring the original symlink also failed; the copied content is stranded at %s and needs manual repair", name, target, err, temp)
		}
		removeTemp()
		return false, fmt.Errorf("finalizing %s: rename failed (%w); restored the original symlink at %s", name, err, target)
	}
	return true, nil
}

// isMdmOwnedCopyInstall reports whether the real entry at target looks like a
// copy install mdm wrote, and so may be replaced with a link. A directory
// qualifies if it holds a SKILL.md, a file if it parses as an agent definition.
// Both are weak ownership signals, not proof, and deliberately of equal
// strength, so neither shape is the easier one to sweep up by accident.
func isMdmOwnedCopyInstall(target string, info os.FileInfo) (bool, error) {
	if info.IsDir() {
		if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
			return false, nil
		}
		return true, nil
	}
	// ParseAgentMd returns (nil, nil) for a file with no name/description
	// frontmatter, so a read error still surfaces as an error here.
	agent, err := agentfile.ParseAgentMd(target)
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", target, err)
	}
	return agent != nil, nil
}

// ensureCanonicalCopy makes sure the content exists at canonicalPath before the
// install path stops holding it. Once the install path is a symlink, the
// canonical copy is the only place the content lives. An existing canonical copy
// is left as it is. A partial copy is cleaned up, so a retry does not take a
// half-written entry for a complete one.
func ensureCanonicalCopy(canonical, canonicalPath, target, name string, isDir bool) error {
	if _, err := os.Stat(canonicalPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", canonicalPath, err)
	}
	if isDir {
		if err := copyDirFn(target, canonicalPath); err != nil {
			_ = os.RemoveAll(canonicalPath)
			return fmt.Errorf("copying %s into %s: %w", name, canonical, err)
		}
		return nil
	}
	// Unlike copyDirFn, copyFileFn does not create its destination's parent:
	// the canonical directory needs making explicitly here.
	if err := os.MkdirAll(canonical, 0755); err != nil {
		return fmt.Errorf("preparing %s: %w", canonical, err)
	}
	if err := copyFileFn(target, canonicalPath); err != nil {
		_ = removeFileFn(canonicalPath)
		return fmt.Errorf("copying %s into %s: %w", name, canonical, err)
	}
	return nil
}

// copyToLink replaces one real install at target with an mdm symlink to
// <canonical>/<canonicalName>, reporting whether it converted anything. The
// canonical copy is created first when missing, then the original is set aside
// with a rename so a failed link can put it straight back. canonicalName comes
// from the caller, never from target's basename; see conversionPath.
func copyToLink(canonical, target, canonicalName string) (bool, error) {
	installDir := filepath.Dir(target)
	name := filepath.Base(target)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	owned, err := isMdmOwnedCopyInstall(target, info)
	if err != nil {
		return false, err
	}
	if !owned {
		return false, nil
	}
	// Never link something inside the canonical tree back to itself.
	if isInsideOrEqual(resolvedDir(target), canonical) {
		return false, nil
	}

	canonicalPath := filepath.Join(canonical, canonicalName)
	if err := ensureCanonicalCopy(canonical, canonicalPath, target, name, info.IsDir()); err != nil {
		return false, err
	}

	backup, err := reserveSiblingName(installDir, name)
	if err != nil {
		return false, fmt.Errorf("preparing backup for %s: %w", name, err)
	}
	if err := renameFn(target, backup); err != nil {
		return false, fmt.Errorf("setting aside %s: %w", name, err)
	}
	if !createSymlink(canonicalPath, target) {
		if rerr := renameFn(backup, target); rerr != nil {
			return false, fmt.Errorf("linking %s: could not create the symlink, and restoring the copy also failed (%v); the content is stranded at %s and needs manual repair", name, rerr, backup)
		}
		return false, fmt.Errorf("linking %s: could not create the symlink at %s; restored the copy", name, target)
	}
	if info.IsDir() {
		_ = os.RemoveAll(backup)
	} else {
		_ = removeFileFn(backup)
	}
	return true, nil
}
