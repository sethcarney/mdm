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
		})
	}
	return candidates
}

// warnAgentNamesNotInSource reports every requested name the source no longer
// yields. Such a definition stays exactly as installed.
func warnAgentNamesNotInSource(requested []string, selected []*agentfile.AgentFile, sourceRef string) {
	for _, filterName := range requested {
		matched := false
		for _, a := range selected {
			if skillNameMatches(a.Name, filterName) {
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

// recordAgentUpdate writes the definition's refreshed lock entry to whichever
// lock the scope keeps. A failed lock write is a warning, not a stop.
func recordAgentUpdate(name string, entry lock.AgentLockEntry, global bool, cwd string) {
	var err error
	if global {
		err = lock.AddAgentToGlobalState(name, entry)
	} else {
		err = lock.AddAgentToLocalLock(name, entry, cwd)
	}
	if err != nil {
		ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
	}
}

// runAgentUpdateGroups re-fetches each group once and reinstalls every selected
// definition into every harness it is currently installed to, per
// agentInstalledHarnesses. The scope's configured-harness list can differ.
func runAgentUpdateGroups(groups []updateGroup, global bool, cwd string, allowHiddenChars bool, stats *updateStats) {
	mode := currentInstallMode(global, cwd)
	claimed := map[string]string{} // disk name -> the source that claimed it
	for _, g := range groups {
		if len(g.skills) > 1 {
			fmt.Printf("%sFetching %d agent definition(s) from %s in one pass...%s\n", ansiDim, len(g.skills), g.source, ansiReset)
		}
		vlog(verboseFlag, "updating agent definition(s) from %q: %v", g.source, g.names)

		parsed := source.ParseSource(g.source)
		searchRoot, cloneDir, cleanup := fetchAgentSource(parsed, verboseFlag)

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
			installedHarnesses := agentInstalledHarnesses(name, global, cwd)
			if len(installedHarnesses) == 0 {
				ui.LogWarn(fmt.Sprintf("%s: not installed in any harness, skipping", a.Name))
				continue
			}

			installedAny, failedHarnesses := reinstallAgentIntoHarnesses(a, installedHarnesses, global, cwd, mode)

			// The lock must describe the disk. When no harness took the new
			// version, every harness copy still holds the version the entry
			// already names, so the entry stays. The canonical file is the one
			// thing that may have moved on: installAgentFile writes it before
			// the first harness write, so it can hold the new bytes while the
			// lock and every harness hold the old ones. The entry is not this
			// run's to rewrite for that alone - the recorded source is what
			// every harness is still running.
			if !installedAny {
				ui.LogWarn(fmt.Sprintf("%s: update failed for every installed harness (%s) - lock entry left unchanged", a.Name, strings.Join(failedHarnesses, ", ")))
				continue
			}
			if len(failedHarnesses) == 0 {
				ui.LogSuccess(a.Name)
			} else {
				ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", a.Name, strings.Join(failedHarnesses, ", ")))
			}

			entry := baseEntry
			entry.AgentPath = agentFileRepoPath(a.Path, cloneDir)
			entry.Format = string(agentCanonicalFormat(a))
			recordAgentUpdate(name, entry, global, cwd)
			stats.updated++
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
	fmt.Printf("%sUpdate complete:%s %d updated, %d already up to date\n", ansiText, ansiReset, stats.updated, stats.skipped)
	fmt.Println()
}
