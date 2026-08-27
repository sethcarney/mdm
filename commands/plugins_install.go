package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
)

func buildPluginsInstallCmd() *cobra.Command {
	var allowHiddenChars bool
	var skipMCP bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore all plugins from " + lockName,
		Long: `Restore every plugin recorded in ` + lockName + `, re-fetching each
from its recorded source and ref. Intended for CI and onboarding, like
'mdm skills install'.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsInstall(allowHiddenChars, skipMCP)
		},
	}

	cmd.Flags().BoolVar(&allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	cmd.Flags().BoolVar(&skipMCP, "skip-mcp", false, "Do not wire MCP server config")
	return cmd
}

func runPluginsInstall(allowHiddenChars, skipMCP bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadPluginsLock(cwd)
	if len(lk.Plugins) == 0 {
		fmt.Printf("\n%sNo plugins found in %s.%s\n\n", ansiDim, lockName, ansiReset)
		fmt.Printf("Add plugins with %smdm plugins add <source>%s\n\n", ansiText, ansiReset)
		return
	}

	names := selectPluginLockEntries(lk, nil)
	fmt.Printf("\n%sRestoring %d plugin(s) from %s...%s\n", ansiText, len(names), lockName, ansiReset)
	for _, name := range names {
		reinstallPlugin(name, lk.Plugins[name], allowHiddenChars, skipMCP)
	}
	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
