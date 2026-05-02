package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

type SyncOptions struct {
	Yes bool
}

func buildSyncCmd(ver string) *cobra.Command {
	var opts SyncOptions

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync skills from node_modules into agent directories",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			runSync(opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runSync(opts SyncOptions) {
	cwd, _ := os.Getwd()

	fmt.Printf("\n%sScanning node_modules for skills...%s\n\n", ansiDim, ansiReset)

	found := skill.DiscoverNodeModuleSkills(cwd)
	if len(found) == 0 {
		fmt.Printf("%sNo skills found in node_modules.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Install skills packages with npm/yarn/pnpm first, then run %smdm skills sync%s\n\n", ansiText, ansiReset)
		return
	}

	// Flatten to *skill.Skill slice, deduplicating by name
	seen := map[string]bool{}
	var skills []*skill.Skill
	for _, f := range found {
		if !seen[f.Skill.Name] {
			seen[f.Skill.Name] = true
			skills = append(skills, f.Skill)
		}
	}

	fmt.Printf("%sFound %d skill(s) in node_modules:%s\n\n", ansiText, len(skills), ansiReset)
	for _, s := range skills {
		shortPath := shortenPath(s.Path, cwd)
		fmt.Printf("  %s%s%s  %s%s%s\n    %s%s%s\n\n",
			ansiText, s.Name, ansiReset,
			ansiDim, s.Description, ansiReset,
			ansiDim, shortPath, ansiReset)
	}

	// Skill selection
	var selectedSkills []*skill.Skill
	if opts.Yes || len(skills) == 1 {
		selectedSkills = skills
	} else {
		options := make([]ui.UIOption, len(skills))
		for i, s := range skills {
			options[i] = ui.UIOption{Label: s.Name, Value: sanitizeName(s.Name), Hint: s.Description}
		}
		var initSel []int
		for i := range skills {
			initSel = append(initSel, i)
		}
		indices, ok := ui.UiMultiselect("Which skills would you like to sync?", options, true, initSel, nil)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		for _, i := range indices {
			selectedSkills = append(selectedSkills, skills[i])
		}
	}

	global, mode, agents, ok := promptScopeAndAgents(AddOptions{Yes: opts.Yes}, cwd)
	if !ok {
		return
	}

	fmt.Println()
	for _, s := range selectedSkills {
		sName := sanitizeName(s.Name)
		fmt.Printf("%sSyncing %s%s%s...\n", ansiDim, ansiText, s.Name, ansiReset)

		var failedAgents []string
		for _, agentName := range agents {
			result := installSkillForAgent(s, agentName, global, mode)
			if !result.Success {
				failedAgents = append(failedAgents, agentName)
			}
		}

		if len(failedAgents) == 0 {
			ui.LogSuccess(s.Name)
		} else {
			ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", s.Name, strings.Join(failedAgents, ", ")))
		}

		// Use relative path from node_modules as source
		relPath, err := filepath.Rel(cwd, s.Path)
		if err != nil {
			relPath = s.Path
		}
		relPath = filepath.ToSlash(relPath)

		_ = lock.AddSkillToLock(sName, lock.SkillLockEntry{
			Source:     relPath,
			SourceType: string(source.SourceTypeLocal),
			SourceURL:  relPath,
			PluginName: sName,
		})
		if !global {
			_ = lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     relPath,
				SourceType: string(source.SourceTypeLocal),
			}, cwd)
		}
	}

	fmt.Println()
	printInstallSummary(len(selectedSkills), global, agents, mode)
}
