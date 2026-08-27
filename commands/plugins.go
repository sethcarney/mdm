package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/experimental"
	"github.com/sethcarney/mdm/internal/plugin"
)

// pluginsSpecVersion is the Agent Plugins spec revision this build
// implements. See docs/specs/plugins.md and the upstream spec:
// https://agent-plugins.org
const pluginsSpecVersion = plugin.SpecVersion

func printPluginsBanner() {
	fmt.Fprintf(os.Stderr, "%s⚠ experimental:%s %sAgent Plugins support tracks spec v%s - commands and file formats may change%s\n",
		ansiYellow, ansiReset, ansiDim, pluginsSpecVersion, ansiReset)
}

func buildPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "plugins",
		Short:  "Manage Agent Plugins [experimental]",
		Hidden: !experimental.Enabled(experimental.Plugins),
		Long: fmt.Sprintf(`Manage Agent Plugins - portable packages of skills and MCP servers
following the vendor-neutral agent-plugins.org standard.

This command group is experimental: it tracks Agent Plugins spec v%s
and may change or be removed in any release.

%sExamples:%s
  mdm plugins init my-plugin
  mdm plugins validate ./my-plugin`, pluginsSpecVersion, ansiBold, ansiReset),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !experimental.Enabled(experimental.Plugins) {
				return fmt.Errorf("plugins is experimental - enable it with 'mdm experimental enable plugins' or %s=plugins", experimental.EnvVar)
			}
			printPluginsBanner()
			return nil
		},
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
