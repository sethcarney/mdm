package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
)

// experimental_install: restore skills from skills-lock.json

func buildInstallFromLockCmd(ver string) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore skills from skills-lock.json",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			runInstallFromLock(yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runInstallFromLock(yes bool) {
	cwd, _ := os.Getwd()
	l := lock.ReadLocalLock(cwd)

	if len(l.Skills) == 0 {
		fmt.Printf("\n%sNo skills in skills-lock.json.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add skills with %smdm skills add <package>%s\n\n", ansiText, ansiReset)
		return
	}

	fmt.Printf("\n%sRestoring %d skill(s) from skills-lock.json...%s\n\n", ansiText, len(l.Skills), ansiReset)

	// Group by source to batch installations
	type sourceGroup struct {
		source     string
		sourceType string
		ref        string
		skills     []string
	}
	sourceMap := map[string]*sourceGroup{}
	for sName, entry := range l.Skills {
		key := entry.Source + "|" + entry.Ref
		if g, ok := sourceMap[key]; ok {
			g.skills = append(g.skills, sName)
		} else {
			sourceMap[key] = &sourceGroup{
				source:     entry.Source,
				sourceType: entry.SourceType,
				ref:        entry.Ref,
				skills:     []string{sName},
			}
		}
	}

	for _, group := range sourceMap {
		fmt.Printf("%sInstalling from %s...%s\n", ansiDim, group.source, ansiReset)

		opts := AddOptions{
			Project: true,
			Yes:     yes,
			Skills:  group.skills,
		}

		src := group.source
		if group.ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + group.ref
		}
		runAdd(src, opts)
	}

	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}
