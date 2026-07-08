package commands

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
)

func buildKnowledgeUpdateCmd() *cobra.Command {
	var allowHiddenChars bool

	cmd := &cobra.Command{
		Use:   "update [bundles...]",
		Short: "Re-fetch installed bundles from their recorded source and ref",
		Long: fmt.Sprintf(`Re-fetch knowledge bundles from the source and ref recorded in
knowledge-lock.json, re-running the hidden-character scan and
conformance validation.

%sExamples:%s
  mdm knowledge update
  mdm knowledge update sales`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeUpdate(args, allowHiddenChars)
		},
	}

	cmd.Flags().BoolVar(&allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	return cmd
}

// selectKnowledgeLockEntries returns the sorted lock entry names matching the
// given filters (all entries when no filter is given).
func selectKnowledgeLockEntries(lk lock.KnowledgeLockFile, filters []string) []string {
	var names []string
	for name := range lk.Bundles {
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

// reinstallKnowledgeBundle re-fetches one lock entry and reinstalls it into
// its recorded directory, refreshing the lock entry's hash and UpdatedAt.
func reinstallKnowledgeBundle(name string, entry lock.KnowledgeLockEntry, allowHiddenChars bool) {
	src := entry.Source
	if entry.Ref != "" && entry.SourceType != string(source.SourceTypeLocal) && !strings.Contains(src, "#") {
		src += "#" + entry.Ref
	}
	runKnowledgeAdd(src, KnowledgeAddOptions{
		Dir:              filepath.FromSlash(path.Dir(entry.InstallDir)),
		Bundles:          []string{name},
		Yes:              true,
		AllowHiddenChars: allowHiddenChars,
	})
}

func runKnowledgeUpdate(filters []string, allowHiddenChars bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadKnowledgeLock(cwd)
	if len(lk.Bundles) == 0 {
		fmt.Printf("\n%sNo knowledge bundles installed.%s\n\n", ansiDim, ansiReset)
		return
	}

	names := selectKnowledgeLockEntries(lk, filters)
	if len(names) == 0 {
		fmt.Printf("\n%sNo matching bundles found in knowledge-lock.json.%s\n\n", ansiDim, ansiReset)
		return
	}

	for _, name := range names {
		entry := lk.Bundles[name]
		fmt.Printf("%sUpdating %s from %s...%s\n", ansiDim, name, source.FormatSourceInput(entry.Source, entry.Ref), ansiReset)
		reinstallKnowledgeBundle(name, entry, allowHiddenChars)
	}
	fmt.Printf("%sUpdate complete:%s %d bundle(s) refreshed\n\n", ansiText, ansiReset, len(names))
}
