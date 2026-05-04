package commands

import (
	"fmt"
	"os"
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

func buildAgentsListCmd() *cobra.Command {
	var global bool
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
			scope := "project"
			if global {
				scope = "global"
			}

			if len(args) == 0 {
				return pickAndSaveAgents(global, scope, cwd)
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
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Add to global configured agents")
	return cmd
}

// pickAndSaveAgents shows an interactive picker pre-seeded with the current
// configured list and replaces the entire list with the user's selection.
func pickAndSaveAgents(global bool, scope, cwd string) error {
	current := lock.GetConfiguredAgents(global, cwd)
	currentSet := map[string]bool{}
	for _, a := range current {
		currentSet[a] = true
	}

	var options []ui.UIOption
	for name, cfg := range agent.AllAgents {
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: name})
	}
	sort.Slice(options, func(i, j int) bool {
		return options[i].Label < options[j].Label
	})

	var initSel []int
	for i, opt := range options {
		if currentSet[opt.Value] {
			initSel = append(initSel, i)
		}
	}

	selected, ok := ui.UiSearchMultiselect("Which agents do you want to configure?", options, nil, initSel)
	if !ok {
		return nil
	}
	var newList []string
	for _, i := range selected {
		newList = append(newList, options[i].Value)
	}
	if len(newList) == 0 {
		fmt.Printf("%sNo agents selected.%s\n", ansiDim, ansiReset)
		return nil
	}
	sort.Strings(newList)
	if err := lock.SetConfiguredAgents(newList, global, cwd); err != nil {
		return fmt.Errorf("saving agents: %w", err)
	}
	printAgentsSaved(newList, scope)
	return nil
}

func buildAgentsRemoveCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:     "remove [agents...]",
		Aliases: []string{"rm", "r"},
		Short:   "Remove agents from your configured list",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
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
			} else {
				var options []ui.UIOption
				for _, name := range configured {
					cfg := agent.AllAgents[name]
					label := name
					if cfg != nil {
						label = cfg.DisplayName
					}
					options = append(options, ui.UIOption{Label: label, Value: name})
				}

				selected, ok := ui.UiSearchMultiselect("Which agents do you want to remove?", options, nil, nil)
				if !ok {
					return nil
				}
				for _, i := range selected {
					toRemove = append(toRemove, options[i].Value)
				}
				if len(toRemove) == 0 {
					fmt.Printf("%sNo agents selected.%s\n", ansiDim, ansiReset)
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
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Remove from global configured agents")
	return cmd
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
