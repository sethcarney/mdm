// `mdm agents install`: restore every definition mdm.lock or mdm-state.json
// records, from its recorded source and ref.
package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildAgentsInstallCmd() *cobra.Command {
	var opts restoreOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore agent definitions from " + lockName,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			restoreAgentsFromLock(opts)
			if restoreFailed {
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&opts.allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	cmd.Flags().BoolVar(&opts.copy, "copy", false, "Copy files instead of symlinking (switches the scope to copy mode)")
	cmd.Flags().BoolVar(&opts.symlink, "symlink", false, "Symlink files from .agents/agents (the default; switches a scope back from copy mode)")
	// The install mode is one switch with two settings, so asking for
	// both is a contradiction rather than a precedence puzzle.
	cmd.MarkFlagsMutuallyExclusive("copy", "symlink")
	return cmd
}

// restoreAgentsFromLock installs every agent definition recorded in the local
// and global locks, mirroring restoreSkillsFromCurrentLock in install.go.
func restoreAgentsFromLock(opts restoreOptions) {
	cwd, _ := os.Getwd()

	localAgents := lock.ReadProjectLock(cwd).Agents
	globalAgents := lock.ReadGlobalState().Agents

	hasLocal := len(localAgents) > 0
	hasGlobal := len(globalAgents) > 0
	vlog(verboseFlag, "install from lock: local=%d agent(s) global=%d agent(s)", len(localAgents), len(globalAgents))

	switch {
	case !hasLocal && !hasGlobal:
		// A project with no agent definitions is a normal outcome.
		return

	default:
		global, ok := chooseRestoreScope(len(localAgents), len(globalAgents), opts.yes, "agent definition", cwd)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		if global {
			restoreAgentsMap(globalAgents, true, opts, cwd)
		} else {
			restoreAgentsMap(localAgents, false, opts, cwd)
		}
	}
}

// restoreTargets decides where each entry goes back: the harness list the
// entry records, or, for an entry written before the list existed, the
// harnesses whose file for it mdm can show it wrote. Entries with neither are
// returned in unlisted for the caller to ask about. Restoring an old entry
// into every capable harness instead would write into directories the user
// never chose, .github/agents among them, which is committed.
func restoreTargets(entries map[string]lock.AgentLockEntry, global bool, cwd string) (harnessesFor map[string][]string, unlisted []string) {
	harnessesFor = make(map[string][]string, len(entries))
	for name, e := range entries {
		targets := agentRecordedHarnesses(e)
		if targets == nil {
			targets = agentOwnedHarnesses(name, e, global, cwd)
		}
		if len(targets) == 0 {
			unlisted = append(unlisted, name)
			continue
		}
		harnessesFor[name] = targets
	}
	sort.Strings(unlisted)
	return harnessesFor, unlisted
}

// restoreAgentsMap groups entries by source and calls runAgentAdd once per
// group, so a repo holding many definitions is cloned once per restore.
func restoreAgentsMap(entries map[string]lock.AgentLockEntry, global bool, opts restoreOptions, cwd string) {
	fmt.Printf("\n%sRestoring %d agent definition(s)...%s\n\n", ansiText, len(entries), ansiReset)

	refs := make(map[string]sourceRef, len(entries))
	for name, e := range entries {
		refs[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	groups := groupBySourceRef(refs)

	// Force: a restore reinstalls the lock's own entries, so the recorded
	// source is by definition the one each definition belongs to, even when
	// that source has since moved the file.
	baseOpts := AgentOptions{Yes: opts.yes, AllowHiddenChars: opts.allowHiddenChars, Copy: opts.copy, Symlink: opts.symlink, Force: true}
	if global {
		baseOpts.Global = true
	} else {
		baseOpts.Project = true
	}

	// Each definition goes back into the harnesses its lock entry names. Only
	// an entry with no list, and no file mdm can vouch for on disk, needs the
	// user asked, and then once for all such entries rather than per group.
	harnessesFor, unlisted := restoreTargets(entries, global, cwd)
	if len(unlisted) > 0 {
		harnesses, ok := promptAgentHarnesses(baseOpts, global, cwd)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		for _, name := range unlisted {
			harnessesFor[name] = harnesses
		}
	}
	baseOpts.HarnessesFor = harnessesFor
	var all [][]string
	for _, list := range harnessesFor {
		all = append(all, list)
	}
	baseOpts.Harnesses = unionHarnesses(all...)

	vlog(verboseFlag, "grouped %d agent definition(s) into %d source group(s)", len(entries), len(groups))
	var unrestorable []string
	for _, group := range groups {
		vlog(verboseFlag, "restoring agent definitions from %q (ref=%q): %v", group.source, group.ref, group.names)
		// Same reasoning as the skills restore: fetching a missing local path
		// exits the process, which would abandon every later definition.
		if why := unreachableLocalSource(group.source, cwd); why != "" {
			for _, name := range group.names {
				ui.LogWarn(fmt.Sprintf("%s: %s", name, why))
				unrestorable = append(unrestorable, name)
			}
			continue
		}
		fmt.Printf("%sInstalling from %s...%s\n", ansiDim, group.source, ansiReset)
		groupOpts := baseOpts
		groupOpts.Agents = group.names
		src := group.source
		if group.ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + group.ref
		}
		_ = runAgentAdd(src, groupOpts)
	}

	reportUnrestorable(unrestorable, "agent definition",
		"Move it into the repository, or re-add it from a source your team can reach.")
	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
