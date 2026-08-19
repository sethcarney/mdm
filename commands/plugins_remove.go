package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildPluginsRemoveCmd() *cobra.Command {
	var yes, purgeData bool

	cmd := &cobra.Command{
		Use:     "remove [plugins...]",
		Short:   "Remove installed Agent Plugins",
		Aliases: []string{"rm", "r"},
		Long: fmt.Sprintf(`Remove plugins, their skill links, their MCP config entries, and their
plugins-lock.json entries.

The plugin's persistent data directory is preserved unless --purge-data
is given.

If no plugin names are provided an interactive selection menu is shown.

%sExamples:%s
  mdm plugins remove
  mdm plugins remove toolkit -y
  mdm plugins remove toolkit --purge-data`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsRemove(args, yes, purgeData)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&purgeData, "purge-data", false, "Also delete the plugin's persistent data directory")
	return cmd
}

func selectPluginsToRemove(lk lock.PluginLockFile, names []string, yes bool) ([]string, bool) {
	if len(names) > 0 {
		var keep []string
		for _, name := range names {
			if _, ok := lk.Plugins[name]; ok {
				keep = append(keep, name)
			} else {
				ui.LogWarn(fmt.Sprintf("%s is not in plugins-lock.json", name))
			}
		}
		return keep, len(keep) > 0
	}

	all := make([]string, 0, len(lk.Plugins))
	for name := range lk.Plugins {
		all = append(all, name)
	}
	sort.Strings(all)
	if yes || len(all) == 1 {
		return all, true
	}
	options := make([]ui.UIOption, len(all))
	for i, name := range all {
		options[i] = ui.UIOption{Label: name, Value: name, Hint: "./" + lk.Plugins[name].InstallDir}
	}
	indices, ok := ui.UiMultiselect("Which plugins would you like to remove?", options, true, nil, nil)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []string
	for _, i := range indices {
		selected = append(selected, all[i])
	}
	return selected, true
}

func runPluginsRemove(names []string, yes, purgeData bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadPluginsLock(cwd)
	if len(lk.Plugins) == 0 {
		fmt.Printf("\n%sNo plugins installed.%s\n\n", ansiDim, ansiReset)
		return
	}

	selected, ok := selectPluginsToRemove(lk, names, yes)
	if !ok {
		return
	}

	fmt.Println()
	for _, name := range selected {
		if removeInstalledPlugin(name, lk.Plugins[name], purgeData, cwd) {
			ui.LogSuccess(name)
		}
	}
	fmt.Println()
}

// removeInstalledPlugin unwinds one plugin in reverse install order: MCP
// config entries, skill links, the install directory, then the lock entry.
func removeInstalledPlugin(name string, entry lock.PluginLockEntry, purgeData bool, cwd string) bool {
	installDir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
	if !isPathSafe(cwd, installDir) || installDir == filepath.Clean(cwd) {
		ui.LogError(fmt.Sprintf("%s: refusing to remove %s", name, entry.InstallDir))
		return false
	}

	unwirePluginMCP(name, entry, cwd)
	removePluginSkillLinks(name, entry, cwd)

	if err := os.RemoveAll(installDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", name, err))
		return false
	}
	if purgeData && entry.DataDir != "" {
		dataDir := filepath.Join(cwd, filepath.FromSlash(entry.DataDir))
		if isPathSafe(pluginsDataBaseDir(cwd), dataDir) && dataDir != filepath.Clean(pluginsDataBaseDir(cwd)) {
			_ = os.RemoveAll(dataDir)
		}
	}
	if err := lock.RemovePluginFromLock(name, cwd); err != nil {
		ui.LogWarn(fmt.Sprintf("could not update plugins-lock.json: %v", err))
	}
	return true
}

// removePluginSkillLinks deletes the canonical and per-agent links for
// every skill the lock records for this plugin, but only when the link (or
// its copy fallback) is actually owned by it: another plugin's symlink or
// a skills-lock-tracked standalone install that took over the name is
// left alone.
func removePluginSkillLinks(pluginName string, entry lock.PluginLockEntry, cwd string) {
	canonicalBase := getCanonicalSkillsDir(false, cwd)
	localSkills := lock.ReadLocalLock(cwd).Skills
	for _, skillName := range entry.Skills {
		canonicalDir := filepath.Join(canonicalBase, skillName)
		if !isPathSafe(canonicalBase, canonicalDir) {
			continue
		}
		owner, standalone := skillDirOwner(canonicalDir, cwd)
		if owner != "" && owner != pluginName {
			continue
		}
		if standalone {
			if _, tracked := localSkills[skillName]; tracked {
				continue
			}
		}
		for _, agentName := range entry.SkillAgents {
			if agent.UsesSharedSkillsDir(agentName) {
				continue
			}
			agentBase := getAgentBaseDir(agentName, false, cwd)
			if agentBase != "" {
				removeAgentSkillDir(agentBase, skillName, "")
			}
		}
		removeCanonicalSkillDir(canonicalDir)
	}
}

// removeCanonicalSkillDir removes a canonical skill entry, whether it is
// the plugin symlink or a copy-mode fallback directory.
func removeCanonicalSkillDir(canonicalDir string) {
	info, err := os.Lstat(canonicalDir)
	if err != nil {
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(canonicalDir)
		return
	}
	_ = os.RemoveAll(canonicalDir)
}
