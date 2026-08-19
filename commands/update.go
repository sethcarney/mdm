package commands

import (
	"fmt"
	"os"
	"sort"

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

Skills that share a source repository and target ref are re-fetched together,
so a repo holding many skills is cloned once per update run rather than once
per skill.

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

// ── Remote tag cache ───────────────────────────────────────────────────────────

// remoteTagCache memoizes `git ls-remote --tags` per repository URL for the
// lifetime of a single update run. Without it, updating N skills that all live
// in one repo means N identical round trips to the remote — and for a private
// repo, N credential handshakes.
type remoteTagCache struct {
	entries map[string]remoteTagResult
}

type remoteTagResult struct {
	tags []string
	err  error
}

func newRemoteTagCache() *remoteTagCache {
	return &remoteTagCache{entries: map[string]remoteTagResult{}}
}

// fetch returns the tag list for gitURL, hitting the remote only on first use.
// Failures are cached too: a repo that is unreachable now will not become
// reachable within the same run, and re-trying it once per skill is what makes
// a broken remote feel like a hang.
func (c *remoteTagCache) fetch(gitURL string) ([]string, error) {
	if c == nil {
		return git.FetchRemoteTags(gitURL)
	}
	if r, ok := c.entries[gitURL]; ok {
		vlog(verboseFlag, "reusing cached tag list for %s (%d tag(s))", gitURL, len(r.tags))
		return r.tags, r.err
	}
	vlog(verboseFlag, "fetching remote tags from %s", gitURL)
	tags, err := git.FetchRemoteTags(gitURL)
	c.entries[gitURL] = remoteTagResult{tags: tags, err: err}
	return tags, err
}

// checkRemoteTagUpToDate compares the current semver tag against the latest
// stable release on the remote. Returns (upToDate, latestTag, err).
func checkRemoteTagUpToDate(gitURL, currentRef string, tags *remoteTagCache) (bool, string, error) {
	if !git.IsSemverTag(currentRef) {
		return false, "", fmt.Errorf("not pinned to a version tag; use `mdm skills add <source>#<tag>` to pin")
	}
	allTags, err := tags.fetch(gitURL)
	if err != nil {
		return false, "", err
	}
	latest := git.LatestSemverTag(allTags)
	vlog(verboseFlag, "remote has %d tag(s); latest stable=%q (current=%s)", len(allTags), latest, currentRef)
	if latest == "" {
		return false, "", fmt.Errorf("no stable version tags found on remote")
	}
	if git.CompareSemverTags(latest, currentRef) > 0 {
		return false, latest, nil
	}
	return true, "", nil
}

// ── Candidates and grouping ────────────────────────────────────────────────────

// updateCandidate is a lock entry normalized across the global and project lock
// formats so both scopes share a single update path.
type updateCandidate struct {
	lockName   string // key in the lock file; used for user-facing messages
	filterName string // name handed to `mdm skills add --skill`
	source     string
	sourceType string
	ref        string
}

// updateGroup is the set of skills that resolve to the same source at the same
// target ref. Every group costs exactly one fetch.
type updateGroup struct {
	source string   // source input with the target ref applied
	skills []string // filterName values, deduplicated
	names  []string // lockName values, for messages
	seen   map[string]bool
}

// add records a candidate in the group. Two lock entries can share an
// installable name (the lock key and the recorded plugin name need not agree),
// and passing the same name twice to `--skill` would inflate the update count
// without installing anything extra.
func (g *updateGroup) add(c updateCandidate) {
	g.names = append(g.names, c.lockName)
	if g.seen[c.filterName] {
		return
	}
	g.seen[c.filterName] = true
	g.skills = append(g.skills, c.filterName)
}

func collectGlobalCandidates(skillFilter []string) []updateCandidate {
	l := lock.ReadSkillLock()
	names := make([]string, 0, len(l.Skills))
	for name := range l.Skills {
		names = append(names, name)
	}
	sort.Strings(names)

	var candidates []updateCandidate
	for _, name := range names {
		entry := l.Skills[name]
		if !matchesFilter(name, entry.PluginName, skillFilter) {
			continue
		}
		filterName := entry.PluginName
		if filterName == "" {
			filterName = name
		}
		candidates = append(candidates, updateCandidate{
			lockName:   name,
			filterName: filterName,
			source:     entry.Source,
			sourceType: entry.SourceType,
			ref:        entry.Ref,
		})
	}
	return candidates
}

func collectProjectCandidates(skillFilter []string, cwd string) []updateCandidate {
	l := lock.ReadLocalLock(cwd)
	names := make([]string, 0, len(l.Skills))
	for name := range l.Skills {
		names = append(names, name)
	}
	sort.Strings(names)

	var candidates []updateCandidate
	for _, name := range names {
		entry := l.Skills[name]
		if !matchesFilter(name, "", skillFilter) {
			continue
		}
		candidates = append(candidates, updateCandidate{
			lockName:   name,
			filterName: name,
			source:     entry.Source,
			sourceType: entry.SourceType,
			ref:        entry.Ref,
		})
	}
	return candidates
}

// checkCandidateUpToDate resolves whether a candidate already sits on the latest
// stable tag, reusing tags already fetched for the same repository.
func checkCandidateUpToDate(c updateCandidate, tags *remoteTagCache) (bool, string, error) {
	if !isGitSourceType(c.sourceType) {
		return true, "", nil
	}
	parsed := source.ParseSource(c.source)
	return checkRemoteTagUpToDate(parsed.URL, c.ref, tags)
}

// upToDateCheck reports whether a candidate is already current, and if not, the
// tag it should move to. It is a parameter of planUpdates so the grouping logic
// can be exercised without touching a remote.
type upToDateCheck func(updateCandidate) (upToDate bool, newRef string, err error)

// planUpdates decides which candidates need re-fetching and buckets them by the
// source they will be fetched from. Skills sharing a repo and target ref land in
// one group, which is what collapses 28 clones of the same repo into one.
func planUpdates(candidates []updateCandidate, check upToDateCheck, stats *updateStats) []updateGroup {
	groups := map[string]*updateGroup{}
	var order []string

	for _, c := range candidates {
		if c.sourceType == string(source.SourceTypeLocal) {
			vlog(verboseFlag, "skip %s: local source is not remotely updatable", c.lockName)
			stats.skipped++
			continue
		}
		if !isGitSourceType(c.sourceType) {
			vlog(verboseFlag, "skip %s: source type %q is not updatable", c.lockName, c.sourceType)
			stats.skipped++
			continue
		}

		fmt.Printf("%sChecking %s...%s\n", ansiDim, c.lockName, ansiReset)
		vlog(verboseFlag, "checking %s: ref=%q source=%q", c.lockName, c.ref, c.source)

		isUpToDate, newRef, err := check(c)
		if err != nil {
			vlog(verboseFlag, "remote tag check failed for %s: %v", c.lockName, err)
			ui.LogWarn(fmt.Sprintf("Skipping %s: %v", c.lockName, err))
			stats.skipped++
			continue
		}
		if isUpToDate {
			ui.LogInfo(fmt.Sprintf("%s is up to date (%s)", c.lockName, c.ref))
			stats.skipped++
			continue
		}

		targetRef := newRef
		if targetRef != "" {
			fmt.Printf("  %s→ upgrading %s → %s%s\n", ansiDim, c.ref, targetRef, ansiReset)
		} else {
			targetRef = c.ref
		}

		// Strip any fragment already on the recorded source before re-applying the
		// target ref, so two entries that differ only in how the ref was written
		// still group together.
		src := source.AppendFragmentRef(stripSourceRef(c.source), targetRef, "")

		g, ok := groups[src]
		if !ok {
			g = &updateGroup{source: src, seen: map[string]bool{}}
			groups[src] = g
			order = append(order, src)
		}
		g.add(c)
	}

	result := make([]updateGroup, 0, len(order))
	for _, key := range order {
		result = append(result, *groups[key])
	}
	return result
}

// runUpdateGroups re-fetches each group with a single `mdm skills add` call.
// Passing every skill name in one call is what keeps the clone count at one per
// repository instead of one per skill.
func runUpdateGroups(groups []updateGroup, global bool, opts UpdateOptions, stats *updateStats) {
	for _, g := range groups {
		if len(g.skills) > 1 {
			fmt.Printf("%sFetching %d skills from %s in one pass...%s\n", ansiDim, len(g.skills), g.source, ansiReset)
		}
		vlog(verboseFlag, "updating from %q: %v", g.source, g.names)

		addOpts := AddOptions{
			Yes:              true,
			Skills:           g.skills,
			AllowHiddenChars: opts.AllowHiddenChars,
		}
		if global {
			addOpts.Global = true
		} else {
			addOpts.Project = true
		}
		runAdd(g.source, addOpts)
		stats.updated += len(g.skills)
	}
}

// hintPluginOwnedUpdateFilters points an explicit filter that names a
// plugin-owned skill at mdm plugins update — plugin skills never appear in
// the skills lock section, so the name would otherwise silently match
// nothing.
func hintPluginOwnedUpdateFilters(skillFilter []string, cwd string) {
	for _, name := range skillFilter {
		if owner := pluginOwningSkill(name, cwd); owner != "" {
			ui.LogWarn(fmt.Sprintf("%s is managed by plugin %s — update it with 'mdm plugins update %s'", name, owner, owner))
		}
	}
}

func runUpdateWithOpts(skillFilter []string, opts UpdateOptions) {
	global, project, ok := resolveUpdateScope(opts)
	if !ok {
		return
	}
	vlog(verboseFlag, "update scope: global=%v project=%v filter=%v", global, project, skillFilter)

	// One cache for the whole run: a repo shared by the global and project
	// scopes is still only asked for its tags once.
	tags := newRemoteTagCache()
	check := func(c updateCandidate) (bool, string, error) { return checkCandidateUpToDate(c, tags) }
	var stats updateStats

	if global {
		groups := planUpdates(collectGlobalCandidates(skillFilter), check, &stats)
		vlog(verboseFlag, "global: %d source group(s) to fetch", len(groups))
		runUpdateGroups(groups, true, opts, &stats)
	}
	if project {
		cwd, _ := os.Getwd()
		hintPluginOwnedUpdateFilters(skillFilter, cwd)
		groups := planUpdates(collectProjectCandidates(skillFilter, cwd), check, &stats)
		vlog(verboseFlag, "project: %d source group(s) to fetch", len(groups))
		runUpdateGroups(groups, false, opts, &stats)
	}

	fmt.Println()
	if stats.updated == 0 && stats.skipped == 0 {
		fmt.Printf("%sNo skills to update.%s\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sUpdate complete:%s %d updated, %d already up to date\n", ansiText, ansiReset, stats.updated, stats.skipped)
	fmt.Println()
}
