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

func filterInstalledByName(installed []*InstalledSkill, names []string) ([]*InstalledSkill, bool) {
	var result []*InstalledSkill
	for _, s := range installed {
		for _, f := range names {
			if skillNameMatches(s.Name, f) {
				result = append(result, s)
				break
			}
		}
	}
	if len(result) == 0 {
		fmt.Printf("%sNo matching skills found.%s\n", ansiDim, ansiReset)
		return nil, false
	}
	return result, true
}

func selectSkillsToRemove(installed []*InstalledSkill, skillFilter []string, opts RemoveOptions) ([]*InstalledSkill, bool) {
	if len(skillFilter) == 1 && skillFilter[0] == "*" {
		return installed, true
	}
	if len(skillFilter) > 0 {
		return filterInstalledByName(installed, skillFilter)
	}
	if opts.Yes || len(installed) == 1 {
		return installed, true
	}
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
		return nil, false
	}
	var selected []*InstalledSkill
	for _, i := range indices {
		selected = append(selected, installed[i])
	}
	return selected, true
}

func removeSkillFromDisk(sk *InstalledSkill, agentsToRemove []string, global bool, cwd string) {
	sName := sanitizeName(sk.Name)
	for _, agentName := range agentsToRemove {
		agentBase := getAgentBaseDir(agentName, global, cwd)
		if agentBase == "" {
			continue
		}
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
	canonicalDir := getCanonicalPath(sk.Name, global)
	if canonicalDir != "" && isPathSafe(getCanonicalSkillsDir(global, cwd), canonicalDir) {
		os.RemoveAll(canonicalDir)
	}
	if global {
		_ = lock.RemoveSkillFromLock(sName)
	} else {
		_ = lock.RemoveSkillFromLocalLock(sName, cwd)
	}
	ui.LogSuccess("Removed " + sk.Name)
}

func runRemove(positional []string, opts RemoveOptions) {
	cwd, _ := os.Getwd()
	global := opts.Global

	skillFilter := opts.Skills
	if len(positional) > 0 {
		skillFilter = append(skillFilter, positional...)
	}

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

	scopeGlobal := &global
	installed, err := listInstalledSkills(scopeGlobal, opts.Agents)
	if err != nil || len(installed) == 0 {
		var cleaned int
		if global {
			cleaned = cleanOrphanedLockEntries(cwd)
		} else {
			cleaned = cleanOrphanedLocalLockEntries(cwd)
		}
		if cleaned > 0 {
			fmt.Printf("%sCleaned up %d orphaned lock entr%s with no files on disk.%s\n",
				ansiDim, cleaned, map[bool]string{true: "ies", false: "y"}[cleaned != 1], ansiReset)
			return
		}
		fmt.Printf("%sNo skills installed.%s\n", ansiDim, ansiReset)
		return
	}

	toRemove, ok := selectSkillsToRemove(installed, skillFilter, opts)
	if !ok || len(toRemove) == 0 {
		return
	}

	if !opts.Yes && !confirmRemove(toRemove) {
		return
	}

	fmt.Println()
	for _, sk := range toRemove {
		agentsToRemove := sk.Agents
		if len(opts.Agents) > 0 {
			agentsToRemove = opts.Agents
		}
		removeSkillFromDisk(sk, agentsToRemove, global, cwd)
	}
	if !global {
		cleanOrphanedLocalLockEntries(cwd)
	}
	fmt.Println()
}

func confirmRemove(toRemove []*InstalledSkill) bool {
	var names []string
	for _, s := range toRemove {
		names = append(names, s.Name)
	}
	confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d skill(s): %s?", len(toRemove), strings.Join(names, ", ")))
	if !ok || !confirmed {
		fmt.Println("Cancelled.")
		return false
	}
	return true
}

// cleanOrphanedLockEntries removes global lock entries whose skill files no
// longer exist on disk. Returns the number of entries removed.
func cleanOrphanedLockEntries(cwd string) int {
	globalLock := lock.ReadSkillLock()
	if len(globalLock.Skills) == 0 {
		return 0
	}
	canonicalBase := getCanonicalSkillsDir(true, cwd)
	var removed []string
	for name := range globalLock.Skills {
		skillDir := filepath.Join(canonicalBase, sanitizeName(name))
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMd); os.IsNotExist(err) {
			removed = append(removed, name)
		}
	}
	for _, name := range removed {
		_ = lock.RemoveSkillFromLock(sanitizeName(name))
	}
	return len(removed)
}

// cleanOrphanedLocalLockEntries removes project lock entries whose skill files
// no longer exist on disk. Returns the number of entries removed.
func cleanOrphanedLocalLockEntries(cwd string) int {
	localLock := lock.ReadLocalLock(cwd)
	if len(localLock.Skills) == 0 {
		return 0
	}
	canonicalBase := getCanonicalSkillsDir(false, cwd)
	var removed []string
	for name := range localLock.Skills {
		skillDir := filepath.Join(canonicalBase, sanitizeName(name))
		skillMd := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(skillMd); os.IsNotExist(err) {
			removed = append(removed, name)
		}
	}
	for _, name := range removed {
		_ = lock.RemoveSkillFromLocalLock(sanitizeName(name), cwd)
	}
	return len(removed)
}
