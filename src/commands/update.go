package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

type UpdateOptions struct {
	Global  bool
	Project bool
	Yes     bool
}

func buildUpdateCmd() *cobra.Command {
	var opts UpdateOptions

	cmd := &cobra.Command{
		Use:     "update [skills...]",
		Short:   "Update installed skills",
		Aliases: []string{"check"},
		Long: fmt.Sprintf(`Update installed skills to their latest versions.

%sExamples:%s
  mdm skills update
  mdm skills update my-skill
  mdm skills update -g`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runUpdateWithOpts(args, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Update global skills only")
	f.BoolVarP(&opts.Project, "project", "p", false, "Update project skills only")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip scope prompt")

	return cmd
}

func runUpdateWithOpts(skillFilter []string, opts UpdateOptions) {
	// Determine scope
	global := opts.Global
	project := opts.Project
	if !global && !project && !opts.Yes {
		idx, ok := ui.UiSelect("Update which scope?", []ui.UIOption{
			{Label: "Both", Hint: "project and global"},
			{Label: "Project"},
			{Label: "Global"},
		})
		if !ok {
			return
		}
		switch idx {
		case 0:
			global = true
			project = true
		case 1:
			project = true
		case 2:
			global = true
		}
	} else if !global && !project {
		global = true
		project = true
	}

	updated := 0
	skipped := 0
	failed := 0

	// Check global skills from lock
	if global {
		l := lock.ReadSkillLock()
		for sName, entry := range l.Skills {
			if len(skillFilter) > 0 {
				found := false
				for _, f := range skillFilter {
					if strings.EqualFold(sName, f) || strings.EqualFold(f, entry.PluginName) {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			if entry.SourceType != string(source.SourceTypeGitHub) && entry.SourceType != string(source.SourceTypeGitLab) && entry.SourceType != string(source.SourceTypeGit) {
				skipped++
				continue
			}

			fmt.Printf("%sChecking %s...%s\n", ansiDim, sName, ansiReset)
			isUpToDate, err := checkSkillUpToDate(sName, entry)
			if err != nil {
				ui.LogWarn(fmt.Sprintf("Could not check %s: %v", sName, err))
				skipped++
				continue
			}
			if isUpToDate {
				ui.LogInfo(sName + " is up to date")
				skipped++
				continue
			}

			// Re-run add to update
			addOpts := AddOptions{
				Global: true,
				Yes:    true,
				Skills: []string{entry.PluginName},
			}
			runAdd(entry.Source, addOpts)
			updated++
		}
	}

	// Check project skills from local lock
	if project {
		cwd, _ := os.Getwd()
		localLock := lock.ReadLocalLock(cwd)
		for sName, entry := range localLock.Skills {
			if len(skillFilter) > 0 {
				found := false
				for _, f := range skillFilter {
					if strings.EqualFold(sName, f) {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			if entry.SourceType != string(source.SourceTypeGitHub) && entry.SourceType != string(source.SourceTypeGitLab) && entry.SourceType != string(source.SourceTypeGit) {
				skipped++
				continue
			}

			fmt.Printf("%sChecking %s...%s\n", ansiDim, sName, ansiReset)

			addOpts := AddOptions{
				Project: true,
				Yes:     true,
				Skills:  []string{sName},
			}
			src := entry.Source
			if entry.Ref != "" && !strings.Contains(src, "#") {
				src = src + "#" + entry.Ref
			}
			runAdd(src, addOpts)
			updated++
		}
	}

	fmt.Println()
	if updated == 0 && skipped == 0 && failed == 0 {
		fmt.Printf("%sNo skills to update.%s\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sUpdate complete:%s %d updated, %d already up to date", ansiText, ansiReset, updated, skipped)
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	fmt.Println()
	fmt.Println()
}

func checkSkillUpToDate(skillName string, entry lock.SkillLockEntry) (bool, error) {
	if entry.SkillFolderHash == "" || entry.SkillPath == "" {
		return false, nil
	}
	ownerRepo := ""
	parsed := source.ParseSource(entry.Source)
	ownerRepo = source.GetOwnerRepo(parsed)
	if ownerRepo == "" {
		return false, nil
	}

	token := lock.GetGitHubToken()
	ref := entry.Ref
	latestHash, err := blob.FetchSkillFolderHash(ownerRepo, entry.SkillPath, token, &ref)
	if err != nil {
		return false, err
	}
	return latestHash == entry.SkillFolderHash, nil
}
