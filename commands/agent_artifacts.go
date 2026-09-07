// mdm agents manages agent-definition files: single markdown files that give a
// harness a named subagent persona, distinct from the prompt libraries
// `mdm skills` installs. The harness concept `mdm agents` named before this
// release is now `mdm harnesses`.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

// ─── Command tree ──────────────────────────────────────────────────────────────

func buildAgentArtifactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agent definitions for AI harnesses",
		Long: fmt.Sprintf(`Manage agent definitions — single markdown files that give a harness
a named subagent persona (e.g. Claude Code subagents), distinct from the
reusable prompt libraries %smdm skills%s installs.

%sExamples:%s
  mdm agents add owner/repo
  mdm agents add owner/repo --agent code-reviewer
  mdm agents list
  mdm agents remove code-reviewer`, ansiText, ansiReset, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}

	cmd.AddCommand(
		buildAgentAddCmd(),
		buildAgentListCmd(),
		buildAgentRemoveCmd(),
		buildAgentsUpdateCmd(),
		buildAgentsInstallCmd(),
	)

	return cmd
}

// AgentOptions holds the flags shared by the agent-definition subcommands.
type AgentOptions struct {
	Global           bool
	Project          bool
	Harnesses        []string // empty = prompt; "*" = all
	Agents           []string // empty = prompt; "*" = all
	Yes              bool
	AllowHiddenChars bool
}

// asAddOptions adapts AgentOptions to the AddOptions fields
// promptScopeAndHarnesses and commitScopeInstallMode read.
func (o AgentOptions) asAddOptions() AddOptions {
	return AddOptions{Global: o.Global, Project: o.Project, Harnesses: o.Harnesses, Yes: o.Yes}
}

// ─── add ────────────────────────────────────────────────────────────────────────

func buildAgentAddCmd() *cobra.Command {
	var opts AgentOptions

	cmd := &cobra.Command{
		Use:     "add <source>",
		Short:   "Add agent definitions from GitHub, a URL, or a local path",
		Aliases: []string{"a"},
		Long: fmt.Sprintf(`Add one or more agent-definition markdown files from GitHub, a URL,
or a local path.

The --harness and --agent (-a) flags accept multiple values. You can
pass them space-separated after the flag or repeat the flag for each value:

  mdm agents add owner/repo --harness claude-code cursor
  mdm agents add owner/repo --agent code-reviewer --agent test-writer

%sExamples:%s
  mdm agents add owner/repo
  mdm agents add owner/repo --agent code-reviewer
  mdm agents add owner/repo --harness claude-code cursor
  mdm agents add ./my-agents`, ansiBold, ansiReset),
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Nothing installed anywhere is a failed run. The exit lives
			// here, not in runAgentAdd, because the restore path calls
			// runAgentAdd once per source group and must survive an empty one.
			if !runAgentAdd(args[0], opts) {
				os.Exit(1)
			}
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Install globally (user-level)")
	f.BoolVarP(&opts.Project, "project", "p", false, "Force project-scope install")
	f.StringArrayVar(&opts.Harnesses, "harness", nil, "Harnesses to install to (repeatable, use '*' for all)")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Agent definition names to install (repeatable, use '*' for all)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")

	_ = cmd.RegisterFlagCompletionFunc("harness", harnessFlagCompletion)

	return cmd
}

// fetchAgentSource materializes sourceInput on disk and returns the directory
// to search, the git clone root for AgentPath bookkeeping (empty for a local
// path), and a cleanup func for any temp clone.
func fetchAgentSource(parsed source.ParsedSource, verbose bool) (searchRoot, cloneDir string, cleanup func()) {
	noop := func() {}
	switch parsed.Type {
	case source.SourceTypeLocal:
		if _, err := os.Stat(parsed.LocalPath); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Path not found: %s\n", ansiText, ansiReset, parsed.LocalPath)
			os.Exit(1)
		}
		return parsed.LocalPath, "", noop
	case source.SourceTypeWellKnown:
		fmt.Fprintf(os.Stderr, "%sError:%s well-known registries are not supported for agent definitions\n", ansiText, ansiReset)
		os.Exit(1)
		return "", "", noop
	default:
		tmpDir, err := cloneForAdd(parsed, parsed.Ref, AddOptions{Verbose: verbose})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err.Error())
			os.Exit(1)
		}
		return tmpDir, tmpDir, func() { _ = git.CleanupTempDir(tmpDir) }
	}
}

// runAgentAdd reports whether at least one definition reached at least one
// harness.
func runAgentAdd(sourceInput string, opts AgentOptions) bool {
	cwd, _ := os.Getwd()
	parsed := source.ParseSource(sourceInput)
	vlog(verboseFlag, "source %q → type=%s url=%s ref=%q subpath=%q",
		sourceInput, parsed.Type, parsed.URL, parsed.Ref, parsed.Subpath)
	fmt.Println()

	searchRoot, cloneDir, cleanup := fetchAgentSource(parsed, verboseFlag)
	defer cleanup()

	agents, err := agentfile.DiscoverAgentFiles(searchRoot, parsed.Subpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err)
		os.Exit(1)
	}
	if len(agents) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo agent definitions found in %s%s\n", ansiText, sourceInput, ansiReset)
		os.Exit(1)
	}

	selected, ok := selectAgents(agents, opts)
	if !ok {
		return false
	}

	// The hidden-character gate every install path runs before writing a byte.
	// It sits after selection and before the scope prompts: a blocked install
	// must not first talk the user through choosing harnesses.
	if !checkAgentFilesMarkdownForHiddenChars(selected, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	global, harnesses, ok := promptScopeAndHarnesses(opts.asAddOptions(), cwd)
	if !ok {
		return false
	}

	mode, ok := commitScopeInstallMode(opts.asAddOptions(), global, cwd)
	if !ok {
		return false
	}

	baseEntry := agentLockEntry(parsed, sourceInput)
	fmt.Println()
	outcome := installAgentsForHarnesses(selected, harnesses, global, mode, baseEntry, cloneDir, cwd)
	fmt.Println()
	printAgentInstallSummary(outcome, global, mode)
	return outcome.installed > 0
}

// filterAgentsByName keeps agents whose name matches one of names (by the
// same case/sanitized rule skills use).
func filterAgentsByName(agents []*agentfile.AgentFile, names []string) []*agentfile.AgentFile {
	var filtered []*agentfile.AgentFile
	for _, a := range agents {
		for _, f := range names {
			if skillNameMatches(a.Name, f) {
				filtered = append(filtered, a)
				break
			}
		}
	}
	return filtered
}

func selectAgents(agents []*agentfile.AgentFile, opts AgentOptions) ([]*agentfile.AgentFile, bool) {
	if len(opts.Agents) > 0 && opts.Agents[0] == "*" {
		return agents, true
	}
	if len(opts.Agents) > 0 {
		filtered := filterAgentsByName(agents, opts.Agents)
		if len(filtered) == 0 {
			fmt.Fprintf(os.Stderr, "%sNo matching agent definitions found.%s\n", ansiText, ansiReset)
			return nil, false
		}
		return filtered, true
	}
	if opts.Yes || len(agents) == 1 {
		return agents, true
	}
	options := make([]ui.UIOption, len(agents))
	for i, a := range agents {
		options[i] = ui.UIOption{Label: a.Name, Value: agentDiskName(a.Name), Hint: a.Description}
	}
	indices, ok := ui.UiSearchMultiselect("Which agent definitions would you like to install?", options, nil, nil, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []*agentfile.AgentFile
	for _, i := range indices {
		selected = append(selected, agents[i])
	}
	return selected, true
}

// agentLockEntry builds the source-level part of the lock entry shared by every
// definition installed from this invocation.
func agentLockEntry(parsed source.ParsedSource, sourceInput string) lock.AgentLockEntry {
	entry := lock.AgentLockEntry{
		Source:     stripSourceRef(sourceInput),
		SourceType: string(parsed.Type),
	}
	if parsed.Type == source.SourceTypeLocal {
		entry.Source = parsed.LocalPath
		return entry
	}
	entry.Ref = parsed.Ref
	if entry.Ref == "" {
		entry.Ref = git.DefaultBranch(parsed.URL)
	}
	return entry
}

// agentFileRepoPath returns the repo-relative path to a discovered agent
// definition file. cloneDir is the git clone root, empty for a local install.
func agentFileRepoPath(agentPath, cloneDir string) string {
	if cloneDir == "" || agentPath == "" {
		return ""
	}
	rel, err := filepath.Rel(cloneDir, agentPath)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// agentInstallOutcome is what an add run did. A definition can be skipped by
// every harness it was aimed at and install nowhere.
type agentInstallOutcome struct {
	installed int      // definitions that reached at least one harness
	harnesses []string // harnesses that actually received something, in the order given
	fallbacks *symlinkFallbacks
	// materialized is kept apart from fallbacks: a real file by design and a
	// real file because a symlink was refused have different causes and
	// different remedies, and merging them tells the user the wrong one.
	materialized *materializedInstalls
}

// installAgentsForHarnesses installs each selected definition into every
// requested harness, and records it in the lock only when at least one harness
// received it. A harness with no agent-definition directory recorded is a
// skip with a reason.
func installAgentsForHarnesses(agents []*agentfile.AgentFile, harnesses []string, global bool, mode InstallMode, baseEntry lock.AgentLockEntry, cloneDir, cwd string) agentInstallOutcome {
	var fallbacks symlinkFallbacks
	var materialized materializedInstalls
	outcome := agentInstallOutcome{fallbacks: &fallbacks, materialized: &materialized}
	received := map[string]bool{}

	for _, a := range agents {
		name := agentDiskName(a.Name)
		fmt.Printf("%sInstalling %s%s%s...\n", ansiDim, ansiText, a.Name, ansiReset)

		var failures agentFailures
		var skipReasons []string
		installedAny := false
		for _, harnessName := range harnesses {
			result := installAgentFile(a, harnessName, global, cwd, mode)
			fallbacks.note(harnessName, result)
			materialized.note(harnessName, result)
			failures.note(harnessName, result)
			switch {
			case result.Success:
				installedAny = true
				if !received[harnessName] {
					received[harnessName] = true
					outcome.harnesses = append(outcome.harnesses, harnessName)
				}
			case result.Skipped:
				skipReasons = append(skipReasons, result.Error)
			}
		}

		for _, reason := range skipReasons {
			ui.LogInfo(fmt.Sprintf("%s: skipped — %s", a.Name, reason))
		}

		if !installedAny {
			reportAgentFailure(a.Name, &failures)
			continue
		}
		if failures.any() {
			reportAgentFailure(a.Name, &failures)
		} else {
			ui.LogSuccess(a.Name)
		}
		outcome.installed++

		entry := baseEntry
		entry.AgentPath = agentFileRepoPath(a.Path, cloneDir)
		entry.Format = string(agentCanonicalFormat(a))

		if global {
			if err := lock.AddAgentToGlobalState(name, entry); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		} else {
			if err := lock.AddAgentToLocalLock(name, entry, cwd); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		}
	}
	return outcome
}

// printAgentInstallSummary is printInstallSummary's counterpart for agent
// definitions. The harness list names only the harnesses that received a file.
func printAgentInstallSummary(outcome agentInstallOutcome, global bool, mode InstallMode) {
	scope := "project"
	if global {
		scope = "global"
	}
	if outcome.installed == 0 {
		fmt.Printf("%sNo agent definitions were installed (%s scope).%s\n\n", ansiYellow, scope, ansiReset)
		outcome.fallbacks.warn()
		return
	}
	noun := "agent definition"
	if outcome.installed != 1 {
		noun = "agent definitions"
	}
	modeNote := string(mode)
	if outcome.fallbacks.any() {
		modeNote += " mode, copied where symlinks failed"
	} else {
		modeNote += " mode"
	}
	fmt.Printf("%s✓ Installed %d %s (%s scope, %s)%s\n", ansiText, outcome.installed, noun, scope, modeNote, ansiReset)
	if len(outcome.harnesses) > 0 {
		fmt.Printf("%s  Harnesses: %s%s\n", ansiDim, strings.Join(harnessDisplayNames(outcome.harnesses), ", "), ansiReset)
	}
	fmt.Println()
	outcome.materialized.explain()
	outcome.fallbacks.warn()
}

// ─── list ───────────────────────────────────────────────────────────────────────

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

// agentInstalledSomewhere reports whether any harness still has a copy, so a
// `--harness X` removal knows whether the canonical file and lock entry are
// still needed by harnesses outside the filter.
func agentInstalledSomewhere(name string, global bool, cwd string) bool {
	return len(agentInstalledHarnesses(name, global, cwd)) > 0
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
		fmt.Printf("Add one with %smdm agents add <source>%s\n\n", ansiText, ansiReset)
	}
}

// ─── remove ─────────────────────────────────────────────────────────────────────

func buildAgentRemoveCmd() *cobra.Command {
	var opts AgentOptions

	cmd := &cobra.Command{
		Use:     "remove [names...]",
		Short:   "Remove installed agent definitions",
		Aliases: []string{"rm", "r"},
		Long: fmt.Sprintf(`Remove installed agent definitions.

If no names are provided an interactive selection menu is shown.

%sExamples:%s
  mdm agents remove
  mdm agents remove code-reviewer
  mdm agents remove code-reviewer -y`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runAgentRemove(args, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Remove from global scope")
	f.BoolVarP(&opts.Project, "project", "p", false, "Remove from project scope")
	f.StringArrayVar(&opts.Harnesses, "harness", nil, "Remove from specific harnesses (repeatable)")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Agent definition names to remove (repeatable)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")

	_ = cmd.RegisterFlagCompletionFunc("harness", harnessFlagCompletion)

	return cmd
}

func resolveAgentRemoveScope(opts AgentOptions) (global bool, ok bool) {
	if opts.Global {
		return true, true
	}
	if opts.Project || opts.Yes {
		return false, true
	}
	idx, ok := ui.UiSelect("Which scope?", []ui.UIOption{
		{Label: "Project", Hint: "remove from this project"},
		{Label: "Global", Hint: "remove from your user account"},
	})
	if !ok {
		return false, false
	}
	return idx == 1, true
}

func selectAgentsToRemove(lockNames, filterNames []string, opts AgentOptions) ([]string, bool) {
	if len(filterNames) == 1 && filterNames[0] == "*" {
		return lockNames, true
	}
	if len(filterNames) > 0 {
		var keep []string
		for _, f := range filterNames {
			for _, n := range lockNames {
				if skillNameMatches(n, f) {
					keep = append(keep, n)
					break
				}
			}
		}
		if len(keep) == 0 {
			fmt.Fprintf(os.Stderr, "%sNo matching agent definitions found.%s\n", ansiText, ansiReset)
			return nil, false
		}
		return keep, true
	}
	if opts.Yes || len(lockNames) == 1 {
		return lockNames, true
	}
	options := make([]ui.UIOption, len(lockNames))
	for i, n := range lockNames {
		options[i] = ui.UIOption{Label: n, Value: n}
	}
	indices, ok := ui.UiSearchMultiselect("Which agent definitions would you like to remove?", options, nil, nil, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []string
	for _, i := range indices {
		selected = append(selected, lockNames[i])
	}
	return selected, true
}

// removeFileFn is removeAgentFromDisk's deletion seam: tests swap it for a
// failing version. Shared mutable state, so those tests must not run in
// parallel.
var removeFileFn = os.Remove

// removeAgentFromDisk deletes one definition's per-harness copy for every
// harness in harnessFilter (all harnesses when empty). It drops the canonical
// file and the lock entry only once no harness, including harnesses outside
// harnessFilter, still has a copy. It returns fullyRemoved=false with a nil
// error when the copies in scope went but the definition lives elsewhere.
func removeAgentFromDisk(name string, harnessFilter []string, format agentfile.Format, global bool, cwd string) (fullyRemoved bool, err error) {
	harnesses := harnessFilter
	if len(harnesses) == 0 {
		for n := range harness.AllHarnesses {
			harnesses = append(harnesses, n)
		}
	}

	var failed []string
	for _, harnessName := range harnesses {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target == "" {
			continue
		}
		if !isPathSafe(harness.AgentsInstallDirFor(harnessName, global, cwd), target) {
			continue
		}
		if rmErr := removeFileFn(target); rmErr != nil && !os.IsNotExist(rmErr) {
			failed = append(failed, fmt.Sprintf("%s (%v)", harnessName, rmErr))
		}
	}
	if len(failed) > 0 {
		return false, fmt.Errorf("could not remove from %s", strings.Join(failed, ", "))
	}

	if agentInstalledSomewhere(name, global, cwd) {
		return false, nil
	}

	canonicalDir := harness.CanonicalAgentsDir(global, cwd)
	canonicalPath := agentCanonicalPath(name, format, global, cwd)
	if isPathSafe(canonicalDir, canonicalPath) {
		if rmErr := removeFileFn(canonicalPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return false, fmt.Errorf("could not remove the canonical file: %w", rmErr)
		}
	}

	var lockErr error
	if global {
		lockErr = lock.RemoveAgentFromGlobalState(name)
	} else {
		lockErr = lock.RemoveAgentFromLocalLock(name, cwd)
	}
	if lockErr != nil {
		return false, fmt.Errorf("could not update lock file: %w", lockErr)
	}
	return true, nil
}

func runAgentRemove(positional []string, opts AgentOptions) {
	cwd, _ := os.Getwd()
	filterNames := append(append([]string{}, opts.Agents...), positional...)

	global, ok := resolveAgentRemoveScope(opts)
	if !ok {
		return
	}

	lockNames, lockEntries := agentLockEntries(global, cwd)
	if len(lockNames) == 0 {
		fmt.Printf("%sNo agent definitions installed.%s\n", ansiDim, ansiReset)
		return
	}

	toRemove, ok := selectAgentsToRemove(lockNames, filterNames, opts)
	if !ok || len(toRemove) == 0 {
		return
	}

	if !opts.Yes {
		confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %d agent definition(s): %s?", len(toRemove), strings.Join(toRemove, ", ")))
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
	}

	fmt.Println()
	for _, name := range toRemove {
		fullyRemoved, err := removeAgentFromDisk(name, opts.Harnesses, lockedAgentFormat(lockEntries[name]), global, cwd)
		switch {
		case err != nil:
			ui.LogError(fmt.Sprintf("%s: %v", name, err))
		case fullyRemoved:
			ui.LogSuccess("Removed " + name)
		default:
			ui.LogWarn(fmt.Sprintf("%s: removed from the given harness(es), but it is still installed elsewhere — keeping the definition and its lock entry", name))
		}
	}
	fmt.Println()
}

// ─── install (restore from lock) ───────────────────────────────────────────────

func buildAgentsInstallCmd() *cobra.Command {
	var opts restoreOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore agent definitions from " + lockName,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			restoreAgentsFromLock(opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&opts.allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	return cmd
}

// restoreAgentsFromLock installs every agent definition recorded in the local
// and global locks, mirroring restoreSkillsFromCurrentLock in install.go.
func restoreAgentsFromLock(opts restoreOptions) {
	cwd, _ := os.Getwd()

	localAgents := lock.ReadProjectLock(cwd).Agents
	globalAgents := lock.ReadGlobalState().Agents

	hasLocal := len(localAgents) > 0
	hasGlobal := len(globalAgents) > 0
	vlog(verboseFlag, "install from lock: local=%d agent(s) global=%d agent(s)", len(localAgents), len(globalAgents))

	switch {
	case !hasLocal && !hasGlobal:
		// A project with no agent definitions is a normal outcome.
		return

	case hasLocal && !hasGlobal:
		restoreAgentsMap(localAgents, false, opts, cwd)

	case !hasLocal && hasGlobal:
		if !opts.yes {
			msg := fmt.Sprintf("Found %d agent definition(s) in the global state file (%s). Install them?", len(globalAgents), lock.GetGlobalStatePath())
			confirmed, ok := ui.UiConfirm(msg)
			if !ok || !confirmed {
				return
			}
		}
		restoreAgentsMap(globalAgents, true, opts, cwd)

	default: // both scopes have agent definitions
		if opts.yes {
			restoreAgentsMap(localAgents, false, opts, cwd)
		} else {
			idx, ok := ui.UiSelect("Restore agent definitions from which lock file?", []ui.UIOption{
				{Label: fmt.Sprintf("Local  — %d agent definition(s)", len(localAgents)), Hint: lock.GetProjectLockPath(cwd)},
				{Label: fmt.Sprintf("Global — %d agent definition(s)", len(globalAgents)), Hint: lock.GetGlobalStatePath()},
			})
			if !ok {
				return
			}
			if idx == 1 {
				restoreAgentsMap(globalAgents, true, opts, cwd)
			} else {
				restoreAgentsMap(localAgents, false, opts, cwd)
			}
		}
	}
}

// restoreAgentsMap groups entries by source and calls runAgentAdd once per
// group, so a repo holding many definitions is cloned once per restore.
func restoreAgentsMap(entries map[string]lock.AgentLockEntry, global bool, opts restoreOptions, cwd string) {
	fmt.Printf("\n%sRestoring %d agent definition(s)...%s\n\n", ansiText, len(entries), ansiReset)

	refs := make(map[string]sourceRef, len(entries))
	for name, e := range entries {
		refs[name] = sourceRef{source: e.Source, ref: e.Ref}
	}
	groups := groupBySourceRef(refs)

	baseOpts := AgentOptions{Yes: opts.yes, AllowHiddenChars: opts.allowHiddenChars}
	if global {
		baseOpts.Global = true
	} else {
		baseOpts.Project = true
	}

	// Resolve harnesses once so the user is not prompted for each source group.
	harnesses, ok := promptHarnesses(baseOpts.asAddOptions(), global, cwd)
	if !ok {
		fmt.Println("Cancelled.")
		return
	}
	baseOpts.Harnesses = harnesses

	vlog(verboseFlag, "grouped %d agent definition(s) into %d source group(s)", len(entries), len(groups))
	for _, group := range groups {
		vlog(verboseFlag, "restoring agent definitions from %q (ref=%q): %v", group.source, group.ref, group.names)
		fmt.Printf("%sInstalling from %s...%s\n", ansiDim, group.source, ansiReset)
		groupOpts := baseOpts
		groupOpts.Agents = group.names
		src := group.source
		if group.ref != "" && !strings.Contains(src, "#") {
			src = src + "#" + group.ref
		}
		_ = runAgentAdd(src, groupOpts)
	}

	fmt.Printf("%sDone.%s\n\n", ansiText, ansiReset)
}

// ─── update ─────────────────────────────────────────────────────────────────────

func buildAgentsUpdateCmd() *cobra.Command {
	var opts UpdateOptions

	cmd := &cobra.Command{
		Use:   "update [names...]",
		Short: "Update installed agent definitions",
		Long: fmt.Sprintf(`Update installed agent definitions to their latest versions.

Definitions that share a source repository and target ref are re-fetched
together, so a repo holding many agent definitions is cloned once per
update run rather than once per definition. Every harness a definition is
currently installed to is refreshed — not just the canonical copy — so a
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
			installedHarnesses := agentInstalledHarnesses(name, global, cwd)
			if len(installedHarnesses) == 0 {
				ui.LogWarn(fmt.Sprintf("%s: not installed in any harness, skipping", a.Name))
				continue
			}

			installedAny, failedHarnesses := reinstallAgentIntoHarnesses(a, installedHarnesses, global, cwd, mode)

			// The lock must describe the disk. If every harness install
			// failed, nothing changed on disk.
			if !installedAny {
				ui.LogWarn(fmt.Sprintf("%s: update failed for every installed harness (%s) — lock entry left unchanged", a.Name, strings.Join(failedHarnesses, ", ")))
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
