package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
)

func buildListCmd() *cobra.Command {
	var globalFlag bool
	var projectFlag bool
	var agentFilter []string
	var jsonMode bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List installed skills",
		Aliases: []string{"ls"},
		Long: fmt.Sprintf(`List installed skills.

%sExamples:%s
  mdm skills list
  mdm skills list -g
  mdm skills list --json`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			var gFlag *bool
			if globalFlag {
				t := true
				gFlag = &t
			} else if projectFlag {
				f := false
				gFlag = &f
			}
			runListWithOpts(gFlag, agentFilter, jsonMode)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&globalFlag, "global", "g", false, "List global skills")
	f.BoolVarP(&projectFlag, "project", "p", false, "List project skills")
	f.StringArrayVarP(&agentFilter, "agent", "a", nil, "Filter by specific agents")
	f.BoolVar(&jsonMode, "json", false, "Output as JSON")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

func runListWithOpts(globalFlag *bool, agentFilter []string, jsonMode bool) {
	skills, err := listInstalledSkills(globalFlag, agentFilter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(skills) == 0 {
		if jsonMode {
			fmt.Println("[]")
		} else {
			fmt.Printf("%sNo skills installed.%s\n\n", ansiDim, ansiReset)
			fmt.Printf("Add your first skill with %smdm skills add <package>%s\n", ansiText, ansiReset)
		}
		return
	}

	// Sort by scope then name
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Scope != skills[j].Scope {
			return skills[i].Scope < skills[j].Scope
		}
		return skills[i].Name < skills[j].Name
	})

	if jsonMode {
		out, _ := json.MarshalIndent(skills, "", "  ")
		fmt.Println(string(out))
		return
	}

	cwd, _ := os.Getwd()

	// Group by scope
	byScope := map[string][]*InstalledSkill{}
	for _, s := range skills {
		byScope[s.Scope] = append(byScope[s.Scope], s)
	}

	for _, scope := range []string{"project", "global"} {
		scopeSkills, ok := byScope[scope]
		if !ok {
			continue
		}
		printSkillsForScope(scopeSkills, scope, cwd)
	}

	if !jsonMode && term.IsTerminal(os.Stdin.Fd()) {
		fmt.Printf("%s[s]%s show skill details  %s[any key]%s exit  ", ansiText, ansiReset, ansiDim, ansiReset)
		if pressedS() {
			fmt.Println()
			printSkillSummaries(skills, cwd)
		} else {
			fmt.Println()
		}
	}
}

func printSkillsForScope(scopeSkills []*InstalledSkill, scope, cwd string) {
	scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
	fmt.Printf("%s%s skills:%s\n\n", ansiText, scopeTitle, ansiReset)
	for _, s := range scopeSkills {
		fmt.Printf("  %s%s%s", ansiText, s.Name, ansiReset)
		if s.Description != "" {
			fmt.Printf("  %s%s%s", ansiDim, s.Description, ansiReset)
		}
		fmt.Println()
		if len(s.Agents) > 0 {
			var displayNames []string
			for _, a := range s.Agents {
				if cfg := agent.AllAgents[a]; cfg != nil {
					displayNames = append(displayNames, cfg.DisplayName)
				} else {
					displayNames = append(displayNames, a)
				}
			}
			fmt.Printf("    %sagents: %s%s\n", ansiDim, strings.Join(displayNames, ", "), ansiReset)
		}
		if len(s.Compatibility) > 0 {
			fmt.Printf("    %scompatibility: %s%s\n", ansiDim, formatCompatibility(s.Compatibility), ansiReset)
		}
		shortPath := shortenPath(s.Path, cwd)
		fmt.Printf("    %s%s%s\n", ansiDim, shortPath, ansiReset)
	}
	fmt.Println()
}

func formatCompatibility(compat map[string]string) string {
	keys := make([]string, 0, len(compat))
	for k := range compat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(compat))
	for _, k := range keys {
		parts = append(parts, k+": "+compat[k])
	}
	return strings.Join(parts, ", ")
}

func printSkillSummaries(skills []*InstalledSkill, cwd string) {
	fmt.Printf("\n%sSkill details:%s\n\n", ansiBold, ansiReset)
	for _, s := range skills {
		fmt.Printf("  %s%s%s\n", ansiBold, s.Name, ansiReset)
		if s.Description != "" {
			fmt.Printf("  %s%s%s\n", ansiDim, s.Description, ansiReset)
		}
		shortPath := shortenPath(s.Path, cwd)
		fmt.Printf("  %spath: %s%s\n", ansiDim, shortPath, ansiReset)
		// Read SKILL.md for a preview
		skillMd := filepath.Join(s.Path, "SKILL.md")
		data, err := os.ReadFile(skillMd)
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			// Skip frontmatter block
			start := 0
			if len(lines) > 0 && lines[0] == "---" {
				for i := 1; i < len(lines); i++ {
					if lines[i] == "---" {
						start = i + 1
						break
					}
				}
			}
			// Print up to 8 non-empty lines of content
			printed := 0
			for _, line := range lines[start:] {
				if printed >= 8 {
					fmt.Printf("  %s...%s\n", ansiDim, ansiReset)
					break
				}
				if strings.TrimSpace(line) == "" {
					continue
				}
				fmt.Printf("  %s%s%s\n", ansiDim, line, ansiReset)
				printed++
			}
		}
		fmt.Println()
	}
}
