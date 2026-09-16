// `mdm agents add`: fetch a source, discover its definitions, install each one
// into every selected harness, and record the lock entry.
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

func buildAgentAddCmd() *cobra.Command {
	var opts AgentOptions

	cmd := &cobra.Command{
		Use:     "add <source>",
		Short:   "Add agent definitions from GitHub, a URL, or a local path",
		Aliases: []string{"a"},
		Long: fmt.Sprintf(`Add one or more agent-definition files (markdown, or Codex TOML) from GitHub, a URL,
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
		// `mdm agents add claude-code cursor` was how harnesses were
		// configured before this release. cobra's "accepts 1 arg(s),
		// received 2" says nothing about where that went, so a list made
		// only of harness names gets the hint instead.
		Args: func(cmd *cobra.Command, args []string) error {
			if allHarnessNames(args) {
				printHarnessNamesHint(args, "add", "a source of agent definitions", "<source>", "installs")
				os.Exit(1)
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
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
	f.BoolVar(&opts.Force, "force", false, "Replace a definition already installed under the same name from another source")
	// The install mode is one switch with two settings, so asking for
	// both is a contradiction rather than a precedence puzzle.
	cmd.MarkFlagsMutuallyExclusive("copy", "symlink")

	_ = cmd.RegisterFlagCompletionFunc("harness", harnessFlagCompletion)

	return cmd
}

// fetchAgentSource materializes sourceInput on disk and returns the directory
// to search, the root AgentPath is recorded relative to - the git clone for a
// remote source, the local directory itself for a local one - and a cleanup
// func for any temp clone. A local install records the path too: it is what
// lets `mdm agents remove` tell the one file `mdm agents add .` discovered
// from the harness files mdm then wrote beside it.
func fetchAgentSource(parsed source.ParsedSource, verbose bool) (searchRoot, rootDir string, cleanup func()) {
	noop := func() {}
	switch parsed.Type {
	case source.SourceTypeLocal:
		if _, err := os.Stat(parsed.LocalPath); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Path not found: %s\n", ansiText, ansiReset, parsed.LocalPath)
			os.Exit(1)
		}
		return parsed.LocalPath, parsed.LocalPath, noop
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
		printHarnessNamesHint([]string{sourceInput}, "add", "a source of agent definitions", "<source>", "installs")
		os.Exit(1)
	}
	parsed := source.ParseSource(sourceInput)
	vlog(verboseFlag, "source %q → type=%s url=%s ref=%q subpath=%q",
		sourceInput, parsed.Type, parsed.URL, parsed.Ref, parsed.Subpath)
	fmt.Println()

	searchRoot, rootDir, cleanup := fetchAgentSource(parsed, verboseFlag)
	defer cleanup()

	// Reported, not exited: the restore path calls this once per source
	// group and has to survive one that no longer holds anything.
	agents, err := agentfile.DiscoverAgentFiles(searchRoot, parsed.Subpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err)
		return false
	}
	if len(agents) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo agent definitions found in %s%s\n", ansiText, sourceInput, ansiReset)
		fmt.Fprintf(os.Stderr, "%sLooked in the directory itself, any agentsDirs it declares, and %s.%s\n", ansiDim, strings.Join(agentfile.ConventionalDirs, ", "), ansiReset)
		return false
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

	global, harnesses, ok := promptAgentScopeAndHarnesses(opts, cwd)
	if !ok {
		return false
	}

	mode, ok := commitScopeInstallMode(opts.asAddOptions(), global, cwd)
	if !ok {
		return false
	}

	baseEntry := agentLockEntry(parsed, sourceInput)
	fmt.Println()
	outcome := installAgents(selected, harnesses, global, mode, baseEntry, rootDir, cwd, agentInstallRun{harnessesFor: opts.HarnessesFor, force: opts.Force})
	fmt.Println()
	printAgentInstallSummary(outcome, global, mode)
	// A definition mdm already owns is not a failure to install it. A refused
	// one is, even alongside definitions that landed: a batch where a name
	// was refused must not read, to a script, like one where every name went
	// in.
	return outcome.refused == 0 && (outcome.installed > 0 || outcome.alreadyInstalled > 0)
}

// promptAgentScopeAndHarnesses resolves the scope the way promptScopeAndHarnesses
// does for skills, then picks harnesses with promptAgentHarnesses.
func promptAgentScopeAndHarnesses(opts AgentOptions, cwd string) (bool, []string, bool) {
	global := opts.Global
	if !global && !opts.Project && !opts.Yes {
		idx, ok := ui.UiSelect("Install scope?", []ui.UIOption{
			{Label: "Project", Hint: "installs for this project only"},
			{Label: "Global", Hint: "installs for your user account"},
		})
		if !ok {
			return false, nil, false
		}
		global = idx == 1
	}
	harnesses, ok := promptAgentHarnesses(opts, global, cwd)
	if !ok {
		return false, nil, false
	}
	return global, harnesses, true
}

// agentCapableHarnesses lists, sorted, every harness with an agent-definition
// directory recorded for the scope. It is the whole universe of the agents
// picker: a harness without one is a skip on install, so offering it only
// produces a line saying so.
func agentCapableHarnesses(global bool, cwd string) []string {
	var out []string
	for name := range harness.AllHarnesses {
		if harness.AgentsInstallDirFor(name, global, cwd) != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// promptAgentHarnesses returns the harnesses an agent install targets. It is
// not the skills picker: that one is shaped by skills directories, locks the
// shared-directory harnesses (which is most of the ones that take agent
// definitions, so Codex could never be deselected and every interactive run
// materialized a TOML file), and saves the selection as the scope's skills
// defaults. This picker offers exactly the harnesses that can take a
// definition, locks none of them, and records nothing; the configured skills
// list is only the default selection.
func promptAgentHarnesses(opts AgentOptions, global bool, cwd string) ([]string, bool) {
	if len(opts.Harnesses) > 0 && opts.Harnesses[0] == "*" {
		return agentCapableHarnesses(global, cwd), true
	}
	if len(opts.Harnesses) > 0 {
		return validateNamedHarnesses(opts.Harnesses)
	}
	capable := agentCapableHarnesses(global, cwd)
	if len(capable) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo harness has an agent-definition directory recorded for this scope.%s\n", ansiText, ansiReset)
		return nil, false
	}
	detected := stringSet(harness.DetectInstalledHarnesses())
	configured := stringSet(lock.GetConfiguredHarnesses(global, cwd))

	preferred := keepIn(capable, configured)
	if len(preferred) == 0 {
		preferred = keepIn(capable, detected)
	}
	if opts.Yes {
		// Nothing configured and nothing detected: every harness that can
		// take a definition, rather than none.
		if len(preferred) == 0 {
			return capable, true
		}
		return preferred, true
	}

	options := make([]ui.UIOption, 0, len(capable))
	var initSel []int
	preferredSet := stringSet(preferred)
	for i, name := range capable {
		opt := ui.UIOption{Label: harness.AllHarnesses[name].DisplayName, Value: name}
		if !detected[name] {
			opt.Hint = "not detected"
		}
		options = append(options, opt)
		if preferredSet[name] {
			initSel = append(initSel, i)
		}
	}
	selected, ok := ui.UiSearchMultiselect("Which harnesses should receive the agent definitions?", options, nil, initSel, false)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(selected))
	for _, i := range selected {
		result = append(result, options[i].Value)
	}
	return result, true
}

func stringSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// keepIn returns the names in order that are also in set.
func keepIn(names []string, set map[string]bool) []string {
	var out []string
	for _, n := range names {
		if set[n] {
			out = append(out, n)
		}
	}
	return out
}

// filterAgentsByName keeps agents whose name matches one of names, by the
// disk-name rule agentNameMatches applies.
func filterAgentsByName(agents []*agentfile.AgentFile, names []string) []*agentfile.AgentFile {
	var filtered []*agentfile.AgentFile
	for _, a := range agents {
		for _, f := range names {
			if agentNameMatches(a.Name, f) {
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

// agentFileRepoPath returns the source-relative path to a discovered agent
// definition file: relative to the git clone root for a remote source, and to
// the local directory for a local one.
func agentFileRepoPath(agentPath, rootDir string) string {
	return repoRelPath(agentPath, rootDir)
}

// agentInstallOutcome is what an add run did. A definition can be skipped by
// every harness it was aimed at and install nowhere.
type agentInstallOutcome struct {
	alreadyInstalled int      // definitions skipped because their file is the canonical copy the lock already records
	refused          int      // definitions refused because the lock records their name from another source
	installed        int      // definitions that reached at least one harness
	harnesses        []string // harnesses that actually received something, in the order given
	fallbacks        *symlinkFallbacks
	// materialized is kept apart from fallbacks: a real file by design and a
	// real file because a symlink was refused have different causes and
	// different remedies, and merging them tells the user the wrong one.
	materialized *materializedInstalls
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
		if cfg.AgentNameRegexp == nil || cfg.AgentNameRegexp.MatchString(rawName) {
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

// agentInstallRun is what one add run knows beyond the harness list it was
// given: a per-definition override of that list, keyed by disk name, which
// the restore path uses to put each definition back into exactly the
// harnesses its lock entry names; and whether a definition the lock records
// from another source may be replaced.
type agentInstallRun struct {
	harnessesFor map[string][]string
	force        bool
}

// sameAgentSource reports whether two entries name the same source. A local
// path is compared resolved, since the lock keeps it cwd-relative and a fresh
// entry carries it absolute; a remote one by the URL it parses to, so
// "owner/repo" and its https form agree. The ref is not compared: re-adding
// at another ref re-pins the same definition.
func sameAgentSource(a, b lock.AgentLockEntry, cwd string) bool {
	if a.SourceType != b.SourceType {
		return false
	}
	if a.SourceType == string(source.SourceTypeLocal) {
		return resolveLocalAgentSourceAbs(a, cwd) == resolveLocalAgentSourceAbs(b, cwd)
	}
	au, bu := source.ParseSource(a.Source).URL, source.ParseSource(b.Source).URL
	if au == "" || bu == "" {
		return a.Source == b.Source
	}
	return au == bu
}

// agentSourceConflict explains why installing a definition as name would
// silently replace the one the lock already records under it, or returns ""
// when it would not. Two frontmatter names can sanitize to one disk name -
// "critic" from one source and "Critic" from another - and the guard inside
// one run (claimed) cannot see across runs. The lock can: a recorded entry
// from a different source, or from a different file of the same source, is
// a collision. An entry written before AgentPath was recorded for its source
// cannot be compared by file and is compared by source alone.
func agentSourceConflict(prior, incoming lock.AgentLockEntry, incomingPath, name, rootDir, cwd string) string {
	if !sameAgentSource(prior, incoming, cwd) {
		return fmt.Sprintf("%s is already installed from %s (%s); installing %s from %s would replace it - remove it with `mdm agents remove %s` first, or pass --force to replace it",
			name, prior.Source, prior.AgentPath, incomingPath, incoming.Source, name)
	}
	if prior.AgentPath != "" && incomingPath != "" && prior.AgentPath != incomingPath {
		// A definition that simply moved within the same source is a relocation,
		// not a second definition of the same name: the old path is gone, so
		// there is nothing ambiguous to refuse. Only refuse when the prior file
		// still exists alongside the new one - then two files really do install
		// as one name. rootDir is the fetched source; an empty one (no root to
		// check) keeps the old refuse-always behavior.
		if rootDir == "" || fileExists(filepath.Join(rootDir, filepath.FromSlash(prior.AgentPath))) {
			return fmt.Sprintf("%s is already installed from %s in %s; %s in the same source would replace it - remove it with `mdm agents remove %s` first, or pass --force to replace it",
				name, prior.Source, prior.AgentPath, incomingPath, name)
		}
	}
	return ""
}

// foreignAgentTargets splits a definition's target harnesses into those safe to
// write and those already holding a file mdm did not create. A target is safe
// when the lock already records the definition there (priorHarnesses - mdm's
// own install), when nothing is on disk yet, when the file present is one mdm
// wrote (a symlink to the canonical file or a copy of its bytes), or when the
// source file IS the destination (an in-place adoption of a hand-written file,
// which installAgentFile leaves untouched). Anything else on disk is the user's
// own hand-written file, and `mdm agents add` must not silently overwrite it -
// doing so replaced a committed .claude/agents/<name>.md with a symlink into the
// gitignored canonical directory, losing it on the next clone. canonicalPath is
// read as it stands before this run rewrites it, so a definition mdm already
// owns still matches while a foreign file does not.
func foreignAgentTargets(a *agentfile.AgentFile, name string, targets, priorHarnesses []string, global bool, cwd string) (safe, blocked []string) {
	prior := stringSet(priorHarnesses)
	canonicalPath := agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd)
	for _, h := range targets {
		target := agentHarnessPath(name, h, global, cwd)
		switch {
		case prior[h], target == "":
			// Recorded by the lock, or no directory to write into (a skip
			// installAgentFile reports on its own). Either way, not a foreign file.
			safe = append(safe, h)
		case !fileExists(target):
			safe = append(safe, h)
		case sameFileOnDisk(a.Path, target):
			safe = append(safe, h) // adopting the source file in place
		case mdmOwnsAgentFile(target, h, canonicalPath):
			safe = append(safe, h) // mdm's own file from an earlier run
		default:
			blocked = append(blocked, h)
		}
	}
	return safe, blocked
}

// fileExists reports whether a file or symlink (even a dangling one) is at path.
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// discoveredFileIsMdmOwn reports whether a file `mdm agents add .` rediscovered
// is one mdm already installed for the recorded entry: the canonical file, a
// symlink into it, or a materialized harness copy (Copilot's .agent.md, Codex's
// .toml). Such a file is not a new source, so reinstalling it - or worse,
// treating its path as a conflicting source - is wrong. Only the same-file /
// canonical case was caught before, so a materialized copy at its own path read
// as a fresh source and stopped `add .` with a spurious conflict.
func discoveredFileIsMdmOwn(a *agentfile.AgentFile, name string, entry lock.AgentLockEntry, global bool, cwd string) bool {
	canonicalPath := agentCanonicalPath(name, agentCanonicalFormat(a), global, cwd)
	if sameFileOnDisk(a.Path, canonicalPath) {
		return true
	}
	for _, h := range agentInstalledIn(name, entry, global, cwd) {
		target := agentHarnessPath(name, h, global, cwd)
		if target != "" && sameFileOnDisk(a.Path, target) && mdmOwnsAgentFile(target, h, canonicalPath) {
			return true
		}
	}
	return false
}

// targetsFor returns the harnesses one definition goes to.
func (r agentInstallRun) targetsFor(name string, harnesses []string) []string {
	if override, ok := r.harnessesFor[name]; ok && len(override) > 0 {
		return override
	}
	return harnesses
}

// installAgentsForHarnesses installs each selected definition into every
// requested harness, and records it in the lock only when at least one harness
// received it. A harness with no agent-definition directory recorded is a
// skip with a reason. A definition no harness accepted leaves nothing behind.
func installAgentsForHarnesses(agents []*agentfile.AgentFile, harnesses []string, global bool, mode InstallMode, baseEntry lock.AgentLockEntry, rootDir, cwd string) agentInstallOutcome {
	return installAgents(agents, harnesses, global, mode, baseEntry, rootDir, cwd, agentInstallRun{})
}

// installAgents is installAgentsForHarnesses with the run's extra knowledge.
func installAgents(agents []*agentfile.AgentFile, harnesses []string, global bool, mode InstallMode, baseEntry lock.AgentLockEntry, rootDir, cwd string, run agentInstallRun) agentInstallOutcome {
	fallbacks := symlinkFallbacks{noun: "agent definitions", group: "agents"}
	var materialized materializedInstalls
	outcome := agentInstallOutcome{fallbacks: &fallbacks, materialized: &materialized}
	received := map[string]bool{}
	claimed := map[string]string{} // disk name -> the source that claimed it
	_, recorded := agentLockEntries(global, cwd)

	for _, a := range agents {
		name := agentDiskName(a.Name)
		// `mdm agents add .` discovers mdm's own installed files - the canonical
		// file, the harness symlinks into it, and materialized copies (Copilot's
		// .agent.md, Codex's .toml). Installing one of those again would only
		// rewrite its lock entry's source to the project itself, after which
		// `agents update` can never find it upstream again, and rediscovering a
		// materialized copy at a new path used to read as a source conflict that
		// stopped `add .` outright.
		if entry, ok := recorded[name]; ok && discoveredFileIsMdmOwn(a, name, entry, global, cwd) {
			ui.LogInfo(fmt.Sprintf("%s: already installed from %s - to add a harness, run mdm agents add %s --harness <name>", a.Name, entry.Source, entry.Source))
			outcome.alreadyInstalled++
			continue
		}
		// Two names can sanitize to one file name ("Code Reviewer" and
		// "code-reviewer"). Installing both would write one canonical file and
		// count two, so the name belongs to whichever source claimed it first.
		if prior, ok := claimed[name]; ok {
			ui.LogWarn(fmt.Sprintf("%s: %s and %s both install as %s - skipping the second, rename one of them", a.Name, prior, a.Path, name))
			continue
		}
		claimed[name] = a.Path

		targets := run.targetsFor(name, harnesses)
		agentPath := agentFileRepoPath(a.Path, rootDir)
		// Where the definition is already installed, resolved before the
		// canonical file is rewritten. An entry written before the harness
		// list existed records none, and reading the field raw would answer
		// "nowhere" for exactly the installs this has to protect.
		var priorHarnesses []string
		if prior, ok := recorded[name]; ok {
			priorHarnesses = agentHeldHarnesses(name, prior, global, cwd)
			if conflict := agentSourceConflict(prior, baseEntry, agentPath, name, rootDir, cwd); conflict != "" {
				if !run.force {
					ui.LogError(fmt.Sprintf("%s: %s", a.Name, conflict))
					outcome.refused++
					continue
				}
				// A forced replacement is a replacement everywhere: the
				// harnesses still serving the prior definition get the new
				// one too, or the lock would name one source while some
				// harness served another.
				targets = unionHarnesses(targets, priorHarnesses)
			}
		}
		// Never silently overwrite a harness file mdm did not write. --force is
		// the opt-in to replace it, matching agentSourceConflict above; without
		// it, a foreign file blocks that harness and the definition still lands
		// in the others.
		if !run.force {
			safe, blocked := foreignAgentTargets(a, name, targets, priorHarnesses, global, cwd)
			if len(blocked) > 0 {
				var paths []string
				for _, h := range blocked {
					paths = append(paths, shortenPath(agentHarnessPath(name, h, global, cwd), cwd))
				}
				ui.LogError(fmt.Sprintf("%s: %s already exists and was not written by mdm - remove it or pass --force to replace it", a.Name, strings.Join(paths, ", ")))
				outcome.refused++
				targets = safe
			}
		}
		if len(targets) == 0 {
			continue
		}

		installedTo := installOneAgent(a, name, targets, global, cwd, mode, &outcome)
		if len(installedTo) == 0 {
			continue
		}
		outcome.installed++
		for _, harnessName := range installedTo {
			if !received[harnessName] {
				received[harnessName] = true
				outcome.harnesses = append(outcome.harnesses, harnessName)
			}
		}

		entry := baseEntry
		entry.AgentPath = agentPath
		entry.Format = string(agentCanonicalFormat(a))
		// The list is what remove, update and install act on. A harness an
		// earlier add of the same definition reached is still holding it, so
		// this run's harnesses join that list rather than replacing it.
		entry.Harnesses = unionHarnesses(priorHarnesses, installedTo)
		recordAgentEntry(name, entry, global, cwd)
	}
	return outcome
}

// installOneAgent installs one definition into each of harnesses, prints its
// per-definition lines, and returns the harnesses that received it, in the
// order given. A definition no harness accepted has its canonical write
// rolled back and returns nothing.
func installOneAgent(a *agentfile.AgentFile, name string, harnesses []string, global bool, cwd string, mode InstallMode, outcome *agentInstallOutcome) []string {
	fmt.Printf("%sInstalling %s%s%s...\n", ansiDim, ansiText, a.Name, ansiReset)
	warnAgentNamePattern(a.Name, harnesses)
	rollback := canonicalRollback(a, name, global, cwd)

	var failures agentFailures
	var skipReasons []string
	var installedTo []string
	for _, harnessName := range harnesses {
		result := installAgentFile(a, harnessName, global, cwd, mode)
		outcome.fallbacks.note(harnessName, result)
		outcome.materialized.note(harnessName, result)
		failures.note(harnessName, result)
		switch {
		case result.Success:
			installedTo = append(installedTo, harnessName)
		case result.Skipped:
			skipReasons = append(skipReasons, result.Error)
		}
	}

	for _, reason := range skipReasons {
		ui.LogInfo(fmt.Sprintf("%s: skipped - %s", a.Name, reason))
	}

	if len(installedTo) == 0 {
		rollback()
		reportAgentFailure(a.Name, &failures)
		return nil
	}
	if failures.any() {
		reportAgentFailure(a.Name, &failures)
	} else {
		ui.LogSuccess(a.Name)
	}
	return installedTo
}

// printAgentInstallSummary is printInstallSummary's counterpart for agent
// definitions. The harness list names only the harnesses that received a file.
func printAgentInstallSummary(outcome agentInstallOutcome, global bool, mode InstallMode) {
	scope := "project"
	if global {
		scope = "global"
	}
	if outcome.installed == 0 && outcome.alreadyInstalled > 0 {
		fmt.Printf("%s%d agent definition(s) already installed (%s scope); nothing to do.%s\n\n", ansiDim, outcome.alreadyInstalled, scope, ansiReset)
		return
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
