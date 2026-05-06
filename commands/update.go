package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/git"
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

func updateGlobalSkills(skillFilter []string, stats *updateStats) {
	l := lock.ReadSkillLock()
	for sName, entry := range l.Skills {
		if !matchesFilter(sName, entry.PluginName, skillFilter) {
			continue
		}
		if !isGitSourceType(entry.SourceType) {
			stats.skipped++
			continue
		}
		fmt.Printf("%sChecking %s...%s\n", ansiDim, sName, ansiReset)
		isUpToDate, err := checkSkillUpToDate(sName, entry)
		if err != nil {
			ui.LogWarn(fmt.Sprintf("Could not check %s: %v", sName, err))
			stats.skipped++
			continue
		}
		if isUpToDate {
			ui.LogInfo(sName + " is up to date")
			stats.skipped++
			continue
		}
		runAdd(entry.Source, AddOptions{Global: true, Yes: true, Skills: []string{entry.PluginName}})
		stats.updated++
	}
}

func updateProjectSkills(skillFilter []string, cwd string, stats *updateStats) {
	localLock := lock.ReadLocalLock(cwd)
	for sName, entry := range localLock.Skills {
		if !matchesFilter(sName, "", skillFilter) {
			continue
		}
		if !isGitSourceType(entry.SourceType) {
			stats.skipped++
			continue
		}
		fmt.Printf("%sChecking %s...%s\n", ansiDim, sName, ansiReset)
		src := entry.Source
		if entry.Ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + entry.Ref
		}
		runAdd(src, AddOptions{Project: true, Yes: true, Skills: []string{sName}})
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
		updateGlobalSkills(skillFilter, &stats)
	}
	if project {
		cwd, _ := os.Getwd()
		updateProjectSkills(skillFilter, cwd, &stats)
	}
	fmt.Println()
	if stats.updated == 0 && stats.skipped == 0 {
		fmt.Printf("%sNo skills to update.%s\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sUpdate complete:%s %d updated, %d already up to date\n", ansiText, ansiReset, stats.updated, stats.skipped)
	fmt.Println()
}

func checkSkillUpToDate(skillName string, entry lock.SkillLockEntry) (bool, error) {
	parsed := source.ParseSource(entry.Source)

	// GitHub: prefer folder-level hash check (most precise — only changes when the skill
	// folder itself changes, not when unrelated files in the repo change).
	if entry.SourceType == string(source.SourceTypeGitHub) &&
		entry.SkillFolderHash != "" && entry.SkillPath != "" {
		ownerRepo := source.GetOwnerRepo(parsed)
		if ownerRepo != "" {
			token := lock.GetGitHubToken()
			ref := entry.Ref
			latestHash, err := blob.FetchSkillFolderHash(ownerRepo, entry.SkillPath, token, &ref)
			if err == nil {
				return latestHash == entry.SkillFolderHash, nil
			}
			// On GitHub API error fall through to commit SHA check below.
		}
	}

	// Universal fallback: compare remote commit SHA via git ls-remote.
	// Works for GitHub, GitLab, Bitbucket, and any self-hosted git server.
	if entry.CommitSHA != "" {
		currentSHA, err := git.FetchRemoteCommitSHA(parsed.URL, entry.Ref)
		if err != nil {
			return false, err
		}
		return currentSHA == entry.CommitSHA, nil
	}

	// No hash stored — treat as outdated so we always update once to populate the hash.
	return false, nil
}
