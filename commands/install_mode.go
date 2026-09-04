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

// conversionPath is one install path to convert, paired with the name mdm's
// canonical copy of that asset has inside the group's canonical directory.
//
// The two names are not always the same. A harness may read an agent
// definition under an extension of its own (harness.AgentFileExt): GitHub
// Copilot installs `.github/agents/critic.agent.md` from the canonical
// `.agents/agents/critic.md`. Recovering the canonical name downstream from
// the target's basename therefore invented a SECOND canonical file,
// `.agents/agents/critic.agent.md`, on a copy → symlink switch, and pointed
// the harness at that duplicate instead of at the real canonical file; a
// later `mdm agents remove critic` then deleted the real pair and left the
// duplicate orphaned. Only the code that builds the paths knows both the
// definition name and the harness, so the mapping travels with the path
// from there rather than being guessed at the far end.
type conversionPath struct {
	target        string
	canonicalName string
}

// selfNamedConversionPath pairs a target with a canonical name equal to its
// own basename. That is the rule for skills, whose install path is the
// canonical directory entry name verbatim, with no per-harness suffix.
func selfNamedConversionPath(target string) conversionPath {
	return conversionPath{target: target, canonicalName: filepath.Base(target)}
}

// conversionGroup is one set of install paths plus the canonical root they
// were installed from. Skills and agent definitions live under different
// canonical roots (.agents/skills and .agents/agents), and the converters
// use that root to tell mdm's own links from a user's, so the two cannot
// share one canonicalDir argument — the roots have to travel with the paths.
type conversionGroup struct {
	canonicalDir string
	paths        []conversionPath
}

// scopeConversionGroups lists everything in the scope that a mode change has
// to re-materialize. The install mode is a property of the SCOPE, promised
// by the spec and by docs/agent-artifacts.md as one mode per scope, so the
// sweep covers agent definitions as well as skills: enumerating only skills
// left `mdm skills add --copy` recording copy for the scope while every
// agent definition in it stayed a symlink.
//
// Skills come first, which is the order the conversion already ran in.
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
// total so a partial failure can still say how far it got — the same
// contract rematerializeScope has for a single group, which it delegates to
// unchanged.
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
// existing on-disk file of every definition the scope's lock records, for
// each harness with an agent concept, deduplicated. Harnesses are walked in
// sorted order so the conversion order is deterministic; AllHarnesses is a
// map, and the surrounding code reports "converted N of them" on failure,
// which is only meaningful against a stable order.
//
// Each path carries the canonical file name for its definition, which is
// name+agentCanonicalExt and NOT the target's basename: a harness that
// overrides the extension it reads (harness.AgentFileExt) installs
// "<name>.agent.md" from the canonical "<name>.md".
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

// scopeInstallPaths lists the existing on-disk install path of every skill
// the scope records, for each harness the scope supports, deduplicated. It
// sweeps every harness rather than configuredHarnesses: that list only records
// what the interactive picker last saved, so consulting it would skip harnesses
// installed with `--harness <harness> -y` and leave the scope half converted. The
// sweep is safe because rematerializeScope converts only what mdm installed.
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
			// A skill's install path basename IS its canonical directory
			// name — filepath.Join(base, sanitizeName(skillName)) above —
			// so the canonical name is unchanged from what the converter
			// used to derive for itself.
			paths = append(paths, selfNamedConversionPath(target))
		}
	}
	return paths
}

// copyDirFn, copyFileFn, and renameFn are the steps of a conversion that
// tests swap for failing versions, since neither failure can be forced
// reliably at the OS level. Production always uses the defaults. They are
// shared mutable state, so tests that swap them must not run in parallel.
// removeFileFn (defined in agent_artifacts.go, alongside the same caveat)
// is reused here for the file-shaped cleanup and swap steps below.
var (
	copyDirFn  = copyDirectory
	copyFileFn = copyFile
	renameFn   = os.Rename
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
// directories holding a SKILL.md or real files that parse as an agent
// definition (agent definitions are single files, not directories).
// Anything else is the user's own and is skipped, not counted. Neither
// check is strong — a SKILL.md or a name-and-description frontmatter block
// is something a user's own file could have too — but they are the same
// strength, so a mode switch is no more likely to sweep up a user's
// directory than a user's file. The canonical directory is never removed:
// harnesses that read the shared directory install into it in copy mode
// too, and doctor and remove resolve it for every locked skill.
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

// linkToCopy replaces one mdm symlink at target with a real copy of what it
// points at (a directory for a skill, a single file for an agent
// definition), reporting whether it converted anything. The copy is built
// beside the target — in a temp directory for a directory target, in a temp
// file for a file target — and renamed into place, so a failed copy leaves
// the link untouched, and a failed rename puts the link back.
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
	var temp string
	var removeTemp func()
	if srcInfo.IsDir() {
		// MkdirTemp creates it 0700; take the source's mode instead, so the
		// result matches a fresh copy install.
		temp, err = os.MkdirTemp(installDir, name+".mdm-tmp-")
		if err != nil {
			return false, fmt.Errorf("preparing temp dir for %s: %w", name, err)
		}
		removeTemp = func() { _ = os.RemoveAll(temp) }
		if err := os.Chmod(temp, srcInfo.Mode().Perm()); err != nil {
			removeTemp()
			return false, fmt.Errorf("preparing temp dir for %s: %w", name, err)
		}
		if err := copyDirFn(resolved, temp); err != nil {
			removeTemp()
			return false, fmt.Errorf("copying %s: %w", name, err)
		}
	} else {
		// Reserve a unique sibling name the same way copyToLink reserves its
		// backup name, then free it: copyFileFn creates the file itself, so
		// it (not a separate chmod) is what gives the result the source's mode.
		reserved, err := os.MkdirTemp(installDir, name+".mdm-tmp-")
		if err != nil {
			return false, fmt.Errorf("preparing temp file for %s: %w", name, err)
		}
		if err := os.Remove(reserved); err != nil {
			return false, fmt.Errorf("preparing temp file for %s: %w", name, err)
		}
		temp = reserved
		removeTemp = func() { _ = removeFileFn(temp) }
		if err := copyFileFn(resolved, temp); err != nil {
			removeTemp()
			return false, fmt.Errorf("copying %s: %w", name, err)
		}
	}

	// Remove the link only, never what it points at.
	if err := os.Remove(target); err != nil {
		removeTemp()
		return false, err
	}
	if err := renameFn(temp, target); err != nil {
		// Put the link back through createSymlink, which writes it RELATIVE
		// like every other mdm link. If even that fails, keep temp: it is
		// the only thing left holding the content.
		if !createSymlink(resolved, target) {
			return false, fmt.Errorf("finalizing %s: rename to %s failed (%v), and restoring the original symlink also failed; the copied content is stranded at %s and needs manual repair", name, target, err, temp)
		}
		removeTemp()
		return false, fmt.Errorf("finalizing %s: rename failed (%w); restored the original symlink at %s", name, err, target)
	}
	return true, nil
}

// copyToLink replaces one real install at target — a skill directory or an
// agent-definition file — with an mdm symlink to <canonical>/<canonicalName>,
// reporting whether it converted anything. The canonical copy is created
// first when missing, since it is the only place the content can live once
// the install path is a link; the original is then set aside with a rename
// so a failed link can put it straight back.
//
// canonicalName is supplied by the caller and is deliberately NOT derived
// from target's basename: a harness that reads agent definitions under its
// own extension installs "<name>.agent.md" from the canonical "<name>.md",
// and deriving the name here would create a duplicate canonical file under
// the harness's extension and link the install at that duplicate. For a
// skill the two are the same string, so nothing about the skill conversion
// changes. name below stays the basename: it names only sibling temp files
// and the install this error is about.
func copyToLink(canonical, target, canonicalName string) (bool, error) {
	installDir := filepath.Dir(target)
	name := filepath.Base(target)
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if info.IsDir() {
		// Every mdm copy install of a skill holds a SKILL.md; a directory
		// without one is someone else's. This is a weak signal — it cannot
		// tell mdm's own directory from a user's directory that happens to
		// hold a SKILL.md of its own — but it is the same strength as the
		// file check below, so neither shape is the easier one to sweep up
		// by accident.
		if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
			return false, nil
		}
	} else {
		// Every mdm copy install of an agent definition parses as one: name
		// and description frontmatter present. A file that does not parse —
		// including one a user hand-wrote at the same path the lock happens
		// to track — is someone else's, mirroring the SKILL.md check above.
		// ParseAgentMd returns (nil, nil) for a file with no such
		// frontmatter, so a read error still surfaces as an error here.
		agent, err := agentfile.ParseAgentMd(target)
		if err != nil {
			return false, fmt.Errorf("checking %s: %w", target, err)
		}
		if agent == nil {
			return false, nil
		}
	}
	// Never link something inside the canonical tree back to itself.
	if isInsideOrEqual(resolvedDir(target), canonical) {
		return false, nil
	}

	canonicalPath := filepath.Join(canonical, canonicalName)
	if _, err := os.Stat(canonicalPath); err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Errorf("checking %s: %w", canonicalPath, err)
		}
		if info.IsDir() {
			if err := copyDirFn(target, canonicalPath); err != nil {
				_ = os.RemoveAll(canonicalPath)
				return false, fmt.Errorf("copying %s into %s: %w", name, canonical, err)
			}
		} else {
			// Unlike copyDirFn, copyFileFn does not create its destination's
			// parent: the canonical directory needs making explicitly here.
			if err := os.MkdirAll(canonical, 0755); err != nil {
				return false, fmt.Errorf("preparing %s: %w", canonical, err)
			}
			if err := copyFileFn(target, canonicalPath); err != nil {
				_ = removeFileFn(canonicalPath)
				return false, fmt.Errorf("copying %s into %s: %w", name, canonical, err)
			}
		}
	}

	// MkdirTemp only reserves a unique sibling name; the rename needs it free.
	backup, err := os.MkdirTemp(installDir, name+".mdm-tmp-")
	if err != nil {
		return false, fmt.Errorf("preparing backup for %s: %w", name, err)
	}
	if err := os.Remove(backup); err != nil {
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
