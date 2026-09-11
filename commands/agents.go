// mdm agents manages agent-definition files: single markdown or TOML files that give a
// harness a named subagent persona, distinct from the prompt libraries
// `mdm skills` installs. The harness concept `mdm agents` named before this
// release is now `mdm harnesses`.
package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

func buildAgentArtifactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agent definitions for AI harnesses",
		Long: fmt.Sprintf(`Manage agent definitions - single markdown or TOML files that give a harness
a named subagent persona (e.g. Claude Code subagents), distinct from the
reusable prompt libraries %smdm skills%s installs.

%sExamples:%s
  mdm agents add owner/repo
  mdm agents add owner/repo --agent code-reviewer
  mdm agents list
  mdm agents remove code-reviewer`, ansiText, ansiReset, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(
		buildAgentAddCmd(),
		buildAgentListCmd(),
		buildAgentRemoveCmd(),
		buildAgentsUpdateCmd(),
		buildAgentsInstallCmd(),
	)

	return cmd
}

// AgentOptions holds the flags shared by the agent-definition subcommands.
type AgentOptions struct {
	Global           bool
	Project          bool
	Harnesses        []string // empty = prompt; "*" = all
	Agents           []string // empty = prompt; "*" = all
	Yes              bool
	AllowHiddenChars bool
	Copy             bool
	Symlink          bool
}

// asAddOptions adapts AgentOptions to the AddOptions fields
// promptScopeAndHarnesses and commitScopeInstallMode read. The mode flags
// belong here: the install mode is scope-wide, so an agent definition sets it
// for the scope exactly as a skill does.
func (o AgentOptions) asAddOptions() AddOptions {
	return AddOptions{
		Global:    o.Global,
		Project:   o.Project,
		Harnesses: o.Harnesses,
		Yes:       o.Yes,
		Copy:      o.Copy,
		Symlink:   o.Symlink,
	}
}
