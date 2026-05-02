package commands

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
)

// agentFlagCompletion provides shell completion for --agent flags.
func agentFlagCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	names := make([]string, 0, len(agent.AllAgents))
	for name, cfg := range agent.AllAgents {
		if cfg != nil {
			names = append(names, name+"\t"+cfg.DisplayName)
		}
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func buildSkillsCmd(ver string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills for AI agents",
		Long: fmt.Sprintf(`Manage skills — reusable markdown-based prompt libraries for AI agents.

%sExamples:%s
  mdm skills add vercel-labs/agent-skills
  mdm skills find typescript
  mdm skills list
  mdm skills remove my-skill
  mdm skills update
  mdm skills init my-skill`, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(
		buildAddCmd(ver),
		buildRemoveCmd(),
		buildListCmd(),
		buildFindCmd(),
		buildUpdateCmd(),
		buildInitCmd(ver),
		buildInstallFromLockCmd(ver),
		buildSyncCmd(ver),
	)

	return cmd
}
