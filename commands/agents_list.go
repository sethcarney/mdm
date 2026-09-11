// `mdm agents list`: the lock entries of a scope, checked against the disk.
package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
)

func buildAgentListCmd() *cobra.Command {
	var globalFlag, projectFlag bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List installed agent definitions",
		Aliases: []string{"ls"},
		Long: fmt.Sprintf(`List installed agent definitions.

%sExamples:%s
  mdm agents list
  mdm agents list -g`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runAgentList(globalFlag, projectFlag)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&globalFlag, "global", "g", false, "List global agent definitions")
	f.BoolVarP(&projectFlag, "project", "p", false, "List project agent definitions")

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

// agentInstalledHarnesses returns, sorted, every harness with an
// agent-definition directory recorded for this scope that has a file on disk
// for this definition. A harness copy can go missing while the canonical file
// stays untouched.
func agentInstalledHarnesses(name string, global bool, cwd string) []string {
	var found []string
	for harnessName := range harness.AllHarnesses {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target == "" {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			found = append(found, harnessName)
		}
	}
	sort.Strings(found)
	return found
}

// agentEntryStatus is the on-disk health of one lock entry, checked against
// both the canonical file and every harness's own copy.
type agentEntryStatus struct {
	CanonicalMissing bool
	InstalledIn      []string // harness names, sorted; empty means installed nowhere
}

func agentStatusFor(name string, format agentfile.Format, global bool, cwd string) agentEntryStatus {
	canonical := agentCanonicalPath(name, format, global, cwd)
	_, err := os.Stat(canonical)
	return agentEntryStatus{
		CanonicalMissing: err != nil,
		InstalledIn:      agentInstalledHarnesses(name, global, cwd),
	}
}

func harnessDisplayNames(names []string) []string {
	var out []string
	for _, n := range names {
		if cfg := harness.AllHarnesses[n]; cfg != nil {
			out = append(out, cfg.DisplayName)
		} else {
			out = append(out, n)
		}
	}
	return out
}

func runAgentList(globalFlag, projectFlag bool) {
	cwd, _ := os.Getwd()
	scopes := []bool{false, true}
	if globalFlag {
		scopes = []bool{true}
	} else if projectFlag {
		scopes = []bool{false}
	}

	fmt.Println()
	total := 0
	for _, global := range scopes {
		names, agents := agentLockEntries(global, cwd)
		if len(names) == 0 {
			continue
		}
		total += len(names)
		scopeTitle := "Project"
		if global {
			scopeTitle = "Global"
		}
		fmt.Printf("%s%s agent definitions:%s\n\n", ansiText, scopeTitle, ansiReset)
		for _, name := range names {
			entry := agents[name]
			st := agentStatusFor(name, lockedAgentFormat(entry), global, cwd)

			var problems []string
			if st.CanonicalMissing {
				problems = append(problems, "canonical file missing")
			}
			if len(st.InstalledIn) == 0 {
				problems = append(problems, "not installed in any harness")
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
