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

// restoreOptions carries what `mdm skills install` was asked for. The mode
// flags belong here and not only on `skills add`: install is the command
// that restores a scope which already exists, so it is where a user changes
// the mode of one, and `skills add` needs a source to name.
type restoreOptions struct {
	yes              bool
	allowHiddenChars bool
	copy             bool
	symlink          bool
}

func buildInstallFromLockCmd(ver string) *cobra.Command {
	var opts restoreOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore skills from " + lockName,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			runInstallFromLock(opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&opts.allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	cmd.Flags().BoolVar(&opts.copy, "copy", false, "Copy files instead of symlinking (switches the scope to copy mode)")
	cmd.Flags().BoolVar(&opts.symlink, "symlink", false, "Symlink files from .agents/skills (the default; switches a scope back from copy mode)")
	// The install mode is one switch with two settings, so asking for
	// both is a contradiction rather than a precedence puzzle.
	cmd.MarkFlagsMutuallyExclusive("copy", "symlink")
	return cmd
}

// hintPluginsInstall points at `mdm plugins install` when the project has a
// plugin section in its lock — plugin restore is a separate command.
func hintPluginsInstall(cwd string) {
	if len(lock.ReadPluginsLock(cwd).Plugins) == 0 {
		return
	}
	fmt.Printf("%sThis project also has plugins — restore them with 'mdm plugins install'.%s\n", ansiDim, ansiReset)
}

func runInstallFromLock(opts restoreOptions) {
	cwd, _ := os.Getwd()
	hintPluginsInstall(cwd)

	localL := lock.ReadLocalLock(cwd)
	globalL := lock.ReadGlobalState()

	hasLocal := len(localL.Skills) > 0
	hasGlobal := len(globalL.Skills) > 0
	vlog(verboseFlag, "install from lock: local=%d skill(s) global=%d skill(s)", len(localL.Skills), len(globalL.Skills))

	switch {
	case !hasLocal && !hasGlobal:
		fmt.Printf("\n%sNo %s found.%s\n\n", ansiDim, lockName, ansiReset)
		fmt.Printf("Add skills with %smdm skills add <package>%s\n\n", ansiText, ansiReset)

	case hasLocal && !hasGlobal:
		// Only local lock has skills — restore silently
		restoreFromLocalLock(localL, opts)

	case !hasLocal && hasGlobal:
		// Only global lock has skills — explain and ask
		fmt.Printf("\n%sNo skills found in the local %s.%s\n", ansiDim, lockName, ansiReset)
		fmt.Printf("%sFound %d skill(s) in the global state file (%s).%s\n\n",
			ansiDim, len(globalL.Skills), lock.GetGlobalStatePath(), ansiReset)
		if !opts.yes {
			confirmed, ok := ui.UiConfirm("Install from the globally recorded skills?")
			if !ok || !confirmed {
				fmt.Println("Cancelled.")
				return
			}
		}
		restoreFromGlobalLock(globalL, opts)

	default: // both have skills
		if opts.yes {
			// Default to local when -y flag is used
			restoreFromLocalLock(localL, opts)
		} else {
			idx, ok := ui.UiSelect("Install from which lock file?", []ui.UIOption{
				{Label: fmt.Sprintf("Local  — %d skill(s)", len(localL.Skills)), Hint: lock.GetProjectLockPath(cwd)},
				{Label: fmt.Sprintf("Global — %d skill(s)", len(globalL.Skills)), Hint: lock.GetGlobalStatePath()},
			})
			if !ok {
				fmt.Println("Cancelled.")
				return
			}
			if idx == 1 {
				restoreFromGlobalLock(globalL, opts)
			} else {
				restoreFromLocalLock(localL, opts)
			}
		}
	}
}

// restoreFromLocalLock installs all skills recorded in the project-level lock file.
func restoreFromLocalLock(l lock.LocalSkillLockFile, opts restoreOptions) {
	fmt.Printf("\n%sRestoring %d skill(s) from the local %s...%s\n\n", ansiText, len(l.Skills), lockName, ansiReset)

	// Convert local entries to a common source/ref map.
	entries := make(map[string]sourceRef, len(l.Skills))
	for name, e := range l.Skills {
		entries[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	restoreSkills(entries, AddOptions{Project: true, Yes: opts.yes, AllowHiddenChars: opts.allowHiddenChars, Copy: opts.copy, Symlink: opts.symlink})
}

// restoreFromGlobalLock installs all skills recorded in the global lock file.
func restoreFromGlobalLock(l lock.GlobalState, opts restoreOptions) {
	fmt.Printf("\n%sRestoring %d skill(s) from the global state file...%s\n\n", ansiText, len(l.Skills), ansiReset)

	entries := make(map[string]sourceRef, len(l.Skills))
	for name, e := range l.Skills {
		entries[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	restoreSkills(entries, AddOptions{Global: true, Yes: opts.yes, AllowHiddenChars: opts.allowHiddenChars, Copy: opts.copy, Symlink: opts.symlink})
}

// sourceRef holds the source URL/path and optional ref for a lock entry.
type sourceRef struct {
	source string
	ref    string
}

// restoreSkills groups lock entries by source and calls runAdd for each group.
func restoreSkills(entries map[string]sourceRef, baseOpts AddOptions) {
	type sourceGroup struct {
		source string
		ref    string
		skills []string
	}
	sourceMap := map[string]*sourceGroup{}
	for name, e := range entries {
		// Normalize: strip a trailing #fragment from the source when it duplicates
		// the ref field (e.g. "git@host:repo.git#main" with ref="main" should group
		// with "git@host:repo.git" with ref="main").
		normalizedSource := e.source
		if e.ref != "" {
			if idx := strings.LastIndex(normalizedSource, "#"); idx >= 0 {
				if strings.EqualFold(normalizedSource[idx+1:], e.ref) {
					normalizedSource = normalizedSource[:idx]
				}
			}
		}
		key := normalizedSource + "|" + e.ref
		if g, ok := sourceMap[key]; ok {
			g.skills = append(g.skills, name)
		} else {
			sourceMap[key] = &sourceGroup{source: normalizedSource, ref: e.ref, skills: []string{name}}
		}
	}

	// Resolve agents once so the user is not prompted for each source group.
	if len(baseOpts.Agents) == 0 {
		cwd, _ := os.Getwd()
		agents, ok := promptAgents(baseOpts, baseOpts.Global, cwd)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		baseOpts.Agents = agents
	}

	vlog(verboseFlag, "grouped %d skill(s) into %d source group(s)", len(entries), len(sourceMap))
	// Iterate groups (and each group's skills) in sorted order so restores are
	// deterministic rather than following map iteration order.
	keys := make([]string, 0, len(sourceMap))
	for key := range sourceMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := sourceMap[key]
		sort.Strings(group.skills)
		vlog(verboseFlag, "restoring from %q (ref=%q): %v", group.source, group.ref, group.skills)
		fmt.Printf("%sInstalling from %s...%s\n", ansiDim, group.source, ansiReset)
		opts := baseOpts
		opts.Skills = group.skills
		src := group.source
		if group.ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + group.ref
		}
		runAdd(src, opts)
	}

	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
