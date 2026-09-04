package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/fork"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

type RemoveOptions struct {
	Global    bool
	Harnesses []string
	Skills    []string
	Yes       bool
	All       bool
}

func buildRemoveCmd() *cobra.Command {
	var opts RemoveOptions

	cmd := &cobra.Command{
		Use:     "remove [skills...]",
		Short:   "Remove installed skills",
		Aliases: []string{"rm", "r"},
		Long: fmt.Sprintf(`Remove installed skills from harnesses.

If no skill names are provided an interactive selection menu is shown.

The --harness and --skill (-s) flags accept multiple values — space-
separated after the flag or repeated:

  mdm skills remove --harness claude-code cursor
  mdm skills remove --harness claude-code --harness cursor

%sExamples:%s
  mdm skills remove
  mdm skills remove my-skill
  mdm skills remove skill1 skill2 -y
  mdm skills remove --global my-skill
  mdm skills remove --all`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if opts.All {
				opts.Skills = []string{"*"}
				opts.Harnesses = []string{"*"}
				opts.Yes = true
			}
			runRemove(args, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Remove from global scope")
	f.StringArrayVar(&opts.Harnesses, "harness", nil, "Remove from specific harnesses (repeatable)")
	f.StringArrayVarP(&opts.Skills, "skill", "s", nil, "Skill names to remove (repeatable)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	f.BoolVar(&opts.All, "all", false, "Shorthand for --skill '*' --harness '*' -y")

	_ = cmd.RegisterFlagCompletionFunc("harness", harnessFlagCompletion)

	return cmd
}

func filterInstalledByName(installed []*InstalledSkill, names []string) ([]*InstalledSkill, bool) {
	var result []*InstalledSkill
	for _, s := range installed {
		for _, f := range names {
			if skillNameMatches(s.Name, f) {
				result = append(result, s)
				break
			}
		}
	}
	if len(result) == 0 {
		fmt.Printf("%sNo matching skills found.%s\n", ansiDim, ansiReset)
		return nil, false
	}
	return result, true
}

func selectSkillsToRemove(installed []*InstalledSkill, skillFilter []string, opts RemoveOptions) ([]*InstalledSkill, bool) {
	if len(skillFilter) == 1 && skillFilter[0] == "*" {
		return installed, true
	}
	if len(skillFilter) > 0 {
		return filterInstalledByName(installed, skillFilter)
	}
	if opts.Yes || len(installed) == 1 {
		return installed, true
	}
	options := make([]ui.UIOption, len(installed))
	for i, s := range installed {
		hint := s.Description
		if len(s.Harnesses) > 0 {
			hint = strings.Join(s.Harnesses, ", ")
		}
		options[i] = ui.UIOption{Label: s.Name, Value: sanitizeName(s.Name), Hint: hint}
	}
	indices, ok := ui.UiSearchMultiselect("Which skills would you like to remove?", options, nil, nil, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []*InstalledSkill
	for _, i := range indices {
		selected = append(selected, installed[i])
	}
	return selected, true
}

// resolveLocalSourceAbs returns the absolute path of the skill's source
// directory when it was installed from a local path, or "" otherwise.
func resolveLocalSourceAbs(sName string, global bool, cwd string) string {
	if global {
		if e, ok := lock.ReadGlobalState().Skills[sName]; ok && e.SourceType == string(source.SourceTypeLocal) {
			abs, _ := filepath.Abs(e.Source)
			return abs
		}
		return ""
	}
	le, ok := lock.ReadLocalLock(cwd).Skills[sName]
	if !ok || le.SourceType != string(source.SourceTypeLocal) {
		return ""
	}
	src := le.Source
	if !filepath.IsAbs(src) {
		src = filepath.Clean(filepath.Join(cwd, src))
	}
	return src
}

// removeAllFn is the directory-deletion seam for removals: tests swap it for a
// failing version. Shared mutable state, so those tests must not run in
// parallel.
var removeAllFn = os.RemoveAll

// removeHarnessSkillDir deletes the skill directory for one candidate name
// under a harness base. A path it declines to touch is not an error. It reports
// a failed deletion, so the caller can keep the lock describing the disk.
func removeHarnessSkillDir(harnessBase, name, localSourceAbs string) error {
	harnessSkillDir := filepath.Join(harnessBase, name)
	harnessSkillAbs, _ := filepath.Abs(harnessSkillDir)
	if localSourceAbs != "" && isInsideOrEqual(harnessSkillAbs, localSourceAbs) {
		return nil
	}
	if !isPathSafe(harnessBase, harnessSkillDir) {
		return nil
	}
	info, err := os.Lstat(harnessSkillDir)
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(harnessSkillDir)
	}
	if isCherryPickedSource(harnessSkillDir) {
		return nil
	}
	return removeAllFn(harnessSkillDir)
}

// isCherryPickedSource reports whether a directory is a cherry-picked fork: the
// project's own source, carrying edits that exist nowhere else. A harness skills
// directory can be the forks directory itself (OpenClaw reads ./skills), which
// makes a fork look like an installed skill to every scan.
func isCherryPickedSource(dir string) bool {
	if info, err := os.Lstat(dir); err != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	return fork.IsFork(dir)
}

// harnessesRetainingSkill reports which harnesses outside a scoped removal
// still hold sk. A disk probe cannot answer this: most harnesses read the
// shared .agents/skills directory, so probing them reads the very directory
// whose fate is being decided. A harness with its own skills directory proves
// itself; a shared-directory one counts only when configured or installed here.
func harnessesRetainingSkill(sk *InstalledSkill, removing []string, global bool, cwd string) []string {
	inUse := map[string]bool{}
	for _, name := range lock.GetConfiguredHarnesses(global, cwd) {
		inUse[name] = true
	}
	for _, name := range harness.DetectInstalledHarnesses() {
		inUse[name] = true
	}

	sName := sanitizeName(sk.Name)
	dirName := filepath.Base(sk.Path)

	var retained []string
	for name := range harness.AllHarnesses {
		if contains(removing, name) {
			continue
		}
		if harness.UsesSharedSkillsDir(name) && !inUse[name] {
			continue
		}
		harnessBase := getHarnessBaseDir(name, global, cwd)
		if harnessBase == "" {
			continue
		}
		if harnessHasSkill(harnessBase, dirName, sName, sk.Name) {
			retained = append(retained, name)
		}
	}
	sort.Strings(retained)
	return retained
}

// removeSkillInstalls deletes the skill's install under each harness in
// harnessesToRemove and returns a description of every deletion that failed.
// retained lists harnesses outside the filter that still hold the skill. While
// it is non-empty, a harness reading the canonical directory is skipped, since
// "its" copy is the copy those harnesses still use.
func removeSkillInstalls(sk *InstalledSkill, harnessesToRemove, retained []string, sName, localSourceAbs string, global bool, cwd string) []string {
	var failed []string
	for _, harnessName := range harnessesToRemove {
		if len(retained) > 0 && harness.UsesSharedSkillsDir(harnessName) {
			vlog(verboseFlag, "skip harness %q: shares the canonical dir, still needed by %v", harnessName, retained)
			continue
		}
		harnessBase := getHarnessBaseDir(harnessName, global, cwd)
		if harnessBase == "" {
			vlog(verboseFlag, "skip harness %q: no base dir resolved", harnessName)
			continue
		}
		for _, name := range []string{sName, filepath.Base(sk.Path)} {
			if rmErr := removeHarnessSkillDir(harnessBase, name, localSourceAbs); rmErr != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", harnessName, rmErr))
			}
		}
	}
	return failed
}

// removeCanonicalSkillCopy deletes mdm's own copy of the skill. It refuses when
// the canonical directory sits inside a local source, is a cherry-picked fork,
// or resolves outside the canonical skills tree. A refusal is not an error.
func removeCanonicalSkillCopy(sk *InstalledSkill, localSourceAbs string, global bool, cwd string) error {
	canonicalDir := getCanonicalPath(sk.Name, global)
	canonicalAbs, _ := filepath.Abs(canonicalDir)
	skipCanonical := localSourceAbs != "" && isInsideOrEqual(canonicalAbs, localSourceAbs)
	if !skipCanonical && canonicalDir != "" && !isCherryPickedSource(canonicalDir) &&
		isPathSafe(getCanonicalSkillsDir(global, cwd), canonicalDir) {
		if rmErr := removeAllFn(canonicalDir); rmErr != nil {
			return fmt.Errorf("could not remove %s: %w", canonicalDir, rmErr)
		}
	}
	return nil
}

// removeSkillLockEntry drops the skill's record from whichever lock the scope
// keeps. It runs last, after the disk is already clear, so the lock never
// claims a skill is gone while its files are still there.
func removeSkillLockEntry(sName string, global bool, cwd string) error {
	var lockErr error
	if global {
		lockErr = lock.RemoveSkillFromGlobalState(sName)
	} else {
		lockErr = lock.RemoveSkillFromLocalLock(sName, cwd)
	}
	if lockErr != nil {
		return fmt.Errorf("could not update the lock file: %w", lockErr)
	}
	return nil
}

// removeSkillFromDisk deletes the installs for the harnesses in harnessFilter
// and, when nothing outside that filter still holds the skill, the canonical
// directory and the lock entry too. It returns the harnesses that still hold
// the skill; a non-empty list means the canonical copy and the lock entry were
// kept. On error the lock entry stays, so the failure stays visible.
func removeSkillFromDisk(sk *InstalledSkill, harnessFilter []string, global bool, cwd string) (retained []string, err error) {
	sName := sanitizeName(sk.Name)
	localSourceAbs := resolveLocalSourceAbs(sName, global, cwd)

	harnessesToRemove := harnessFilter
	if len(harnessFilter) == 0 {
		// No filter removes the skill outright, so every harness is in scope.
		// Sweeping all of them, not only the ones the skill was detected in,
		// matters: detection needs the harness's tool installed, and a directory
		// an earlier `--harness X` install wrote would keep a link to the
		// canonical directory this call deletes.
		for name := range harness.AllHarnesses {
			harnessesToRemove = append(harnessesToRemove, name)
		}
	} else {
		// Computed before any deletion: for a shared-directory harness in the
		// filter, the deletion below would destroy the evidence this reads.
		retained = harnessesRetainingSkill(sk, harnessFilter, global, cwd)
	}
	vlog(verboseFlag, "removing %q from harnesses=%v (localSource=%q, retained=%v)", sk.Name, harnessesToRemove, localSourceAbs, retained)

	failed := removeSkillInstalls(sk, harnessesToRemove, retained, sName, localSourceAbs, global, cwd)
	if len(failed) > 0 {
		return retained, fmt.Errorf("could not remove from %s", strings.Join(failed, ", "))
	}

	if len(retained) > 0 {
		return retained, nil
	}

	if rmErr := removeCanonicalSkillCopy(sk, localSourceAbs, global, cwd); rmErr != nil {
		return nil, rmErr
	}

	if lockErr := removeSkillLockEntry(sName, global, cwd); lockErr != nil {
		return nil, lockErr
	}
	return nil, nil
}

func resolveRemoveScope(opts RemoveOptions) (global bool, ok bool) {
	if opts.Global {
		return true, true
	}
	if opts.Yes {
		return false, true
	}
	idx, ok := ui.UiSelect("Which scope?", []ui.UIOption{
		{Label: "Project", Hint: "remove from this project"},
		{Label: "Global", Hint: "remove from your user account"},
	})
	if !ok {
		return false, false
	}
	return idx == 1, true
}

func handleNoInstalled(global bool, cwd string) {
	var cleaned int
	var lockErr error
	if global {
		cleaned, lockErr = cleanOrphanedLockEntries(cwd)
	} else {
		cleaned, lockErr = cleanOrphanedLocalLockEntries(cwd)
	}
	if lockErr != nil {
		ui.LogWarn(fmt.Sprintf("the lock file could not be updated: %v", lockErr))
		fmt.Println()
	}
	if cleaned > 0 {
		fmt.Printf("%sCleaned up %d orphaned lock entr%s with no files on disk.%s\n",
			ansiDim, cleaned, map[bool]string{true: "ies", false: "y"}[cleaned != 1], ansiReset)
		return
	}
	fmt.Printf("%sNo skills installed.%s\n", ansiDim, ansiReset)
}

// executeRemovals removes each selected skill and reports what happened to it.
// The returned error covers only the orphan sweep at the end.
func executeRemovals(toRemove []*InstalledSkill, harnessFilter []string, global bool, cwd string) error {
	failures := 0
	for _, sk := range toRemove {
		retained, err := removeSkillFromDisk(sk, harnessFilter, global, cwd)
		switch {
		case err != nil:
			failures++
			ui.LogError(fmt.Sprintf("%s: %v", sk.Name, err))
		case len(retained) > 0:
			ui.LogWarn(fmt.Sprintf("%s: removed from %s, but %s still %s it — keeping the skill and its lock entry",
				sk.Name,
				strings.Join(harnessDisplayNames(harnessFilter), ", "),
				strings.Join(harnessDisplayNames(retained), ", "),
				map[bool]string{true: "have", false: "has"}[len(retained) != 1]))
		default:
			ui.LogSuccess("Removed " + sk.Name)
		}
	}
	// The sweep drops lock entries whose canonical SKILL.md is gone. Skip it
	// after a failure: the skill is still on disk, and its entry has to stay.
	if !global && failures == 0 {
		if _, err := cleanOrphanedLocalLockEntries(cwd); err != nil {
			return err
		}
	}
	return nil
}

func runRemove(positional []string, opts RemoveOptions) {
	cwd, _ := os.Getwd()

	skillFilter := append(opts.Skills, positional...)

	global, ok := resolveRemoveScope(opts)
	if !ok {
		return
	}
	vlog(verboseFlag, "remove: global=%v filter=%v harnesses=%v", global, skillFilter, opts.Harnesses)

	scopeGlobal := &global
	installed, err := listInstalledSkills(scopeGlobal, opts.Harnesses)
	if err != nil {
		vlog(verboseFlag, "listing installed skills failed: %v", err)
	}
	if err != nil || len(installed) == 0 {
		handleNoInstalled(global, cwd)
		return
	}
	vlog(verboseFlag, "found %d installed skill(s) in scope", len(installed))

	toRemove, ok := selectSkillsToRemove(installed, skillFilter, opts)
	if !ok || len(toRemove) == 0 {
		return
	}
	toRemove = excludePluginOwnedSkills(toRemove, global, cwd)
	if len(toRemove) == 0 {
		return
	}

	if !opts.Yes && !confirmRemove(toRemove) {
		return
	}

	fmt.Println()
	lockErr := executeRemovals(toRemove, opts.Harnesses, global, cwd)
	fmt.Println()
	if lockErr != nil {
		ui.LogWarn(fmt.Sprintf("the lock file could not be updated: %v", lockErr))
		fmt.Println()
	}
}

// excludePluginOwnedSkills drops skills an installed plugin owns; `mdm plugins
// remove` handles those. Only project scope can be plugin-owned.
func excludePluginOwnedSkills(toRemove []*InstalledSkill, global bool, cwd string) []*InstalledSkill {
	if global {
		return toRemove
	}
	var keep []*InstalledSkill
	for _, sk := range toRemove {
		if owner := pluginOwningSkill(sk.Name, cwd); owner != "" {
			ui.LogWarn(fmt.Sprintf("%s is managed by plugin %s - remove it with 'mdm plugins remove %s'", sk.Name, owner, owner))
			continue
		}
		keep = append(keep, sk)
	}
	return keep
}

func confirmRemove(toRemove []*InstalledSkill) bool {
	var names []string
	for _, s := range toRemove {
		names = append(names, s.Name)
	}
	confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d skill(s): %s?", len(toRemove), strings.Join(names, ", ")))
	if !ok || !confirmed {
		fmt.Println("Cancelled.")
		return false
	}
	return true
}

// cleanOrphanedLockEntries removes global lock entries whose skill files no
// longer exist on disk. Returns the number of entries removed and any lock write error.
func cleanOrphanedLockEntries(cwd string) (int, error) {
	globalLock := lock.ReadGlobalState()
	if len(globalLock.Skills) == 0 {
		return 0, nil
	}
	canonicalBase := getCanonicalSkillsDir(true, cwd)
	var removed []string
	for name := range globalLock.Skills {
		skillDir := filepath.Join(canonicalBase, sanitizeName(name))
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMd); os.IsNotExist(err) {
			removed = append(removed, name)
		}
	}
	var firstErr error
	for _, name := range removed {
		if err := lock.RemoveSkillFromGlobalState(sanitizeName(name)); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(removed), firstErr
}

// cleanOrphanedLocalLockEntries removes project lock entries whose skill files
// no longer exist on disk. Returns the number of entries removed and any lock write error.
func cleanOrphanedLocalLockEntries(cwd string) (int, error) {
	localLock := lock.ReadLocalLock(cwd)
	if len(localLock.Skills) == 0 {
		return 0, nil
	}
	canonicalBase := getCanonicalSkillsDir(false, cwd)
	var removed []string
	for name := range localLock.Skills {
		skillDir := filepath.Join(canonicalBase, sanitizeName(name))
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMd); os.IsNotExist(err) {
			removed = append(removed, name)
		}
	}
	var firstErr error
	for _, name := range removed {
		if err := lock.RemoveSkillFromLocalLock(sanitizeName(name), cwd); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(removed), firstErr
}
