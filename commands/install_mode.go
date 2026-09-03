package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
)

// applyScopeInstallMode reconciles the requested mode with the one the scope
// records. The mode is scope-wide, so a change re-materializes every existing
// install first and records the mode only once that succeeds; a partial
// failure reports how far it got, records nothing, and refuses to proceed so
// a re-run can finish the job. It never prompts: an explicit --copy or
// --symlink is the consent, and the conversion is lossless in both
// directions. It returns the mode to install with and whether to proceed.
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

	// The mode is changing. Gate on that, not on a recorded string: a scope
	// that predates the switch has symlinked installs and no recorded mode,
	// and is the usual --copy case. Nothing installed means nothing to convert.
	installed := scopeInstallPaths(global, cwd)
	if len(installed) > 0 {
		from := current
		if from == "" {
			from = string(InstallModeSymlink)
		}
		fmt.Printf("\n%sThis scope installs in %s mode. Switching it to %s mode re-materializes every skill already installed here.%s\n",
			ansiDim, from, requested, ansiReset)

		// Convert before recording, so a partial failure leaves the recorded
		// mode exactly as it was.
		n, err := rematerializeScope(requested, getCanonicalSkillsDir(global, cwd), installed)
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

// scopeInstallPaths lists the existing on-disk install path of every skill
// the scope records, for each agent the scope supports, deduplicated. It
// sweeps every agent rather than configuredAgents: that list only records
// what the interactive picker last saved, so consulting it would skip agents
// installed with `-a <agent> -y` and leave the scope half converted. The
// sweep is safe because rematerializeScope converts only what mdm installed.
func scopeInstallPaths(global bool, cwd string) []string {
	skills := scopeSkillNames(global, cwd)
	agents := allAgentsForScope(global)

	seen := map[string]bool{}
	var paths []string
	for _, skillName := range skills {
		for _, agentName := range agents {
			// A shared-dir agent's install path is the canonical directory,
			// real in both modes and never convertible.
			if agent.UsesSharedSkillsDir(agentName) {
				continue
			}
			base := getAgentBaseDir(agentName, global, cwd)
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
			paths = append(paths, target)
		}
	}
	return paths
}

// copyDirFn and renameFn are the steps of a conversion that tests swap for
// failing versions, since neither failure can be forced reliably at the OS
// level. Production always uses the defaults. They are shared mutable state,
// so tests that swap them must not run in parallel.
var (
	copyDirFn = copyDirectory
	renameFn  = os.Rename
)

// resolvedDir returns dir with symlinks resolved (and, on Windows, short
// names and case normalized), falling back to the absolute path when it does
// not exist, in which case nothing can be inside it anyway.
func resolvedDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

// rematerializeScope converts the given install paths into the requested
// mode, so a mode change never leaves a scope half symlinked and half copied.
// It returns how many installs it converted, also on error, so the caller
// can say how far it got.
//
// Only what mdm installed is touched: symlink to copy converts links
// pointing at <canonicalDir>/<name>, and copy to symlink converts real
// directories holding a SKILL.md. Anything else is the user's own and is
// skipped, not counted. The canonical directory is never removed: agents
// that read the shared directory install into it in copy mode too, and
// doctor and remove resolve it for every locked skill.
func rematerializeScope(to InstallMode, canonicalDir string, installPaths []string) (int, error) {
	canonical := resolvedDir(canonicalDir)
	converted := 0
	for _, target := range installPaths {
		var did bool
		var err error
		if to == InstallModeCopy {
			did, err = linkToCopy(canonical, target)
		} else {
			did, err = copyToLink(canonical, target)
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

// linkToCopy replaces one mdm symlink at target with a real copy of what it
// points at, reporting whether it converted anything. The copy is built in a
// temp directory beside the target and renamed into place, so a failed copy
// leaves the link untouched, and a failed rename puts the link back.
func linkToCopy(canonical, target string) (bool, error) {
	installDir := filepath.Dir(target)
	skillName := filepath.Base(target)
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

	// The temp dir sits in installDir so the final rename stays on one
	// filesystem. MkdirTemp creates it 0700; take the source's mode instead,
	// so the result matches a fresh copy install.
	temp, err := os.MkdirTemp(installDir, skillName+".mdm-tmp-")
	if err != nil {
		return false, fmt.Errorf("preparing temp dir for %s: %w", skillName, err)
	}
	if srcInfo, statErr := os.Stat(resolved); statErr == nil {
		if err := os.Chmod(temp, srcInfo.Mode().Perm()); err != nil {
			_ = os.RemoveAll(temp)
			return false, fmt.Errorf("preparing temp dir for %s: %w", skillName, err)
		}
	}
	if err := copyDirFn(resolved, temp); err != nil {
		_ = os.RemoveAll(temp)
		return false, fmt.Errorf("copying %s: %w", skillName, err)
	}
	// Remove the link only, never what it points at.
	if err := os.Remove(target); err != nil {
		_ = os.RemoveAll(temp)
		return false, err
	}
	if err := renameFn(temp, target); err != nil {
		// Put the link back through createSymlink, which writes it RELATIVE
		// like every other mdm link. If even that fails, keep temp: it is
		// the only thing left holding the content.
		if !createSymlink(resolved, target) {
			return false, fmt.Errorf("finalizing %s: rename to %s failed (%v), and restoring the original symlink also failed; the copied content is stranded at %s and needs manual repair", skillName, target, err, temp)
		}
		_ = os.RemoveAll(temp)
		return false, fmt.Errorf("finalizing %s: rename failed (%w); restored the original symlink at %s", skillName, err, target)
	}
	return true, nil
}

// copyToLink replaces one real skill directory at target with an mdm symlink
// to <canonical>/<name>, reporting whether it converted anything. The
// canonical copy is created first when missing, since it is the only place
// the content can live once the agent directory is a link; the copy is then
// set aside with a rename so a failed link can put it straight back.
func copyToLink(canonical, target string) (bool, error) {
	installDir := filepath.Dir(target)
	skillName := filepath.Base(target)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, nil
	}
	// Every mdm copy install holds a SKILL.md; a directory without one is
	// someone else's.
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		return false, nil
	}
	// Never link a directory inside the canonical tree to itself.
	if isInsideOrEqual(resolvedDir(target), canonical) {
		return false, nil
	}

	canonicalSkill := filepath.Join(canonical, skillName)
	if _, err := os.Stat(canonicalSkill); err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("checking %s: %w", canonicalSkill, err)
		}
		if err := copyDirFn(target, canonicalSkill); err != nil {
			_ = os.RemoveAll(canonicalSkill)
			return false, fmt.Errorf("copying %s into %s: %w", skillName, canonical, err)
		}
	}

	// MkdirTemp only reserves a unique sibling name; the rename needs it free.
	backup, err := os.MkdirTemp(installDir, skillName+".mdm-tmp-")
	if err != nil {
		return false, fmt.Errorf("preparing backup dir for %s: %w", skillName, err)
	}
	if err := os.Remove(backup); err != nil {
		return false, fmt.Errorf("preparing backup dir for %s: %w", skillName, err)
	}
	if err := renameFn(target, backup); err != nil {
		return false, fmt.Errorf("setting aside %s: %w", skillName, err)
	}
	if !createSymlink(canonicalSkill, target) {
		if rerr := renameFn(backup, target); rerr != nil {
			return false, fmt.Errorf("linking %s: could not create the symlink, and restoring the copy also failed (%v); the content is stranded at %s and needs manual repair", skillName, rerr, backup)
		}
		return false, fmt.Errorf("linking %s: could not create the symlink at %s; restored the copy", skillName, target)
	}
	_ = os.RemoveAll(backup)
	return true, nil
}
