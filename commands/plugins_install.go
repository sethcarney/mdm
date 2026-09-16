package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
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
			// A restore that installed less than the lock describes must not
			// look green to CI.
			if restoreFailed {
				os.Exit(1)
			}
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
	var unrestorable []string
	for _, name := range names {
		// Same reasoning as the knowledge restore: one missing local path must
		// not take every later plugin down with it.
		if why := unreachableLocalSource(lk.Plugins[name].Source, cwd); why != "" {
			ui.LogWarn(fmt.Sprintf("%s: %s", name, why))
			unrestorable = append(unrestorable, name)
			continue
		}
		reinstallPlugin(name, lk.Plugins[name], allowHiddenChars, skipMCP)
	}
	reportUnrestorable(unrestorable, "plugin",
		"Move it into the repository, or re-add it from a source your team can reach.")
	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
