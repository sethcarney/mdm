// `mdm agents add`: fetch a source, discover its definitions, install each one
// into every selected harness, and record the lock entry.
package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

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
	f.BoolVar(&opts.Copy, "copy", false, "Copy files instead of symlinking (switches the scope to copy mode)")
	f.BoolVar(&opts.Symlink, "symlink", false, "Symlink files from .agents/agents (the default; switches a scope back from copy mode)")
	// The install mode is one switch with two settings, so asking for
	// both is a contradiction rather than a precedence puzzle.
	cmd.MarkFlagsMutuallyExclusive("copy", "symlink")

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
	// `mdm agents add cursor` was how a harness was configured before this
	// release. A bare harness name is never a source, so it would otherwise
	// fail as a git clone of a repository called "cursor".
	if harness.AllHarnesses[sourceInput] != nil {
		fmt.Fprintf(os.Stderr, "%s%s is a harness, not a source of agent definitions.%s\n", ansiText, sourceInput, ansiReset)
		fmt.Fprintf(os.Stderr, "Harness management moved to %smdm harnesses add %s%s in this release; %smdm agents add <source>%s installs agent definitions.\n",
			ansiText, sourceInput, ansiReset, ansiText, ansiReset)
		os.Exit(1)
	}
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

// agentNameCharsFor returns the regex character class harnessName's
// documented AgentNamePattern allows. Letters and hyphens are always in it;
// digits and underscores are added only when the pattern's own text says so.
// Empty means no documented pattern, so mdm has nothing to check against.
func agentNameCharsFor(pattern string) string {
	if pattern == "" {
		return ""
	}
	chars := "a-z-"
	if strings.Contains(pattern, "digit") {
		chars += "0-9"
	}
	if strings.Contains(pattern, "underscore") {
		chars += "_"
	}
	return chars
}

// warnAgentNamePattern warns once, naming every target harness whose
// documented naming rule this definition's frontmatter name does not
// satisfy. mdm does not rewrite the name - the source owns it, which is what
// keeps a symlink install possible - so the install still happens; this only
// says what to fix.
func warnAgentNamePattern(rawName string, harnesses []string) {
	var bad []string
	for _, harnessName := range harnesses {
		cfg := harness.AllHarnesses[harnessName]
		if cfg == nil {
			continue
		}
		chars := agentNameCharsFor(cfg.AgentNamePattern)
		if chars == "" || regexp.MustCompile("^["+chars+"]+$").MatchString(rawName) {
			continue
		}
		bad = append(bad, fmt.Sprintf("%s (%s)", cfg.DisplayName, cfg.AgentNamePattern))
	}
	if len(bad) == 0 {
		return
	}
	ui.LogWarn(fmt.Sprintf("%s: its name will not satisfy %s", rawName, strings.Join(bad, ", ")))
}

// canonicalRollback returns the undo for the canonical copy this run is about
// to write for one definition. installAgentFile writes that copy before the
// first harness write, so a definition no harness accepts after that point
// would otherwise leave a file at .agents/agents/<name> that no lock entry
// names: `agents list` and `agents remove` read the lock and cannot see it,
// `mdm agents add .` rediscovers it as a source, and checkAgentFormatCollision
// refuses the name in the other format forever, pointing at a remove that
// reports the name is not installed.
//
// A canonical file already on disk is not this run's to delete. It belongs to
// an earlier install the lock still names, or it is the source itself, which is
// what discovery hands back for `mdm agents add .`.
func canonicalRollback(a *agentfile.AgentFile, name string, global bool, cwd string) func() {
	path := agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd)
	if _, err := os.Stat(path); err == nil {
		return func() {}
	}
	return func() { _ = removeFileFn(path) }
}

// installAgentsForHarnesses installs each selected definition into every
// requested harness, and records it in the lock only when at least one harness
// received it. A harness with no agent-definition directory recorded is a
// skip with a reason. A definition no harness accepted leaves nothing behind.
func installAgentsForHarnesses(agents []*agentfile.AgentFile, harnesses []string, global bool, mode InstallMode, baseEntry lock.AgentLockEntry, cloneDir, cwd string) agentInstallOutcome {
	var fallbacks symlinkFallbacks
	var materialized materializedInstalls
	outcome := agentInstallOutcome{fallbacks: &fallbacks, materialized: &materialized}
	received := map[string]bool{}
	claimed := map[string]string{} // disk name -> the source that claimed it

	for _, a := range agents {
		name := agentDiskName(a.Name)
		// Two names can sanitize to one file name ("Code Reviewer" and
		// "code-reviewer"). Installing both would write one canonical file and
		// count two, so the name belongs to whichever source claimed it first.
		if prior, ok := claimed[name]; ok {
			ui.LogWarn(fmt.Sprintf("%s: %s and %s both install as %s - skipping the second, rename one of them", a.Name, prior, a.Path, name))
			continue
		}
		claimed[name] = a.Path
		fmt.Printf("%sInstalling %s%s%s...\n", ansiDim, ansiText, a.Name, ansiReset)
		warnAgentNamePattern(a.Name, harnesses)
		rollback := canonicalRollback(a, name, global, cwd)

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
			ui.LogInfo(fmt.Sprintf("%s: skipped - %s", a.Name, reason))
		}

		if !installedAny {
			rollback()
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
