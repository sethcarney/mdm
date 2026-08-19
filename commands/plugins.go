package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/plugin"
)

// pluginsSpecVersion is the Agent Plugins spec revision this build
// implements. See docs/specs/plugins.md and the upstream spec:
// https://agent-plugins.org
const pluginsSpecVersion = plugin.SpecVersion

func buildPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugins",
		Short: "Manage Agent Plugins",
		Long: fmt.Sprintf(`Manage Agent Plugins — portable packages of skills and MCP servers
following the vendor-neutral agent-plugins.org standard.

Tracks Agent Plugins spec v%s.

%sExamples:%s
  mdm plugins init my-plugin
  mdm plugins validate ./my-plugin`, pluginsSpecVersion, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}
	cmd.AddCommand(
		buildPluginsAddCmd(),
		buildPluginsListCmd(),
		buildPluginsRemoveCmd(),
		buildPluginsUpdateCmd(),
		buildPluginsValidateCmd(),
		buildPluginsInitCmd(),
		buildPluginsInstallCmd(),
	)
	return cmd
}
