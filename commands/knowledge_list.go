package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/okf"
	"github.com/sethcarney/mdm/internal/source"
)

func buildKnowledgeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List installed knowledge bundles",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeList()
		},
	}
}

func runKnowledgeList() {
	cwd, _ := os.Getwd()
	lk := lock.ReadKnowledgeLock(cwd)
	if len(lk.Bundles) == 0 {
		fmt.Printf("\n%sNo knowledge bundles installed.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add one with %smdm knowledge add <source>%s\n\n", ansiText, ansiReset)
		return
	}

	names := make([]string, 0, len(lk.Bundles))
	for name := range lk.Bundles {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println()
	for _, name := range names {
		entry := lk.Bundles[name]
		installDir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
		status := ""
		docs := 0
		if b, err := okf.LoadBundle(installDir); err == nil {
			docs = len(b.Docs)
		} else {
			status = ansiRed + "  missing on disk" + ansiReset
		}
		fmt.Printf("  %s%s%s  %s%d document(s)%s%s\n", ansiBold+ansiText, name, ansiReset, ansiDim, docs, ansiReset, status)
		fmt.Printf("      %s%s  spec v%s%s\n", ansiDim, source.FormatSourceInput(entry.Source, entry.Ref), entry.SpecVersion, ansiReset)
		fmt.Printf("      %s./%s%s\n", ansiDim, entry.InstallDir, ansiReset)
	}
	fmt.Println()
}
