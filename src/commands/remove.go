package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

type RemoveOptions struct {
	Global bool
	Agents []string
	Skills []string
	Yes    bool
	All    bool
}

func buildRemoveCmd() *cobra.Command {
	var opts RemoveOptions

	cmd := &cobra.Command{
		Use:     "remove [skills...]",
		Short:   "Remove installed skills",
		Aliases: []string{"rm", "r"},
		Long: fmt.Sprintf(`Remove installed skills from agents.

If no skill names are provided an interactive selection menu is shown.

The --agent (-a) and --skill (-s) flags accept multiple values — space-
separated after the flag or repeated:

  mdm skills remove -a claude-code cursor
  mdm skills remove -a claude-code -a cursor

%sExamples:%s
  mdm skills remove
  mdm skills remove my-skill
  mdm skills remove skill1 skill2 -y
  mdm skills remove --global my-skill
  mdm skills remove --all`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			if opts.All {
				opts.Skills = []string{"*"}
				opts.Agents = []string{"*"}
				opts.Yes = true
			}
			runRemove(args, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Remove from global scope")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Remove from specific agents (repeatable)")
	f.StringArrayVarP(&opts.Skills, "skill", "s", nil, "Skill names to remove (repeatable)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	f.BoolVar(&opts.All, "all", false, "Shorthand for --skill '*' --agent '*' -y")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

func runRemove(positional []string, opts RemoveOptions) {
	cwd, _ := os.Getwd()
	global := opts.Global

	// Merge positional + --skill args
	skillFilter := opts.Skills
	if len(positional) > 0 {
		skillFilter = append(skillFilter, positional...)
	}

	// Determine scope if not specified
	if !opts.Global && !opts.Yes {
		idx, ok := ui.UiSelect("Which scope?", []ui.UIOption{
			{Label: "Project", Hint: "remove from this project"},
			{Label: "Global", Hint: "remove from your user account"},
		})
		if !ok {
			return
		}
		global = idx == 1
	}

	// Discover installed skills in the appropriate scope
	scopeGlobal := &global
	installed, err := listInstalledSkills(scopeGlobal, opts.Agents)
	if err != nil || len(installed) == 0 {
		fmt.Printf("%sNo skills installed.%s\n", ansiDim, ansiReset)
		return
	}

	// If skill filter provided, narrow down
	var toRemove []*InstalledSkill
	if len(skillFilter) > 0 && !(len(skillFilter) == 1 && skillFilter[0] == "*") {
		for _, s := range installed {
			for _, f := range skillFilter {
				if strings.EqualFold(s.Name, f) || strings.EqualFold(sanitizeName(s.Name), sanitizeName(f)) {
					toRemove = append(toRemove, s)
					break
				}
			}
		}
		if len(toRemove) == 0 {
			fmt.Printf("%sNo matching skills found.%s\n", ansiDim, ansiReset)
			return
		}
	} else if len(skillFilter) == 1 && skillFilter[0] == "*" {
		toRemove = installed
	} else if opts.Yes || len(installed) == 1 {
		toRemove = installed
	} else {
		// Interactive selection
		options := make([]ui.UIOption, len(installed))
		for i, s := range installed {
			hint := s.Description
			if len(s.Agents) > 0 {
				hint = strings.Join(s.Agents, ", ")
			}
			options[i] = ui.UIOption{Label: s.Name, Value: sanitizeName(s.Name), Hint: hint}
		}
		indices, ok := ui.UiMultiselect("Which skills would you like to remove?", options, true, nil, nil)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		for _, i := range indices {
			toRemove = append(toRemove, installed[i])
		}
	}

	if len(toRemove) == 0 {
		return
	}

	// Confirm
	if !opts.Yes && len(toRemove) > 0 {
		var names []string
		for _, s := range toRemove {
			names = append(names, s.Name)
		}
		confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d skill(s): %s?", len(toRemove), strings.Join(names, ", ")))
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
	}

	fmt.Println()

	// Remove each skill
	for _, sk := range toRemove {
		sName := sanitizeName(sk.Name)

		// Remove from each agent's skills directory
		agentsToRemove := sk.Agents
		if len(opts.Agents) > 0 {
			agentsToRemove = opts.Agents
		}

		for _, agentName := range agentsToRemove {
			agentBase := getAgentBaseDir(agentName, global, cwd)
			if agentBase == "" {
				continue
			}
			// Try both the sanitized name and the directory name
			for _, name := range []string{sName, filepath.Base(sk.Path)} {
				agentSkillDir := filepath.Join(agentBase, name)
				if !isPathSafe(agentBase, agentSkillDir) {
					continue
				}
				if info, err := os.Lstat(agentSkillDir); err == nil {
					if info.Mode()&os.ModeSymlink != 0 {
						os.Remove(agentSkillDir)
					} else {
						os.RemoveAll(agentSkillDir)
					}
				}
			}
		}

		// Remove canonical directory
		canonicalDir := getCanonicalPath(sk.Name, global)
		if canonicalDir != "" && isPathSafe(getCanonicalSkillsDir(global, cwd), canonicalDir) {
			os.RemoveAll(canonicalDir)
		}

		// Update lock files
		_ = lock.RemoveSkillFromLock(sName)
		if !global {
			_ = lock.RemoveSkillFromLocalLock(sName, cwd)
		}

		ui.LogSuccess("Removed " + sk.Name)
	}

	fmt.Println()
}
