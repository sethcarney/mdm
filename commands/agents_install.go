// `mdm agents install`: restore every definition mdm.lock or mdm-state.json
// records, from its recorded source and ref.
package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
)

func buildAgentsInstallCmd() *cobra.Command {
	var opts restoreOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore agent definitions from " + lockName,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			restoreAgentsFromLock(opts)
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

// restoreAgentsMap groups entries by source and calls runAgentAdd once per
// group, so a repo holding many definitions is cloned once per restore.
func restoreAgentsMap(entries map[string]lock.AgentLockEntry, global bool, opts restoreOptions, cwd string) {
	fmt.Printf("\n%sRestoring %d agent definition(s)...%s\n\n", ansiText, len(entries), ansiReset)

	refs := make(map[string]sourceRef, len(entries))
	for name, e := range entries {
		refs[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	groups := groupBySourceRef(refs)

	baseOpts := AgentOptions{Yes: opts.yes, AllowHiddenChars: opts.allowHiddenChars, Copy: opts.copy, Symlink: opts.symlink}
	if global {
		baseOpts.Global = true
	} else {
		baseOpts.Project = true
	}

	// Resolve harnesses once so the user is not prompted for each source group.
	harnesses, ok := promptAgentHarnesses(baseOpts, global, cwd)
	if !ok {
		fmt.Println("Cancelled.")
		return
	}
	baseOpts.Harnesses = harnesses

	vlog(verboseFlag, "grouped %d agent definition(s) into %d source group(s)", len(entries), len(groups))
	for _, group := range groups {
		vlog(verboseFlag, "restoring agent definitions from %q (ref=%q): %v", group.source, group.ref, group.names)
		fmt.Printf("%sInstalling from %s...%s\n", ansiDim, group.source, ansiReset)
		groupOpts := baseOpts
		groupOpts.Agents = group.names
		src := group.source
		if group.ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + group.ref
		}
		_ = runAgentAdd(src, groupOpts)
	}

	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
