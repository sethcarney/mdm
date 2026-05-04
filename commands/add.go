package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

%sExamples:%s
  mdm skills add vercel-labs/agent-skills
  mdm skills add vercel-labs/agent-skills -g
  mdm skills add vercel-labs/agent-skills -a claude-code cursor
  mdm skills add vercel-labs/agent-skills --agent claude-code --agent cursor
  mdm skills add https://github.com/owner/repo
  mdm skills add ./my-local-skill`, ansiBold, ansiReset),
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

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

// ─── Main add command ──────────────────────────────────────────────────────────

func runAdd(sourceInput string, opts AddOptions) {
	cwd, _ := os.Getwd()

	if sourceInput == "" {
		fmt.Fprintf(os.Stderr, "%sError:%s Please provide a package source.\n\n", ansiText, ansiReset)
		fmt.Printf("  %s$%s mdm skills add <package>\n\n", ansiDim, ansiReset)
		fmt.Printf("  %sExamples:%s\n", ansiDim, ansiReset)
		fmt.Printf("    mdm skills add vercel-labs/agent-skills\n")
		fmt.Printf("    mdm skills add https://github.com/owner/repo\n")
		os.Exit(1)
	}

	parsed := source.ParseSource(sourceInput)

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
	indices, ok := ui.UiMultiselect("Which skills would you like to install?", options, true, initSel, nil)
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
			_ = lock.AddSkillToLock(sName, lock.SkillLockEntry{
				Source:     sourceID,
				SourceType: string(source.SourceTypeWellKnown),
				SourceURL:  parsed.URL,
				PluginName: s.InstallName,
			})
		} else {
			_ = lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     parsed.URL,
				SourceType: string(source.SourceTypeWellKnown),
			}, cwd)
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

	selectedSkills, ok := selectSkills(skills, opts)
	if !ok {
		return
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
	}, cwd, false)
	maybeShowFindPrompt(cwd)
}

// ─── GitHub / GitLab / git ─────────────────────────────────────────────────────

func tryBlobFastInstall(parsed source.ParsedSource, opts AddOptions, cwd, ownerRepo, sourceInput string) bool {
	if parsed.Type != source.SourceTypeGitHub || ownerRepo == "" {
		return false
	}
	blobOpts := struct {
		Subpath         string
		SkillFilter     string
		Ref             string
		Token           string
		IncludeInternal bool
	}{
		Subpath:     parsed.Subpath,
		SkillFilter: skillFilterFromOpts(opts, parsed),
		Ref:         parsed.Ref,
		Token:       lock.GetGitHubToken(),
	}
	spin := ui.NewSpinner("Fetching skills...")
	blobResult, _ := blob.TryBlobInstall(ownerRepo, blobOpts)
	spin.Stop("")
	if blobResult == nil || len(blobResult.Skills) == 0 {
		return false
	}
	runAddBlob(blobResult, parsed, opts, cwd, ownerRepo, sourceInput)
	return true
}

func runAddGitOrHub(parsed source.ParsedSource, opts AddOptions, cwd, sourceInput string) {
	ownerRepo := source.GetOwnerRepo(parsed)

	if tryBlobFastInstall(parsed, opts, cwd, ownerRepo, sourceInput) {
		return
	}

	// Clone repo
	ref := parsed.Ref
	spin := ui.NewSpinner("Cloning " + parsed.URL + "...")
	tmpDir, err := git.CloneRepo(parsed.URL, ref)
	spin.Stop("")
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
	skills := discoverSkillsInDir(searchRoot, opts.FullDepth, skillFilter)
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

	// Start audit concurrently while scope/agent prompts run
	auditCh := startInstallAudit(ownerRepo, parsed.Type, opts.SkipAudit, selectedSkills)

	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}

	if !confirmInstallAfterAudit(<-auditCh, opts.Yes) {
		return
	}

	fmt.Println()

	// Build lock entry
	lockRef := ref
	if lockRef == "" {
		lockRef = "main"
	}
	commitSHA, _ := git.GetLocalCommitSHA(tmpDir)
	baseLockEntry := lock.SkillLockEntry{
		Source:     stripSourceRef(sourceInput),
		SourceType: string(parsed.Type),
		SourceURL:  parsed.URL,
		Ref:        lockRef,
		CommitSHA:  commitSHA,
	}

	installSkillsForAgents(selectedSkills, agents, global, mode, baseLockEntry, cwd, ownerRepo != "" && parsed.Type == source.SourceTypeGitHub)

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
	indices, ok := ui.UiMultiselect("Which skills would you like to install?", options, true, initSel, nil)
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
		folderHash := ""
		if result.Tree != nil {
			folderHash = blob.GetSkillFolderHashFromTree(result.Tree, bSkill.RepoPath)
		}
		if global {
			_ = lock.AddSkillToLock(sName, lock.SkillLockEntry{
				Source:          stripSourceRef(sourceInput),
				SourceType:      string(source.SourceTypeGitHub),
				SourceURL:       parsed.URL,
				Ref:             ref,
				SkillPath:       bSkill.RepoPath,
				SkillFolderHash: folderHash,
				PluginName:      sName,
			})
		} else {
			_ = lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     stripSourceRef(sourceInput),
				Ref:        ref,
				SourceType: string(source.SourceTypeGitHub),
			}, cwd)
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

	auditCh := startBlobInstallAudit(ownerRepo, opts.SkipAudit, selectedBlob)
	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}
	if !confirmInstallAfterAudit(<-auditCh, opts.Yes) {
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

func installSkillsForAgents(skills []*skill.Skill, agents []string, global bool, mode InstallMode, baseLockEntry lock.SkillLockEntry, cwd string, computeHash bool) {
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

		lockEntry := baseLockEntry
		lockEntry.PluginName = sName
		if computeHash && s.Path != "" {
			if hash, err := lock.ComputeSkillFolderHash(s.Path); err == nil {
				lockEntry.SkillFolderHash = hash
			}
		}

		if global {
			_ = lock.AddSkillToLock(sName, lockEntry)
		} else {
			_ = lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     baseLockEntry.Source,
				Ref:        baseLockEntry.Ref,
				SourceType: baseLockEntry.SourceType,
			}, cwd)
		}
	}

	fmt.Println()
	printInstallSummary(len(skills), global, agents, mode)
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
	indices, ok := ui.UiMultiselect("Which skills would you like to install?", options, true, initSel, nil)
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

func buildInstalledNonUniversal(detected []string) []string {
	var result []string
	for _, a := range agent.GetNonUniversalAgents() {
		for _, d := range detected {
			if d == a {
				result = append(result, a)
				break
			}
		}
	}
	return result
}

func buildNonUniversalOptions(installedNonUniversal []string, global bool) []ui.UIOption {
	var options []ui.UIOption
	for _, a := range installedNonUniversal {
		cfg := agent.AllAgents[a]
		if cfg == nil || (global && cfg.GlobalSkillsDir == "") {
			continue
		}
		options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: a})
	}
	for name, cfg := range agent.AllAgents {
		if agent.IsUniversalAgent(name) || (global && cfg.GlobalSkillsDir == "") {
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
	for _, a := range agent.GetUniversalAgents() {
		cfg := agent.AllAgents[a]
		if cfg == nil || (global && cfg.GlobalSkillsDir == "") {
			continue
		}
		lockedOptions = append(lockedOptions, ui.UIOption{Label: cfg.DisplayName, Value: a})
	}
	return lockedOptions
}

func computeAgentInitSel(options []ui.UIOption, installedNonUniversal []string, lastSelected []string) []int {
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
			for _, d := range installedNonUniversal {
				if opt.Value == d {
					initSel = append(initSel, i)
					break
				}
			}
		}
	}
	return initSel
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
	installedNonUniversal := buildInstalledNonUniversal(detected)
	options := buildNonUniversalOptions(installedNonUniversal, global)
	lockedOptions := buildLockedAgentOptions(global)

	// If the user has a configured agent list for this scope, use it as the
	// default — for both --yes and the interactive picker.
	configured := lock.GetConfiguredAgents(global, cwd)

	if opts.Yes {
		if len(configured) > 0 {
			return configured, true
		}
		return yesAgents(options, lockedOptions, detected), true
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

	initSel := computeAgentInitSel(options, installedNonUniversal, configured)
	selectedIndices, ok := ui.UiSearchMultiselect("Which agents would you like to install to?", options, lockedOptions, initSel)
	if !ok {
		return nil, false
	}

	var result []string
	for _, lo := range lockedOptions {
		result = append(result, lo.Value)
	}
	for _, i := range selectedIndices {
		result = append(result, options[i].Value)
	}
	_ = lock.SetConfiguredAgents(result, global, cwd)
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
	Name   string
	Audits []auditProvider
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
			audits, _ := fetchSkillAudits(ownerRepo + "/" + slug)
			results = append(results, installAuditEntry{Name: s.Name, Audits: audits})
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
			audits, _ := fetchSkillAudits(ownerRepo + "/" + slug)
			results = append(results, installAuditEntry{Name: s.Name, Audits: audits})
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
			Status:    status,
			RiskLevel: string(a.Severity),
			Summary:   summary,
		})
	}
	return installAuditEntry{Name: ownerRepo, Audits: audits}
}

type auditIssue struct {
	skillName string
	provider  auditProvider
}

func collectAuditIssues(entries []installAuditEntry) []auditIssue {
	var issues []auditIssue
	for _, e := range entries {
		for _, a := range e.Audits {
			if a.Status == "warn" || a.Status == "fail" {
				issues = append(issues, auditIssue{e.Name, a})
			}
		}
	}
	return issues
}

func printAuditIssueEntry(iss auditIssue) {
	color := auditStatusColor(iss.provider.Status)
	badge := auditStatusBadge(iss.provider.Status)
	rl := ""
	if iss.provider.RiskLevel != "" && iss.provider.RiskLevel != "NONE" {
		rl = fmt.Sprintf("  %s%s%s", riskLevelColor(iss.provider.RiskLevel), iss.provider.RiskLevel, ansiReset)
	}
	fmt.Printf("  %s%s%s %s%s%s  %s%s%s%s\n",
		color, badge, ansiReset,
		ansiDim, iss.provider.Provider, ansiReset,
		ansiText, iss.skillName, ansiReset,
		rl)
	if iss.provider.Summary != "" {
		fmt.Printf("    %s%s%s\n", ansiDim, iss.provider.Summary, ansiReset)
	}
}

func confirmInstallAfterAudit(entries []installAuditEntry, autoYes bool) bool {
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
	fmt.Printf("%s⚠  %s found:%s\n\n", ansiYellow, label, ansiReset)
	for _, iss := range issues {
		printAuditIssueEntry(iss)
	}
	fmt.Println()

	if autoYes {
		fmt.Printf("%sContinuing with --yes flag.%s\n\n", ansiDim, ansiReset)
		return true
	}
	idx, ok := ui.UiSelect("Security findings detected. Continue?", []ui.UIOption{
		{Label: "Cancel installation"},
		{Label: "Install anyway", Hint: "proceed despite findings"},
	})
	if !ok || idx == 0 {
		fmt.Println("Installation cancelled.")
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
