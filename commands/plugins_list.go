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
	var jsonMode bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List installed Agent Plugins",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsList(jsonMode)
		},
	}

	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")

	return cmd
}

// pluginListItem is one element of `mdm plugins list --json`. The field names
// are a public contract: a rename keeps the old key.
type pluginListItem struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Source      string   `json:"source"`
	Ref         string   `json:"ref,omitempty"`
	SpecVersion string   `json:"specVersion"`
	InstallDir  string   `json:"installDir"`
	Skills      []string `json:"skills"`
	// Harnesses is the lock's SkillAgents: the harnesses the plugin's skills
	// were installed for. The text output has always labelled it "harnesses".
	Harnesses  []string `json:"harnesses"`
	MCPServers int      `json:"mcpServers"`
	Valid      bool     `json:"valid"`
}

func runPluginsList(jsonMode bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadPluginsLock(cwd)

	if len(lk.Plugins) == 0 && !jsonMode {
		fmt.Printf("\n%sNo plugins installed.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add one with %smdm plugins add <source>%s\n\n", ansiText, ansiReset)
		return
	}

	names := make([]string, 0, len(lk.Plugins))
	for name := range lk.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	if jsonMode {
		items := make([]pluginListItem, 0, len(names))
		for _, name := range names {
			entry := lk.Plugins[name]
			installDir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
			_, _, err := plugin.LoadManifest(installDir)
			items = append(items, pluginListItem{
				Name:        name,
				Version:     entry.Version,
				Source:      entry.Source,
				Ref:         entry.Ref,
				SpecVersion: entry.SpecVersion,
				InstallDir:  entry.InstallDir,
				Skills:      emptyIfNil(entry.Skills),
				Harnesses:   emptyIfNil(entry.SkillAgents),
				MCPServers:  countWiredServers(entry.MCP),
				Valid:       err == nil,
			})
		}
		printJSON(items)
		return
	}

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
			fmt.Printf("      %sharnesses: %s%s\n", ansiDim, strings.Join(entry.SkillAgents, ", "), ansiReset)
		}
	}
	fmt.Println()
}
