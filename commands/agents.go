// mdm agents manages agent-definition files: single markdown or TOML files that give a
// harness a named subagent persona, distinct from the prompt libraries
// `mdm skills` installs. The harness concept `mdm agents` named before this
// release is now `mdm harnesses`.
package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/harness"
)

// allHarnessNames reports whether names is non-empty and every entry is a key
// of harness.AllHarnesses - the shape of a v1 `mdm agents add claude-code
// cursor`, which configured harnesses before this release.
func allHarnessNames(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, n := range names {
		if harness.AllHarnesses[n] == nil {
			return false
		}
	}
	return true
}

// printHarnessNamesHint explains that names are harnesses, which `mdm agents`
// no longer manages, and points at the `mdm harnesses` subcommand that does.
// verb is the agents subcommand the user ran, and the rest completes the
// sentence "<names> is a harness, not <noun>. ... `mdm agents <verb>
// <placeholder>` <does> agent definitions." A bare harness name is never a
// source and never a lock key, so without this the same v1 muscle memory
// failed as a git clone of a repository called "cursor", or as a removal that
// found nothing and exited 0.
func printHarnessNamesHint(names []string, verb, noun, placeholder, does string) {
	list := strings.Join(names, " ")
	are := "is a harness"
	if len(names) > 1 {
		are = "are harnesses"
	}
	fmt.Fprintf(os.Stderr, "%s%s %s, not %s.%s\n", ansiText, list, are, noun, ansiReset)
	fmt.Fprintf(os.Stderr, "Harness management moved to %smdm harnesses %s %s%s in this release; %smdm agents %s %s%s %s agent definitions.\n",
		ansiText, verb, list, ansiReset, ansiText, verb, placeholder, ansiReset, does)
}

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
