package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/fork"
	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

// defaultForksDir is where cherry-picked skills land, relative to the project
// root. It matches the directory `mdm skills add <repo>` already scans first,
// so a fork is publishable the moment it is committed.
const defaultForksDir = "skills"

type CherryPickOptions struct {
	Dir              string
	Skills           []string
	As               string
	Agents           []string
	Install          bool
	Global           bool
	Project          bool
	Copy             bool
	Yes              bool
	Force            bool
	DryRun           bool
	ListOnly         bool
	Status           bool
	FullDepth        bool
	AllowHiddenChars bool
	NoAttribution    bool
}

func buildCherryPickCmd(ver string) *cobra.Command {
	var opts CherryPickOptions

	cmd := &cobra.Command{
		Use:     "cherry-pick [source]",
		Short:   "Fork third-party skills into this project as your own",
		Aliases: []string{"fork", "cp"},
		Long: fmt.Sprintf(`Copy selected skills out of someone else's repository and into your own,
so you can edit them and ship them as yours.

Unlike %smdm skills add%s — which installs a skill into each agent's skills
directory and tracks the upstream source so %smdm skills update%s can replace it
— cherry-pick vendors the skill into ./%s, where it becomes part of your
repository. Nothing overwrites it afterwards; divergence from upstream is the
point.

Every forked skill carries its provenance: %s%s%s records the source, ref,
commit, and license machine-readably, and %s%s%s states the same in prose. When
the source ships a license file that the skill directory itself does not, it is
copied in as %s%s%s.

%sLicensing:%s a fork redistributes someone else's work. mdm records what it can
find and warns when a source declares no license at all, but honouring the terms
— attribution, share-alike, or asking first — is yours to do.

%sExamples:%s
  mdm skills cherry-pick vercel-labs/agent-skills
  mdm skills cherry-pick owner/repo#v1.2.0 -s code-review
  mdm skills cherry-pick owner/repo -s code-review --as our-code-review
  mdm skills cherry-pick ./.agents/skills/code-review     # fork one you already installed
  mdm skills cherry-pick owner/repo -s code-review --install -a claude-code
  mdm skills cherry-pick --status`,
			ansiBold, ansiReset, ansiBold, ansiReset, defaultForksDir,
			ansiText, fork.OriginFileName, ansiReset,
			ansiText, fork.AttributionFileName, ansiReset,
			ansiText, fork.UpstreamLicenseFileName, ansiReset,
			ansiBold, ansiReset, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			fmt.Println()
			if opts.Status {
				runCherryPickStatus(opts)
				return
			}
			if len(args) == 0 || args[0] == "" {
				fmt.Fprintf(os.Stderr, "%sError:%s Please provide a source to cherry-pick from.\n\n", ansiText, ansiReset)
				fmt.Printf("  %s$%s mdm skills cherry-pick <source>\n", ansiDim, ansiReset)
				fmt.Printf("  %s$%s mdm skills cherry-pick --status\n\n", ansiDim, ansiReset)
				os.Exit(1)
			}
			if len(opts.Agents) > 0 {
				opts.Install = true
			}
			runCherryPick(args[0], opts)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.Dir, "dir", "d", defaultForksDir, "Directory to fork skills into (relative to the project root)")
	f.StringArrayVarP(&opts.Skills, "skill", "s", nil, "Skill names to fork (repeatable, use '*' for all)")
	f.StringVar(&opts.As, "as", "", "Rename the forked skill (single skill only)")
	f.BoolVarP(&opts.Install, "install", "i", false, "Also install the forks into your agents' skills directories")
	f.StringArrayVarP(&opts.Agents, "agent", "a", nil, "Agents to install the forks to (implies --install)")
	f.BoolVarP(&opts.Global, "global", "g", false, "Install the forks globally (with --install)")
	f.BoolVarP(&opts.Project, "project", "p", false, "Install the forks for this project only (with --install)")
	f.BoolVar(&opts.Copy, "copy", false, "Copy files instead of symlinking (with --install)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts")
	f.BoolVar(&opts.Force, "force", false, "Replace an existing fork, discarding local edits")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Show what would be forked without writing anything")
	f.BoolVarP(&opts.ListOnly, "list", "l", false, "List the skills available at the source without forking")
	f.BoolVar(&opts.Status, "status", false, "Show the forks in this project and whether they have been edited")
	f.BoolVar(&opts.FullDepth, "full-depth", false, "Search all subdirectories")
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	f.BoolVar(&opts.NoAttribution, "no-attribution", false, "Do not write "+fork.AttributionFileName+" (you remain responsible for the license terms)")

	_ = cmd.RegisterFlagCompletionFunc("agent", agentFlagCompletion)

	return cmd
}

// ─── Source materialization ────────────────────────────────────────────────────

// upstreamContext is what the fetch step learned about the source: where its
// files are on disk, and how to describe where they came from.
type upstreamContext struct {
	parsed     source.ParsedSource
	input      string // source as typed, without any #ref fragment
	root       string // materialized repository (or local dir) root
	searchRoot string // root plus any subpath — where skills are discovered
	ref        string
	commit     string
}

func fetchCherryPickSource(parsed source.ParsedSource, sourceInput string) (upstreamContext, func()) {
	up := upstreamContext{parsed: parsed, input: stripSourceRef(sourceInput), ref: parsed.Ref}

	if parsed.Type == source.SourceTypeLocal {
		if _, err := os.Stat(parsed.LocalPath); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Path not found: %s\n", ansiText, ansiReset, parsed.LocalPath)
			os.Exit(1)
		}
		up.root = parsed.LocalPath
		up.searchRoot = parsed.LocalPath
		return up, func() {}
	}

	tmpDir, err := cloneForAdd(parsed, parsed.Ref, AddOptions{Verbose: verboseFlag})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err.Error())
		os.Exit(1)
	}
	cleanup := func() { _ = git.CleanupTempDir(tmpDir) }

	up.root = tmpDir
	up.searchRoot = tmpDir
	if parsed.Subpath != "" {
		up.searchRoot = filepath.Join(tmpDir, parsed.Subpath)
		if _, err := os.Stat(up.searchRoot); err != nil {
			cleanup()
			fmt.Fprintf(os.Stderr, "%sSubpath not found:%s %s\n", ansiText, ansiReset, parsed.Subpath)
			os.Exit(1)
		}
	}
	if up.ref == "" {
		up.ref = git.DefaultBranch(parsed.URL)
	}
	up.commit, _ = git.GetLocalCommitSHA(tmpDir)
	return up, cleanup
}

// ─── Main flow ─────────────────────────────────────────────────────────────────

func runCherryPick(sourceInput string, opts CherryPickOptions) {
	cwd, _ := os.Getwd()
	parsed := source.ParseSource(sourceInput)
	vlog(verboseFlag, "source %q → type=%s url=%s ref=%q subpath=%q",
		sourceInput, parsed.Type, parsed.URL, parsed.Ref, parsed.Subpath)

	if parsed.Type == source.SourceTypeWellKnown {
		fmt.Fprintf(os.Stderr, "%sError:%s well-known registries serve files without a repository to fork from.\n", ansiText, ansiReset)
		fmt.Fprintf(os.Stderr, "%sInstall it with 'mdm skills add', then cherry-pick the installed copy.%s\n\n", ansiDim, ansiReset)
		os.Exit(1)
	}

	up, cleanup := fetchCherryPickSource(parsed, sourceInput)
	defer cleanup()

	filter := skillFilterFromOpts(AddOptions{Skills: opts.Skills}, parsed)
	skills := discoverSkillsInDir(up.searchRoot, opts.FullDepth, filter)
	if len(skills) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo skills found in %s%s\n", ansiText, sourceInput, ansiReset)
		os.Exit(1)
	}

	if opts.ListOnly {
		fmt.Printf("%sAvailable skills:%s\n\n", ansiText, ansiReset)
		for _, s := range skills {
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, s.Description, ansiReset)
		}
		return
	}

	selected, ok := selectSkillsWithPrompt(skills,
		AddOptions{Skills: opts.Skills, Yes: opts.Yes}, "Which skills would you like to cherry-pick?")
	if !ok {
		return
	}
	if opts.As != "" && len(selected) != 1 {
		fmt.Fprintf(os.Stderr, "%sError:%s --as renames a single skill, but %d were selected.\n\n", ansiText, ansiReset, len(selected))
		os.Exit(1)
	}
	if !checkDiskSkillsMarkdownForHiddenChars(selected, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	licenses := resolveLicenses(selected, up)
	if !confirmUnlicensed(selected, licenses, opts) {
		return
	}

	fmt.Println()
	var forked []string
	for _, s := range selected {
		if dir, ok := cherryPickSkill(s, licenses[s.Name], up, opts, cwd); ok {
			forked = append(forked, dir)
		}
	}
	if len(forked) == 0 {
		os.Exit(1)
	}
	printCherryPickSummary(forked, licenses, opts)
	if opts.DryRun || !opts.Install {
		return
	}
	installForks(forked, opts, cwd)
}

// ─── Licensing ─────────────────────────────────────────────────────────────────

// licenseInfo is what mdm could determine about the terms a skill is published
// under. File is the license file that will end up inside the fork; it is empty
// when the source ships none.
type licenseInfo struct {
	ID       string // SPDX-style identifier, when recognizable
	Path     string // absolute path of the license file at the source
	File     string // the name the fork will carry it under
	InSkill  bool   // the file is inside the skill directory, so it copies itself
	Declared bool   // the SKILL.md frontmatter declared a license
}

// resolveLicense looks for terms in the skill directory first and the
// repository root second, which is the order a reader would check.
func resolveLicense(s *skill.Skill, up upstreamContext) licenseInfo {
	info := licenseInfo{ID: s.License, Declared: s.License != ""}

	if name := fork.FindLicenseFile(s.Path); name != "" {
		info.Path = filepath.Join(s.Path, name)
		info.File = name
		info.InSkill = true
	} else if name := fork.FindLicenseFile(up.root); name != "" {
		info.Path = filepath.Join(up.root, name)
		info.File = fork.UpstreamLicenseFileName
	}

	if info.ID == "" && info.Path != "" {
		if data, err := os.ReadFile(info.Path); err == nil {
			info.ID = fork.DetectLicenseID(string(data))
		}
	}
	return info
}

func resolveLicenses(skills []*skill.Skill, up upstreamContext) map[string]licenseInfo {
	out := make(map[string]licenseInfo, len(skills))
	for _, s := range skills {
		out[s.Name] = resolveLicense(s, up)
	}
	return out
}

// confirmUnlicensed stops before copying anything a source gave no permission
// to copy. Absent a license the default is no redistribution rights, which is
// exactly the case a fork runs into later, not now.
func confirmUnlicensed(skills []*skill.Skill, licenses map[string]licenseInfo, opts CherryPickOptions) bool {
	var unlicensed []string
	for _, s := range skills {
		if lic := licenses[s.Name]; lic.Path == "" && !lic.Declared {
			unlicensed = append(unlicensed, s.Name)
		}
	}
	if len(unlicensed) == 0 {
		return true
	}

	fmt.Printf("\n%sNo license found for: %s%s\n", ansiYellow, strings.Join(unlicensed, ", "), ansiReset)
	fmt.Printf("%sNeither the skill's frontmatter nor the source repository declares one. Absent a\n", ansiDim)
	fmt.Printf("license, the default is that no redistribution rights are granted — check with the\n")
	fmt.Printf("original author before publishing this fork.%s\n\n", ansiReset)

	if opts.Yes || opts.DryRun {
		return true
	}
	ok, answered := ui.UiConfirm("Fork anyway?")
	if !answered || !ok {
		fmt.Println("Cancelled.")
		return false
	}
	return true
}

// ─── Forking one skill ─────────────────────────────────────────────────────────

func cherryPickSkill(s *skill.Skill, lic licenseInfo, up upstreamContext, opts CherryPickOptions, cwd string) (string, bool) {
	localName := s.Name
	if opts.As != "" {
		localName = opts.As
	}
	destBase := filepath.Join(cwd, opts.Dir)
	destDir := filepath.Join(destBase, sanitizeName(localName))
	if !isPathSafe(cwd, destBase) || !isPathSafe(destBase, destDir) {
		ui.LogError(fmt.Sprintf("%s: refusing to write outside the project (--dir %s)", s.Name, opts.Dir))
		return "", false
	}

	if opts.DryRun {
		fmt.Printf("  %s%s%s → %s%s%s\n", ansiText, s.Name, ansiReset, ansiDim, shortenPath(destDir, cwd), ansiReset)
		return destDir, true
	}
	if !clearForkDest(destDir, s.Name, cwd, opts.Force) {
		return "", false
	}
	if err := copyDirectory(s.Path, destDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", s.Name, err))
		return "", false
	}
	if localName != s.Name {
		if err := rewriteForkName(destDir, localName); err != nil {
			ui.LogError(fmt.Sprintf("%s: %v", s.Name, err))
			return "", false
		}
	}
	if lic.Path != "" && !lic.InSkill {
		if err := copyFile(lic.Path, filepath.Join(destDir, lic.File)); err != nil {
			ui.LogWarn(fmt.Sprintf("%s: could not copy the upstream license: %v", s.Name, err))
		}
	}

	origin := fork.NewOrigin(localName, upstreamRecord(s, up, lic, cwd))
	if !opts.NoAttribution {
		attribution := filepath.Join(destDir, fork.AttributionFileName)
		if err := os.WriteFile(attribution, []byte(fork.RenderAttribution(origin)), 0644); err != nil {
			ui.LogWarn(fmt.Sprintf("%s: could not write %s: %v", s.Name, fork.AttributionFileName, err))
		}
	}
	if hash, err := fork.HashDir(destDir); err == nil {
		origin.ContentHash = hash
	}
	if err := fork.WriteOrigin(destDir, origin); err != nil {
		ui.LogWarn(fmt.Sprintf("%s: could not write %s: %v", s.Name, fork.OriginFileName, err))
	}

	ui.LogSuccess(fmt.Sprintf("%s → %s", localName, shortenPath(destDir, cwd)))
	return destDir, true
}

// clearForkDest empties the destination, refusing to discard an existing fork
// unless --force was given: the directory is the user's own source tree by
// then, and their edits are the whole point of having forked.
func clearForkDest(destDir, skillName, cwd string, force bool) bool {
	if _, err := os.Stat(destDir); err == nil {
		if !force {
			ui.LogError(fmt.Sprintf("%s: %s already exists — pass --force to replace it (local edits will be lost)",
				skillName, shortenPath(destDir, cwd)))
			return false
		}
		ui.LogWarn(fmt.Sprintf("%s: replacing %s", skillName, shortenPath(destDir, cwd)))
	}
	if err := cleanAndCreateDir(destDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", skillName, err))
		return false
	}
	return true
}

func rewriteForkName(destDir, newName string) error {
	skillMd := filepath.Join(destDir, "SKILL.md")
	data, err := os.ReadFile(skillMd)
	if err != nil {
		return err
	}
	rewritten, ok := skill.SetFrontmatterName(string(data), newName)
	if !ok {
		return fmt.Errorf("could not rename the skill: no name field in SKILL.md frontmatter")
	}
	return os.WriteFile(skillMd, []byte(rewritten), 0644)
}

// upstreamRecord describes where the material came from. When the fork is taken
// from a skill that was itself installed by mdm, the lock file knows the real
// upstream — recording the local directory alone would credit the wrong author.
func upstreamRecord(s *skill.Skill, up upstreamContext, lic licenseInfo, cwd string) fork.Upstream {
	rec := fork.Upstream{
		Name:       s.Name,
		Source:     up.input,
		SourceType: string(up.parsed.Type),
		SourceURL:  up.parsed.URL,
		Ref:        up.ref,
		Commit:     up.commit,
		SkillPath:  skillMdRepoPath(s.Path, up.root),
		License:    lic.ID,
	}
	if lic.Path != "" {
		rec.LicenseFile = lic.File
	}
	if up.parsed.Type != source.SourceTypeLocal {
		return rec
	}

	rec.Source = toRelSourcePath(up.parsed.LocalPath, cwd)
	rec.SourceURL = ""
	if origin, ok := installedSkillOrigin(s.Name, cwd); ok {
		rec.Via = rec.Source
		rec.Source = origin.Source
		rec.SourceType = origin.SourceType
		rec.SourceURL = source.ParseSource(origin.Source).URL
		rec.Ref = origin.Ref
		rec.SkillPath = origin.SkillPath
	}
	return rec
}

// installedSkillOrigin returns the lock entry recorded when this skill was
// installed, project scope first. Only remote sources are worth reporting: a
// local-to-local chain says nothing the path does not already say.
func installedSkillOrigin(skillName, cwd string) (lock.LocalSkillLockEntry, bool) {
	key := sanitizeName(skillName)
	if e, ok := lock.ReadLocalLock(cwd).Skills[key]; ok && isRemoteSourceType(e.SourceType) {
		return e, true
	}
	if e, ok := lock.ReadGlobalState().Skills[key]; ok && isRemoteSourceType(e.SourceType) {
		return lock.LocalSkillLockEntry{
			Source:     e.Source,
			Ref:        e.Ref,
			SourceType: e.SourceType,
			SkillPath:  e.SkillPath,
		}, true
	}
	return lock.LocalSkillLockEntry{}, false
}

func isRemoteSourceType(st string) bool {
	return st != "" && st != string(source.SourceTypeLocal)
}

// ─── Reporting ─────────────────────────────────────────────────────────────────

func printCherryPickSummary(forked []string, licenses map[string]licenseInfo, opts CherryPickOptions) {
	fmt.Println()
	if opts.DryRun {
		fmt.Printf("%sDry run — nothing was written.%s\n\n", ansiDim, ansiReset)
		return
	}

	dir := "./" + filepath.ToSlash(opts.Dir)
	fmt.Printf("%sCherry-picked %d skill(s) into %s%s%s\n\n", ansiText, len(forked), ansiBold, dir, ansiReset)
	fmt.Printf("  %sThey are yours now: 'mdm skills update' leaves them alone, so edit and commit\n", ansiDim)
	fmt.Printf("  them like any other source in this repository.%s\n\n", ansiReset)

	if !opts.Install {
		fmt.Printf("  %sInstall them into your agents with:%s\n", ansiDim, ansiReset)
		fmt.Printf("  %s$%s mdm skills add %s\n\n", ansiDim, ansiReset, dir)
	}
	if opts.NoAttribution {
		fmt.Printf("  %s%s was not written (--no-attribution) — the upstream license terms still apply.%s\n\n",
			ansiYellow, fork.AttributionFileName, ansiReset)
		return
	}
	for _, lic := range licenses {
		if lic.Path != "" || lic.Declared {
			continue
		}
		fmt.Printf("  %sSome forks have no upstream license — see %s before publishing them.%s\n\n",
			ansiYellow, fork.AttributionFileName, ansiReset)
		return
	}
}

// clobbersForks reports whether installing to this agent would write over the
// forks directory itself. Installing a skill replaces the agent's <skills
// dir>/<name>, so when that directory is the forks directory — OpenClaw reads
// ./skills, mdm's own default — the install would destroy the very source it is
// installing from.
func clobbersForks(agentName string, global bool, cwd, forksRoot string) bool {
	forksAbs, err := filepath.Abs(forksRoot)
	if err != nil {
		return false
	}
	forksAbs = filepath.Clean(forksAbs)
	for _, base := range []string{getAgentBaseDir(agentName, global, cwd), getCanonicalSkillsDir(global, cwd)} {
		if base == "" {
			continue
		}
		abs, err := filepath.Abs(base)
		if err != nil {
			continue
		}
		if filepath.Clean(abs) == forksAbs {
			return true
		}
	}
	return false
}

// dropClobberingAgents removes agents that read the forks directory directly.
// They need no install: the fork is already sitting where they look for skills.
func dropClobberingAgents(agents []string, global bool, cwd, forksRoot string) []string {
	kept := make([]string, 0, len(agents))
	for _, name := range agents {
		if clobbersForks(name, global, cwd, forksRoot) {
			display := name
			if a := agent.AllAgents[name]; a != nil {
				display = a.DisplayName
			}
			ui.LogInfo(fmt.Sprintf("%s already reads %s — skipping its install so the fork is not overwritten",
				display, shortenPath(forksRoot, cwd)))
			continue
		}
		kept = append(kept, name)
	}
	return kept
}

func installForks(forked []string, opts CherryPickOptions, cwd string) {
	var skills []*skill.Skill
	for _, dir := range forked {
		s, err := skill.ParseSkillMd(filepath.Join(dir, "SKILL.md"), true)
		if err == nil && s != nil {
			skills = append(skills, s)
		}
	}
	if len(skills) == 0 {
		return
	}

	addOpts := AddOptions{
		Global:  opts.Global,
		Project: opts.Project,
		Agents:  opts.Agents,
		Yes:     opts.Yes,
		Copy:    opts.Copy,
	}
	global, agents, ok := promptScopeAndAgents(addOpts, cwd)
	if !ok {
		return
	}

	// The recorded source is the forks directory, not the upstream repository:
	// these skills now update from your copy, and `mdm skills update` skips
	// local sources entirely, so nothing upstream can overwrite your edits.
	forksRoot := filepath.Join(cwd, opts.Dir)
	if agents = dropClobberingAgents(agents, global, cwd, forksRoot); len(agents) == 0 {
		return
	}

	// After the clobber filter: an agent list it empties means nothing gets
	// installed, and a scope converted for an install that never happens is
	// exactly the mix this switch exists to avoid.
	mode, ok := commitScopeInstallMode(addOpts, global, cwd)
	if !ok {
		return
	}

	fmt.Println()
	installSkillsForAgents(skills, agents, global, mode, lock.SkillLockEntry{
		Source:     forksRoot,
		SourceType: string(source.SourceTypeLocal),
		SourceURL:  forksRoot,
	}, cwd, "")
}

// ─── Status ────────────────────────────────────────────────────────────────────

func runCherryPickStatus(opts CherryPickOptions) {
	cwd, _ := os.Getwd()
	root := filepath.Join(cwd, opts.Dir)
	dir := "./" + filepath.ToSlash(opts.Dir)

	forks, err := fork.ListForks(root)
	if err != nil || len(forks) == 0 {
		fmt.Printf("%sNo cherry-picked skills in %s%s\n\n", ansiText, dir, ansiReset)
		fmt.Printf("  %s$%s mdm skills cherry-pick <source>\n\n", ansiDim, ansiReset)
		return
	}

	fmt.Printf("%sForked skills in %s%s%s\n\n", ansiText, ansiBold, dir, ansiReset)
	for _, f := range forks {
		fmt.Printf("  %s%s%s  %s%s%s\n", ansiText, f.Name, ansiReset, ansiDim, forkUpstreamLabel(f.Origin), ansiReset)
		fmt.Printf("    %slicense: %s   forked: %s   %s%s\n",
			ansiDim, forkLicenseLabel(f.Origin), f.Origin.ForkedAt, forkStateLabel(f), ansiReset)
	}
	fmt.Println()
	fmt.Printf("  %sForks never update from upstream. To take a newer version, cherry-pick it again\n", ansiDim)
	fmt.Printf("  with --force (replacing your edits) or into a new name with --as.%s\n\n", ansiReset)
}

func forkUpstreamLabel(o fork.Origin) string {
	label := o.Upstream.SourceURL
	if label == "" {
		label = o.Upstream.Source
	}
	if o.Upstream.Ref != "" {
		label += "#" + o.Upstream.Ref
	}
	if o.Upstream.Name != "" && o.Upstream.Name != o.Skill {
		label += " (" + o.Upstream.Name + ")"
	}
	return label
}

func forkLicenseLabel(o fork.Origin) string {
	switch {
	case o.Upstream.License != "":
		return o.Upstream.License
	case o.Upstream.LicenseFile != "":
		return o.Upstream.LicenseFile
	default:
		return "none declared"
	}
}

func forkStateLabel(f fork.Fork) string {
	switch {
	case f.HashErr != nil || f.Hash == "" || f.Origin.ContentHash == "":
		return ""
	case f.Modified():
		return "edited since fork"
	default:
		return "unmodified"
	}
}
