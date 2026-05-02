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
		Aliases: []string{"a", "install", "i"},
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

func runAddWellKnown(parsed source.ParsedSource, opts AddOptions, cwd string) {
	spin := ui.NewSpinner("Fetching skills from " + parsed.URL + "...")
	skills, err := registry.FetchAllWellKnownSkills(parsed.URL)
	spin.Stop("")
	if err != nil || len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo skills found at %s%s\n", ansiText, parsed.URL, ansiReset)
		os.Exit(1)
	}

	// Apply skill filter
	filtered := skills
	if len(opts.Skills) > 0 && opts.Skills[0] != "*" {
		var keep []*registry.WellKnownSkill
		for _, s := range skills {
			for _, f := range opts.Skills {
				if strings.EqualFold(s.Name, f) || strings.EqualFold(s.InstallName, f) {
					keep = append(keep, s)
					break
				}
			}
		}
		filtered = keep
	}

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

	// Skill selection (well-known path)
	var selectedSkills []*registry.WellKnownSkill
	if len(opts.Skills) > 0 && opts.Skills[0] == "*" {
		selectedSkills = filtered
	} else if len(filtered) == 1 || opts.Yes {
		selectedSkills = filtered
	} else {
		// Move preselected to front
		var front, rest []*registry.WellKnownSkill
		for _, s := range filtered {
			pre := false
			for _, p := range opts.PreselectedSkills {
				if strings.EqualFold(s.Name, p) || strings.EqualFold(sanitizeName(s.Name), sanitizeName(p)) {
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
			return
		}
		for _, i := range indices {
			selectedSkills = append(selectedSkills, filtered[i])
		}
	}

	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}

	sourceID := registry.GetWellKnownSourceIdentifier(parsed.URL)
	fmt.Println()

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

		// Update global lock
		ownerRepo := ""
		_ = lock.AddSkillToLock(sName, lock.SkillLockEntry{
			Source:     sourceID,
			SourceType: string(source.SourceTypeWellKnown),
			SourceURL:  parsed.URL,
			PluginName: s.InstallName,
		})

		// Update local lock if project scope
		if !global {
			_ = lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     parsed.URL,
				SourceType: string(source.SourceTypeWellKnown),
			}, cwd)
		}

		_ = ownerRepo
	}

	fmt.Println()
	printInstallSummary(len(selectedSkills), global, agents, mode)
	maybeShowFindPrompt(cwd)
}

// ─── Local path ────────────────────────────────────────────────────────────────

func runAddLocal(parsed source.ParsedSource, opts AddOptions, cwd string) {
	localPath := parsed.LocalPath
	if !pathExists(localPath) {
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

	selectedSkills := selectSkills(skills, opts)

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

func runAddGitOrHub(parsed source.ParsedSource, opts AddOptions, cwd, sourceInput string) {
	ownerRepo := source.GetOwnerRepo(parsed)
	token := lock.GetGitHubToken()

	// Try blob fast-install (vercel/vercel-labs only)
	if parsed.Type == source.SourceTypeGitHub && ownerRepo != "" {
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
			Token:       token,
		}

		spin := ui.NewSpinner("Fetching skills...")
		blobResult, _ := blob.TryBlobInstall(ownerRepo, blobOpts)
		spin.Stop("")

		if blobResult != nil && len(blobResult.Skills) > 0 {
			runAddBlob(blobResult, parsed, opts, cwd, ownerRepo, sourceInput)
			return
		}
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
		if !pathExists(searchRoot) {
			fmt.Fprintf(os.Stderr, "%sSubpath not found:%s %s\n", ansiText, ansiReset, parsed.Subpath)
			os.Exit(1)
		}
	}

	skillFilter := skillFilterFromOpts(opts, parsed)
	skills := discoverSkillsInDir(searchRoot, opts.FullDepth, skillFilter)
	if len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo skills found in %s%s\n", ansiText, parsed.URL, ansiReset)
		os.Exit(1)
	}

	if opts.ListOnly {
		fmt.Printf("%sAvailable skills:%s\n\n", ansiText, ansiReset)
		for _, s := range skills {
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, s.Description, ansiReset)
		}
		return
	}

	selectedSkills := selectSkills(skills, opts)

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
	baseLockEntry := lock.SkillLockEntry{
		Source:     sourceInput,
		SourceType: string(parsed.Type),
		SourceURL:  parsed.URL,
		Ref:        lockRef,
	}

	installSkillsForAgents(selectedSkills, agents, global, mode, baseLockEntry, cwd, ownerRepo != "" && parsed.Type == source.SourceTypeGitHub)

	maybeShowFindPrompt(cwd)
}

// ─── Blob fast install ─────────────────────────────────────────────────────────

func runAddBlob(result *blob.BlobInstallResult, parsed source.ParsedSource, opts AddOptions, cwd, ownerRepo, sourceInput string) {
	skills := result.Skills

	// Apply skill filter
	skillFilter := skillFilterFromOpts(opts, parsed)
	if skillFilter != "" && skillFilter != "*" {
		var keep []*blob.BlobSkill
		for _, s := range skills {
			if strings.EqualFold(s.Name, skillFilter) || strings.EqualFold(blob.ToSkillSlug(s.Name), skillFilter) {
				keep = append(keep, s)
			}
		}
		skills = keep
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

	// Skill selection (blob/GitHub path)
	var selectedBlob []*blob.BlobSkill
	if (len(opts.Skills) > 0 && opts.Skills[0] == "*") || opts.Yes || len(skills) == 1 {
		selectedBlob = skills
	} else {
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
			return
		}
		for _, i := range indices {
			selectedBlob = append(selectedBlob, skills[i])
		}
	}

	// Start audit concurrently while scope/agent prompts run
	auditCh := startBlobInstallAudit(ownerRepo, opts.SkipAudit, selectedBlob)

	global, mode, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		return
	}

	if !confirmInstallAfterAudit(<-auditCh, opts.Yes) {
		return
	}

	fmt.Println()
	ref := result.Tree.Branch
	if parsed.Ref != "" {
		ref = parsed.Ref
	}

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

		// Compute folder hash from tree
		folderHash := ""
		if result.Tree != nil {
			folderHash = blob.GetSkillFolderHashFromTree(result.Tree, bSkill.RepoPath)
		}

		_ = lock.AddSkillToLock(sName, lock.SkillLockEntry{
			Source:          sourceInput,
			SourceType:      string(source.SourceTypeGitHub),
			SourceURL:       parsed.URL,
			Ref:             ref,
			SkillPath:       bSkill.RepoPath,
			SkillFolderHash: folderHash,
			PluginName:      sName,
		})
		if !global {
			_ = lock.AddSkillToLocalLock(sName, lock.LocalSkillLockEntry{
				Source:     sourceInput,
				Ref:        ref,
				SourceType: string(source.SourceTypeGitHub),
			}, cwd)
		}
	}

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

		_ = lock.AddSkillToLock(sName, lockEntry)
		if !global {
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
			if strings.EqualFold(s.Name, skillFilter) || strings.EqualFold(sanitizeName(s.Name), sanitizeName(skillFilter)) {
				filtered = append(filtered, s)
			}
		}
		skills = filtered
	}

	return skills
}

// ─── Prompt helpers ────────────────────────────────────────────────────────────

func selectSkills(skills []*skill.Skill, opts AddOptions) []*skill.Skill {
	if (len(opts.Skills) > 0 && opts.Skills[0] == "*") || opts.Yes || len(skills) == 1 {
		if len(opts.Skills) > 0 && opts.Skills[0] != "*" {
			// Filter to only named skills
			var filtered []*skill.Skill
			for _, s := range skills {
				for _, f := range opts.Skills {
					if strings.EqualFold(s.Name, f) || strings.EqualFold(sanitizeName(s.Name), sanitizeName(f)) {
						filtered = append(filtered, s)
						break
					}
				}
			}
			return filtered
		}
		return skills
	}

	if len(opts.Skills) > 0 {
		var filtered []*skill.Skill
		for _, s := range skills {
			for _, f := range opts.Skills {
				if strings.EqualFold(s.Name, f) || strings.EqualFold(sanitizeName(s.Name), sanitizeName(f)) {
					filtered = append(filtered, s)
					break
				}
			}
		}
		return filtered
	}

	skills, initSel := reorderSkillsPreselectedFirst(skills, opts.PreselectedSkills)
	options := make([]ui.UIOption, len(skills))
	for i, s := range skills {
		options[i] = ui.UIOption{Label: s.Name, Value: sanitizeName(s.Name), Hint: s.Description}
	}
	indices, ok := ui.UiMultiselect("Which skills would you like to install?", options, true, initSel, nil)
	if !ok {
		fmt.Println("Cancelled.")
		os.Exit(0)
	}
	var selected []*skill.Skill
	for _, i := range indices {
		selected = append(selected, skills[i])
	}
	return selected
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

// promptAgents returns the list of agents to install to.
func promptAgents(opts AddOptions, global bool, cwd string) ([]string, bool) {
	if len(opts.Agents) > 0 && opts.Agents[0] == "*" {
		var all []string
		for name := range agent.AllAgents {
			if global && agent.AllAgents[name].GlobalSkillsDir == "" {
				continue
			}
			all = append(all, name)
		}
		sort.Strings(all)
		return all, true
	}

	if len(opts.Agents) > 0 {
		var validated []string
		for _, a := range opts.Agents {
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

	// Detect installed agents
	detected := agent.DetectInstalledAgents()

	// Universal agents (always shown as locked in UI)
	universalAgents := agent.GetUniversalAgents()
	nonUniversal := agent.GetNonUniversalAgents()

	// Filter to installed non-universal agents
	var installedNonUniversal []string
	for _, a := range nonUniversal {
		for _, d := range detected {
			if d == a {
				installedNonUniversal = append(installedNonUniversal, a)
				break
			}
		}
	}

	// Build UI options from non-universal installed agents
	var options []ui.UIOption
	for _, a := range installedNonUniversal {
		cfg := agent.AllAgents[a]
		if cfg == nil {
			continue
		}
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		options = append(options, ui.UIOption{Label: cfg.DisplayName, Value: a})
	}

	// Add non-installed agents at the bottom
	for name, cfg := range agent.AllAgents {
		if agent.IsUniversalAgent(name) {
			continue
		}
		if global && cfg.GlobalSkillsDir == "" {
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

	// Build locked options for universal agents
	var lockedOptions []ui.UIOption
	for _, a := range universalAgents {
		cfg := agent.AllAgents[a]
		if cfg == nil {
			continue
		}
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		lockedOptions = append(lockedOptions, ui.UIOption{Label: cfg.DisplayName, Value: a})
	}

	// Load last selected agents as initial selection
	lastSelected := lock.GetLastSelectedAgents()
	lastSelectedSet := map[string]bool{}
	for _, a := range lastSelected {
		lastSelectedSet[a] = true
	}

	// Initial selection: last selected non-universal agents
	var initSel []int
	for i, opt := range options {
		if lastSelectedSet[opt.Value] {
			initSel = append(initSel, i)
		}
	}
	// If nothing was previously selected, select detected agents by default
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

	message := "Which agents would you like to install to?"
	if opts.Yes {
		// Return all detected + universal
		var result []string
		for _, lo := range lockedOptions {
			result = append(result, lo.Value)
		}
		for _, opt := range options {
			alreadyDetected := false
			for _, d := range detected {
				if d == opt.Value {
					alreadyDetected = true
					break
				}
			}
			if alreadyDetected {
				result = append(result, opt.Value)
			}
		}
		return result, true
	}

	if len(options) == 0 && len(lockedOptions) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo agents available.%s\n", ansiText, ansiReset)
		return nil, false
	}

	// If only universal agents, skip prompt
	if len(options) == 0 {
		var result []string
		for _, lo := range lockedOptions {
			result = append(result, lo.Value)
		}
		return result, true
	}

	selectedIndices, ok := ui.UiSearchMultiselect(message, options, lockedOptions, initSel)
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

	// Save selection for next time
	_ = lock.SaveSelectedAgents(result)

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
			if strings.EqualFold(s.Name, pre) || strings.EqualFold(sanitizeName(s.Name), sanitizeName(pre)) {
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
	isPublic := (srcType == source.SourceTypeGitHub || srcType == source.SourceTypeGitLab) && ownerRepo != ""
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
			slug := sanitizeName(s.Name)
			results = append(results, installAuditEntry{Name: s.Name, Audits: fetchSkillAudits(ownerRepo + "/" + slug)})
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
			results = append(results, installAuditEntry{Name: s.Name, Audits: fetchSkillAudits(ownerRepo + "/" + slug)})
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

func confirmInstallAfterAudit(entries []installAuditEntry, autoYes bool) bool {
	if len(entries) == 0 {
		return true
	}

	type issue struct {
		skillName string
		provider  auditProvider
	}
	var issues []issue
	for _, e := range entries {
		for _, a := range e.Audits {
			if a.Status == "warn" || a.Status == "fail" {
				issues = append(issues, issue{e.Name, a})
			}
		}
	}
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

func skillFilterFromOpts(opts AddOptions, parsed source.ParsedSource) string {
	if len(opts.Skills) > 0 {
		return opts.Skills[0]
	}
	return parsed.SkillFilter
}
