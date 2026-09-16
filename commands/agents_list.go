// `mdm agents list`: the lock entries of a scope, checked against the disk.
package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
)

func buildAgentListCmd() *cobra.Command {
	var globalFlag, projectFlag, jsonMode bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List installed agent definitions",
		Aliases: []string{"ls"},
		Long: fmt.Sprintf(`List installed agent definitions.

%sExamples:%s
  mdm agents list
  mdm agents list -g
  mdm agents list --json`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runAgentList(globalFlag, projectFlag, jsonMode)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&globalFlag, "global", "g", false, "List global agent definitions")
	f.BoolVarP(&projectFlag, "project", "p", false, "List project agent definitions")
	f.BoolVar(&jsonMode, "json", false, "Output as JSON")

	return cmd
}

// agentLockEntries returns the sorted names and lock entries for one scope.
func agentLockEntries(global bool, cwd string) ([]string, map[string]lock.AgentLockEntry) {
	var m map[string]lock.AgentLockEntry
	if global {
		m = lock.ReadGlobalState().Agents
	} else {
		m = lock.ReadProjectLock(cwd).Agents
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, m
}

// agentEntryStatus is the on-disk health of one lock entry, checked against
// the canonical file and the harnesses the entry says hold the definition.
type agentEntryStatus struct {
	CanonicalMissing bool
	InstalledIn      []string // harness names, sorted; empty means installed nowhere
	MissingFrom      []string // harnesses the lock names whose file has gone; sorted
}

// agentStatusFor reads the entry's harnesses off the disk. An entry written
// before the lock recorded a harness list is inferred from the files mdm can
// show it wrote, so a hand-written definition of the same name in some other
// harness is not reported as an install.
func agentStatusFor(name string, entry lock.AgentLockEntry, global bool, cwd string) agentEntryStatus {
	canonical := agentCanonicalPath(name, lockedAgentFormat(entry), global, cwd)
	_, err := os.Stat(canonical)
	return agentEntryStatus{
		CanonicalMissing: err != nil,
		InstalledIn:      agentInstalledIn(name, entry, global, cwd),
		MissingFrom:      agentMissingFrom(name, entry, global, cwd),
	}
}

// harnessDisplayName returns the harness's display name, or the name itself
// when it is not a known harness.
func harnessDisplayName(name string) string {
	if cfg := harness.AllHarnesses[name]; cfg != nil {
		return cfg.DisplayName
	}
	return name
}

func harnessDisplayNames(names []string) []string {
	var out []string
	for _, n := range names {
		out = append(out, harnessDisplayName(n))
	}
	return out
}

// agentListItem is one element of `mdm agents list --json`. Scope is a field
// rather than a grouping wrapper, matching `mdm skills list --json`, and the
// harness lists are canonical names rather than the display names the text
// output prints: the canonical name is the stable identifier, and a caller
// can map it to a display name itself. The field names are a public contract:
// a rename keeps the old key.
type agentListItem struct {
	Name             string   `json:"name"`
	Scope            string   `json:"scope"`
	Source           string   `json:"source"`
	Ref              string   `json:"ref,omitempty"`
	CanonicalMissing bool     `json:"canonicalMissing"`
	InstalledIn      []string `json:"installedIn"`
	MissingFrom      []string `json:"missingFrom"`
}

// agentListScopes returns the scopes to list, as the globals each covers.
func agentListScopes(globalFlag, projectFlag bool) []bool {
	if globalFlag {
		return []bool{true}
	}
	if projectFlag {
		return []bool{false}
	}
	return []bool{false, true}
}

func scopeName(global bool) string {
	if global {
		return "global"
	}
	return "project"
}

func runAgentListJSON(scopes []bool, cwd string) {
	items := []agentListItem{}
	for _, global := range scopes {
		names, agents := agentLockEntries(global, cwd)
		for _, name := range names {
			entry := agents[name]
			st := agentStatusFor(name, entry, global, cwd)
			items = append(items, agentListItem{
				Name:             name,
				Scope:            scopeName(global),
				Source:           entry.Source,
				Ref:              entry.Ref,
				CanonicalMissing: st.CanonicalMissing,
				InstalledIn:      emptyIfNil(st.InstalledIn),
				MissingFrom:      emptyIfNil(st.MissingFrom),
			})
		}
	}
	printJSON(items)
}

func runAgentList(globalFlag, projectFlag, jsonMode bool) {
	cwd, _ := os.Getwd()
	scopes := agentListScopes(globalFlag, projectFlag)

	if jsonMode {
		runAgentListJSON(scopes, cwd)
		return
	}

	fmt.Println()
	total := 0
	for _, global := range scopes {
		names, agents := agentLockEntries(global, cwd)
		if len(names) == 0 {
			continue
		}
		total += len(names)
		scope := scopeName(global)
		scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
		fmt.Printf("%s%s agent definitions:%s\n\n", ansiText, scopeTitle, ansiReset)
		for _, name := range names {
			entry := agents[name]
			st := agentStatusFor(name, entry, global, cwd)

			var problems []string
			if st.CanonicalMissing {
				problems = append(problems, "canonical file missing")
			}
			if len(st.InstalledIn) == 0 {
				problems = append(problems, "not installed in any harness")
			} else if len(st.MissingFrom) > 0 {
				problems = append(problems, "missing from "+strings.Join(harnessDisplayNames(st.MissingFrom), ", "))
			}
			status := ""
			if len(problems) > 0 {
				status = ansiRed + "  " + strings.Join(problems, "; ") + ansiReset
			}

			fmt.Printf("  %s%s%s%s\n", ansiText, name, ansiReset, status)
			fmt.Printf("    %s%s%s\n", ansiDim, source.FormatSourceInput(entry.Source, entry.Ref), ansiReset)
			if len(st.InstalledIn) > 0 {
				fmt.Printf("    %sinstalled in: %s%s\n", ansiDim, strings.Join(harnessDisplayNames(st.InstalledIn), ", "), ansiReset)
			}
		}
		fmt.Println()
	}
	if total == 0 {
		fmt.Printf("%sNo agent definitions installed.%s\n\n", ansiDim, ansiReset)
		fmt.Printf("Add one with %smdm agents add <source>%s\n", ansiText, ansiReset)
		fmt.Printf("%sLooking for the configured harnesses? That list moved to mdm harnesses list.%s\n\n", ansiDim, ansiReset)
	}
}
