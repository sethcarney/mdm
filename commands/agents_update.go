// `mdm agents update`: re-fetch each definition from its recorded source and
// refresh every harness it is installed to.
package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildAgentsUpdateCmd() *cobra.Command {
	var opts UpdateOptions

	cmd := &cobra.Command{
		Use:   "update [names...]",
		Short: "Update installed agent definitions",
		Long: fmt.Sprintf(`Update installed agent definitions to their latest versions.

Definitions that share a source repository and target ref are re-fetched
together, so a repo holding many agent definitions is cloned once per
update run rather than once per definition. Every harness a definition is
currently installed to is refreshed - not just the canonical copy - so a
copy-mode harness install picks up the change too, instead of going stale.

%sExamples:%s
  mdm agents update
  mdm agents update code-reviewer
  mdm agents update -g`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runAgentsUpdateWithOpts(args, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Update global agent definitions only")
	f.BoolVarP(&opts.Project, "project", "p", false, "Update project agent definitions only")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip scope prompt")
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")

	return cmd
}

// currentInstallMode reads the scope's recorded install mode without
// reconciling it. An update never switches modes; commitScopeInstallMode does.
func currentInstallMode(global bool, cwd string) InstallMode {
	if lock.GetInstallMode(global, cwd) == lock.InstallModeCopy {
		return InstallModeCopy
	}
	return InstallModeSymlink
}

// collectAgentCandidates adapts one scope's agent lock entries to
// updateCandidate, the shape planUpdates takes for skills.
func collectAgentCandidates(global bool, filter []string, cwd string) []updateCandidate {
	names, agents := agentLockEntries(global, cwd)

	var candidates []updateCandidate
	for _, name := range names {
		entry := agents[name]
		if !matchesFilter(name, "", filter) {
			continue
		}
		candidates = append(candidates, updateCandidate{
			lockName:   name,
			filterName: name,
			source:     entry.Source,
			sourceType: entry.SourceType,
			ref:        entry.Ref,
			command:    "agents",
		})
	}
	return candidates
}

// readCanonicalAgent returns the canonical file's current bytes, or nil when
// there is none to restore.
func readCanonicalAgent(name string, a *agentfile.AgentFile, global bool, cwd string) []byte {
	data, err := os.ReadFile(agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd))
	if err != nil {
		return nil
	}
	return data
}

// restoreCanonicalAgent puts previous back at the canonical path. A restore
// that fails is reported: the file then holds the new bytes, and the warning
// is the one place that says so.
func restoreCanonicalAgent(name string, a *agentfile.AgentFile, previous []byte, global bool, cwd string) {
	if previous == nil {
		return
	}
	path := agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd)
	if err := replaceFileWith(path, previous); err != nil {
		ui.LogWarn(fmt.Sprintf("%s: could not restore the previous canonical file at %s: %v", name, path, err))
	}
}

// warnAgentNamesNotInSource reports every requested name the source no longer
// yields. Such a definition stays exactly as installed.
func warnAgentNamesNotInSource(requested []string, selected []*agentfile.AgentFile, sourceRef string) {
	for _, filterName := range requested {
		matched := false
		for _, a := range selected {
			if agentNameMatches(a.Name, filterName) {
				matched = true
				break
			}
		}
		if !matched {
			ui.LogWarn(fmt.Sprintf("%s: not found in %s, leaving the existing install alone", filterName, sourceRef))
		}
	}
}

// reinstallAgentIntoHarnesses reinstalls one definition into every harness
// given, and reports whether any install succeeded plus the names that failed.
func reinstallAgentIntoHarnesses(a *agentfile.AgentFile, harnesses []string, global bool, cwd string, mode InstallMode) (installedAny bool, failedHarnesses []string) {
	for _, harnessName := range harnesses {
		if result := installAgentFile(a, harnessName, global, cwd, mode); result.Success {
			installedAny = true
		} else {
			failedHarnesses = append(failedHarnesses, harnessName)
		}
	}
	return installedAny, failedHarnesses
}

// recordAgentEntry writes the definition's lock entry to whichever lock the
// scope keeps. A local source is recorded in the project lock as the
// cwd-relative form, as skills record it, so the lock stays portable; the
// global state file keeps the absolute path, since no cwd is implied there.
// A failed lock write is a warning, not a stop.
func recordAgentEntry(name string, entry lock.AgentLockEntry, global bool, cwd string) {
	var err error
	if global {
		err = lock.AddAgentToGlobalState(name, entry)
	} else {
		if entry.SourceType == string(source.SourceTypeLocal) {
			entry.Source = toRelSourcePath(entry.Source, cwd)
		}
		err = lock.AddAgentToLocalLock(name, entry, cwd)
	}
	if err != nil {
		ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
	}
}

// refreshableHarnesses splits the harnesses holding a definition into the
// ones an update may overwrite and the ones it must not. A harness file is
// refreshed only when mdmOwnsAgentFile can show mdm wrote it from the
// canonical file as it stands before the update; a hand-edited copy is the
// user's, and the update leaves it and says so.
func refreshableHarnesses(name string, held []string, canonicalPath string, global bool, cwd string) (targets, foreign []string) {
	for _, harnessName := range held {
		if mdmOwnsAgentFile(agentHarnessPath(name, harnessName, global, cwd), harnessName, canonicalPath) {
			targets = append(targets, harnessName)
		} else {
			foreign = append(foreign, harnessName)
		}
	}
	return targets, foreign
}

// updateOneAgent reinstalls one fetched definition into every harness that
// holds it and records the result. It returns true when the lock entry was
// refreshed.
func updateOneAgent(a *agentfile.AgentFile, name string, entry lock.AgentLockEntry, baseEntry lock.AgentLockEntry, rootDir string, global bool, cwd string, mode InstallMode) bool {
	held := agentInstalledIn(name, entry, global, cwd)
	if len(held) == 0 {
		ui.LogWarn(fmt.Sprintf("%s: not installed in any harness, skipping", a.Name))
		return false
	}
	canonicalPath := agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd)
	targets, foreign := refreshableHarnesses(name, held, canonicalPath, global, cwd)
	for _, harnessName := range foreign {
		ui.LogInfo(fmt.Sprintf("%s: left %s alone: the file there is not the one mdm wrote", a.Name, harnessDisplayName(harnessName)))
	}
	if len(targets) == 0 {
		ui.LogWarn(fmt.Sprintf("%s: no harness copy mdm wrote is left to refresh - lock entry left unchanged", a.Name))
		return false
	}

	// installAgentFile writes the canonical file before the first harness
	// write. If no harness then takes the new version, the previous bytes go
	// back, so the canonical file never runs ahead of the lock and of every
	// harness copy.
	previous := readCanonicalAgent(name, a, global, cwd)
	installedAny, failedHarnesses := reinstallAgentIntoHarnesses(a, targets, global, cwd, mode)

	// The lock must describe the disk. When no harness took the new version,
	// every harness copy still holds the version the entry already names, so
	// the entry stays and the canonical file is restored to match it.
	if !installedAny {
		ui.LogWarn(fmt.Sprintf("%s: update failed for every installed harness (%s) - lock entry left unchanged", a.Name, strings.Join(failedHarnesses, ", ")))
		restoreCanonicalAgent(name, a, previous, global, cwd)
		return false
	}
	if len(failedHarnesses) == 0 {
		ui.LogSuccess(a.Name)
	} else {
		ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", a.Name, strings.Join(failedHarnesses, ", ")))
	}

	fresh := baseEntry
	fresh.AgentPath = agentFileRepoPath(a.Path, rootDir)
	fresh.Format = string(agentCanonicalFormat(a))
	// An entry with a list keeps it: a harness whose file went missing is
	// still where the lock says the definition belongs. One without a list
	// gets the harnesses this update found holding it, so the inference is
	// never needed for it again.
	fresh.Harnesses = agentRecordedHarnesses(entry)
	if fresh.Harnesses == nil {
		fresh.Harnesses = held
	}
	recordAgentEntry(name, fresh, global, cwd)
	return true
}

// runAgentUpdateGroups re-fetches each group once and reinstalls every selected
// definition into every harness its lock entry says holds it, per
// agentInstalledIn. The scope's configured-harness list can differ.
func runAgentUpdateGroups(groups []updateGroup, global bool, cwd string, allowHiddenChars bool, stats *updateStats) {
	mode := currentInstallMode(global, cwd)
	claimed := map[string]string{} // disk name -> the source that claimed it
	_, entries := agentLockEntries(global, cwd)
	for _, g := range groups {
		if len(g.skills) > 1 {
			fmt.Printf("%sFetching %d agent definition(s) from %s in one pass...%s\n", ansiDim, len(g.skills), g.source, ansiReset)
		}
		vlog(verboseFlag, "updating agent definition(s) from %q: %v", g.source, g.names)

		parsed := source.ParseSource(g.source)
		searchRoot, rootDir, cleanup := fetchAgentSource(parsed, verboseFlag)

		found, err := agentfile.DiscoverAgentFiles(searchRoot, parsed.Subpath)
		if err != nil {
			ui.LogWarn(fmt.Sprintf("could not fetch %s: %v", g.source, err))
			cleanup()
			continue
		}
		selected := filterAgentsByName(found, g.skills)

		// An update overwrites a file the harness already loads, so the
		// incoming version gets the same scan the first install got. The
		// whole group is dropped, matching the skills path.
		if !checkAgentFilesMarkdownForHiddenChars(selected, allowHiddenChars) {
			cleanup()
			continue
		}

		baseEntry := agentLockEntry(parsed, g.source)

		warnAgentNamesNotInSource(g.skills, selected, g.source)

		for _, a := range selected {
			name := agentDiskName(a.Name)
			// One lock key matches every definition whose name sanitizes to it,
			// and they would all be written to the one canonical file.
			if prior, ok := claimed[name]; ok {
				ui.LogWarn(fmt.Sprintf("%s: %s and %s both install as %s - skipping the second, rename one of them", a.Name, prior, a.Path, name))
				continue
			}
			claimed[name] = a.Path
			if updateOneAgent(a, name, entries[name], baseEntry, rootDir, global, cwd, mode) {
				stats.updated++
			}
		}
		cleanup()
	}
}

func runAgentsUpdateWithOpts(filter []string, opts UpdateOptions) {
	global, project, ok := resolveUpdateScope(opts)
	if !ok {
		return
	}
	vlog(verboseFlag, "agents update scope: global=%v project=%v filter=%v", global, project, filter)

	tags := newRemoteTagCache()
	check := func(c updateCandidate) (bool, string, error) { return checkCandidateUpToDate(c, tags) }
	var stats updateStats

	cwd, _ := os.Getwd()
	if global {
		groups := planUpdates(collectAgentCandidates(true, filter, cwd), check, &stats)
		vlog(verboseFlag, "global: %d source group(s) to fetch", len(groups))
		runAgentUpdateGroups(groups, true, cwd, opts.AllowHiddenChars, &stats)
	}
	if project {
		groups := planUpdates(collectAgentCandidates(false, filter, cwd), check, &stats)
		vlog(verboseFlag, "project: %d source group(s) to fetch", len(groups))
		runAgentUpdateGroups(groups, false, cwd, opts.AllowHiddenChars, &stats)
	}

	fmt.Println()
	if stats.updated == 0 && stats.skipped == 0 {
		fmt.Printf("%sNo agent definitions to update.%s\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sUpdate complete:%s %d updated, %d unchanged\n", ansiText, ansiReset, stats.updated, stats.skipped)
	fmt.Println()
}
