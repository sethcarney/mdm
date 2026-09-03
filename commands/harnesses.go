package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/fork"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildHarnessesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harnesses",
		Short: "Manage the AI harnesses mdm installs into",
		Long: fmt.Sprintf(`Manage the list of AI harnesses mdm should support by default.

Configured harnesses are used as the default selection when running
%smdm skills add%s without an explicit %s--harness%s flag.

%sExamples:%s
  mdm harnesses list
  mdm harnesses add claude-code cursor
  mdm harnesses remove cursor
  mdm harnesses add --global claude-code`, ansiBold, ansiReset, ansiBold, ansiReset, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(
		buildHarnessesListCmd(),
		buildHarnessesAddCmd(),
		buildHarnessesRemoveCmd(),
	)

	return cmd
}

type harnessListItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Scope       string `json:"scope"`
	Installed   bool   `json:"installed"`
}

func buildHarnessesListCmd() *cobra.Command {
	var global bool
	var jsonMode bool
	var available bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List configured harnesses",
		RunE: func(cmd *cobra.Command, args []string) error {
			if available {
				return runHarnessesListAvailable(jsonMode)
			}

			cwd, _ := os.Getwd()
			configured := lock.GetConfiguredHarnesses(global, cwd)
			scope := "project"
			if global {
				scope = "global"
			}

			if jsonMode {
				items := make([]harnessListItem, 0, len(configured))
				for _, name := range configured {
					cfg := harness.AllHarnesses[name]
					item := harnessListItem{Name: name, Scope: scope}
					if cfg != nil {
						item.DisplayName = cfg.DisplayName
						item.Installed = cfg.DetectInstalled != nil && cfg.DetectInstalled()
					} else {
						item.DisplayName = name
					}
					items = append(items, item)
				}
				out, _ := json.MarshalIndent(items, "", "  ")
				fmt.Println(string(out))
				return nil
			}

			if len(configured) == 0 {
				fmt.Printf("%sNo harnesses configured for %s scope.%s\n", ansiDim, scope, ansiReset)
				fmt.Printf("Run %smdm harnesses add%s to configure your harnesses.\n", ansiBold, ansiReset)
				return nil
			}
			fmt.Printf("%s%s scope harnesses:%s\n\n", ansiBold, strings.ToUpper(scope[:1])+scope[1:], ansiReset)
			for _, name := range configured {
				cfg := harness.AllHarnesses[name]
				if cfg == nil {
					fmt.Printf("  %s%s%s %s(unknown)%s\n", ansiText, name, ansiReset, ansiDim, ansiReset)
					continue
				}
				detected := ""
				if cfg.DetectInstalled != nil && cfg.DetectInstalled() {
					detected = fmt.Sprintf("  %s✓ installed%s", ansiGreen, ansiReset)
				}
				fmt.Printf("  %s%-28s%s%s\n", ansiText, cfg.DisplayName, ansiReset, detected)
			}
			fmt.Println()
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "List global configured harnesses")
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&available, "available", false, "List all harnesses known to mdm (not just configured ones)")
	return cmd
}

func runHarnessesListAvailable(jsonMode bool) error {
	type availableItem struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Installed   bool   `json:"installed"`
	}

	items := make([]availableItem, 0, len(harness.AllHarnesses))
	for name, cfg := range harness.AllHarnesses {
		if cfg.ExcludeFromPicker {
			continue
		}
		items = append(items, availableItem{
			Name:        name,
			DisplayName: cfg.DisplayName,
			Installed:   cfg.DetectInstalled != nil && cfg.DetectInstalled(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].DisplayName < items[j].DisplayName
	})

	if jsonMode {
		out, _ := json.MarshalIndent(items, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	fmt.Printf("%sAll available harnesses:%s\n\n", ansiBold, ansiReset)
	for _, item := range items {
		detected := ""
		if item.Installed {
			detected = fmt.Sprintf("  %s✓ installed%s", ansiGreen, ansiReset)
		}
		fmt.Printf("  %s%-28s%s %s%s%s%s\n", ansiText, item.DisplayName, ansiReset, ansiDim, item.Name, ansiReset, detected)
	}
	fmt.Println()
	return nil
}

func buildHarnessesAddCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:     "add [harnesses...]",
		Aliases: []string{"a"},
		Short:   "Add harnesses to your configured list",
		Long: fmt.Sprintf(`Add one or more harnesses to your configured list.

If no harness names are provided an interactive picker is shown.
Use %s--global%s / %s-g%s to configure harnesses at the user level.`, ansiBold, ansiReset, ansiBold, ansiReset),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()

			if len(args) == 0 {
				if !cmd.Flags().Changed("global") {
					isGlobal, ok := promptHarnessScope()
					if !ok {
						return nil
					}
					global = isGlobal
				}
				scope := "project"
				if global {
					scope = "global"
				}
				selected, err := pickAndSaveHarnesses(global, scope, cwd)
				if err != nil {
					return err
				}
				if len(selected) > 0 {
					runHarnessSetup(selected, cwd)
				}
				return nil
			}

			scope := "project"
			if global {
				scope = "global"
			}
			toAdd, ok := validateNamedHarnesses(args)
			if !ok {
				return fmt.Errorf("no valid harnesses specified")
			}
			if err := lock.AddToConfiguredAgents(toAdd, global, cwd); err != nil {
				ui.LogError(fmt.Sprintf("could not save harness configuration: %v", err))
				return nil
			}
			for _, name := range toAdd {
				cfg := harness.AllHarnesses[name]
				displayName := name
				if cfg != nil {
					displayName = cfg.DisplayName
				}
				fmt.Printf("%s✓%s Added %s%s%s to %s configured harnesses\n",
					ansiGreen, ansiReset, ansiBold, displayName, ansiReset, scope)
			}
			runHarnessSetup(toAdd, cwd)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Add to global configured harnesses")
	return cmd
}

// pickAndSaveHarnesses shows an interactive picker pre-seeded with the current
// configured list and replaces the entire list with the user's selection.
// Truly universal harnesses (share .agents/skills AND have no unique instruction
// file) are excluded from the picker — they are always supported and need no
// configuration. Returns the saved harness names so the caller can act on them.
func pickAndSaveHarnesses(global bool, scope, cwd string) ([]string, error) {
	current := lock.GetConfiguredHarnesses(global, cwd)
	currentSet := map[string]bool{}
	for _, a := range current {
		currentSet[a] = true
	}

	var options []ui.UIOption
	var lockedOptions []ui.UIOption
	for name, cfg := range harness.AllHarnesses {
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		if harness.NeedsNoTracking(name) {
			lockedOptions = append(lockedOptions, ui.UIOption{Label: cfg.DisplayName, Value: name})
			continue
		}
		options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: name})
	}
	sort.Slice(options, func(i, j int) bool {
		return options[i].Label < options[j].Label
	})
	sort.Slice(lockedOptions, func(i, j int) bool {
		return lockedOptions[i].Label < lockedOptions[j].Label
	})

	var initSel []int
	for i, opt := range options {
		if currentSet[opt.Value] {
			initSel = append(initSel, i)
		}
	}

	selected, ok := ui.UiSearchMultiselect("Which harnesses do you want to configure?", options, lockedOptions, initSel, false)
	if !ok {
		return nil, nil
	}
	var newList []string
	for _, i := range selected {
		newList = append(newList, options[i].Value)
	}
	if len(newList) == 0 {
		fmt.Printf("%sNo harnesses selected.%s\n", ansiDim, ansiReset)
		return nil, nil
	}
	sort.Strings(newList)
	if err := lock.SetConfiguredHarnesses(newList, global, cwd); err != nil {
		ui.LogError(fmt.Sprintf("could not save harness configuration: %v", err))
		return nil, nil
	}
	printHarnessesSaved(newList, scope)

	// Return only newly added harnesses so setup only runs for them, not for
	// harnesses that were already configured before this invocation.
	var newlyAdded []string
	for _, name := range newList {
		if !currentSet[name] {
			newlyAdded = append(newlyAdded, name)
		}
	}
	return newlyAdded, nil
}

func promptHarnessScope() (isGlobal bool, ok bool) {
	opts := []ui.UIOption{
		{Label: "Project", Value: "project", Hint: lockName + " in this directory"},
		{Label: "Global", Value: "global", Hint: "~/.agents/mdm-state.json"},
	}
	idx, ok := ui.UiSelect("Configure harnesses for which scope?", opts)
	if !ok {
		return false, false
	}
	return idx == 1, true
}

func buildHarnessesRemoveCmd() *cobra.Command {
	var global bool
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove [harnesses...]",
		Aliases: []string{"rm", "r"},
		Short:   "Remove harnesses from your configured list",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHarnessesRemove(cmd, args, global, yes)
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "Remove from global configured harnesses")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func runHarnessesRemove(cmd *cobra.Command, args []string, global, yes bool) error {
	cwd, _ := os.Getwd()

	if len(args) == 0 && !cmd.Flags().Changed("global") && !yes {
		isGlobal, ok := promptHarnessScope()
		if !ok {
			return nil
		}
		global = isGlobal
	}

	scope := "project"
	if global {
		scope = "global"
	}

	configured := lock.GetConfiguredHarnesses(global, cwd)
	if len(configured) == 0 {
		fmt.Printf("%sNo harnesses configured for %s scope.%s\n", ansiDim, scope, ansiReset)
		return nil
	}

	if yes && len(args) == 0 {
		return fmt.Errorf("harness names are required when using --yes")
	}

	toRemove, ok := resolveHarnessesToRemove(args, configured)
	if !ok {
		return nil
	}

	if !yes && !confirmHarnessesRemoval(toRemove) {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := lock.RemoveFromConfiguredAgents(toRemove, global, cwd); err != nil {
		ui.LogError(fmt.Sprintf("could not save harness configuration: %v", err))
		return nil
	}
	for _, name := range toRemove {
		cfg := harness.AllHarnesses[name]
		displayName := name
		if cfg != nil {
			displayName = cfg.DisplayName
		}
		fmt.Printf("%s✓%s Removed %s%s%s from %s configured harnesses\n",
			ansiGreen, ansiReset, ansiBold, displayName, ansiReset, scope)
	}
	fmt.Println()
	cleanUpRemovedHarnessFiles(toRemove, global, cwd)
	return nil
}

// resolveHarnessesToRemove returns harnesses from explicit args or via interactive
// picker when no args are provided.
func resolveHarnessesToRemove(args []string, configured []string) ([]string, bool) {
	if len(args) > 0 {
		validated, ok := validateNamedHarnesses(args)
		if !ok {
			return nil, false
		}
		return validated, true
	}
	return pickHarnessesToRemove(configured)
}

// confirmHarnessesRemoval shows a confirmation prompt listing the harnesses to be
// removed. Returns true when the user confirms.
func confirmHarnessesRemoval(toRemove []string) bool {
	var displayNames []string
	for _, name := range toRemove {
		cfg := harness.AllHarnesses[name]
		if cfg != nil {
			displayNames = append(displayNames, cfg.DisplayName)
		} else {
			displayNames = append(displayNames, name)
		}
	}
	confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d harness(es): %s?", len(toRemove), strings.Join(displayNames, ", ")))
	return ok && confirmed
}

// pickHarnessesToRemove shows an interactive picker with nothing pre-selected;
// the user checks the harnesses they want to remove.
func pickHarnessesToRemove(configured []string) ([]string, bool) {
	var options []ui.UIOption
	for _, name := range configured {
		cfg := harness.AllHarnesses[name]
		label := name
		if cfg != nil {
			label = cfg.DisplayName
		}
		options = append(options, ui.UIOption{Label: label, Value: name})
	}

	indices, ok := ui.UiMultiselect("Which harnesses would you like to remove?", options, false, nil, nil)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	if len(indices) == 0 {
		fmt.Printf("%sNo harnesses selected.%s\n", ansiDim, ansiReset)
		return nil, false
	}
	var toRemove []string
	for _, i := range indices {
		toRemove = append(toRemove, options[i].Value)
	}
	return toRemove, true
}

// removeHarnessSkillsDir deletes a harness's own skills directory, preserving any
// cherry-picked forks inside it. A harness's directory can be the project's forks
// directory (OpenClaw reads ./skills, mdm's default fork destination), and a
// fork is the project's source code - edits that exist nowhere else - not
// something mdm installed and may delete. Returns the number of forks kept, and
// whether anything was removed at all.
func removeHarnessSkillsDir(skillsPath string) (kept int, removed bool) {
	info, err := os.Lstat(skillsPath)
	if err != nil {
		return 0, false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(skillsPath)
		return 0, true
	}

	entries, err := os.ReadDir(skillsPath)
	if err != nil {
		_ = os.RemoveAll(skillsPath)
		return 0, true
	}
	for _, e := range entries {
		if e.IsDir() && fork.IsFork(filepath.Join(skillsPath, e.Name())) {
			kept++
		}
	}
	if kept == 0 {
		_ = os.RemoveAll(skillsPath)
		return 0, true
	}
	for _, e := range entries {
		path := filepath.Join(skillsPath, e.Name())
		if e.IsDir() && fork.IsFork(path) {
			continue
		}
		_ = os.RemoveAll(path)
	}
	return kept, true
}

func reportHarnessSkillsDirCleanup(skillsPath, displayName string) {
	kept, removed := removeHarnessSkillsDir(skillsPath)
	if !removed {
		return
	}
	if kept > 0 {
		ui.LogInfo(fmt.Sprintf("Cleaned %s skills directory - kept %d cherry-picked skill(s)", displayName, kept))
		return
	}
	ui.LogInfo("Removed " + displayName + " skills directory")
}

// cleanUpRemovedHarnessFiles removes the skills directory and instructions file
// that belong exclusively to each harness being removed. Shared resources
// (.agents/skills, AGENTS.md) are never touched.
func cleanUpRemovedHarnessFiles(toRemove []string, global bool, cwd string) {
	vlog(verboseFlag, "cleaning up files for removed harness(es): %v (global=%v)", toRemove, global)
	for _, name := range toRemove {
		cfg := harness.AllHarnesses[name]
		if cfg == nil {
			vlog(verboseFlag, "skip %q: unknown harness, no files to clean", name)
			continue
		}

		// Remove the harness's unique skills directory (skip shared .agents/skills).
		if !harness.UsesSharedSkillsDir(name) {
			var skillsPath string
			if global {
				skillsPath = cfg.GlobalSkillsDir
			} else {
				skillsPath = filepath.Join(cwd, cfg.SkillsDir)
			}
			if skillsPath != "" {
				reportHarnessSkillsDirCleanup(skillsPath, cfg.DisplayName)
			}
		}

		// Remove the harness's instructions file (project scope only; skip when
		// the harness has no unique instructions file or reads AGENTS.md natively).
		if !global && !cfg.NativeInstructions {
			instrPath := filepath.Join(cwd, cfg.InstructionsFile)
			if _, err := os.Lstat(instrPath); err == nil {
				_ = os.Remove(instrPath)
				ui.LogInfo("Removed " + cfg.InstructionsFile)
			}
		}
	}
}

// ─── Harness setup (auto-link rules + install locked skills) ──────────────────

// runHarnessSetup links instruction files to AGENTS.md and installs any already-
// locked skills for the newly configured harnesses. It is intentionally silent when
// there is nothing to do so the happy-path output stays clean.
func runHarnessSetup(harnessNames []string, cwd string) {
	linkNewHarnessRules(harnessNames, cwd)
	installLockedSkillsForHarnesses(harnessNames, cwd)
}

// linkNewHarnessRules links each harness's instruction file to AGENTS.md when
// AGENTS.md already exists as a real (non-symlink) file in the project directory.
// Instruction files that already exist as real files are skipped with a hint to
// run `mdm rules link` instead, to avoid silent data loss.
func linkNewHarnessRules(harnessNames []string, cwd string) {
	agentsMDPath := filepath.Join(cwd, agentsMDFile)
	info, err := os.Lstat(agentsMDPath)
	if err != nil || !info.Mode().IsRegular() {
		return
	}

	var toLink []harnessCandidate
	var skippedFiles []string
	for _, name := range harnessNames {
		cfg := harness.AllHarnesses[name]
		if cfg == nil || cfg.NativeInstructions {
			continue
		}
		targetPath := filepath.Join(cwd, cfg.InstructionsFile)
		targetInfo, statErr := os.Lstat(targetPath)
		if statErr == nil && targetInfo.Mode()&os.ModeSymlink == 0 {
			// Existing real file - skip to avoid silent data loss.
			skippedFiles = append(skippedFiles, cfg.InstructionsFile)
			continue
		}
		toLink = append(toLink, harnessCandidate{name: name, displayName: cfg.DisplayName, file: cfg.InstructionsFile})
	}

	if len(skippedFiles) > 0 {
		fmt.Println()
		for _, f := range skippedFiles {
			fmt.Printf("  %s~%s %-35s %sskipped (existing file - run `mdm rules link` to replace)%s\n",
				ansiYellow, ansiReset, f, ansiDim, ansiReset)
		}
	}

	if len(toLink) == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("%sLinking instruction files → %s%s\n", ansiText, agentsMDFile, ansiReset)
	fmt.Println()
	createHarnessSymlinks(toLink, cwd, agentsMDPath, true)
}

type skillLinkSpec struct {
	skillName   string
	harnessName string
	global      bool
}

// harnessesNeedingSkillLinks returns harnesses from harnessNames that have a unique
// (non-shared) skills directory and therefore need explicit skill linking.
func harnessesNeedingSkillLinks(harnessNames []string) []string {
	var result []string
	for _, name := range harnessNames {
		if !harness.UsesSharedSkillsDir(name) && harness.AllHarnesses[name] != nil {
			result = append(result, name)
		}
	}
	return result
}

// collectSkillLinkSpecs gathers (skill, harness, global) triples for skills that
// are recorded in either the project or global lock file but not yet installed
// for the given target harnesses.
func collectSkillLinkSpecs(targets []string, cwd string) []skillLinkSpec {
	var specs []skillLinkSpec
	localLk := lock.ReadLocalLock(cwd)
	for skillName := range localLk.Skills {
		for _, harnessName := range targets {
			if !isSkillInstalled(skillName, harnessName, false) {
				specs = append(specs, skillLinkSpec{skillName, harnessName, false})
			}
		}
	}
	globalLk := lock.ReadGlobalState()
	for skillName := range globalLk.Skills {
		for _, harnessName := range targets {
			a := harness.AllHarnesses[harnessName]
			if a == nil || a.GlobalSkillsDir == "" {
				continue
			}
			if !isSkillInstalled(skillName, harnessName, true) {
				specs = append(specs, skillLinkSpec{skillName, harnessName, true})
			}
		}
	}
	return specs
}

// installLockedSkillsForHarnesses installs all locked skills (from the project and
// global lock files) for harnesses that have a unique skills directory. Harnesses that
// use the shared .agents/skills directory already have access to every installed
// skill automatically and are skipped.
func installLockedSkillsForHarnesses(harnessNames []string, cwd string) {
	targets := harnessesNeedingSkillLinks(harnessNames)
	if len(targets) == 0 {
		vlog(verboseFlag, "no harnesses need explicit skill links (all use shared skills dir)")
		return
	}
	specs := collectSkillLinkSpecs(targets, cwd)
	vlog(verboseFlag, "linking locked skills: %d target harness(es), %d link spec(s)", len(targets), len(specs))
	if len(specs) == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("%sLinking skills from lock file...%s\n", ansiText, ansiReset)
	fmt.Println()

	succeeded := 0
	for _, spec := range specs {
		if !linkInstalledSkillToHarness(spec.skillName, spec.harnessName, spec.global, cwd) {
			continue
		}
		succeeded++
		harnessDisplay := spec.harnessName
		if cfg := harness.AllHarnesses[spec.harnessName]; cfg != nil {
			harnessDisplay = cfg.DisplayName
		}
		fmt.Printf("  %s✓%s %-35s → %s\n", ansiGreen, ansiReset, spec.skillName, harnessDisplay)
	}
	if succeeded > 0 {
		fmt.Println()
		ui.LogSuccess(fmt.Sprintf("Linked %d skill(s)", succeeded))
		fmt.Println()
	}
}

func printHarnessesSaved(harnesses []string, scope string) {
	fmt.Printf("%s✓%s Configured %d harness(es) for %s scope:\n", ansiGreen, ansiReset, len(harnesses), scope)
	for _, name := range harnesses {
		cfg := harness.AllHarnesses[name]
		displayName := name
		if cfg != nil {
			displayName = cfg.DisplayName
		}
		fmt.Printf("  %s%s%s\n", ansiText, displayName, ansiReset)
	}
}
