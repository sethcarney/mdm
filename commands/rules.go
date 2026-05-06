package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

const agentsMDFile = "AGENTS.md"

func buildRulesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Manage agent instruction files",
		Long: fmt.Sprintf(`Manage project-level instruction files for AI agents.

%sAGENTS.md%s is the universal source of truth — read natively by Codex, Gemini CLI,
OpenCode, and Replit. Use %smdm rules link%s to symlink agent-specific files
(CLAUDE.md, .cursorrules, .windsurfrules, etc.) to it so every tool sees the
same instructions.

%sSubcommands:%s
  link    Set up AGENTS.md as the source of truth and symlink agent files to it
  status  Show the current state of all agent instruction files
  unlink  Remove symlinks created by mdm rules link`, ansiBold, ansiReset, ansiBold, ansiReset, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(
		buildRulesLinkCmd(),
		buildRulesStatusCmd(),
		buildRulesUnlinkCmd(),
	)

	return cmd
}

// ─── link ─────────────────────────────────────────────────────────────────────

func buildRulesLinkCmd() *cobra.Command {
	var agentFilter []string
	var yes bool

	cmd := &cobra.Command{
		Use:   "link",
		Short: "Set up AGENTS.md as the source of truth for all agent rules",
		Long: fmt.Sprintf(`Interactively set up AGENTS.md as the single source of truth.

You will be prompted to select which AI tools you use. The command then:

  1. Checks whether any of your agent instruction files already have content
  2. If one does — promotes its content into AGENTS.md
  3. If several do — asks which one to use as the source
  4. Symlinks all agent-specific files (CLAUDE.md, .cursorrules, etc.) → AGENTS.md

Existing real files are replaced with symlinks only after confirmation (or with -y).

%sExamples:%s
  mdm rules link
  mdm rules link --agent claude-code cursor
  mdm rules link -y`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runRulesLink(agentFilter, yes)
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&agentFilter, "agent", "a", nil, "Skip prompt and link specific agents (repeatable)")
	f.BoolVarP(&yes, "yes", "y", false, "Replace existing real files without prompting")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

// agentCandidate is an agent that has a non-AGENTS.md instruction file.
type agentCandidate struct {
	name        string
	displayName string
	file        string
}

// ruleFile holds an existing instruction file with its content and display metadata.
type ruleFile struct {
	file        string
	agentLabels []string
	preview     string
	content     []byte
}

func scanForExistingRuleFiles(cwd string) []ruleFile {
	seen := map[string]bool{}
	fileAgentLabels := map[string][]string{}
	fileContent := map[string][]byte{}

	for name, a := range agent.AllAgents {
		if a.NativeInstructions {
			continue
		}
		fullPath := filepath.Join(cwd, a.InstructionsFile)
		info, err := os.Lstat(fullPath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil || len(strings.TrimSpace(string(data))) == 0 {
			continue
		}
		fileAgentLabels[a.InstructionsFile] = append(fileAgentLabels[a.InstructionsFile], agent.AllAgents[name].DisplayName)
		if !seen[a.InstructionsFile] {
			seen[a.InstructionsFile] = true
			fileContent[a.InstructionsFile] = data
		}
	}

	var found []ruleFile
	for file := range seen {
		data := fileContent[file]
		preview := strings.TrimSpace(string(data))
		if nl := strings.Index(preview, "\n"); nl > 0 {
			preview = preview[:nl]
		}
		if len(preview) > 55 {
			preview = preview[:52] + "..."
		}
		sort.Strings(fileAgentLabels[file])
		found = append(found, ruleFile{file: file, agentLabels: fileAgentLabels[file], preview: preview, content: data})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].file < found[j].file })
	return found
}

func pickAndWriteSourceOfTruth(found []ruleFile, agentsMDPath string) bool {
	const noneLabel = "None of these — start with an empty AGENTS.md"
	opts := make([]ui.UIOption, 0, len(found)+1)
	for _, f := range found {
		hint := strings.Join(f.agentLabels, ", ")
		if f.preview != "" {
			hint = hint + " · " + f.preview
		}
		opts = append(opts, ui.UIOption{Label: f.file, Value: f.file, Hint: hint})
	}
	opts = append(opts, ui.UIOption{Label: noneLabel, Value: "__none__"})

	idx, ok := ui.UiSelect("Which file contains your current rules?", opts)
	if !ok {
		fmt.Println("Cancelled.")
		return false
	}
	fmt.Println()

	var content []byte
	var successMsg string
	if opts[idx].Value != "__none__" {
		content = found[idx].content
		successMsg = fmt.Sprintf("Copied %s → %s", found[idx].file, agentsMDFile)
	} else {
		successMsg = "Created empty " + agentsMDFile
	}
	if err := os.WriteFile(agentsMDPath, content, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s could not write %s: %v\n", ansiRed, ansiReset, agentsMDFile, err)
		return false
	}
	ui.LogSuccess(successMsg)
	fmt.Println()
	return true
}

func resolveSourceOfTruth(cwd, agentsMDPath string) bool {
	if info, err := os.Lstat(agentsMDPath); err == nil && info.Mode()&os.ModeSymlink == 0 {
		data, _ := os.ReadFile(agentsMDPath)
		if len(strings.TrimSpace(string(data))) > 0 {
			fmt.Println()
			ui.LogSuccess("AGENTS.md already set up as source of truth")
			fmt.Println()
			return true
		}
	}

	found := scanForExistingRuleFiles(cwd)
	fmt.Println()

	if len(found) == 0 {
		fmt.Printf("  %s•%s No existing instruction files found in this project.\n", ansiDim, ansiReset)
		fmt.Println()
		if err := os.WriteFile(agentsMDPath, []byte(""), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s could not create %s: %v\n", ansiRed, ansiReset, agentsMDFile, err)
			return false
		}
		ui.LogSuccess("Created empty " + agentsMDFile)
		fmt.Println()
		return true
	}

	return pickAndWriteSourceOfTruth(found, agentsMDPath)
}

func buildLinkableCandidates() ([]agentCandidate, []ui.UIOption) {
	var linkable []agentCandidate
	var lockedOptions []ui.UIOption
	for name, a := range agent.AllAgents {
		if a.InstructionsFile == "" {
			continue
		}
		if a.NativeInstructions {
			lockedOptions = append(lockedOptions, ui.UIOption{Label: a.DisplayName, Value: name, Hint: "reads AGENTS.md natively"})
			continue
		}
		linkable = append(linkable, agentCandidate{name: name, displayName: a.DisplayName, file: a.InstructionsFile})
	}
	sort.Slice(linkable, func(i, j int) bool { return linkable[i].displayName < linkable[j].displayName })
	sort.Slice(lockedOptions, func(i, j int) bool { return lockedOptions[i].Label < lockedOptions[j].Label })
	return linkable, lockedOptions
}

func selectAgentsToLink(agentFilter []string, linkable []agentCandidate, lockedOptions []ui.UIOption, cwd string) ([]agentCandidate, bool) {
	if len(agentFilter) > 0 {
		var selected []agentCandidate
		for _, c := range linkable {
			if contains(agentFilter, c.name) {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			fmt.Printf("%sNo matching agents found.%s\n", ansiDim, ansiReset)
			return nil, false
		}
		return selected, true
	}

	// Configured agents (project then global) take precedence over auto-detection.
	// If the user has explicitly configured their agents, pre-select only those;
	// otherwise fall back to whatever is detected as installed.
	configuredSet := make(map[string]bool)
	for _, a := range lock.GetConfiguredAgents(false, cwd) {
		configuredSet[a] = true
	}
	for _, a := range lock.GetConfiguredAgents(true, cwd) {
		configuredSet[a] = true
	}

	preSelected := make([]int, 0)
	if len(configuredSet) > 0 {
		for i, c := range linkable {
			if configuredSet[c.name] {
				preSelected = append(preSelected, i)
			}
		}
	} else {
		for i, c := range linkable {
			a := agent.AllAgents[c.name]
			if a.DetectInstalled != nil && a.DetectInstalled() {
				preSelected = append(preSelected, i)
			}
		}
	}
	options := make([]ui.UIOption, len(linkable))
	for i, c := range linkable {
		options[i] = ui.UIOption{Label: c.displayName, Value: c.name, Hint: c.file}
	}
	indices, ok := ui.UiSearchMultiselect("Which AI tools are you using in this project?", options, lockedOptions, preSelected)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []agentCandidate
	for _, i := range indices {
		selected = append(selected, linkable[i])
	}
	return selected, true
}

func createAgentSymlinks(selected []agentCandidate, cwd, agentsMDPath string, yes bool) {
	linked := 0
	skipped := 0
	for _, c := range selected {
		targetPath := filepath.Join(cwd, c.file)

		// Ensure parent directory exists (e.g. .github/ for Copilot).
		symlinkDir := filepath.Dir(targetPath)
		if symlinkDir != cwd {
			if err := os.MkdirAll(symlinkDir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "  %s✗%s %-35s could not create parent dir: %v\n", ansiRed, ansiReset, c.file, err)
				continue
			}
		}

		// Compute the symlink target relative to the file's own directory so
		// that files in subdirectories (e.g. .github/copilot-instructions.md)
		// resolve correctly: ../AGENTS.md rather than the broken AGENTS.md.
		symlinkTarget, err := filepath.Rel(symlinkDir, agentsMDPath)
		if err != nil {
			symlinkTarget = agentsMDFile
		}

		info, statErr := os.Lstat(targetPath)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				dest, _ := os.Readlink(targetPath)
				if dest == symlinkTarget {
					fmt.Printf("  %s→%s %-35s %salready linked%s\n", ansiDim, ansiReset, c.file, ansiDim, ansiReset)
					skipped++
					continue
				}
				os.Remove(targetPath)
			} else {
				if !yes {
					confirmed, ok := ui.UiConfirm(fmt.Sprintf("Replace %s (real file) with a symlink?", c.file))
					if !ok || !confirmed {
						fmt.Printf("  %s~%s %-35s %sskipped%s\n", ansiYellow, ansiReset, c.file, ansiDim, ansiReset)
						skipped++
						continue
					}
					fmt.Println()
				}
				os.Remove(targetPath)
			}
		}

		if err := os.Symlink(symlinkTarget, targetPath); err != nil {
			fmt.Fprintf(os.Stderr, "  %s✗%s %-35s %v\n", ansiRed, ansiReset, c.file, err)
			continue
		}
		fmt.Printf("  %s✓%s %-35s → %s%s%s\n", ansiGreen, ansiReset, c.file, ansiCyan, symlinkTarget, ansiReset)
		linked++
	}

	fmt.Println()
	if linked > 0 {
		fmt.Printf("%sLinked %d file(s) → %s%s\n", ansiText, linked, agentsMDFile, ansiReset)
	}
	if skipped > 0 {
		fmt.Printf("%s%d file(s) skipped%s\n", ansiDim, skipped, ansiReset)
	}
	fmt.Println()
}

func runRulesLink(agentFilter []string, yes bool) {
	cwd, _ := os.Getwd()
	agentsMDPath := filepath.Join(cwd, agentsMDFile)

	if !resolveSourceOfTruth(cwd, agentsMDPath) {
		return
	}

	linkable, lockedOptions := buildLinkableCandidates()
	selected, ok := selectAgentsToLink(agentFilter, linkable, lockedOptions, cwd)
	if !ok {
		return
	}
	if len(selected) == 0 {
		fmt.Printf("%sNo agents selected.%s\n", ansiDim, ansiReset)
		return
	}

	// Persist the selection into configuredAgents so skills add and other
	// commands default to the same set. Only applies when the user went
	// through the interactive picker (not --agent flag).
	if len(agentFilter) == 0 {
		var names []string
		for _, c := range selected {
			names = append(names, c.name)
		}
		_ = lock.AddToConfiguredAgents(names, false, cwd)
	}

	fmt.Println()
	createAgentSymlinks(selected, cwd, agentsMDPath, yes)
}

// ─── status ───────────────────────────────────────────────────────────────────

func buildRulesStatusCmd() *cobra.Command {
	var agentFilter []string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state of agent instruction files",
		Long: fmt.Sprintf(`Show whether each agent's instruction file exists, is a symlink,
or is missing.

%sExamples:%s
  mdm rules status
  mdm rules status --agent claude-code cursor`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runRulesStatus(agentFilter)
		},
	}

	cmd.Flags().StringArrayVarP(&agentFilter, "agent", "a", nil, "Limit to specific agents (repeatable)")
	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

func runRulesStatus(agentFilter []string) {
	cwd, _ := os.Getwd()

	// Group by instruction file so shared entries (e.g. AGENTS.md) appear once.
	fileAgents := map[string][]string{}
	for name, a := range agent.AllAgents {
		if a.InstructionsFile == "" {
			continue
		}
		if len(agentFilter) > 0 && !contains(agentFilter, name) {
			continue
		}
		fileAgents[a.InstructionsFile] = append(fileAgents[a.InstructionsFile], a.DisplayName)
	}

	if len(fileAgents) == 0 {
		fmt.Printf("%sNo agents with known instruction files.%s\n", ansiDim, ansiReset)
		return
	}

	files := make([]string, 0, len(fileAgents))
	for f := range fileAgents {
		files = append(files, f)
	}
	sort.Strings(files)

	fmt.Println()
	fmt.Printf("  %s%-38s %-12s %s%s\n", ansiBold, "File", "State", "Details", ansiReset)
	fmt.Printf("  %s%s%s\n", ansiDim, strings.Repeat("─", 72), ansiReset)

	for _, file := range files {
		agents := fileAgents[file]
		sort.Strings(agents)
		targetPath := filepath.Join(cwd, file)

		var stateLabel, hint string
		info, err := os.Lstat(targetPath)
		if os.IsNotExist(err) {
			stateLabel = fmt.Sprintf("%smissing%s", ansiDim, ansiReset)
		} else if info.Mode()&os.ModeSymlink != 0 {
			dest, _ := os.Readlink(targetPath)
			if _, e2 := os.Stat(targetPath); e2 != nil {
				stateLabel = fmt.Sprintf("%sbroken%s", ansiRed, ansiReset)
				hint = fmt.Sprintf("→ %s (target missing)", dest)
			} else {
				stateLabel = fmt.Sprintf("%slinked%s", ansiGreen, ansiReset)
				hint = fmt.Sprintf("→ %s%s%s", ansiCyan, dest, ansiReset)
			}
		} else {
			stateLabel = fmt.Sprintf("%sreal file%s", ansiYellow, ansiReset)
		}

		agentList := strings.Join(agents, ", ")
		if len(agentList) > 30 {
			agentList = agentList[:27] + "..."
		}

		fmt.Printf("  %-38s %-22s %s\n", file, stateLabel, hint)
		fmt.Printf("  %sagents: %s%s\n\n", ansiDim, agentList, ansiReset)
	}
}

// ─── unlink ───────────────────────────────────────────────────────────────────

func buildRulesUnlinkCmd() *cobra.Command {
	var agentFilter []string
	var yes bool

	cmd := &cobra.Command{
		Use:   "unlink",
		Short: "Remove symlinks from agent instruction files",
		Long: fmt.Sprintf(`Remove symlinks that were created by %smdm rules link%s.
Only symlinks are removed — real files are never touched.

%sExamples:%s
  mdm rules unlink
  mdm rules unlink --agent cursor windsurf
  mdm rules unlink -y`, ansiBold, ansiReset, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runRulesUnlink(agentFilter, yes)
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&agentFilter, "agent", "a", nil, "Limit to specific agents (repeatable)")
	f.BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

type symlinkedFile struct {
	file string
	dest string
}

func collectSymlinkedFiles(cwd string, agentFilter []string) []symlinkedFile {
	var result []symlinkedFile
	seen := map[string]bool{}
	for name, a := range agent.AllAgents {
		if a.InstructionsFile == "" {
			continue
		}
		if len(agentFilter) > 0 && !contains(agentFilter, name) {
			continue
		}
		targetPath := filepath.Join(cwd, a.InstructionsFile)
		info, err := os.Lstat(targetPath)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if seen[a.InstructionsFile] {
			continue
		}
		seen[a.InstructionsFile] = true
		dest, _ := os.Readlink(targetPath)
		result = append(result, symlinkedFile{file: a.InstructionsFile, dest: dest})
	}
	return result
}

func runRulesUnlink(agentFilter []string, yes bool) {
	cwd, _ := os.Getwd()
	found := collectSymlinkedFiles(cwd, agentFilter)

	if len(found) == 0 {
		fmt.Printf("%sNo symlinked instruction files found.%s\n", ansiDim, ansiReset)
		return
	}

	sort.Slice(found, func(i, j int) bool { return found[i].file < found[j].file })

	// When no --agent filter and not --yes, let the user pick which symlinks to remove.
	var toRemove []symlinkedFile
	if len(agentFilter) == 0 && !yes {
		options := make([]ui.UIOption, len(found))
		for i, r := range found {
			options[i] = ui.UIOption{Label: r.file, Value: r.file, Hint: "→ " + r.dest}
		}
		indices, ok := ui.UiMultiselect("Which symlinks would you like to remove?", options, false, nil, nil)
		if !ok {
			fmt.Println("Cancelled.")
			return
		}
		if len(indices) == 0 {
			fmt.Printf("%sNo files selected.%s\n", ansiDim, ansiReset)
			return
		}
		for _, i := range indices {
			toRemove = append(toRemove, found[i])
		}
	} else {
		toRemove = found
	}

	fmt.Println()
	for _, r := range toRemove {
		fmt.Printf("  %s%s%s → %s\n", ansiYellow, r.file, ansiReset, r.dest)
	}
	fmt.Println()

	if !yes {
		confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d symlink(s)?", len(toRemove)))
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
		fmt.Println()
	}

	for _, r := range toRemove {
		if err := os.Remove(filepath.Join(cwd, r.file)); err != nil {
			ui.LogError("Failed to remove " + r.file + ": " + err.Error())
			continue
		}
		ui.LogSuccess("Removed " + r.file)
	}
	fmt.Println()
}
