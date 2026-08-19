package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/plugin"
	"github.com/sethcarney/mdm/internal/source"
)

func buildPluginsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List installed Agent Plugins",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsList()
		},
	}
}

func runPluginsList() {
	cwd, _ := os.Getwd()
	lk := lock.ReadPluginsLock(cwd)
	if len(lk.Plugins) == 0 {
		fmt.Printf("\n%sNo plugins installed.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add one with %smdm plugins add <source>%s\n\n", ansiText, ansiReset)
		return
	}

	names := make([]string, 0, len(lk.Plugins))
	for name := range lk.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println()
	for _, name := range names {
		entry := lk.Plugins[name]
		installDir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
		status := ""
		if _, _, err := plugin.LoadManifest(installDir); err != nil {
			status = ansiRed + "  missing or invalid on disk" + ansiReset
		}
		summary := fmt.Sprintf("%d skill(s)", len(entry.Skills))
		if servers := countWiredServers(entry.MCP); servers > 0 {
			summary += fmt.Sprintf(", %d MCP server(s)", servers)
		}
		version := ""
		if entry.Version != "" {
			version = " v" + entry.Version
		}
		fmt.Printf("  %s%s%s%s  %s%s%s%s\n", ansiBold+ansiText, name, ansiReset, version, ansiDim, summary, ansiReset, status)
		fmt.Printf("      %s%s  spec v%s%s\n", ansiDim, source.FormatSourceInput(entry.Source, entry.Ref), entry.SpecVersion, ansiReset)
		fmt.Printf("      %s./%s%s\n", ansiDim, entry.InstallDir, ansiReset)
		if len(entry.SkillAgents) > 0 && len(entry.Skills) > 0 {
			fmt.Printf("      %sagents: %s%s\n", ansiDim, strings.Join(entry.SkillAgents, ", "), ansiReset)
		}
	}
	fmt.Println()
}
