package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/plugin"
	"github.com/sethcarney/mdm/internal/security/markdownscan"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

const (
	pluginsSubdir     = "plugins"
	pluginsDataSubdir = "plugins-data"
)

// pluginsBaseDir is where installed plugins live: each plugin's directory
// is its PLUGIN_ROOT.
func pluginsBaseDir(cwd string) string {
	return filepath.Join(cwd, agent.AgentsDir, pluginsSubdir)
}

// pluginsDataBaseDir holds each plugin's persistent PLUGIN_DATA directory,
// preserved across updates.
func pluginsDataBaseDir(cwd string) string {
	return filepath.Join(cwd, agent.AgentsDir, pluginsDataSubdir)
}

type PluginsAddOptions struct {
	Plugins          []string
	Agents           []string
	Yes              bool
	DryRun           bool
	AllowHiddenChars bool
	SkipMCP          bool
}

func buildPluginsAddCmd() *cobra.Command {
	var opts PluginsAddOptions

	cmd := &cobra.Command{
		Use:     "add <source>",
		Short:   "Install an Agent Plugin from GitHub, GitLab, URL, or local path",
		Aliases: []string{"a"},
		Long: fmt.Sprintf(`Install a plugin into ./%s/%s/ and record it in %s.

The plugin's skills are linked into the agent skill directories, and its
MCP servers are wired into the agents' MCP config files.

Sources use the same forms as skills: owner/repo shorthand, full URLs,
and local paths, with an optional #ref for version pinning.

%sExamples:%s
  mdm plugins add acme/toolkit
  mdm plugins add acme/toolkit#v1.2.0 -a claude-code
  mdm plugins add ./local-plugin --skip-mcp`, agent.AgentsDir, pluginsSubdir, lockName, ansiBold, ansiReset),
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsAdd(args[0], opts)
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&opts.Plugins, "plugin", "p", nil, "Plugin names to install (repeatable, use '*' for all)")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Agents to install for (repeatable)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts and install all discovered plugins")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Show what would be installed without writing anything")
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	f.BoolVar(&opts.SkipMCP, "skip-mcp", false, "Install skills only; do not write MCP server config")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

// pluginCandidate is a discovered, loadable plugin root awaiting install.
type pluginCandidate struct {
	Name   string
	Root   string
	Report *plugin.Report
}

func runPluginsAdd(sourceInput string, opts PluginsAddOptions) {
	cwd, _ := os.Getwd()
	parsed := source.ParseSource(sourceInput)
	vlog(verboseFlag, "source %q → type=%s url=%s ref=%q subpath=%q",
		sourceInput, parsed.Type, parsed.URL, parsed.Ref, parsed.Subpath)
	fmt.Println()

	searchRoot, cleanup := fetchPluginSource(parsed)
	defer cleanup()

	candidates := discoverPluginCandidates(searchRoot)
	if len(candidates) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo plugins found in %s%s\n", ansiText, sourceInput, ansiReset)
		os.Exit(1)
	}

	selected, ok := selectPluginCandidates(candidates, opts)
	if !ok {
		return
	}

	if !scanPluginCandidates(selected, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	agents, ok := resolvePluginAgents(opts, cwd)
	if !ok {
		os.Exit(1)
	}

	baseEntry := pluginLockEntry(parsed, sourceInput)
	installed := 0
	for _, c := range selected {
		if installPluginCandidate(c, baseEntry, opts, agents, cwd) {
			installed++
		}
	}
	fmt.Println()
	if opts.DryRun {
		fmt.Printf("%sDry run — nothing was written.%s\n\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sInstalled %d plugin(s) to ./%s/%s%s\n\n", ansiText, installed, agent.AgentsDir, pluginsSubdir, ansiReset)
}

// fetchPluginSource materializes the source on disk and returns the
// directory to search plus a cleanup func for any temp clone.
func fetchPluginSource(parsed source.ParsedSource) (string, func()) {
	noop := func() {}
	switch parsed.Type {
	case source.SourceTypeLocal:
		if _, err := os.Stat(parsed.LocalPath); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Path not found: %s\n", ansiText, ansiReset, parsed.LocalPath)
			os.Exit(1)
		}
		return parsed.LocalPath, noop
	case source.SourceTypeWellKnown:
		fmt.Fprintf(os.Stderr, "%sError:%s well-known registries are not supported for plugins\n", ansiText, ansiReset)
		os.Exit(1)
		return "", noop
	default:
		spin := ui.NewSpinner("Cloning " + parsed.URL + "...")
		tmpDir, err := git.CloneRepo(parsed.URL, parsed.Ref)
		spin.Stop("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err.Error())
			os.Exit(1)
		}
		searchRoot := tmpDir
		if parsed.Subpath != "" {
			searchRoot = filepath.Join(tmpDir, parsed.Subpath)
			if _, err := os.Stat(searchRoot); err != nil {
				_ = git.CleanupTempDir(tmpDir)
				fmt.Fprintf(os.Stderr, "%sSubpath not found:%s %s\n", ansiText, ansiReset, parsed.Subpath)
				os.Exit(1)
			}
		}
		return searchRoot, func() { _ = git.CleanupTempDir(tmpDir) }
	}
}

// discoverPluginCandidates loads every plugin root found under searchRoot.
// Rejected plugins (fatal manifest errors) are reported and skipped;
// duplicate names keep the first occurrence.
func discoverPluginCandidates(searchRoot string) []pluginCandidate {
	var candidates []pluginCandidate
	seen := map[string]bool{}
	for _, root := range plugin.FindPluginRoots(searchRoot) {
		report := plugin.Inspect(root)
		if report.Manifest == nil {
			for _, issue := range report.Issues {
				ui.LogWarn(fmt.Sprintf("%s: %s", filepath.Base(root), issue.Message))
			}
			continue
		}
		if seen[report.Manifest.Name] {
			ui.LogWarn(fmt.Sprintf("duplicate plugin name %q at %s — skipped", report.Manifest.Name, root))
			continue
		}
		seen[report.Manifest.Name] = true
		candidates = append(candidates, pluginCandidate{Name: report.Manifest.Name, Root: root, Report: report})
	}
	return candidates
}

func filterPluginCandidates(candidates []pluginCandidate, filters []string) []pluginCandidate {
	if len(filters) == 1 && filters[0] == "*" {
		return candidates
	}
	var keep []pluginCandidate
	for _, c := range candidates {
		for _, f := range filters {
			if skillNameMatches(c.Name, f) {
				keep = append(keep, c)
				break
			}
		}
	}
	return keep
}

func pluginCandidateHint(c pluginCandidate) string {
	hint := fmt.Sprintf("%d skill(s)", len(c.Report.Skills))
	if c.Report.MCP != nil && len(c.Report.MCP.Servers) > 0 {
		hint += fmt.Sprintf(", %d MCP server(s)", len(c.Report.MCP.Servers))
	}
	return hint
}

func selectPluginCandidates(candidates []pluginCandidate, opts PluginsAddOptions) ([]pluginCandidate, bool) {
	if len(opts.Plugins) > 0 {
		keep := filterPluginCandidates(candidates, opts.Plugins)
		if len(keep) == 0 {
			fmt.Fprintf(os.Stderr, "%sNo matching plugins found.%s\n", ansiText, ansiReset)
			os.Exit(1)
		}
		return keep, true
	}
	if opts.Yes || len(candidates) == 1 {
		return candidates, true
	}
	options := make([]ui.UIOption, len(candidates))
	for i, c := range candidates {
		options[i] = ui.UIOption{Label: c.Name, Value: c.Name, Hint: pluginCandidateHint(c)}
	}
	indices, ok := ui.UiMultiselect("Which plugins would you like to install?", options, true, nil, nil)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []pluginCandidate
	for _, i := range indices {
		selected = append(selected, candidates[i])
	}
	return selected, true
}

// scanPluginCandidates runs the hidden-character scan over every selected
// plugin. Plugin markdown is agent-bound, so this gate is mandatory.
func scanPluginCandidates(selected []pluginCandidate, allow bool) bool {
	ok := true
	for _, c := range selected {
		findings, err := markdownscan.ScanMarkdownFiles(c.Root)
		if err != nil {
			fmt.Printf("%sHidden character scan failed for %s: %s%s\n", ansiRed, c.Name, err, ansiReset)
			ok = false
			continue
		}
		if !checkSkillMarkdownForHiddenChars(c.Name, findings, allow) {
			ok = false
		}
	}
	return ok
}

// resolvePluginAgents picks the agents to install for: the --agent flag,
// then the project's configured agents, then detected agents.
func resolvePluginAgents(opts PluginsAddOptions, cwd string) ([]string, bool) {
	if len(opts.Agents) > 0 {
		for _, name := range opts.Agents {
			if agent.AllAgents[name] == nil {
				fmt.Fprintf(os.Stderr, "%sError:%s unknown agent %q\n", ansiText, ansiReset, name)
				return nil, false
			}
		}
		return opts.Agents, true
	}
	if configured := lock.GetConfiguredAgents(false, cwd); len(configured) > 0 {
		return configured, true
	}
	if detected := agent.DetectInstalledAgents(); len(detected) > 0 {
		return detected, true
	}
	fmt.Fprintf(os.Stderr, "%sError:%s no agents detected — pass --agent (e.g. -a claude-code)\n", ansiText, ansiReset)
	return nil, false
}

// pluginLockEntry builds the source-level part of the lock entry shared by
// every plugin installed from this invocation.
func pluginLockEntry(parsed source.ParsedSource, sourceInput string) lock.PluginLockEntry {
	entry := lock.PluginLockEntry{
		Source:      stripSourceRef(sourceInput),
		SourceType:  string(parsed.Type),
		SourceURL:   parsed.URL,
		Subpath:     parsed.Subpath,
		SpecVersion: pluginsSpecVersion,
	}
	if parsed.Type == source.SourceTypeLocal {
		entry.Source = parsed.LocalPath
		entry.SourceURL = ""
	} else {
		entry.Ref = parsed.Ref
		if entry.Ref == "" {
			entry.Ref = git.DefaultBranch(parsed.URL)
		}
	}
	return entry
}

func installPluginCandidate(c pluginCandidate, baseEntry lock.PluginLockEntry, opts PluginsAddOptions, agents []string, cwd string) bool {
	if opts.DryRun {
		fmt.Printf("  %s%s%s  %s → ./%s\n", ansiText, c.Name, ansiReset, pluginCandidateHint(c),
			filepath.ToSlash(filepath.Join(agent.AgentsDir, pluginsSubdir, c.Name)))
		return true
	}

	destDir, ok := copyPluginToInstallDir(c, cwd)
	if !ok {
		return false
	}
	dataDir := filepath.Join(pluginsDataBaseDir(cwd), c.Name)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", c.Name, err))
		return false
	}

	installedSkills, skipped := installPluginSkills(c.Name, destDir, agents, cwd)

	entry := baseEntry
	entry.InstallDir = filepath.ToSlash(filepath.Join(agent.AgentsDir, pluginsSubdir, c.Name))
	entry.DataDir = filepath.ToSlash(filepath.Join(agent.AgentsDir, pluginsDataSubdir, c.Name))
	entry.Version = c.Report.Manifest.Version
	entry.Skills = installedSkills
	entry.SkillAgents = agents
	if hash, err := plugin.HashPluginDir(destDir); err == nil {
		entry.ContentHash = hash
	}
	entry.MCP = wirePluginMCP(c, destDir, dataDir, agents, opts, cwd)
	if err := lock.AddPluginToLock(c.Name, entry, cwd); err != nil {
		ui.LogWarn(fmt.Sprintf("could not update %s: %v", lockName, err))
	}

	msg := fmt.Sprintf("%s (%d skill(s)", c.Name, len(installedSkills))
	if servers := countWiredServers(entry.MCP); servers > 0 {
		msg += fmt.Sprintf(", %d MCP server(s) wired", servers)
	}
	msg += ")"
	if skipped > 0 {
		msg += fmt.Sprintf(" — %d skill(s) skipped", skipped)
	}
	ui.LogSuccess(msg)
	return true
}

// copyPluginToInstallDir copies the plugin verbatim into the project's
// plugin directory, which becomes its PLUGIN_ROOT.
func copyPluginToInstallDir(c pluginCandidate, cwd string) (string, bool) {
	destBase := pluginsBaseDir(cwd)
	destDir := filepath.Join(destBase, c.Name)
	if !isPathSafe(destBase, destDir) || !isPathSafe(cwd, destBase) {
		ui.LogError(fmt.Sprintf("%s: potential path traversal detected", c.Name))
		return "", false
	}
	if err := cleanAndCreateDir(destDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", c.Name, err))
		return "", false
	}
	if err := copyPluginTree(c.Root, c.Root, destDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", c.Name, err))
		return "", false
	}
	return destDir, true
}

// copyPluginTree copies a plugin directory while enforcing the spec's
// package boundary: every file read must resolve inside the plugin root
// after following symlinks. Escaping symlinks are skipped with a warning
// instead of being read, and directory symlinks are never followed (a
// within-root one could form a cycle).
func copyPluginTree(root, src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if isExcluded(e.Name(), e.IsDir()) {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		switch {
		case e.Type()&os.ModeSymlink != 0:
			if plugin.EscapesRoot(root, srcPath) {
				ui.LogWarn(fmt.Sprintf("skipped %s: symlink resolves outside the plugin root", srcPath))
				continue
			}
			info, err := os.Stat(srcPath)
			if err != nil || info.IsDir() {
				continue // broken or directory symlink
			}
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		case e.IsDir():
			if err := copyPluginTree(root, srcPath, dstPath); err != nil {
				return err
			}
		default:
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// installPluginSkills links each skill from the installed plugin directory
// into the canonical skills dir and each agent's skills dir. It returns
// the sanitized names installed and how many were skipped over collisions.
func installPluginSkills(pluginName, destDir string, agents []string, cwd string) ([]string, int) {
	skills, _ := plugin.ListSkills(destDir)
	var installed []string
	skipped := 0
	for _, ps := range skills {
		if !installPluginSkillLink(pluginName, ps, agents, cwd) {
			skipped++
			continue
		}
		installed = append(installed, sanitizeName(ps.Name))
	}
	return installed, skipped
}

// installPluginSkillLink links one plugin skill: the canonical skills dir
// gets a symlink pointing into the plugin's own directory (so provenance
// is visible from the link target and the plugin stays the single source
// of truth), and each agent dir links to the canonical one as usual.
func installPluginSkillLink(pluginName string, ps plugin.PluginSkill, agents []string, cwd string) bool {
	skillName := sanitizeName(ps.Name)
	canonicalBase := getCanonicalSkillsDir(false, cwd)
	canonicalDir := filepath.Join(canonicalBase, skillName)
	if !isPathSafe(canonicalBase, canonicalDir) {
		ui.LogWarn(fmt.Sprintf("skill %s: potential path traversal detected", skillName))
		return false
	}

	owner, standalone := skillDirOwner(canonicalDir, cwd)
	if standalone {
		ui.LogWarn(fmt.Sprintf("skill %s already installed standalone — skipped (remove it with 'mdm skills remove %s' first)", skillName, skillName))
		return false
	}
	if owner != "" && owner != pluginName {
		ui.LogWarn(fmt.Sprintf("skill %s already installed by plugin %s — skipped", skillName, owner))
		return false
	}

	if !createSymlink(ps.Dir, canonicalDir) {
		if err := cleanAndCreateDir(canonicalDir); err != nil {
			ui.LogWarn(fmt.Sprintf("skill %s: %v", skillName, err))
			return false
		}
		if err := copyDirectory(ps.Dir, canonicalDir); err != nil {
			ui.LogWarn(fmt.Sprintf("skill %s: %v", skillName, err))
			return false
		}
	}
	for _, agentName := range agents {
		if agent.UsesSharedSkillsDir(agentName) {
			continue
		}
		linkInstalledSkillToAgent(skillName, agentName, false, cwd)
	}
	return true
}

// skillDirOwner inspects a canonical skill directory: standalone reports a
// real (non-plugin) install, owner names the plugin whose directory the
// symlink points into.
func skillDirOwner(canonicalDir, cwd string) (owner string, standalone bool) {
	info, err := os.Lstat(canonicalDir)
	if err != nil {
		return "", false
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "", true
	}
	target, err := os.Readlink(canonicalDir)
	if err != nil {
		return "", true
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(canonicalDir), target)
	}
	target = filepath.Clean(target)
	base := pluginsBaseDir(cwd)
	if !isInsideOrEqual(target, base) || target == filepath.Clean(base) {
		return "", true
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", true
	}
	return strings.Split(filepath.ToSlash(rel), "/")[0], false
}

// pluginOwningSkill names the plugin that owns an installed skill, or ""
// for standalone skills. The symlink target is authoritative; the lock's
// skill lists cover copy-mode fallbacks.
func pluginOwningSkill(skillName, cwd string) string {
	canonicalDir := filepath.Join(getCanonicalSkillsDir(false, cwd), sanitizeName(skillName))
	if owner, _ := skillDirOwner(canonicalDir, cwd); owner != "" {
		return owner
	}
	for name, entry := range lock.ReadPluginsLock(cwd).Plugins {
		if contains(entry.Skills, sanitizeName(skillName)) {
			return name
		}
	}
	return ""
}

func countWiredServers(mcp map[string][]string) int {
	seen := map[string]bool{}
	for _, ids := range mcp {
		for _, id := range ids {
			seen[id] = true
		}
	}
	return len(seen)
}
