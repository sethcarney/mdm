package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
	"github.com/spf13/cobra"
)

// ─── Options ───────────────────────────────────────────────────────────────────

type AddOptions struct {
	Global            bool
	Project           bool
	Agents            []string // empty = prompt
	Skills            []string // empty = prompt; "*" = all
	PreselectedSkills []string // pre-ticked in the skill picker, but others still shown
	ListOnly          bool
	Yes               bool // skip prompts
	Copy              bool
	All               bool // --all: skill '*', agent '*', -y
	FullDepth         bool
	SkipAudit         bool
	FailOnAudit       bool
	AllowHiddenChars  bool
	Verbose           bool
}

func buildAddCmd(ver string) *cobra.Command {
	var opts AddOptions

	cmd := &cobra.Command{
		Use:     "add <package>",
		Short:   "Add a skill from GitHub or URL",
		Aliases: []string{"a"},
		Long: fmt.Sprintf(`Add a skill package from GitHub, a URL, or a local path.

The --agent (-a) and --skill (-s) flags accept multiple values. You can
pass them space-separated after the flag or repeat the flag for each value
— both styles are equivalent:

  mdm skills add owner/repo -a claude-code cursor
  mdm skills add owner/repo -a claude-code -a cursor

%sVersion pinning:%s
Skills are versioned via the "version:" field in their SKILL.md frontmatter.
To pin to a specific release, append the git tag with "#":

  mdm skills add owner/repo#v1.0.0   # install the v1.0.0 tag
  mdm skills add owner/repo#v2.0.0   # upgrade to v2.0.0 explicitly

To update to the latest version, run:  mdm skills update

%sExamples:%s
  mdm skills add vercel-labs/agent-skills
  mdm skills add vercel-labs/agent-skills -g
  mdm skills add vercel-labs/agent-skills -a claude-code cursor
  mdm skills add vercel-labs/agent-skills --agent claude-code --agent cursor
  mdm skills add https://github.com/owner/repo
  mdm skills add owner/repo#v1.2.0
  mdm skills add ./my-local-skill`, ansiBold, ansiReset, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			src := ""
			if len(args) > 0 {
				src = args[0]
			}
			if opts.All {
				opts.Skills = []string{"*"}
				opts.Agents = []string{"*"}
				opts.Yes = true
			}
			runAdd(src, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Install skill globally (user-level)")
	f.BoolVarP(&opts.Project, "project", "p", false, "Force project-scope install")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Agents to install to (repeatable, use '*' for all)")
	f.StringArrayVarP(&opts.Skills, "skill", "s", nil, "Skill names to install (repeatable, use '*' for all)")
	f.BoolVarP(&opts.ListOnly, "list", "l", false, "List available skills without installing")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	f.BoolVar(&opts.Copy, "copy", false, "Copy files instead of symlinking")
	f.BoolVar(&opts.All, "all", false, "Shorthand for --skill '*' --agent '*' -y")
	f.BoolVar(&opts.FullDepth, "full-depth", false, "Search all subdirectories")
	f.BoolVar(&opts.SkipAudit, "skip-audit", false, "Skip security audit check for public skills")
	f.BoolVar(&opts.FailOnAudit, "fail-on-audit", false, "Exit non-zero when security findings are detected instead of prompting")
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

// vlog writes a diagnostic line to stderr when verbose is set. It is a no-op
// otherwise, so call sites can sprinkle these freely without guarding. Most
// callers pass the package-level verboseFlag (bound to the root --verbose/-v
// flag); pass a literal true inside a block already gated on verbose.
func vlog(verbose bool, format string, a ...interface{}) {
	if !verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "%s[mdm]%s %s\n", ansiDim, ansiReset, fmt.Sprintf(format, a...))
}

// ─── Main add command ──────────────────────────────────────────────────────────

func runAdd(sourceInput string, opts AddOptions) {
	cwd, _ := os.Getwd()

	// Inherit the global --verbose flag so the existing blob/git diagnostic
	// wiring fires, including when runAdd is called from update/find/install.
	opts.Verbose = verboseFlag

	if sourceInput == "" {
		fmt.Fprintf(os.Stderr, "%sError:%s Please provide a package source.\n\n", ansiText, ansiReset)
		fmt.Printf("  %s$%s mdm skills add <package>\n\n", ansiDim, ansiReset)
		fmt.Printf("  %sExamples:%s\n", ansiDim, ansiReset)
		fmt.Printf("    mdm skills add vercel-labs/agent-skills\n")
		fmt.Printf("    mdm skills add https://github.com/owner/repo\n")
		os.Exit(1)
	}

	parsed := source.ParseSource(sourceInput)

	vlog(opts.Verbose, "source %q → type=%s url=%s ref=%q subpath=%q",
		sourceInput, parsed.Type, parsed.URL, parsed.Ref, parsed.Subpath)

	fmt.Println()

	switch parsed.Type {
	case source.SourceTypeWellKnown:
		runAddWellKnown(parsed, opts, cwd)
	case source.SourceTypeLocal:
		runAddLocal(parsed, opts, cwd)
	default:
		runAddGitOrHub(parsed, opts, cwd, sourceInput)
	}
}

// ─── Well-known provider ───────────────────────────────────────────────────────

func filterWellKnownByName(skills []*registry.WellKnownSkill, filters []string) []*registry.WellKnownSkill {
	if len(filters) == 0 || filters[0] == "*" {
		return skills
	}
	var keep []*registry.WellKnownSkill
	for _, s := range skills {
		for _, f := range filters {
			if strings.EqualFold(s.Name, f) || strings.EqualFold(s.InstallName, f) {
				keep = append(keep, s)
				break
			}
		}
	}
	return keep
}

func selectWellKnownSkills(filtered []*registry.WellKnownSkill, opts AddOptions) ([]*registry.WellKnownSkill, bool) {
	if (len(opts.Skills) > 0 && opts.Skills[0] == "*") || len(filtered) == 1 || opts.Yes {
		return filtered, true
	}
	var front, rest []*registry.WellKnownSkill
	for _, s := range filtered {
		pre := false
		for _, p := range opts.PreselectedSkills {
			if skillNameMatches(s.Name, p) {
				pre = true
				break
			}
		}
		if pre {
			front = append(front, s)
		} else {
			rest = append(rest, s)
		}
	}
	filtered = append(front, rest...)
	initSel := make([]int, len(front))
	for i := range front {
		initSel[i] = i
	}
	options := make([]ui.UIOption, len(filtered))
	for i, s := range filtered {
		options[i] = ui.UIOption{Label: s.Name, Value: s.InstallName, Hint: s.Description}
	}
	indices, ok := ui.UiSearchMultiselect("Which skills would you like to install?", options, nil, initSel, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []*registry.WellKnownSkill
	for _, i := range indices {
		selected = append(selected, filtered[i])
	}
	return selected, true
}

func installWellKnownForAgents(selectedSkills []*registry.WellKnownSkill, agents []string, global bool, mode InstallMode, sourceID string, parsed source.ParsedSource, cwd string) {
	for _, s := range selectedSkills {
		sName := sanitizeName(s.InstallName)
		fmt.Printf("%sInstalling %s%s%s...\n", ansiDim, ansiText, s.Name, ansiReset)
		var failedAgents []string
		for _, agentName := range agents {
			result := installWellKnownSkillForAgent(s, agentName, global, mode)
			if !result.Success {
				failedAgents = append(failedAgents, agentName)
			}
		}
		if len(failedAgents) == 0 {
			ui.LogSuccess(s.Name)
		} else {
			ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", s.Name, strings.Join(failedAgents, ", ")))
		}
		if global {
			if err := lock.AddSkillToLock(sName, lock.SkillLockEntry{
				Source:     sourceID,
				SourceType: string(source.SourceTypeWellKnown),
				SourceURL:  parsed.URL,
				PluginName: s.InstallName,
			}); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		} else {
			if err := lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     parsed.URL,
				SourceType: string(source.SourceTypeWellKnown),
			}, cwd); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		}
	}
}

func runAddWellKnown(parsed source.ParsedSource, opts AddOptions, cwd string) {
	spin := ui.NewSpinner("Fetching skills from " + parsed.URL + "...")
	skills, err := registry.FetchAllWellKnownSkills(parsed.URL)
	spin.Stop("")
	if err != nil || len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo skills found at %s%s\n", ansiText, parsed.URL, ansiReset)
		os.Exit(1)
	}

	filtered := filterWellKnownByName(skills, opts.Skills)
	if len(filtered) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo matching skills found.%s\n", ansiText, ansiReset)
		os.Exit(1)
	}

	if opts.ListOnly {
		fmt.Printf("%sAvailable skills from %s:%s\n\n", ansiText, parsed.URL, ansiReset)
		for _, s := range filtered {
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, s.Description, ansiReset)
		}
		return
	}

	selectedSkills, ok := selectWellKnownSkills(filtered, opts)
	if !ok {
		return
	}
	if !checkWellKnownSkillsMarkdownForHiddenChars(selectedSkills, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}

	sourceID := registry.GetWellKnownSourceIdentifier(parsed.URL)
	fmt.Println()
	installWellKnownForAgents(selectedSkills, agents, global, mode, sourceID, parsed, cwd)
	fmt.Println()
	printInstallSummary(len(selectedSkills), global, agents, mode)
	maybeShowFindPrompt(cwd)
}

// ─── Local path ────────────────────────────────────────────────────────────────

func runAddLocal(parsed source.ParsedSource, opts AddOptions, cwd string) {
	localPath := parsed.LocalPath
	if _, err := os.Stat(localPath); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s Path not found: %s\n", ansiText, ansiReset, localPath)
		os.Exit(1)
	}

	skills := discoverSkillsInDir(localPath, opts.FullDepth, "")
	if len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo skills found in %s%s\n", ansiText, localPath, ansiReset)
		os.Exit(1)
	}

	if opts.ListOnly {
		fmt.Printf("%sAvailable skills:%s\n\n", ansiText, ansiReset)
		for _, s := range skills {
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, s.Description, ansiReset)
		}
		return
	}

	// Pre-tick skills already installed from this source in the interactive picker.
	if len(opts.Skills) == 0 {
		opts.PreselectedSkills = mergeUnique(opts.PreselectedSkills, alreadyInstalledFromSource(parsed.URL, cwd))
	}

	selectedSkills, ok := selectSkills(skills, opts)
	if !ok {
		return
	}
	if !checkDiskSkillsMarkdownForHiddenChars(selectedSkills, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}

	fmt.Println()
	installSkillsForAgents(selectedSkills, agents, global, mode, lock.SkillLockEntry{
		Source:     localPath,
		SourceType: string(source.SourceTypeLocal),
		SourceURL:  localPath,
	}, cwd, "")
	maybeShowFindPrompt(cwd)
}

// ─── GitHub / GitLab / git ─────────────────────────────────────────────────────

func tryBlobFastInstall(parsed source.ParsedSource, opts AddOptions, cwd, ownerRepo, sourceInput string) bool {
	if parsed.Type != source.SourceTypeGitHub || ownerRepo == "" {
		return false
	}
	blobOpts := blob.InstallOptions{
		Subpath:     parsed.Subpath,
		SkillFilter: skillFilterFromOpts(opts, parsed),
		Ref:         parsed.Ref,
		Token:       lock.GetGitHubToken(),
	}
	if opts.Verbose {
		blobOpts.Logf = func(f string, a ...interface{}) { vlog(true, f, a...) }
	}
	vlog(opts.Verbose, "trying GitHub API fast-path for %s (no clone)", ownerRepo)
	var spin *ui.Spinner
	if !opts.Verbose {
		spin = ui.NewSpinner("Fetching skills...")
	}
	blobResult, err := blob.TryBlobInstall(ownerRepo, blobOpts)
	if spin != nil {
		spin.Stop("")
	}
	if blobResult == nil || len(blobResult.Skills) == 0 {
		reportFastPathFallback(ownerRepo, err, opts.Verbose)
		return false
	}
	vlog(opts.Verbose, "fast-path found %d skill(s) via API", len(blobResult.Skills))
	runAddBlob(blobResult, parsed, opts, cwd, ownerRepo, sourceInput)
	return true
}

// cloneForAdd shallow-clones the repo for an add operation. In verbose mode it
// streams git's progress to stderr and reports timing; otherwise it shows the
// usual spinner.
func cloneForAdd(parsed source.ParsedSource, ref string, opts AddOptions) (string, error) {
	if !opts.Verbose {
		spin := ui.NewSpinner("Cloning " + parsed.URL + "...")
		tmpDir, err := git.CloneRepo(parsed.URL, ref)
		spin.Stop("")
		return tmpDir, err
	}
	vlog(true, "cloning %s (shallow, --depth 1) — large monorepos can take a while:", parsed.URL)
	start := time.Now()
	tmpDir, err := git.CloneRepoWithOptions(parsed.URL, ref, git.CloneOptions{Verbose: true})
	if err == nil {
		vlog(true, "clone completed in %s", time.Since(start).Round(time.Millisecond))
	}
	return tmpDir, err
}

// reportFastPathFallback explains why the GitHub API fast-path didn't yield an
// install and that we're falling back to a full git clone. A rate-limit hit is
// actionable, so it's surfaced even without --verbose.
func reportFastPathFallback(ownerRepo string, err error, verbose bool) {
	switch {
	case blob.IsRateLimited(err):
		fmt.Fprintf(os.Stderr, "%sGitHub API rate limit reached — falling back to git clone (slower). Set GITHUB_TOKEN to avoid this.%s\n", ansiDim, ansiReset)
	case err == blob.ErrTreeTruncated:
		fmt.Fprintf(os.Stderr, "%sRepository is too large for the GitHub API path — falling back to git clone.%s\n", ansiDim, ansiReset)
	case err != nil:
		vlog(verbose, "fast-path unavailable for %s (%v), falling back to full git clone", ownerRepo, err)
	default:
		vlog(verbose, "fast-path found no installable skills for %s, falling back to full git clone", ownerRepo)
	}
}

func runAddGitOrHub(parsed source.ParsedSource, opts AddOptions, cwd, sourceInput string) {
	ownerRepo := source.GetOwnerRepo(parsed)

	if tryBlobFastInstall(parsed, opts, cwd, ownerRepo, sourceInput) {
		return
	}

	// Clone repo
	ref := parsed.Ref
	tmpDir, err := cloneForAdd(parsed, ref, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err.Error())
		os.Exit(1)
	}
	defer git.CleanupTempDir(tmpDir)

	searchRoot := tmpDir
	if parsed.Subpath != "" {
		searchRoot = filepath.Join(tmpDir, parsed.Subpath)
		if _, err := os.Stat(searchRoot); err != nil {
			fmt.Fprintf(os.Stderr, "%sSubpath not found:%s %s\n", ansiText, ansiReset, parsed.Subpath)
			os.Exit(1)
		}
	}

	skillFilter := skillFilterFromOpts(opts, parsed)
	vlog(opts.Verbose, "scanning %s for SKILL.md files (filter=%q, full-depth=%v)", searchRoot, skillFilter, opts.FullDepth)
	skills := discoverSkillsInDir(searchRoot, opts.FullDepth, skillFilter)
	vlog(opts.Verbose, "discovered %d skill(s)", len(skills))
	if len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo skills found in %s%s\n", ansiText, parsed.URL, ansiReset)
		if strings.HasPrefix(parsed.URL, "https://") {
			fmt.Fprintf(os.Stderr, "%sTip: If you're having trouble accessing a private repository, try the SSH URL format instead (e.g. git@bitbucket.org:owner/repo.git).%s\n", ansiDim, ansiReset)
		}
		os.Exit(1)
	}

	if opts.ListOnly {
		fmt.Printf("%sAvailable skills:%s\n\n", ansiText, ansiReset)
		for _, s := range skills {
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, s.Description, ansiReset)
		}
		return
	}

	// Pre-tick skills already installed from this source in the interactive picker.
	if len(opts.Skills) == 0 {
		opts.PreselectedSkills = mergeUnique(opts.PreselectedSkills, alreadyInstalledFromSource(parsed.URL, cwd))
	}

	selectedSkills, ok := selectSkills(skills, opts)
	if !ok {
		return
	}
	if !checkDiskSkillsMarkdownForHiddenChars(selectedSkills, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	// Start audit concurrently while scope/agent prompts run
	auditCh := startInstallAudit(ownerRepo, parsed.Type, opts.SkipAudit, selectedSkills)

	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}

	if !confirmInstallAfterAudit(<-auditCh, opts.Yes, opts.FailOnAudit) {
		return
	}

	fmt.Println()

	// Build lock entry
	lockRef := ref
	if lockRef == "" {
		lockRef = git.DefaultBranch(parsed.URL)
	}
	baseLockEntry := lock.SkillLockEntry{
		Source:     stripSourceRef(sourceInput),
		SourceType: string(parsed.Type),
		SourceURL:  parsed.URL,
		Ref:        lockRef,
	}

	installSkillsForAgents(selectedSkills, agents, global, mode, baseLockEntry, cwd, tmpDir)

	maybeShowFindPrompt(cwd)
}

// ─── Blob fast install ─────────────────────────────────────────────────────────

func filterBlobSkillsByName(skills []*blob.BlobSkill, filter string) []*blob.BlobSkill {
	var keep []*blob.BlobSkill
	for _, s := range skills {
		if strings.EqualFold(s.Name, filter) || strings.EqualFold(blob.ToSkillSlug(s.Name), filter) {
			keep = append(keep, s)
		}
	}
	return keep
}

func filterBlobSkillsByNames(skills []*blob.BlobSkill, names []string) []*blob.BlobSkill {
	var keep []*blob.BlobSkill
	for _, s := range skills {
		for _, name := range names {
			if strings.EqualFold(s.Name, name) || strings.EqualFold(blob.ToSkillSlug(s.Name), name) {
				keep = append(keep, s)
				break
			}
		}
	}
	return keep
}

func selectBlobSkills(skills []*blob.BlobSkill, opts AddOptions) ([]*blob.BlobSkill, bool) {
	if (len(opts.Skills) > 0 && opts.Skills[0] == "*") || opts.Yes || len(skills) == 1 {
		return skills, true
	}
	options := make([]ui.UIOption, len(skills))
	for i, s := range skills {
		options[i] = ui.UIOption{Label: s.Name, Value: blob.ToSkillSlug(s.Name), Hint: s.Description}
	}
	var initSel []int
	for _, pre := range opts.PreselectedSkills {
		for i, s := range skills {
			if strings.EqualFold(s.Name, pre) || strings.EqualFold(blob.ToSkillSlug(s.Name), sanitizeName(pre)) {
				initSel = append(initSel, i)
				break
			}
		}
	}
	indices, ok := ui.UiSearchMultiselect("Which skills would you like to install?", options, nil, initSel, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []*blob.BlobSkill
	for _, i := range indices {
		selected = append(selected, skills[i])
	}
	return selected, true
}

func installBlobSkillsForAgents(selectedBlob []*blob.BlobSkill, agents []string, global bool, mode InstallMode, result *blob.BlobInstallResult, ref, sourceInput string, parsed source.ParsedSource, cwd string) {
	for _, bSkill := range selectedBlob {
		sName := sanitizeName(bSkill.Name)
		fmt.Printf("%sInstalling %s%s%s...\n", ansiDim, ansiText, bSkill.Name, ansiReset)
		files := make([]struct{ Path, Contents string }, len(bSkill.Files))
		for i, f := range bSkill.Files {
			files[i] = struct{ Path, Contents string }{f.Path, f.Contents}
		}
		var failedAgents []string
		for _, agentName := range agents {
			r := installSkillFilesForAgent(sName, files, agentName, global, mode)
			if !r.Success {
				failedAgents = append(failedAgents, agentName)
			}
		}
		if len(failedAgents) == 0 {
			ui.LogSuccess(bSkill.Name)
		} else {
			ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", bSkill.Name, strings.Join(failedAgents, ", ")))
		}
		if global {
			if err := lock.AddSkillToLock(sName, lock.SkillLockEntry{
				Source:     stripSourceRef(sourceInput),
				SourceType: string(source.SourceTypeGitHub),
				SourceURL:  parsed.URL,
				Ref:        ref,
				SkillPath:  bSkill.RepoPath,
				PluginName: sName,
			}); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		} else {
			if err := lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     stripSourceRef(sourceInput),
				Ref:        ref,
				SourceType: string(source.SourceTypeGitHub),
				SkillPath:  bSkill.RepoPath,
			}, cwd); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		}
	}
}

func runAddBlob(result *blob.BlobInstallResult, parsed source.ParsedSource, opts AddOptions, cwd, ownerRepo, sourceInput string) {
	skills := result.Skills

	if len(opts.Skills) > 0 && opts.Skills[0] != "*" {
		skills = filterBlobSkillsByNames(skills, opts.Skills)
	} else {
		skillFilter := skillFilterFromOpts(opts, parsed)
		if skillFilter != "" && skillFilter != "*" {
			skills = filterBlobSkillsByName(skills, skillFilter)
		}
	}
	if len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo matching skills found.%s\n", ansiText, ansiReset)
		os.Exit(1)
	}

	if opts.ListOnly {
		fmt.Printf("%sAvailable skills:%s\n\n", ansiText, ansiReset)
		for _, s := range skills {
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, s.Description, ansiReset)
		}
		return
	}

	// Pre-tick skills already installed from this source in the interactive picker.
	if len(opts.Skills) == 0 {
		opts.PreselectedSkills = mergeUnique(opts.PreselectedSkills, alreadyInstalledFromSource(parsed.URL, cwd))
	}

	selectedBlob, ok := selectBlobSkills(skills, opts)
	if !ok {
		return
	}
	if !checkBlobSkillsMarkdownForHiddenChars(selectedBlob, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	auditCh := startBlobInstallAudit(ownerRepo, opts.SkipAudit, selectedBlob)
	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}
	if !confirmInstallAfterAudit(<-auditCh, opts.Yes, opts.FailOnAudit) {
		return
	}

	ref := result.Tree.Branch
	if parsed.Ref != "" {
		ref = parsed.Ref
	}

	fmt.Println()
	installBlobSkillsForAgents(selectedBlob, agents, global, mode, result, ref, sourceInput, parsed, cwd)
	fmt.Println()
	printInstallSummary(len(selectedBlob), global, agents, mode)
	maybeShowFindPrompt(cwd)
}

// ─── Shared install logic ──────────────────────────────────────────────────────

func installSkillsForAgents(skills []*skill.Skill, agents []string, global bool, mode InstallMode, baseLockEntry lock.SkillLockEntry, cwd string, cloneDir string) {
	for _, s := range skills {
		sName := sanitizeName(s.Name)
		fmt.Printf("%sInstalling %s%s%s...\n", ansiDim, ansiText, s.Name, ansiReset)

		var failedAgents []string
		for _, agentName := range agents {
			result := installSkillForAgent(s, agentName, global, mode)
			if !result.Success {
				failedAgents = append(failedAgents, agentName)
			}
		}

		if len(failedAgents) == 0 {
			ui.LogSuccess(s.Name)
		} else {
			ui.LogWarn(fmt.Sprintf("%s (failed for: %s)", s.Name, strings.Join(failedAgents, ", ")))
		}

		skillPath := skillMdRepoPath(s.Path, cloneDir)

		lockEntry := baseLockEntry
		lockEntry.PluginName = sName
		lockEntry.SkillPath = skillPath

		if global {
			if err := lock.AddSkillToLock(sName, lockEntry); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		} else {
			localSrc := baseLockEntry.Source
			if baseLockEntry.SourceType == string(source.SourceTypeLocal) {
				localSrc = toRelSourcePath(localSrc, cwd)
			}
			if err := lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     localSrc,
				Ref:        baseLockEntry.Ref,
				SourceType: baseLockEntry.SourceType,
				SkillPath:  skillPath,
			}, cwd); err != nil {
				ui.LogWarn(fmt.Sprintf("could not update lock file: %v", err))
			}
		}
	}

	fmt.Println()
	printInstallSummary(len(skills), global, agents, mode)
}

// skillMdRepoPath returns the repo-relative path to the SKILL.md file for a
// skill discovered during a git clone. cloneDir is the root of the clone; when
// empty (local or blob installs) an empty string is returned.
func skillMdRepoPath(skillPath, cloneDir string) string {
	if cloneDir == "" || skillPath == "" {
		return ""
	}
	rel, err := filepath.Rel(cloneDir, skillPath)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "SKILL.md"
	}
	return rel + "/SKILL.md"
}

// toRelSourcePath converts an absolute local path to a path relative to cwd,
// using forward slashes so the lock file is portable across platforms. Falls
// back to absPath when relativization is not possible (e.g. different Windows
// drives).
func toRelSourcePath(absPath, cwd string) string {
	rel, err := filepath.Rel(cwd, absPath)
	if err != nil || filepath.IsAbs(rel) {
		return absPath
	}
	rel = filepath.ToSlash(rel)
	if rel != "." && !strings.HasPrefix(rel, "..") {
		rel = "./" + rel
	}
	return rel
}

// ─── Skill discovery ───────────────────────────────────────────────────────────

func discoverSkillsInDir(dir string, fullDepth bool, skillFilter string) []*skill.Skill {
	maxDepth := 5
	if fullDepth {
		maxDepth = 20
	}

	allFound := skill.FindSkillDirs(dir, 0, maxDepth)

	var skills []*skill.Skill
	seen := map[string]bool{}
	for _, skillMd := range allFound {
		s, err := skill.ParseSkillMd(filepath.Join(skillMd, "SKILL.md"), false)
		if err != nil || s == nil {
			continue
		}
		if seen[s.Name] {
			continue
		}
		seen[s.Name] = true
		skills = append(skills, s)
	}

	if skillFilter != "" && skillFilter != "*" {
		var filtered []*skill.Skill
		for _, s := range skills {
			if skillNameMatches(s.Name, skillFilter) {
				filtered = append(filtered, s)
			}
		}
		skills = filtered
	}

	return skills
}

// ─── Prompt helpers ────────────────────────────────────────────────────────────

func filterSkillsByName(skills []*skill.Skill, names []string) []*skill.Skill {
	var filtered []*skill.Skill
	for _, s := range skills {
		for _, f := range names {
			if skillNameMatches(s.Name, f) {
				filtered = append(filtered, s)
				break
			}
		}
	}
	return filtered
}

func selectSkills(skills []*skill.Skill, opts AddOptions) ([]*skill.Skill, bool) {
	if (len(opts.Skills) > 0 && opts.Skills[0] == "*") || opts.Yes || len(skills) == 1 {
		if len(opts.Skills) > 0 && opts.Skills[0] != "*" {
			filtered := filterSkillsByName(skills, opts.Skills)
			if len(filtered) == 0 {
				fmt.Fprintf(os.Stderr, "%sNo matching skills found.%s\n", ansiText, ansiReset)
				return nil, false
			}
			return filtered, true
		}
		return skills, true
	}

	if len(opts.Skills) > 0 {
		filtered := filterSkillsByName(skills, opts.Skills)
		if len(filtered) == 0 {
			fmt.Fprintf(os.Stderr, "%sNo matching skills found.%s\n", ansiText, ansiReset)
			return nil, false
		}
		return filtered, true
	}

	skills, initSel := reorderSkillsPreselectedFirst(skills, opts.PreselectedSkills)
	options := make([]ui.UIOption, len(skills))
	for i, s := range skills {
		options[i] = ui.UIOption{Label: s.Name, Value: sanitizeName(s.Name), Hint: s.Description}
	}
	indices, ok := ui.UiSearchMultiselect("Which skills would you like to install?", options, nil, initSel, true)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []*skill.Skill
	for _, i := range indices {
		selected = append(selected, skills[i])
	}
	return selected, true
}

// promptScopeAndAgents asks for global/project scope and agent selection.
// Returns (global bool, mode InstallMode, agents []string, ok bool).
func promptScopeAndAgents(opts AddOptions, cwd string) (bool, InstallMode, []string, bool) {
	// Determine scope
	global := opts.Global
	if !global && !opts.Project && !opts.Yes {
		idx, ok := ui.UiSelect("Install scope?", []ui.UIOption{
			{Label: "Project", Hint: "installs for this project only"},
			{Label: "Global", Hint: "installs for your user account"},
		})
		if !ok {
			return false, "", nil, false
		}
		global = idx == 1
	}

	// Determine install mode
	mode := InstallModeSymlink
	if opts.Copy {
		mode = InstallModeCopy
	}

	// Determine agents
	agents, ok := promptAgents(opts, global, cwd)
	if !ok {
		return false, "", nil, false
	}

	return global, mode, agents, true
}

func allAgentsForScope(global bool) []string {
	var all []string
	for name := range agent.AllAgents {
		if global && agent.AllAgents[name].GlobalSkillsDir == "" {
			continue
		}
		all = append(all, name)
	}
	sort.Strings(all)
	return all
}

func validateNamedAgents(names []string) ([]string, bool) {
	var validated []string
	for _, a := range names {
		if agent.AllAgents[a] != nil {
			validated = append(validated, a)
		} else {
			ui.LogWarn("Unknown agent: " + a)
		}
	}
	if len(validated) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo valid agents specified.%s\n", ansiText, ansiReset)
		return nil, false
	}
	return validated, true
}

func buildDetectedUniqueAgents(detected []string) []string {
	var result []string
	for _, a := range agent.GetUniqueSkillsDirAgents() {
		for _, d := range detected {
			if d == a {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

func buildUniqueAgentOptions(detectedUnique []string, global bool) []ui.UIOption {
	var options []ui.UIOption
	for _, a := range detectedUnique {
		cfg := agent.AllAgents[a]
		if cfg == nil || (global && cfg.GlobalSkillsDir == "") {
			continue
		}
		options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: a})
	}
	for name, cfg := range agent.AllAgents {
		if agent.UsesSharedSkillsDir(name) || (global && cfg.GlobalSkillsDir == "") {
			continue
		}
		alreadyIn := false
		for _, opt := range options {
			if opt.Value == name {
				alreadyIn = true
				break
			}
		}
		if !alreadyIn {
			options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: name, Hint: "not detected"})
		}
	}
	return options
}

func buildLockedAgentOptions(global bool) []ui.UIOption {
	var lockedOptions []ui.UIOption
	for _, a := range agent.GetSharedSkillsDirAgents() {
		cfg := agent.AllAgents[a]
		if cfg == nil || (global && cfg.GlobalSkillsDir == "") {
			continue
		}
		lockedOptions = append(lockedOptions, ui.UIOption{Label: cfg.DisplayName, Value: a})
	}
	return lockedOptions
}

func computeAgentInitSel(options []ui.UIOption, detectedUnique []string, lastSelected []string) []int {
	lastSelectedSet := map[string]bool{}
	for _, a := range lastSelected {
		lastSelectedSet[a] = true
	}
	var initSel []int
	for i, opt := range options {
		if lastSelectedSet[opt.Value] {
			initSel = append(initSel, i)
		}
	}
	if len(initSel) == 0 {
		for i, opt := range options {
			for _, d := range detectedUnique {
				if opt.Value == d {
					initSel = append(initSel, i)
					break
				}
			}
		}
	}
	return initSel
}

// promptAgentsYes resolves the agent list when --yes is set, using configured
// agents when available and falling back to detected agents.
func promptAgentsYes(configured []string, lockedOptions, options []ui.UIOption, detected []string) []string {
	if len(configured) > 0 {
		// configuredAgents only holds non-universal agents; always expand
		// with the locked universal agents for the actual installation.
		added := make(map[string]bool)
		var result []string
		for _, lo := range lockedOptions {
			result = append(result, lo.Value)
			added[lo.Value] = true
		}
		for _, c := range configured {
			if !added[c] {
				result = append(result, c)
			}
		}
		return result
	}
	return yesAgents(options, lockedOptions, detected)
}

func yesAgents(options []ui.UIOption, lockedOptions []ui.UIOption, detected []string) []string {
	var result []string
	for _, lo := range lockedOptions {
		result = append(result, lo.Value)
	}
	detectedSet := map[string]bool{}
	for _, d := range detected {
		detectedSet[d] = true
	}
	for _, opt := range options {
		if detectedSet[opt.Value] {
			result = append(result, opt.Value)
		}
	}
	return result
}

// promptAgents returns the list of agents to install to.
func promptAgents(opts AddOptions, global bool, cwd string) ([]string, bool) {
	if len(opts.Agents) > 0 && opts.Agents[0] == "*" {
		return allAgentsForScope(global), true
	}
	if len(opts.Agents) > 0 {
		return validateNamedAgents(opts.Agents)
	}

	detected := agent.DetectInstalledAgents()
	detectedUnique := buildDetectedUniqueAgents(detected)
	options := buildUniqueAgentOptions(detectedUnique, global)
	lockedOptions := buildLockedAgentOptions(global)

	// If the user has a configured agent list for this scope, use it as the
	// default — for both --yes and the interactive picker.
	configured := lock.GetConfiguredAgents(global, cwd)

	if opts.Yes {
		return promptAgentsYes(configured, lockedOptions, options, detected), true
	}

	if len(options) == 0 && len(lockedOptions) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo agents available.%s\n", ansiText, ansiReset)
		return nil, false
	}
	if len(options) == 0 {
		var result []string
		for _, lo := range lockedOptions {
			result = append(result, lo.Value)
		}
		return result, true
	}

	initSel := computeAgentInitSel(options, detectedUnique, configured)
	selectedIndices, ok := ui.UiSearchMultiselect("Which agents would you like to install to?", options, lockedOptions, initSel, false)
	if !ok {
		return nil, false
	}

	var result []string
	var userSelected []string
	for _, lo := range lockedOptions {
		result = append(result, lo.Value)
	}
	for _, i := range selectedIndices {
		result = append(result, options[i].Value)
		userSelected = append(userSelected, options[i].Value)
	}
	// Preserve any previously configured agents that weren't shown in the
	// picker (e.g. github-copilot: uses .agents/skills so it's in the locked
	// panel, but has a unique instructions file so it was explicitly configured
	// via rules link). Merge them into the saved list so they aren't wiped.
	pickerValues := make(map[string]bool)
	for _, opt := range options {
		pickerValues[opt.Value] = true
	}
	for _, c := range configured {
		if !pickerValues[c] {
			userSelected = append(userSelected, c)
		}
	}
	// Only save the user's explicit non-universal selections. Universal agents
	// (.agents/skills) are always supported — no need to track them.
	if err := lock.SetConfiguredAgents(userSelected, global, cwd); err != nil {
		ui.LogWarn(fmt.Sprintf("could not save agent preferences: %v", err))
	}
	return result, true
}

// ─── Post-install helpers ──────────────────────────────────────────────────────

// reorderSkillsPreselectedFirst moves any skills matching preselected names to
// the front of the slice and returns the reordered slice plus the initSel
// indices (always 0..len(preselected)-1 after reordering).
func reorderSkillsPreselectedFirst(skills []*skill.Skill, preselected []string) ([]*skill.Skill, []int) {
	if len(preselected) == 0 {
		return skills, nil
	}
	isPreselected := func(s *skill.Skill) bool {
		for _, pre := range preselected {
			if skillNameMatches(s.Name, pre) {
				return true
			}
		}
		return false
	}
	var front, rest []*skill.Skill
	for _, s := range skills {
		if isPreselected(s) {
			front = append(front, s)
		} else {
			rest = append(rest, s)
		}
	}
	reordered := append(front, rest...)
	initSel := make([]int, len(front))
	for i := range front {
		initSel[i] = i
	}
	return reordered, initSel
}

func printInstallSummary(count int, global bool, agents []string, mode InstallMode) {
	scope := "project"
	if global {
		scope = "global"
	}
	noun := "skill"
	if count != 1 {
		noun = "skills"
	}
	fmt.Printf("%s✓ Installed %d %s (%s scope, %s mode)%s\n", ansiText, count, noun, scope, string(mode), ansiReset)
	if len(agents) > 0 {
		var displayNames []string
		for _, a := range agents {
			if cfg := agent.AllAgents[a]; cfg != nil {
				displayNames = append(displayNames, cfg.DisplayName)
			} else {
				displayNames = append(displayNames, a)
			}
		}
		fmt.Printf("%s  Agents: %s%s\n", ansiDim, strings.Join(displayNames, ", "), ansiReset)
	}
	fmt.Println()
}

func maybeShowFindPrompt(cwd string) {
	if lock.IsPromptDismissed("findSkillsPrompt") {
		return
	}
	fmt.Printf("%sTip:%s Run %smdm skills find%s to discover more skills.\n", ansiDim, ansiReset, ansiText, ansiReset)
	_ = lock.DismissPrompt("findSkillsPrompt")
}

// ─── Install-time security audit ──────────────────────────────────────────────

type installAuditEntry struct {
	Name    string
	SkillID string // e.g. "owner/repo/skill-slug" or "owner/repo" for OSV
	Audits  []auditProvider
}

func startInstallAudit(ownerRepo string, srcType source.SourceType, skipAudit bool, skills []*skill.Skill) chan []installAuditEntry {
	ch := make(chan []installAuditEntry, 1)
	isPublic := srcType == source.SourceTypeGitHub && ownerRepo != ""
	if !isPublic || skipAudit {
		ch <- nil
		return ch
	}
	go func() {
		// OSV runs concurrently with the per-skill skills.sh fetches
		osvCh := make(chan *registry.OSVResult, 1)
		go func() { osvCh <- registry.FetchOSVAdvisories(ownerRepo, 5000) }()

		var results []installAuditEntry
		for _, s := range skills {
			slug := blob.ToSkillSlug(s.Name)
			skillID := ownerRepo + "/" + slug
			audits, _ := fetchSkillAudits(skillID)
			results = append(results, installAuditEntry{Name: s.Name, SkillID: skillID, Audits: audits})
		}
		if osv := <-osvCh; osv != nil && osv.Count > 0 {
			results = append(results, osvToInstallAuditEntry(ownerRepo, osv))
		}
		ch <- results
	}()
	return ch
}

func startBlobInstallAudit(ownerRepo string, skipAudit bool, skills []*blob.BlobSkill) chan []installAuditEntry {
	ch := make(chan []installAuditEntry, 1)
	if ownerRepo == "" || skipAudit {
		ch <- nil
		return ch
	}
	go func() {
		osvCh := make(chan *registry.OSVResult, 1)
		go func() { osvCh <- registry.FetchOSVAdvisories(ownerRepo, 5000) }()

		var results []installAuditEntry
		for _, s := range skills {
			slug := blob.ToSkillSlug(s.Name)
			skillID := ownerRepo + "/" + slug
			audits, _ := fetchSkillAudits(skillID)
			results = append(results, installAuditEntry{Name: s.Name, SkillID: skillID, Audits: audits})
		}
		if osv := <-osvCh; osv != nil && osv.Count > 0 {
			results = append(results, osvToInstallAuditEntry(ownerRepo, osv))
		}
		ch <- results
	}()
	return ch
}

func osvToInstallAuditEntry(ownerRepo string, osv *registry.OSVResult) installAuditEntry {
	var audits []auditProvider
	for _, a := range osv.Advisories {
		status := "warn"
		if a.Severity == registry.OSVCritical || a.Severity == registry.OSVHigh {
			status = "fail"
		}
		summary := a.ID
		if a.Summary != "" {
			summary += ": " + a.Summary
		}
		audits = append(audits, auditProvider{
			Provider:  "OSV",
			Slug:      a.ID,
			Status:    status,
			RiskLevel: string(a.Severity),
			Summary:   summary,
		})
	}
	return installAuditEntry{Name: ownerRepo, SkillID: ownerRepo, Audits: audits}
}

type auditIssue struct {
	skillName string
	skillID   string
	provider  auditProvider
}

func collectAuditIssues(entries []installAuditEntry) []auditIssue {
	var issues []auditIssue
	for _, e := range entries {
		for _, a := range e.Audits {
			if a.Status == "warn" || a.Status == "fail" {
				issues = append(issues, auditIssue{e.Name, e.SkillID, a})
			}
		}
	}
	return issues
}

func auditIssueURL(iss auditIssue) string {
	if iss.provider.Provider == "OSV" && iss.provider.Slug != "" {
		return "https://osv.dev/vulnerability/" + iss.provider.Slug
	}
	if iss.skillID != "" && iss.provider.Slug != "" {
		return "https://skills.sh/" + iss.skillID + "/security/" + iss.provider.Slug
	}
	return ""
}

func formatAuditIssueEntry(iss auditIssue) string {
	color := auditStatusColor(iss.provider.Status)
	badge := auditStatusBadge(iss.provider.Status)
	rl := ""
	if iss.provider.RiskLevel != "" && iss.provider.RiskLevel != "NONE" {
		rl = fmt.Sprintf("  %s%s%s", riskLevelColor(iss.provider.RiskLevel), iss.provider.RiskLevel, ansiReset)
	}
	s := fmt.Sprintf("  %s%s%s %s%s%s  %s%s%s%s\n",
		color, badge, ansiReset,
		ansiDim, iss.provider.Provider, ansiReset,
		ansiText, iss.skillName, ansiReset,
		rl)
	if iss.provider.Summary != "" {
		s += fmt.Sprintf("    %s%s%s\n", ansiDim, iss.provider.Summary, ansiReset)
	}
	if url := auditIssueURL(iss); url != "" {
		s += fmt.Sprintf("    %s%s%s\n", ansiDim, url, ansiReset)
	}
	return s
}

func formatAuditIssueBlock(label string, issues []auditIssue) string {
	s := fmt.Sprintf("%s⚠  %s found:%s\n\n", ansiYellow, label, ansiReset)
	for _, iss := range issues {
		s += formatAuditIssueEntry(iss)
	}
	s += "\n"
	return s
}

func printAuditIssueEntry(iss auditIssue) {
	fmt.Print(formatAuditIssueEntry(iss))
}

func confirmInstallAfterAudit(entries []installAuditEntry, autoYes, failOnAudit bool) bool {
	if len(entries) == 0 {
		return true
	}
	issues := collectAuditIssues(entries)
	if len(issues) == 0 {
		return true
	}

	hasFail := false
	for _, iss := range issues {
		if iss.provider.Status == "fail" {
			hasFail = true
			break
		}
	}
	label := "Security warnings"
	if hasFail {
		label = "Security issues"
	}
	header := formatAuditIssueBlock(label, issues)

	if autoYes {
		fmt.Print(header)
		if failOnAudit {
			fmt.Fprintf(os.Stderr, "mdm: audit-blocked\n")
			os.Exit(1)
		}
		fmt.Printf("%sContinuing with --yes flag.%s\n\n", ansiDim, ansiReset)
		return true
	}

	idx, ok := ui.UiSelectWithContext("Security findings detected. Continue?", header, []ui.UIOption{
		{Label: "Cancel installation"},
		{Label: "Install anyway", Hint: "proceed despite findings"},
	})
	fmt.Println()
	fmt.Print(header)
	if !ok || idx == 0 {
		fmt.Printf("%s⚠  Installation cancelled due to security findings.%s\n\n", ansiYellow, ansiReset)
		return false
	}
	return true
}

// alreadyInstalledFromSource returns skill names that are already recorded in either the
// global or project lock file as having been installed from parsedURL.
func alreadyInstalledFromSource(parsedURL, cwd string) []string {
	seen := map[string]bool{}
	var result []string

	// Global lock stores a canonical SourceURL field.
	globalL := lock.ReadSkillLock()
	for name, e := range globalL.Skills {
		if strings.EqualFold(e.SourceURL, parsedURL) && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	// Local lock only stores the raw Source input; re-parse to get the canonical URL.
	localL := lock.ReadLocalLock(cwd)
	for name, e := range localL.Skills {
		if strings.EqualFold(source.ParseSource(e.Source).URL, parsedURL) && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	return result
}

// mergeUnique appends items from extra into base, skipping duplicates (case-sensitive).
func mergeUnique(base, extra []string) []string {
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	result := append([]string(nil), base...)
	for _, v := range extra {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// stripSourceRef removes a trailing #fragment from a source URL so that the ref
// is stored only in the dedicated Ref field, not duplicated inside Source.
func stripSourceRef(s string) string {
	if idx := strings.LastIndex(s, "#"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func skillFilterFromOpts(opts AddOptions, parsed source.ParsedSource) string {
	if len(opts.Skills) == 1 {
		return opts.Skills[0]
	}
	if len(opts.Skills) > 1 {
		// Multiple skills requested: skip pre-filtering here and let selectSkills handle it.
		return ""
	}
	return parsed.SkillFilter
}
