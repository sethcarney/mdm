package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
)

func buildKnowledgeInstallCmd() *cobra.Command {
	var allowHiddenChars bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore all bundles from knowledge-lock.json",
		Long: `Restore every knowledge bundle recorded in knowledge-lock.json,
re-fetching each from its recorded source and ref. Intended for CI and
onboarding, like 'mdm skills install'.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeInstall(allowHiddenChars)
		},
	}

	cmd.Flags().BoolVar(&allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	return cmd
}

func runKnowledgeInstall(allowHiddenChars bool) {
	cwd, _ := os.Getwd()
	lk := lock.ReadKnowledgeLock(cwd)
	if len(lk.Bundles) == 0 {
		fmt.Printf("\n%sNo knowledge-lock.json found.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add bundles with %smdm knowledge add <source>%s\n\n", ansiText, ansiReset)
		return
	}

	names := selectKnowledgeLockEntries(lk, nil)
	fmt.Printf("\n%sRestoring %d bundle(s) from knowledge-lock.json...%s\n", ansiText, len(names), ansiReset)
	for _, name := range names {
		reinstallKnowledgeBundle(name, lk.Bundles[name], allowHiddenChars)
	}
	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
