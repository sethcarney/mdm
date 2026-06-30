package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildInstallFromLockCmd(ver string) *cobra.Command {
	var yes bool
	var allowHiddenChars bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore skills from skills-lock.json",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			runInstallFromLock(yes, allowHiddenChars)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	return cmd
}

func runInstallFromLock(yes bool, allowHiddenChars bool) {
	cwd, _ := os.Getwd()

	localL := lock.ReadLocalLock(cwd)
	globalL := lock.ReadSkillLock()

	hasLocal := len(localL.Skills) > 0
	hasGlobal := len(globalL.Skills) > 0
	vlog(verboseFlag, "install from lock: local=%d skill(s) global=%d skill(s)", len(localL.Skills), len(globalL.Skills))

	switch {
	case !hasLocal && !hasGlobal:
		fmt.Printf("\n%sNo skills-lock.json found.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add skills with %smdm skills add <package>%s\n\n", ansiText, ansiReset)

	case hasLocal && !hasGlobal:
		// Only local lock has skills — restore silently
		restoreFromLocalLock(localL, yes, allowHiddenChars)

	case !hasLocal && hasGlobal:
		// Only global lock has skills — explain and ask
		fmt.Printf("\n%sNo skills found in local skills-lock.json.%s\n", ansiDim, ansiReset)
		fmt.Printf("%sFound %d skill(s) in global skills-lock.json (%s).%s\n\n",
			ansiDim, len(globalL.Skills), lock.GetSkillLockPath(), ansiReset)
		if !yes {
			confirmed, ok := ui.UiConfirm("Install from global skills-lock.json?")
			if !ok || !confirmed {
				fmt.Println("Cancelled.")
				return
			}
		}
		restoreFromGlobalLock(globalL, yes, allowHiddenChars)

	default: // both have skills
		if yes {
			// Default to local when -y flag is used
			restoreFromLocalLock(localL, yes, allowHiddenChars)
		} else {
			idx, ok := ui.UiSelect("Install from which skills-lock.json?", []ui.UIOption{
				{Label: fmt.Sprintf("Local  — %d skill(s)", len(localL.Skills)), Hint: lock.GetLocalLockPath(cwd)},
				{Label: fmt.Sprintf("Global — %d skill(s)", len(globalL.Skills)), Hint: lock.GetSkillLockPath()},
			})
			if !ok {
				fmt.Println("Cancelled.")
				return
			}
			if idx == 1 {
				restoreFromGlobalLock(globalL, yes, allowHiddenChars)
			} else {
				restoreFromLocalLock(localL, yes, allowHiddenChars)
			}
		}
	}
}

// restoreFromLocalLock installs all skills recorded in the project-level lock file.
func restoreFromLocalLock(l lock.LocalSkillLockFile, yes bool, allowHiddenChars bool) {
	fmt.Printf("\n%sRestoring %d skill(s) from local skills-lock.json...%s\n\n", ansiText, len(l.Skills), ansiReset)

	// Convert local entries to a common source/ref map.
	entries := make(map[string]sourceRef, len(l.Skills))
	for name, e := range l.Skills {
		entries[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	restoreSkills(entries, AddOptions{Project: true, Yes: yes, AllowHiddenChars: allowHiddenChars})
}

// restoreFromGlobalLock installs all skills recorded in the global lock file.
func restoreFromGlobalLock(l lock.SkillLockFile, yes bool, allowHiddenChars bool) {
	fmt.Printf("\n%sRestoring %d skill(s) from global skills-lock.json...%s\n\n", ansiText, len(l.Skills), ansiReset)

	entries := make(map[string]sourceRef, len(l.Skills))
	for name, e := range l.Skills {
		entries[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	restoreSkills(entries, AddOptions{Global: true, Yes: yes, AllowHiddenChars: allowHiddenChars})
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
	for _, group := range sourceMap {
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
