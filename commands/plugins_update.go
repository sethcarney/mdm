package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
)

func buildPluginsUpdateCmd() *cobra.Command {
	var allowHiddenChars bool
	var skipMCP bool

	cmd := &cobra.Command{
		Use:   "update [plugins...]",
		Short: "Re-fetch installed plugins from their recorded source and ref",
		Long: fmt.Sprintf(`Re-fetch plugins from the source and ref recorded in %s,
re-running the hidden-character scan, re-linking skills, and re-wiring
MCP config. The plugin's persistent data directory is preserved.

%sExamples:%s
  mdm plugins update
  mdm plugins update toolkit`, lockName, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsUpdate(args, allowHiddenChars, skipMCP)
		},
	}

	cmd.Flags().BoolVar(&allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	cmd.Flags().BoolVar(&skipMCP, "skip-mcp", false, "Do not re-wire MCP server config")
	return cmd
}

// selectPluginLockEntries returns the sorted lock entry names matching the
// given filters (all entries when no filter is given).
func selectPluginLockEntries(lk lock.PluginLockFile, filters []string) []string {
	var names []string
	for name := range lk.Plugins {
		if len(filters) == 0 {
			names = append(names, name)
			continue
		}
		for _, f := range filters {
			if skillNameMatches(name, f) {
				names = append(names, name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

// reinstallPlugin re-fetches one lock entry and reinstalls it, refreshing
// links, MCP wiring, and the lock. The data directory is never cleaned.
func reinstallPlugin(name string, entry lock.PluginLockEntry, allowHiddenChars, skipMCP bool) {
	src := entry.Source
	if entry.Ref != "" && entry.SourceType != string(source.SourceTypeLocal) && !strings.Contains(src, "#") {
		src += "#" + entry.Ref
	}
	runPluginsAdd(src, PluginsAddOptions{
		Plugins:          []string{name},
		Agents:           entry.SkillAgents,
		Yes:              true,
		AllowHiddenChars: allowHiddenChars,
		SkipMCP:          skipMCP,
	})
}

func runPluginsUpdate(filters []string, allowHiddenChars, skipMCP bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadPluginsLock(cwd)
	if len(lk.Plugins) == 0 {
		fmt.Printf("\n%sNo plugins installed.%s\n\n", ansiDim, ansiReset)
		return
	}

	names := selectPluginLockEntries(lk, filters)
	if len(names) == 0 {
		fmt.Printf("\n%sNo matching plugins found in %s.%s\n\n", ansiDim, lockName, ansiReset)
		return
	}

	for _, name := range names {
		entry := lk.Plugins[name]
		fmt.Printf("%sUpdating %s from %s...%s\n", ansiDim, name, source.FormatSourceInput(entry.Source, entry.Ref), ansiReset)
		reinstallPlugin(name, entry, allowHiddenChars, skipMCP)
	}
	fmt.Printf("%sUpdate complete:%s %d plugin(s) refreshed\n\n", ansiText, ansiReset, len(names))
}
