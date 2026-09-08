// `mdm agents remove`: delete a definition's harness copies and, once no harness
// holds it, its canonical file and lock entry.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

// agentInstalledOutside reports whether a harness other than the ones in kept
// still has a copy, so a `--harness X` removal knows whether the canonical
// file and lock entry are still needed. A kept copy is the user's own file,
// not an install, and does not count.
func agentInstalledOutside(name string, kept map[string]bool, global bool, cwd string) bool {
	for _, h := range agentInstalledHarnesses(name, global, cwd) {
		if !kept[h] {
			return true
		}
	}
	return false
}

// removeAgentHarnessCopies deletes the definition's file under each harness,
// except inside localSourceAbs, where the file is the user's own: an adopted
// link there is turned back into a real file and the path reported in kept.
// keptHarness names the harnesses whose copy was kept; failed describes every
// deletion or un-adoption that did not succeed.
func removeAgentHarnessCopies(name string, harnesses []string, localSourceAbs, canonicalPath string, global bool, cwd string) (kept []string, keptHarness map[string]bool, failed []string) {
	keptHarness = map[string]bool{}
	for _, harnessName := range harnesses {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target == "" || !isPathSafe(harness.AgentsInstallDirFor(harnessName, global, cwd), target) {
			continue
		}
		if localSourceAbs != "" && isInsideOrEqual(target, localSourceAbs) {
			if _, statErr := os.Lstat(target); statErr != nil {
				continue
			}
			if unErr := unadoptAgentLink(target, canonicalPath); unErr != nil {
				failed = append(failed, fmt.Sprintf("%s (%v)", harnessName, unErr))
				continue
			}
			kept = append(kept, target)
			keptHarness[harnessName] = true
			continue
		}
		if rmErr := removeFileFn(target); rmErr != nil && !os.IsNotExist(rmErr) {
			failed = append(failed, fmt.Sprintf("%s (%v)", harnessName, rmErr))
		}
	}
	return kept, keptHarness, failed
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
			runAgentRemove(args, opts)
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
	if opts.Global {
		return true, true
	}
	if opts.Project || opts.Yes {
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

func selectAgentsToRemove(lockNames, filterNames []string, opts AgentOptions) ([]string, bool) {
	if len(filterNames) == 1 && filterNames[0] == "*" {
		return lockNames, true
	}
	if len(filterNames) > 0 {
		var keep []string
		for _, f := range filterNames {
			for _, n := range lockNames {
				if skillNameMatches(n, f) {
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
	if opts.Yes || len(lockNames) == 1 {
		return lockNames, true
	}
	options := make([]ui.UIOption, len(lockNames))
	for i, n := range lockNames {
		options[i] = ui.UIOption{Label: n, Value: n}
	}
	indices, ok := ui.UiSearchMultiselect("Which agent definitions would you like to remove?", options, nil, nil, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []string
	for _, i := range indices {
		selected = append(selected, lockNames[i])
	}
	return selected, true
}

// removeFileFn is removeAgentFromDisk's deletion seam: tests swap it for a
// failing version. Shared mutable state, so those tests must not run in
// parallel.
var removeFileFn = os.Remove

// removeAgentFromDisk deletes one definition's per-harness copy for every
// harness in harnessFilter (all harnesses when empty). It drops the canonical
// file and the lock entry only once no harness, including harnesses outside
// harnessFilter, still has a copy. It returns fullyRemoved=false with a nil
// error when the copies in scope went but the definition lives elsewhere.
//
// Files inside the local source the definition was added from are the user's
// own and are never deleted; they are returned in kept. `mdm agents add .`
// adopts a hand-written .claude/agents/critic.md by copying it to the canonical
// file and leaving a symlink behind, so a kept link is first turned back into
// the real file it replaced, before the canonical file it points at can go.
func removeAgentFromDisk(name string, harnessFilter []string, format agentfile.Format, global bool, cwd string) (fullyRemoved bool, kept []string, err error) {
	harnesses := harnessFilter
	if len(harnesses) == 0 {
		for n := range harness.AllHarnesses {
			harnesses = append(harnesses, n)
		}
	}
	localSourceAbs := resolveLocalAgentSourceAbs(name, global, cwd)
	canonicalDir := harness.CanonicalAgentsDir(global, cwd)
	canonicalPath := agentCanonicalPath(name, format, global, cwd)

	kept, keptHarness, failed := removeAgentHarnessCopies(name, harnesses, localSourceAbs, canonicalPath, global, cwd)
	if len(failed) > 0 {
		return false, kept, fmt.Errorf("could not remove from %s", strings.Join(failed, ", "))
	}
	if agentInstalledOutside(name, keptHarness, global, cwd) {
		return false, kept, nil
	}

	if localSourceAbs != "" && isInsideOrEqual(canonicalPath, localSourceAbs) {
		if _, statErr := os.Lstat(canonicalPath); statErr == nil {
			kept = append(kept, canonicalPath)
		}
	} else if isPathSafe(canonicalDir, canonicalPath) {
		if rmErr := removeFileFn(canonicalPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return false, kept, fmt.Errorf("could not remove the canonical file: %w", rmErr)
		}
	}

	var lockErr error
	if global {
		lockErr = lock.RemoveAgentFromGlobalState(name)
	} else {
		lockErr = lock.RemoveAgentFromLocalLock(name, cwd)
	}
	if lockErr != nil {
		return false, kept, fmt.Errorf("could not update lock file: %w", lockErr)
	}
	return true, kept, nil
}

// resolveLocalAgentSourceAbs returns the absolute path of the directory the
// definition was added from when that was a local path, or "" otherwise. It is
// the agents-side twin of resolveLocalSourceAbs.
func resolveLocalAgentSourceAbs(name string, global bool, cwd string) string {
	var entry lock.AgentLockEntry
	var ok bool
	if global {
		entry, ok = lock.ReadGlobalState().Agents[name]
	} else {
		entry, ok = lock.ReadProjectLock(cwd).Agents[name]
	}
	if !ok || entry.SourceType != string(source.SourceTypeLocal) || entry.Source == "" {
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

func runAgentRemove(positional []string, opts AgentOptions) {
	cwd, _ := os.Getwd()
	filterNames := append(append([]string{}, opts.Agents...), positional...)

	global, ok := resolveAgentRemoveScope(opts)
	if !ok {
		return
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
	if len(lockNames) == 0 {
		fmt.Printf("%sNo agent definitions installed.%s\n", ansiDim, ansiReset)
		return
	}

	toRemove, ok := selectAgentsToRemove(lockNames, filterNames, opts)
	if !ok || len(toRemove) == 0 {
		return
	}

	if !opts.Yes {
		confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d agent definition(s): %s?", len(toRemove), strings.Join(toRemove, ", ")))
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
	}

	fmt.Println()
	for _, name := range toRemove {
		fullyRemoved, kept, err := removeAgentFromDisk(name, opts.Harnesses, lockedAgentFormat(lockEntries[name]), global, cwd)
		switch {
		case err != nil:
			ui.LogError(fmt.Sprintf("%s: %v", name, err))
		case fullyRemoved:
			ui.LogSuccess("Removed " + name)
		default:
			ui.LogWarn(fmt.Sprintf("%s: removed from the given harness(es), but it is still installed elsewhere - keeping the definition and its lock entry", name))
		}
		if len(kept) > 0 {
			ui.LogInfo(fmt.Sprintf("%s: kept %s: inside the local source it was added from", name, strings.Join(displayPaths(kept, cwd), ", ")))
		}
	}
	fmt.Println()
}
