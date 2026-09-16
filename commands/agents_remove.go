// `mdm agents remove`: delete a definition's harness copies and, once no harness
// holds it, its canonical file and lock entry.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

// agentRemoval is what removeAgentFromDisk did for one definition.
type agentRemoval struct {
	// fullyRemoved is true once the canonical file and the lock entry are
	// gone; false when the definition lives on in a harness outside the
	// filter, or when nothing could be verified as mdm's to remove.
	fullyRemoved bool
	// kept lists the user's own files inside the local source the definition
	// was added from, left as real files.
	kept []string
	// foreign lists files at a harness path that mdm did not write - a
	// hand-written definition, or a copy edited since - left alone, as
	// "<harness>: <path>".
	foreign []string
}

// sameLinkOnDisk reports whether two paths name the same directory entry
// without following a final symlink: an adopted source file that is now a
// link to the canonical file is the same entry as itself and a different one
// from the canonical file it points at, which is exactly the distinction a
// removal after `mdm agents add .` needs.
func sameLinkOnDisk(a, b string) bool {
	ai, err := os.Lstat(a)
	if err != nil {
		return false
	}
	bi, err := os.Lstat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// localSourceKeeper returns the test for a path being the user's own file
// inside the local source the definition was added from. With the source
// file recorded, that is the one file discovery found - `mdm agents add .`
// discovers a hand-written .claude/agents/critic.md and then writes Codex's
// TOML and Copilot's copy beside it, and those are mdm's to delete. An entry
// written before the file was recorded cannot tell the source from what was
// written beside it, so for those nothing inside the source directory is
// deleted, as before. A definition from a remote source keeps nothing.
func localSourceKeeper(entry lock.AgentLockEntry, cwd string) func(path string) bool {
	localSourceAbs := resolveLocalAgentSourceAbs(entry, cwd)
	if localSourceAbs == "" {
		return func(string) bool { return false }
	}
	if entry.AgentPath == "" {
		return func(path string) bool { return isInsideOrEqual(path, localSourceAbs) }
	}
	sourceFile := filepath.Join(localSourceAbs, filepath.FromSlash(entry.AgentPath))
	return func(path string) bool { return sameLinkOnDisk(path, sourceFile) }
}

// removeAgentHarnessCopies deletes the definition's file under each harness.
// A file isSource says is the user's own is kept: an adopted link there is
// turned back into a real file and the path reported in kept. Any other file
// is deleted only when mdmOwnsAgentFile can show mdm wrote it, and is
// otherwise reported in foreign and left where it is. failed describes every
// deletion or un-adoption that did not succeed.
func removeAgentHarnessCopies(name string, harnesses []string, isSource func(string) bool, canonicalPath string, global bool, cwd string) (res agentRemoval, failed []string) {
	for _, harnessName := range harnesses {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target == "" || !isPathSafe(harness.AgentsInstallDirFor(harnessName, global, cwd), target) {
			continue
		}
		if _, statErr := os.Lstat(target); statErr != nil {
			continue
		}
		if isSource(target) {
			if unErr := unadoptAgentLink(target, canonicalPath); unErr != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", harnessName, unErr))
				continue
			}
			res.kept = append(res.kept, target)
			continue
		}
		if !mdmOwnsAgentFile(target, harnessName, canonicalPath) {
			res.foreign = append(res.foreign, harnessDisplayName(harnessName)+": "+target)
			continue
		}
		if rmErr := removeFileFn(target); rmErr != nil && !os.IsNotExist(rmErr) {
			failed = append(failed, fmt.Sprintf("%s (%v)", harnessName, rmErr))
		}
	}
	return res, failed
}

func buildAgentRemoveCmd() *cobra.Command {
	var opts AgentOptions

	cmd := &cobra.Command{
		Use:     "remove [names...]",
		Short:   "Remove installed agent definitions",
		Aliases: []string{"rm", "r"},
		Long: fmt.Sprintf(`Remove installed agent definitions.

If no names are provided an interactive selection menu is shown.

%sExamples:%s
  mdm agents remove
  mdm agents remove code-reviewer
  mdm agents remove code-reviewer -y`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			// A deletion that failed leaves the definition on disk and in
			// the lock; a script must not read that as done.
			if !runAgentRemove(args, opts) {
				os.Exit(1)
			}
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Remove from global scope")
	f.BoolVarP(&opts.Project, "project", "p", false, "Remove from project scope")
	f.StringArrayVar(&opts.Harnesses, "harness", nil, "Remove from specific harnesses (repeatable)")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Agent definition names to remove (repeatable)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")

	_ = cmd.RegisterFlagCompletionFunc("harness", harnessFlagCompletion)

	return cmd
}

func resolveAgentRemoveScope(opts AgentOptions) (global bool, ok bool) {
	return resolveScope(opts.Global, opts.Project, opts.Yes, "remove from")
}

func selectAgentsToRemove(lockNames, filterNames []string, opts AgentOptions) ([]string, bool) {
	return pickForRemoval(lockNames, filterNames, opts.Yes, "Which agent definitions would you like to remove?", filterLockNames,
		func(n string) ui.UIOption { return ui.UIOption{Label: n, Value: n} })
}

// filterLockNames keeps the lock names an explicit filter matches, in the
// filter's order; none matching is reported and fails the removal.
func filterLockNames(lockNames, filterNames []string) ([]string, bool) {
	var keep []string
	for _, f := range filterNames {
		for _, n := range lockNames {
			if agentNameMatches(n, f) {
				keep = append(keep, n)
				break
			}
		}
	}
	if len(keep) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo matching agent definitions found.%s\n", ansiText, ansiReset)
		return nil, false
	}
	return keep, true
}

// removeFileFn is removeAgentFromDisk's deletion seam: tests swap it for a
// failing version. Shared mutable state, so those tests must not run in
// parallel.
var removeFileFn = os.Remove

// removeAgentFromDisk deletes one definition's per-harness copy for every
// harness holding it that is in harnessFilter (every one when empty). Which
// harnesses hold it is the lock entry's list, or, for an entry written before
// the list existed, the harnesses whose file mdm can show it wrote; a
// same-named file anywhere else is the user's and is never looked at. It drops
// the canonical file and the lock entry only once no harness outside the
// filter still holds a copy, and otherwise takes the cleared harnesses off the
// entry's list, so the lock keeps describing the disk.
//
// The file the definition was discovered from in a local source is the user's
// own and is never deleted; it is returned in kept. `mdm agents add .` adopts
// a hand-written .claude/agents/critic.md by copying it to the canonical file
// and leaving a symlink behind, so a kept link is first turned back into the
// real file it replaced, before the canonical file it points at can go.
// Everything mdm wrote beside it - the canonical file, Codex's TOML, Copilot's
// copy - goes with the rest.
func removeAgentFromDisk(name string, harnessFilter []string, entry lock.AgentLockEntry, global bool, cwd string) (agentRemoval, error) {
	held := agentInstalledIn(name, entry, global, cwd)
	targets := held
	if len(harnessFilter) > 0 {
		targets = keepIn(held, stringSet(harnessFilter))
	}
	isSource := localSourceKeeper(entry, cwd)
	canonicalDir := harness.CanonicalAgentsDir(global, cwd)
	canonicalPath := agentCanonicalPath(name, lockedAgentFormat(entry), global, cwd)

	res, failed := removeAgentHarnessCopies(name, targets, isSource, canonicalPath, global, cwd)
	if len(failed) > 0 {
		return res, fmt.Errorf("could not remove from %s", strings.Join(failed, ", "))
	}
	if remaining := withoutHarnesses(held, stringSet(targets)); len(remaining) > 0 {
		if agentRecordedHarnesses(entry) != nil {
			entry.Harnesses = remaining
			if err := writeAgentEntry(name, entry, global, cwd); err != nil {
				return res, fmt.Errorf("could not update lock file: %w", err)
			}
		}
		return res, nil
	}

	if isSource(canonicalPath) {
		res.kept = append(res.kept, canonicalPath)
	} else if isPathSafe(canonicalDir, canonicalPath) {
		if rmErr := removeFileFn(canonicalPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return res, fmt.Errorf("could not remove the canonical file: %w", rmErr)
		}
	}

	var lockErr error
	if global {
		lockErr = lock.RemoveAgentFromGlobalState(name)
	} else {
		lockErr = lock.RemoveAgentFromLocalLock(name, cwd)
	}
	if lockErr != nil {
		return res, fmt.Errorf("could not update lock file: %w", lockErr)
	}
	res.fullyRemoved = true
	return res, nil
}

// writeAgentEntry puts an entry read from the lock back, unchanged apart from
// what the caller edited. recordAgentEntry is for a fresh install and
// relativizes a local source; an entry read from the lock already has the
// form the lock keeps.
func writeAgentEntry(name string, entry lock.AgentLockEntry, global bool, cwd string) error {
	if global {
		return lock.AddAgentToGlobalState(name, entry)
	}
	return lock.AddAgentToLocalLock(name, entry, cwd)
}

// resolveLocalAgentSourceAbs returns the absolute path of the directory the
// definition was added from when that was a local path, or "" otherwise. It is
// the agents-side twin of resolveLocalSourceAbs.
func resolveLocalAgentSourceAbs(entry lock.AgentLockEntry, cwd string) string {
	if entry.SourceType != string(source.SourceTypeLocal) || entry.Source == "" {
		return ""
	}
	src := entry.Source
	if !filepath.IsAbs(src) {
		src = filepath.Join(cwd, src)
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return ""
	}
	return abs
}

// displayPaths shows paths relative to cwd when they sit under it.
func displayPaths(paths []string, cwd string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
			out = append(out, rel)
			continue
		}
		out = append(out, p)
	}
	return out
}

// unadoptAgentLink turns a symlink at target back into a real file holding the
// canonical content. A real file, or a link to something other than the
// canonical file, is left as it is.
func unadoptAgentLink(target, canonicalPath string) error {
	fi, err := os.Lstat(target)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil
	}
	canonicalResolved, err := filepath.EvalSymlinks(canonicalPath)
	if err != nil || filepath.Clean(resolved) != filepath.Clean(canonicalResolved) {
		return nil
	}
	return replaceFileFrom(canonicalPath, target)
}

// hintHarnessNamesToRemove prints the moved-to-`mdm harnesses` hint when a
// requested name is a harness rather than an installed definition, and
// reports whether it did. `mdm agents remove cursor` was how a harness was
// dropped before this release; it would otherwise match no lock entry and
// report that nothing was found. A definition that happens to be named after
// a harness is still a definition, so a name the lock holds is never hinted.
func hintHarnessNamesToRemove(filterNames, lockNames []string) bool {
	var harnessNames []string
	for _, f := range filterNames {
		if harness.AllHarnesses[f] == nil {
			continue
		}
		recorded := false
		for _, n := range lockNames {
			if agentNameMatches(n, f) {
				recorded = true
				break
			}
		}
		if !recorded {
			harnessNames = append(harnessNames, f)
		}
	}
	if len(harnessNames) == 0 {
		return false
	}
	printHarnessNamesHint(harnessNames, "remove", "an installed agent definition", "<name>", "removes")
	return true
}

// runAgentRemove reports whether every selected removal succeeded. A
// cancelled prompt and an empty scope are not failures; an explicit name that
// matched nothing is.
func runAgentRemove(positional []string, opts AgentOptions) bool {
	cwd, _ := os.Getwd()
	filterNames := append(append([]string{}, opts.Agents...), positional...)

	global, ok := resolveAgentRemoveScope(opts)
	if !ok {
		return true
	}

	// An unrecognized name would otherwise match no harness and report a
	// removal that never happened. `agents add` rejects the same typo.
	if len(opts.Harnesses) > 0 {
		validated, valid := validateNamedHarnesses(opts.Harnesses)
		if !valid {
			os.Exit(1)
		}
		// A mixed list warns about the bad names and carries on with the
		// good ones, which is what the removal must then be scoped to.
		opts.Harnesses = validated
	}

	lockNames, lockEntries := agentLockEntries(global, cwd)
	if hintHarnessNamesToRemove(filterNames, lockNames) {
		return false
	}
	// A name the user typed that matched nothing is a failed removal: a
	// script must not read "No matching agent definitions found." as done.
	// "*" and no names at all ask for whatever is there, which can be nothing.
	explicit := len(filterNames) > 0 && (len(filterNames) != 1 || filterNames[0] != "*")
	if len(lockNames) == 0 {
		fmt.Printf("%sNo agent definitions installed.%s\n", ansiDim, ansiReset)
		return !explicit
	}

	toRemove, ok := selectAgentsToRemove(lockNames, filterNames, opts)
	if !ok {
		// An explicit filter fails only by matching nothing; without one,
		// the user cancelled the prompt.
		return !explicit
	}
	if len(toRemove) == 0 {
		return true
	}

	if !opts.Yes {
		confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d agent definition(s): %s?", len(toRemove), strings.Join(toRemove, ", ")))
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return true
		}
	}

	fmt.Println()
	ok = true
	for _, name := range toRemove {
		if !removeOneAgent(name, opts.Harnesses, lockEntries[name], global, cwd) {
			ok = false
		}
	}
	fmt.Println()
	return ok
}

// removeOneAgent removes one definition, prints its lines, and reports
// whether the removal succeeded.
func removeOneAgent(name string, harnessFilter []string, entry lock.AgentLockEntry, global bool, cwd string) bool {
	res, err := removeAgentFromDisk(name, harnessFilter, entry, global, cwd)
	switch {
	case err != nil:
		ui.LogError(fmt.Sprintf("%s: %v", name, err))
	case res.fullyRemoved:
		ui.LogSuccess("Removed " + name)
	default:
		ui.LogWarn(fmt.Sprintf("%s: removed from the given harness(es), but it is still installed elsewhere - keeping the definition and its lock entry", name))
	}
	if len(res.kept) > 0 {
		ui.LogInfo(fmt.Sprintf("%s: kept %s: inside the local source it was added from", name, strings.Join(displayPaths(res.kept, cwd), ", ")))
	}
	for _, f := range res.foreign {
		ui.LogInfo(fmt.Sprintf("%s: left %s alone: not the file mdm wrote there", name, f))
	}
	return err == nil
}
