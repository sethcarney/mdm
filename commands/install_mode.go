package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
)

// applyScopeInstallMode reconciles the mode a command asked for against
// the mode the scope already records. Install mode is scope-wide and
// all-or-nothing, so a change is a real event: it re-materializes the
// scope's existing installs first, so the scope does not end up half
// symlinked and half copied.
//
// It never prompts. The mode only changes on an explicit --copy or
// --symlink, which is the consent; the conversion is lossless in both
// directions (the canonical .agents/skills content is kept, or created
// first, and only mdm's own installs are replaced), so a notice of what it
// did is enough. Keeping the install flow free of an extra confirmation is
// deliberate.
//
// The guarantee holds only when the conversion succeeds. A conversion that
// fails partway leaves the disk genuinely mixed; that path reports how many
// installs it had already converted, records no mode, and refuses to
// proceed, so re-running is what finishes the job.
//
// It returns the mode to install with and whether to proceed.
func applyScopeInstallMode(requested InstallMode, global bool, cwd string) (InstallMode, bool) {
	current := lock.GetInstallMode(global, cwd)

	// A plain symlink install into a scope that has never set the switch
	// writes nothing. Recording the default would create a lock file for
	// a project that does not otherwise need one.
	if current == "" && requested == InstallModeSymlink {
		return InstallModeSymlink, true
	}
	if current == string(requested) {
		return requested, true
	}

	// Past this point the effective mode is genuinely changing. The gate is
	// that, not whether a mode string happens to be recorded: every project
	// that predates this switch has symlinked installs and no recorded
	// mode, and that is the most common way --copy gets turned on. Gating
	// on `current != ""` skipped the conversion for exactly those projects,
	// leaving the lock saying copy over symlinks.
	//
	// A scope with nothing installed has nothing to convert and nothing to
	// report, so it skips straight to recording the mode.
	installed := scopeInstallPaths(global, cwd)
	if len(installed) > 0 {
		from := current
		if from == "" {
			from = string(InstallModeSymlink)
		}
		fmt.Printf("\n%sThis scope installs in %s mode. Switching it to %s mode re-materializes every skill already installed here.%s\n",
			ansiDim, from, requested, ansiReset)

		// Re-materialize before recording the mode. rematerializeScope takes
		// its target mode as a parameter and reads nothing mode-related from
		// the lock, so this order is safe: if conversion fails partway, the
		// scope's recorded mode is left exactly as it was, rather than
		// claiming "copy" while some installs are still symlinks.
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

// scopeSkillNames lists the skills the given scope records, sorted.
//
// The scope decides which lock is read. Reading the project lock in global
// scope would miss every globally installed skill, and would convert
// nothing at all when run from a directory that has no project lock, while
// still recording the new mode and reporting success.
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
// records, for each agent the scope supports that has an install path of its
// own, deduplicated. Only paths that exist are returned, so an empty result
// means the scope has nothing to convert.
//
// Path construction goes through getAgentBaseDir rather than rebuilding it,
// so the global fallback behaves exactly as it does at install time, and the
// lock key is sanitized the same way every other lock-name-to-directory
// conversion sanitizes it.
//
// The sweep deliberately ignores configuredAgents. That list records only the
// agents a user picked in the interactive picker, so `mdm skills add <src> -a
// roo -y` installs into .roo/skills and leaves it empty; a later switch that
// consulted the list would convert only the agents named in it and leave the
// rest symlinked, which is precisely the half-symlinked, half-copied scope
// this module exists to prevent. Reading it also made the result depend on
// WHEN this runs, since the agent picker saves the new selection before the
// mode is applied.
//
// Sweeping every agent is safe because rematerializeScope converts only
// symlinks that resolve inside the scope's canonical skills directory: an
// agent directory that holds a real directory, or a link the user made
// themselves, is skipped and not counted. So the wider sweep cannot invent
// work.
func scopeInstallPaths(global bool, cwd string) []string {
	skills := scopeSkillNames(global, cwd)

	// Hoisted out of the loop: the list is the same for every skill in the
	// scope. allAgentsForScope applies the same scope filter the installer
	// does, so this sweeps exactly the directories an install could have
	// written to.
	agents := allAgentsForScope(global)

	seen := map[string]bool{}
	var paths []string
	for _, skillName := range skills {
		for _, agentName := range agents {
			// An agent that reads the shared .agents/skills directory has no
			// install path of its own: getAgentBaseDir returns the canonical
			// directory, which is a real directory in both modes and so can
			// never be converted. Including it would make a scope that has
			// only such agents look like it had installs to re-materialize,
			// so the user would be told about a conversion that then
			// converts nothing. inferInstallMode skips them for the same
			// reason.
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

// copyDirFn performs the actual directory copy in rematerializeScope. It is
// a package-level variable, defaulting to copyDirectory, purely so tests can
// force a copy failure deterministically without relying on OS-level fault
// injection (permission denial is not reliably forceable on Windows).
// Production code always goes through the default.
//
// This exists solely as a test seam: it is shared mutable state, so no test
// in this package may swap it while running in parallel (t.Parallel) with
// another test that reads or swaps it.
var copyDirFn = copyDirectory

// renameFn performs the final swap in rematerializeScope. Like copyDirFn it
// exists only so tests can force the failure branch deterministically: a
// rename within one directory does not fail on demand otherwise. Production
// code always goes through the default, and the same no-parallel rule
// applies.
var renameFn = os.Rename

// resolvedDir returns dir with symlinks resolved, so it can be compared
// against the fully resolved target of a symlink. On Windows EvalSymlinks
// also normalizes short (8.3) names and letter case, which a raw string
// comparison would otherwise get wrong. It falls back to the absolute path
// when the directory cannot be resolved, which means it does not exist and
// nothing can be inside it anyway.
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
// mode, so a mode change never leaves a scope half symlinked and half
// copied. It returns the number of installs it converted, including on the
// error return so the caller can say how far it got. Callers get the paths
// from scopeInstallPaths, which is where this module resolves scope and agent
// layout, and canonicalDir from getCanonicalSkillsDir for the same scope.
//
// Only what mdm installed is converted, in either direction:
//
//   - Symlink to copy converts only links pointing at <canonicalDir>/<name>
//     (see createSymlink and how installs build the link target). A link
//     resolving anywhere else belongs to the user: replacing a hand-made
//     ~/.roo/skills/pdf -> ~/dev/pdf-skill with a frozen copy would silently
//     break the edit propagation they set it up for.
//   - Copy to symlink converts only real directories holding a SKILL.md,
//     which is what every mdm copy install writes; a directory without one
//     is someone else's and is left alone.
//
// Anything skipped is not counted.
//
// The canonical <canonicalDir>/<name> directory is never removed, and a
// copy-to-symlink conversion creates it first when it is missing (a copy
// install writes it only for agents that read the shared directory). It is
// not an orphan in copy mode: agents that read the shared skills directory
// install INTO it in copy mode too, it is the path `mdm doctor` reports for
// every locked skill, and it is what `mdm skills remove` cleans up.
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
// points at. It reports whether it converted anything; a target that is not
// an mdm link is skipped without error.
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
	// Not one of mdm's links: leave the user's own link alone. The
	// canonical directory itself does not count either, since no mdm
	// link points at it rather than at a skill inside it.
	if resolved == canonical || !isInsideOrEqual(resolved, canonical) {
		return false, nil
	}

	// Copy into a temporary sibling directory first, and only swap it into
	// place once the copy has fully succeeded. This keeps the failure
	// window empty: if the copy fails, the original symlink is untouched
	// and nothing is left half-converted. The temp dir lives in installDir
	// (target's own parent) rather than, say, os.TempDir(), so the final
	// rename stays within one filesystem; a cross-filesystem rename would
	// fail outright.
	temp, err := os.MkdirTemp(installDir, skillName+".mdm-tmp-")
	if err != nil {
		return false, fmt.Errorf("preparing temp dir for %s: %w", skillName, err)
	}
	// MkdirTemp creates the directory 0700. Match the canonical directory's
	// mode instead, so a converted install has the same permissions a fresh
	// copy install would have, rather than ones only its owner can read.
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
		// The link is already gone at this point. Try to put it back so a
		// rename failure leaves the user no worse off than before the
		// attempt, rather than an install path with neither a symlink nor
		// a directory.
		//
		// createSymlink is how every install writes a link, and the link
		// it writes is RELATIVE. Calling os.Symlink here instead would
		// restore an absolute link, shaped unlike every other mdm link,
		// and anything comparing link targets would then read this one as
		// foreign.
		if !createSymlink(resolved, target) {
			// Restoring the symlink also failed: do not delete temp, it is
			// the only thing at hand pointing at the copied content. Name
			// both paths so this is manually recoverable rather than
			// silently stranded.
			return false, fmt.Errorf("finalizing %s: rename to %s failed (%v), and restoring the original symlink also failed; the copied content is stranded at %s and needs manual repair", skillName, target, err, temp)
		}
		_ = os.RemoveAll(temp)
		return false, fmt.Errorf("finalizing %s: rename failed (%w); restored the original symlink at %s", skillName, err, target)
	}
	return true, nil
}

// copyToLink replaces one real skill directory at target with an mdm
// symlink to <canonical>/<name>, creating that canonical copy first when it
// does not exist yet. It reports whether it converted anything; a target
// that is not a real directory holding a SKILL.md is skipped without error.
func copyToLink(canonical, target string) (bool, error) {
	installDir := filepath.Dir(target)
	skillName := filepath.Base(target)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		return false, nil
	}
	// scopeInstallPaths never lists a path inside the canonical directory,
	// but a link that resolved into it would be linking a directory to
	// itself; refuse rather than trust the caller.
	if isInsideOrEqual(resolvedDir(target), canonical) {
		return false, nil
	}

	// The canonical content has to exist before the copy is given up: a
	// symlink install keeps the content there, and this is the only place
	// it could come from once the agent directory is gone. An existing
	// canonical directory is kept as is; every install writes it and the
	// agent copy from the same source in one go, so the two agree.
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

	// Set the copy aside with a rename rather than deleting it, so a
	// failed symlink can put it straight back. MkdirTemp is only used to
	// reserve a unique sibling name; the rename needs that name free.
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
