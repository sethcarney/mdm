package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildKnowledgeRemoveCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:     "remove [bundles...]",
		Short:   "Remove installed knowledge bundles",
		Aliases: []string{"rm", "r"},
		Long: fmt.Sprintf(`Remove knowledge bundles and their knowledge-lock.json entries.

If no bundle names are provided an interactive selection menu is shown.

%sExamples:%s
  mdm knowledge remove
  mdm knowledge remove sales -y`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeRemove(args, yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func selectKnowledgeToRemove(lk lock.KnowledgeLockFile, names []string, yes bool) ([]string, bool) {
	if len(names) > 0 {
		var keep []string
		for _, name := range names {
			if _, ok := lk.Bundles[sanitizeName(name)]; ok {
				keep = append(keep, sanitizeName(name))
			} else {
				ui.LogWarn(fmt.Sprintf("%s is not in knowledge-lock.json", name))
			}
		}
		return keep, len(keep) > 0
	}

	all := make([]string, 0, len(lk.Bundles))
	for name := range lk.Bundles {
		all = append(all, name)
	}
	sort.Strings(all)
	if yes || len(all) == 1 {
		return all, true
	}
	options := make([]ui.UIOption, len(all))
	for i, name := range all {
		options[i] = ui.UIOption{Label: name, Value: name, Hint: "./" + lk.Bundles[name].InstallDir}
	}
	indices, ok := ui.UiMultiselect("Which bundles would you like to remove?", options, true, nil, nil)
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

func runKnowledgeRemove(names []string, yes bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadKnowledgeLock(cwd)
	if len(lk.Bundles) == 0 {
		fmt.Printf("\n%sNo knowledge bundles installed.%s\n\n", ansiDim, ansiReset)
		return
	}

	selected, ok := selectKnowledgeToRemove(lk, names, yes)
	if !ok {
		return
	}

	fmt.Println()
	for _, name := range selected {
		entry := lk.Bundles[name]
		installDir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
		if !isPathSafe(cwd, installDir) || installDir == filepath.Clean(cwd) {
			ui.LogError(fmt.Sprintf("%s: refusing to remove %s", name, entry.InstallDir))
			continue
		}
		if err := os.RemoveAll(installDir); err != nil {
			ui.LogError(fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := lock.RemoveBundleFromKnowledgeLock(name, cwd); err != nil {
			ui.LogWarn(fmt.Sprintf("could not update knowledge-lock.json: %v", err))
		}
		ui.LogSuccess(name)
	}
	fmt.Println()
}
