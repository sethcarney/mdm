package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage your configured agent list",
		Long: fmt.Sprintf(`Manage the list of AI agents mdm should support by default.

Configured agents are used as the default selection when running
%smdm skills add%s without an explicit %s--agent%s flag.

%sExamples:%s
  mdm agents list
  mdm agents add claude-code cursor
  mdm agents remove cursor
  mdm agents add --global claude-code`, ansiBold, ansiReset, ansiBold, ansiReset, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(
		buildAgentsListCmd(),
		buildAgentsAddCmd(),
		buildAgentsRemoveCmd(),
	)

	return cmd
}

type agentListItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Scope       string `json:"scope"`
	Installed   bool   `json:"installed"`
}

func buildAgentsListCmd() *cobra.Command {
	var global bool
	var jsonMode bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			configured := lock.GetConfiguredAgents(global, cwd)
			scope := "project"
			if global {
				scope = "global"
			}

			if jsonMode {
				items := make([]agentListItem, 0, len(configured))
				for _, name := range configured {
					cfg := agent.AllAgents[name]
					item := agentListItem{Name: name, Scope: scope}
					if cfg != nil {
						item.DisplayName = cfg.DisplayName
						item.Installed = cfg.DetectInstalled != nil && cfg.DetectInstalled()
					} else {
						item.DisplayName = name
					}
					items = append(items, item)
				}
				out, _ := json.MarshalIndent(items, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			if len(configured) == 0 {
				fmt.Printf("%sNo agents configured for %s scope.%s\n", ansiDim, scope, ansiReset)
				fmt.Printf("Run %smdm agents add%s to configure your agents.\n", ansiBold, ansiReset)
				return nil
			}
			fmt.Printf("%s%s scope agents:%s\n\n", ansiBold, strings.ToUpper(scope[:1])+scope[1:], ansiReset)
			for _, name := range configured {
				cfg := agent.AllAgents[name]
				if cfg == nil {
					fmt.Printf("  %s%s%s %s(unknown)%s\n", ansiText, name, ansiReset, ansiDim, ansiReset)
					continue
				}
				detected := ""
				if cfg.DetectInstalled != nil && cfg.DetectInstalled() {
					detected = fmt.Sprintf("  %s✓ installed%s", ansiGreen, ansiReset)
				}
				fmt.Printf("  %s%-28s%s%s\n", ansiText, cfg.DisplayName, ansiReset, detected)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "List global configured agents")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	return cmd
}

func buildAgentsAddCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:     "add [agents...]",
		Aliases: []string{"a"},
		Short:   "Add agents to your configured list",
		Long: fmt.Sprintf(`Add one or more agents to your configured list.

If no agent names are provided an interactive picker is shown.
Use %s--global%s / %s-g%s to configure agents at the user level.`, ansiBold, ansiReset, ansiBold, ansiReset),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()

			if len(args) == 0 {
				if !cmd.Flags().Changed("global") {
					isGlobal, ok := promptAgentScope()
					if !ok {
						return nil
					}
					global = isGlobal
				}
				scope := "project"
				if global {
					scope = "global"
				}
				selected, err := pickAndSaveAgents(global, scope, cwd)
				if err != nil {
					return err
				}
				if len(selected) > 0 {
					runAgentSetup(selected, cwd)
				}
				return nil
			}

			scope := "project"
			if global {
				scope = "global"
			}
			toAdd, ok := validateNamedAgents(args)
			if !ok {
				return fmt.Errorf("no valid agents specified")
			}
			if err := lock.AddToConfiguredAgents(toAdd, global, cwd); err != nil {
				return fmt.Errorf("saving agents: %w", err)
			}
			for _, name := range toAdd {
				cfg := agent.AllAgents[name]
				displayName := name
				if cfg != nil {
					displayName = cfg.DisplayName
				}
				fmt.Printf("%s✓%s Added %s%s%s to %s configured agents\n",
					ansiGreen, ansiReset, ansiBold, displayName, ansiReset, scope)
			}
			runAgentSetup(toAdd, cwd)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Add to global configured agents")
	return cmd
}

// pickAndSaveAgents shows an interactive picker pre-seeded with the current
// configured list and replaces the entire list with the user's selection.
// Truly universal agents (share .agents/skills AND have no unique instruction
// file) are excluded from the picker — they are always supported and need no
// configuration. Returns the saved agent names so the caller can act on them.
func pickAndSaveAgents(global bool, scope, cwd string) ([]string, error) {
	current := lock.GetConfiguredAgents(global, cwd)
	currentSet := map[string]bool{}
	for _, a := range current {
		currentSet[a] = true
	}

	var options []ui.UIOption
	var lockedOptions []ui.UIOption
	for name, cfg := range agent.AllAgents {
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		if agent.NeedsNoTracking(name) {
			lockedOptions = append(lockedOptions, ui.UIOption{Label: cfg.DisplayName, Value: name})
			continue
		}
		options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: name})
	}
	sort.Slice(options, func(i, j int) bool {
		return options[i].Label < options[j].Label
	})
	sort.Slice(lockedOptions, func(i, j int) bool {
		return lockedOptions[i].Label < lockedOptions[j].Label
	})

	var initSel []int
	for i, opt := range options {
		if currentSet[opt.Value] {
			initSel = append(initSel, i)
		}
	}

	selected, ok := ui.UiSearchMultiselect("Which agents do you want to configure?", options, lockedOptions, initSel)
	if !ok {
		return nil, nil
	}
	var newList []string
	for _, i := range selected {
		newList = append(newList, options[i].Value)
	}
	if len(newList) == 0 {
		fmt.Printf("%sNo agents selected.%s\n", ansiDim, ansiReset)
		return nil, nil
	}
	sort.Strings(newList)
	if err := lock.SetConfiguredAgents(newList, global, cwd); err != nil {
		return nil, fmt.Errorf("saving agents: %w", err)
	}
	printAgentsSaved(newList, scope)

	// Return only newly added agents so setup only runs for them, not for
	// agents that were already configured before this invocation.
	var newlyAdded []string
	for _, name := range newList {
		if !currentSet[name] {
			newlyAdded = append(newlyAdded, name)
		}
	}
	return newlyAdded, nil
}

func promptAgentScope() (isGlobal bool, ok bool) {
	opts := []ui.UIOption{
		{Label: "Project", Value: "project", Hint: "skills-lock.json in this directory"},
		{Label: "Global", Value: "global", Hint: "~/.agents/skills-lock.json"},
	}
	idx, ok := ui.UiSelect("Configure agents for which scope?", opts)
	if !ok {
		return false, false
	}
	return idx == 1, true
}

func buildAgentsRemoveCmd() *cobra.Command {
	var global bool
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove [agents...]",
		Aliases: []string{"rm", "r"},
		Short:   "Remove agents from your configured list",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()

			if len(args) == 0 && !cmd.Flags().Changed("global") && !yes {
				isGlobal, ok := promptAgentScope()
				if !ok {
					return nil
				}
				global = isGlobal
			}

			scope := "project"
			if global {
				scope = "global"
			}

			configured := lock.GetConfiguredAgents(global, cwd)
			if len(configured) == 0 {
				fmt.Printf("%sNo agents configured for %s scope.%s\n", ansiDim, scope, ansiReset)
				return nil
			}

			var toRemove []string
			if len(args) > 0 {
				validated, ok := validateNamedAgents(args)
				if !ok {
					return fmt.Errorf("no valid agents specified")
				}
				toRemove = validated
			} else if yes {
				return fmt.Errorf("agent names are required when using --yes")
			} else {
				picked, ok := pickAgentsToRemove(configured)
				if !ok {
					return nil
				}
				toRemove = picked
			}

			if !yes {
				var displayNames []string
				for _, name := range toRemove {
					cfg := agent.AllAgents[name]
					if cfg != nil {
						displayNames = append(displayNames, cfg.DisplayName)
					} else {
						displayNames = append(displayNames, name)
					}
				}
				confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d agent(s): %s?", len(toRemove), strings.Join(displayNames, ", ")))
				if !ok || !confirmed {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			if err := lock.RemoveFromConfiguredAgents(toRemove, global, cwd); err != nil {
				return fmt.Errorf("saving agents: %w", err)
			}
			for _, name := range toRemove {
				cfg := agent.AllAgents[name]
				displayName := name
				if cfg != nil {
					displayName = cfg.DisplayName
				}
				fmt.Printf("%s✓%s Removed %s%s%s from %s configured agents\n",
					ansiGreen, ansiReset, ansiBold, displayName, ansiReset, scope)
			}
			fmt.Println()
			cleanUpRemovedAgentFiles(toRemove, global, cwd)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Remove from global configured agents")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

// pickAgentsToRemove shows an interactive picker with nothing pre-selected;
// the user checks the agents they want to remove.
func pickAgentsToRemove(configured []string) ([]string, bool) {
	var options []ui.UIOption
	for _, name := range configured {
		cfg := agent.AllAgents[name]
		label := name
		if cfg != nil {
			label = cfg.DisplayName
		}
		options = append(options, ui.UIOption{Label: label, Value: name})
	}

	indices, ok := ui.UiMultiselect("Which agents would you like to remove?", options, false, nil, nil)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	if len(indices) == 0 {
		fmt.Printf("%sNo agents selected.%s\n", ansiDim, ansiReset)
		return nil, false
	}
	var toRemove []string
	for _, i := range indices {
		toRemove = append(toRemove, options[i].Value)
	}
	return toRemove, true
}

// cleanUpRemovedAgentFiles removes the skills directory and instructions file
// that belong exclusively to each agent being removed. Shared resources
// (.agents/skills, AGENTS.md) are never touched.
func cleanUpRemovedAgentFiles(toRemove []string, global bool, cwd string) {
	for _, name := range toRemove {
		cfg := agent.AllAgents[name]
		if cfg == nil {
			continue
		}

		// Remove the agent's unique skills directory (skip shared .agents/skills).
		if !agent.UsesSharedSkillsDir(name) {
			var skillsPath string
			if global {
				skillsPath = cfg.GlobalSkillsDir
			} else {
				skillsPath = filepath.Join(cwd, cfg.SkillsDir)
			}
			if skillsPath != "" {
				if info, err := os.Lstat(skillsPath); err == nil {
					if info.Mode()&os.ModeSymlink != 0 {
						os.Remove(skillsPath)
					} else {
						os.RemoveAll(skillsPath)
					}
					ui.LogInfo("Removed " + cfg.DisplayName + " skills directory")
				}
			}
		}

		// Remove the agent's instructions file (project scope only; skip when
		// the agent has no unique instructions file or reads AGENTS.md natively).
		if !global && !cfg.NativeInstructions {
			instrPath := filepath.Join(cwd, cfg.InstructionsFile)
			if _, err := os.Lstat(instrPath); err == nil {
				os.Remove(instrPath)
				ui.LogInfo("Removed " + cfg.InstructionsFile)
			}
		}
	}
}

// ─── Agent setup (auto-link rules + install locked skills) ────────────────────

// runAgentSetup links instruction files to AGENTS.md and installs any already-
// locked skills for the newly configured agents. It is intentionally silent when
// there is nothing to do so the happy-path output stays clean.
func runAgentSetup(agentNames []string, cwd string) {
	linkNewAgentRules(agentNames, cwd)
	installLockedSkillsForAgents(agentNames, cwd)
}

// linkNewAgentRules links each agent's instruction file to AGENTS.md when
// AGENTS.md already exists as a real (non-symlink) file in the project directory.
// Instruction files that already exist as real files are skipped with a hint to
// run `mdm rules link` instead, to avoid silent data loss.
func linkNewAgentRules(agentNames []string, cwd string) {
	agentsMDPath := filepath.Join(cwd, agentsMDFile)
	info, err := os.Lstat(agentsMDPath)
	if err != nil || !info.Mode().IsRegular() {
		return
	}

	var toLink []agentCandidate
	var skippedFiles []string
	for _, name := range agentNames {
		cfg := agent.AllAgents[name]
		if cfg == nil || cfg.NativeInstructions {
			continue
		}
		targetPath := filepath.Join(cwd, cfg.InstructionsFile)
		targetInfo, statErr := os.Lstat(targetPath)
		if statErr == nil && targetInfo.Mode()&os.ModeSymlink == 0 {
			// Existing real file — skip to avoid silent data loss.
			skippedFiles = append(skippedFiles, cfg.InstructionsFile)
			continue
		}
		toLink = append(toLink, agentCandidate{name: name, displayName: cfg.DisplayName, file: cfg.InstructionsFile})
	}

	if len(skippedFiles) > 0 {
		fmt.Println()
		for _, f := range skippedFiles {
			fmt.Printf("  %s~%s %-35s %sskipped (existing file — run `mdm rules link` to replace)%s\n",
				ansiYellow, ansiReset, f, ansiDim, ansiReset)
		}
	}

	if len(toLink) == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("%sLinking instruction files → %s%s\n", ansiText, agentsMDFile, ansiReset)
	fmt.Println()
	createAgentSymlinks(toLink, cwd, agentsMDPath, true)
}

type skillLinkSpec struct {
	skillName string
	agentName string
	global    bool
}

// agentsNeedingSkillLinks returns agents from agentNames that have a unique
// (non-shared) skills directory and therefore need explicit skill linking.
func agentsNeedingSkillLinks(agentNames []string) []string {
	var result []string
	for _, name := range agentNames {
		if !agent.UsesSharedSkillsDir(name) && agent.AllAgents[name] != nil {
			result = append(result, name)
		}
	}
	return result
}

// collectSkillLinkSpecs gathers (skill, agent, global) triples for skills that
// are recorded in either the project or global lock file but not yet installed
// for the given target agents.
func collectSkillLinkSpecs(targets []string, cwd string) []skillLinkSpec {
	var specs []skillLinkSpec
	localLk := lock.ReadLocalLock(cwd)
	for skillName := range localLk.Skills {
		for _, agentName := range targets {
			if !isSkillInstalled(skillName, agentName, false) {
				specs = append(specs, skillLinkSpec{skillName, agentName, false})
			}
		}
	}
	globalLk := lock.ReadSkillLock()
	for skillName := range globalLk.Skills {
		for _, agentName := range targets {
			a := agent.AllAgents[agentName]
			if a == nil || a.GlobalSkillsDir == "" {
				continue
			}
			if !isSkillInstalled(skillName, agentName, true) {
				specs = append(specs, skillLinkSpec{skillName, agentName, true})
			}
		}
	}
	return specs
}

// installLockedSkillsForAgents installs all locked skills (from the project and
// global lock files) for agents that have a unique skills directory. Agents that
// use the shared .agents/skills directory already have access to every installed
// skill automatically and are skipped.
func installLockedSkillsForAgents(agentNames []string, cwd string) {
	targets := agentsNeedingSkillLinks(agentNames)
	if len(targets) == 0 {
		return
	}
	specs := collectSkillLinkSpecs(targets, cwd)
	if len(specs) == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("%sLinking skills from lock file...%s\n", ansiText, ansiReset)
	fmt.Println()

	succeeded := 0
	for _, spec := range specs {
		if !linkInstalledSkillToAgent(spec.skillName, spec.agentName, spec.global, cwd) {
			continue
		}
		succeeded++
		agentDisplay := spec.agentName
		if cfg := agent.AllAgents[spec.agentName]; cfg != nil {
			agentDisplay = cfg.DisplayName
		}
		fmt.Printf("  %s✓%s %-35s → %s\n", ansiGreen, ansiReset, spec.skillName, agentDisplay)
	}
	if succeeded > 0 {
		fmt.Println()
		ui.LogSuccess(fmt.Sprintf("Linked %d skill(s)", succeeded))
		fmt.Println()
	}
}

func printAgentsSaved(agents []string, scope string) {
	fmt.Printf("%s✓%s Configured %d agent(s) for %s scope:\n", ansiGreen, ansiReset, len(agents), scope)
	for _, name := range agents {
		cfg := agent.AllAgents[name]
		displayName := name
		if cfg != nil {
			displayName = cfg.DisplayName
		}
		fmt.Printf("  %s%s%s\n", ansiText, displayName, ansiReset)
	}
}
