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
	var jsonMode bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List installed knowledge bundles",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeList(jsonMode)
		},
	}

	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")

	return cmd
}

// knowledgeListItem is one element of `mdm knowledge list --json`. The field
// names are a public contract: a rename keeps the old key.
type knowledgeListItem struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Ref         string `json:"ref,omitempty"`
	SpecVersion string `json:"specVersion"`
	InstallDir  string `json:"installDir"`
	Documents   int    `json:"documents"`
	// Present is whether the bundle loaded off the disk; the text output
	// prints "missing on disk" when it did not.
	Present bool `json:"present"`
}

func runKnowledgeList(jsonMode bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadKnowledgeLock(cwd)

	if len(lk.Bundles) == 0 && !jsonMode {
		fmt.Printf("\n%sNo knowledge bundles installed.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add one with %smdm knowledge add <source>%s\n\n", ansiText, ansiReset)
		return
	}

	names := make([]string, 0, len(lk.Bundles))
	for name := range lk.Bundles {
		names = append(names, name)
	}
	sort.Strings(names)

	if jsonMode {
		items := make([]knowledgeListItem, 0, len(names))
		for _, name := range names {
			entry := lk.Bundles[name]
			docs, present := knowledgeBundleDocs(cwd, entry.InstallDir)
			items = append(items, knowledgeListItem{
				Name:        name,
				Source:      entry.Source,
				Ref:         entry.Ref,
				SpecVersion: entry.SpecVersion,
				InstallDir:  entry.InstallDir,
				Documents:   docs,
				Present:     present,
			})
		}
		printJSON(items)
		return
	}

	fmt.Println()
	for _, name := range names {
		entry := lk.Bundles[name]
		docs, present := knowledgeBundleDocs(cwd, entry.InstallDir)
		status := ""
		if !present {
			status = ansiRed + "  missing on disk" + ansiReset
		}
		fmt.Printf("  %s%s%s  %s%d document(s)%s%s\n", ansiBold+ansiText, name, ansiReset, ansiDim, docs, ansiReset, status)
		fmt.Printf("      %s%s  spec v%s%s\n", ansiDim, source.FormatSourceInput(entry.Source, entry.Ref), entry.SpecVersion, ansiReset)
		fmt.Printf("      %s./%s%s\n", ansiDim, entry.InstallDir, ansiReset)
	}
	fmt.Println()
}

// knowledgeBundleDocs loads the bundle at the lock's install dir and returns
// its document count and whether it loaded at all.
func knowledgeBundleDocs(cwd, installDir string) (int, bool) {
	b, err := okf.LoadBundle(filepath.Join(cwd, filepath.FromSlash(installDir)))
	if err != nil {
		return 0, false
	}
	return len(b.Docs), true
}
