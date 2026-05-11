package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

type UpdateOptions struct {
	Global           bool
	Project          bool
	Yes              bool
	AllowHiddenChars bool
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
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")

	return cmd
}

type updateStats struct{ updated, skipped int }

func resolveUpdateScope(opts UpdateOptions) (global, project bool, ok bool) {
	global = opts.Global
	project = opts.Project
	if !global && !project && !opts.Yes {
		idx, uiOk := ui.UiSelect("Update which scope?", []ui.UIOption{
			{Label: "Both", Hint: "project and global"},
			{Label: "Project"},
			{Label: "Global"},
		})
		if !uiOk {
			return false, false, false
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
		return global, project, true
	}
	if !global && !project {
		global = true
		project = true
	}
	return global, project, true
}

func isGitSourceType(st string) bool {
	return st == string(source.SourceTypeGitHub) ||
		st == string(source.SourceTypeGitLab) ||
		st == string(source.SourceTypeGit)
}

func updateGlobalSkills(skillFilter []string, stats *updateStats, allowHiddenChars bool) {
	l := lock.ReadSkillLock()
	for sName, entry := range l.Skills {
		if !matchesFilter(sName, entry.PluginName, skillFilter) {
			continue
		}
		if entry.SourceType == string(source.SourceTypeLocal) {
			stats.skipped++
			continue
		}
		if !isGitSourceType(entry.SourceType) {
			stats.skipped++
			continue
		}
		fmt.Printf("%sChecking %s...%s\n", ansiDim, sName, ansiReset)
		isUpToDate, newRef, err := checkSkillUpToDate(entry)
		if err != nil {
			ui.LogWarn(fmt.Sprintf("Skipping %s: %v", sName, err))
			stats.skipped++
			continue
		}
		if isUpToDate {
			ui.LogInfo(fmt.Sprintf("%s is up to date (%s)", sName, entry.Ref))
			stats.skipped++
			continue
		}
		src := entry.Source
		if newRef != "" {
			fmt.Printf("  %s→ upgrading %s → %s%s\n", ansiDim, entry.Ref, newRef, ansiReset)
			src = source.AppendFragmentRef(src, newRef, "")
		}
		runAdd(src, AddOptions{Global: true, Yes: true, Skills: []string{entry.PluginName}, AllowHiddenChars: allowHiddenChars})
		stats.updated++
	}
}

// checkRemoteTagUpToDate fetches all tags from gitURL and compares the current
// semver tag against the latest stable release. Returns (upToDate, latestTag, err).
func checkRemoteTagUpToDate(gitURL, currentRef string) (bool, string, error) {
	if !git.IsSemverTag(currentRef) {
		return false, "", fmt.Errorf("not pinned to a version tag; use `mdm skills add <source>#<tag>` to pin")
	}
	tags, err := git.FetchRemoteTags(gitURL)
	if err != nil {
		return false, "", err
	}
	latest := git.LatestSemverTag(tags)
	if latest == "" {
		return false, "", fmt.Errorf("no stable version tags found on remote")
	}
	if git.CompareSemverTags(latest, currentRef) > 0 {
		return false, latest, nil
	}
	return true, "", nil
}

func checkProjectSkillUpToDate(entry lock.LocalSkillLockEntry) (bool, string, error) {
	if !isGitSourceType(entry.SourceType) {
		return true, "", nil
	}
	parsed := source.ParseSource(entry.Source)
	return checkRemoteTagUpToDate(parsed.URL, entry.Ref)
}

func updateProjectSkills(skillFilter []string, cwd string, stats *updateStats, allowHiddenChars bool) {
	localLock := lock.ReadLocalLock(cwd)
	for sName, entry := range localLock.Skills {
		if !matchesFilter(sName, "", skillFilter) {
			continue
		}
		if entry.SourceType == string(source.SourceTypeLocal) {
			stats.skipped++
			continue
		}
		if !isGitSourceType(entry.SourceType) {
			stats.skipped++
			continue
		}
		fmt.Printf("%sChecking %s...%s\n", ansiDim, sName, ansiReset)
		isUpToDate, newRef, err := checkProjectSkillUpToDate(entry)
		if err != nil {
			ui.LogWarn(fmt.Sprintf("Skipping %s: %v", sName, err))
			stats.skipped++
			continue
		}
		if isUpToDate {
			ui.LogInfo(fmt.Sprintf("%s is up to date (%s)", sName, entry.Ref))
			stats.skipped++
			continue
		}
		src := entry.Source
		if newRef != "" {
			fmt.Printf("  %s→ upgrading %s → %s%s\n", ansiDim, entry.Ref, newRef, ansiReset)
			src = source.AppendFragmentRef(src, newRef, "")
		} else if entry.Ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + entry.Ref
		}
		runAdd(src, AddOptions{Project: true, Yes: true, Skills: []string{sName}, AllowHiddenChars: allowHiddenChars})
		stats.updated++
	}
}

func runUpdateWithOpts(skillFilter []string, opts UpdateOptions) {
	global, project, ok := resolveUpdateScope(opts)
	if !ok {
		return
	}
	var stats updateStats
	if global {
		updateGlobalSkills(skillFilter, &stats, opts.AllowHiddenChars)
	}
	if project {
		cwd, _ := os.Getwd()
		updateProjectSkills(skillFilter, cwd, &stats, opts.AllowHiddenChars)
	}
	fmt.Println()
	if stats.updated == 0 && stats.skipped == 0 {
		fmt.Printf("%sNo skills to update.%s\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sUpdate complete:%s %d updated, %d already up to date\n", ansiText, ansiReset, stats.updated, stats.skipped)
	fmt.Println()
}

func checkSkillUpToDate(entry lock.SkillLockEntry) (bool, string, error) {
	if !isGitSourceType(entry.SourceType) {
		return true, "", nil
	}
	parsed := source.ParseSource(entry.Source)
	return checkRemoteTagUpToDate(parsed.URL, entry.Ref)
}
