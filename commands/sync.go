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
	Yes              bool
	AllowHiddenChars bool
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
	cmd.Flags().BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	return cmd
}

func selectSkillsToSync(skills []*skill.Skill, yes bool) ([]*skill.Skill, bool) {
	if yes || len(skills) == 1 {
		return skills, true
	}
	options := make([]ui.UIOption, len(skills))
	for i, s := range skills {
		options[i] = ui.UIOption{Label: s.Name, Value: sanitizeName(s.Name), Hint: s.Description}
	}
	initSel := make([]int, len(skills))
	for i := range skills {
		initSel[i] = i
	}
	indices, ok := ui.UiSearchMultiselect("Which skills would you like to sync?", options, nil, initSel, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []*skill.Skill
	for _, i := range indices {
		selected = append(selected, skills[i])
	}
	return selected, true
}

func syncAndLockSkill(s *skill.Skill, agents []string, global bool, mode InstallMode, cwd string) {
	sName := sanitizeName(s.Name)
	fmt.Printf("%sSyncing %s%s%s...\n", ansiDim, ansiText, s.Name, ansiReset)

	vlog(verboseFlag, "syncing %s from %s → agents=%v (global=%v, mode=%v)", s.Name, s.Path, agents, global, mode)
	var failedAgents []string
	for _, agentName := range agents {
		result := installSkillForAgent(s, agentName, global, mode)
		if !result.Success {
			vlog(verboseFlag, "install failed for %s → agent %q", s.Name, agentName)
			failedAgents = append(failedAgents, agentName)
		}
	}

	if len(failedAgents) == 0 {
		ui.LogSuccess(s.Name)
	} else {
		ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", s.Name, strings.Join(failedAgents, ", ")))
	}

	relPath, err := filepath.Rel(cwd, s.Path)
	if err != nil {
		relPath = s.Path
	}
	relPath = filepath.ToSlash(relPath)

	if global {
		if err := lock.AddSkillToLock(sName, lock.SkillLockEntry{
			Source:     relPath,
			SourceType: string(source.SourceTypeLocal),
			SourceURL:  relPath,
			PluginName: sName,
		}); err != nil {
			ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
		}
	} else {
		if err := lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
			Source:     relPath,
			SourceType: string(source.SourceTypeLocal),
		}, cwd); err != nil {
			ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
		}
	}
}

func runSync(opts SyncOptions) {
	cwd, _ := os.Getwd()

	fmt.Printf("\n%sScanning node_modules for skills...%s\n\n", ansiDim, ansiReset)

	found := skill.DiscoverNodeModuleSkills(cwd)
	vlog(verboseFlag, "scanned node_modules under %s: found %d skill record(s)", cwd, len(found))
	if len(found) == 0 {
		fmt.Printf("%sNo skills found in node_modules.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Install skills packages with npm/yarn/pnpm first, then run %smdm skills sync%s\n\n", ansiText, ansiReset)
		return
	}

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

	selectedSkills, ok := selectSkillsToSync(skills, opts.Yes)
	if !ok {
		return
	}
	if !checkDiskSkillsMarkdownForHiddenChars(selectedSkills, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	global, mode, agents, ok := promptScopeAndAgents(AddOptions{Yes: opts.Yes}, cwd)
	if !ok {
		return
	}

	fmt.Println()
	for _, s := range selectedSkills {
		syncAndLockSkill(s, agents, global, mode, cwd)
	}

	fmt.Println()
	printInstallSummary(len(selectedSkills), global, agents, mode)
}
