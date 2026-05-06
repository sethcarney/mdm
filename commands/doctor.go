package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/skill"
)

const (
	fileSizeWarnBytes  = 20 * 1024  // 20 KB — may strain context windows
	fileSizeErrorBytes = 100 * 1024 // 100 KB — likely too large

	// Maximum filesystem entries walked before the project-wide markdown
	// scan gives up, to avoid hangs on very large repositories.
	markdownWalkLimit = 10_000
)

// Directories always skipped by name during the project-wide markdown walk.
var markdownSkipDirNames = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true,
	"__pycache__": true, ".cache": true, "target": true,
	"coverage": true, ".nyc_output": true, ".venv": true, "venv": true,
}

// ── Types ──────────────────────────────────────────────────────────────────────

type DoctorOptions struct {
	Global  bool
	Project bool
}

type doctorIssue struct {
	Level   string // "error" or "warn"
	Message string
}

type doctorResult struct {
	Name   string
	Scope  string
	Path   string
	Issues []doctorIssue
}

// ── Command ────────────────────────────────────────────────────────────────────

func buildDoctorCmd() *cobra.Command {
	var opts DoctorOptions

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the health of installed skills",
		Long: fmt.Sprintf(`Check installed skills for installation and content issues.

Checks performed:
  • Missing skill directories or SKILL.md files
  • Broken symlinks in agent skill directories
  • Skills modified since install (hash mismatch; global installs with a recorded hash only)
  • Markdown files inside skill directories that are too large
  • Oversized agent instruction files (CLAUDE.md, AGENTS.md, .cursorrules, etc.)
  • Configured agents whose instruction file is not yet linked to AGENTS.md
  • Configured agents with linked rules but missing skill symlinks
  • Missing README in the project root
  • All other .md files in the project that may strain agent context windows

%sExamples:%s
  mdm skills doctor
  mdm skills doctor -g
  mdm skills doctor -p`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runDoctor(opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Check global skills only")
	f.BoolVarP(&opts.Project, "project", "p", false, "Check project skills only")

	return cmd
}

// ── Run ────────────────────────────────────────────────────────────────────────

func runDoctor(opts DoctorOptions) {
	checkGlobal := opts.Global || (!opts.Global && !opts.Project)
	checkProject := opts.Project || (!opts.Global && !opts.Project)

	cwd, _ := os.Getwd()

	var results []doctorResult

	if checkGlobal {
		globalLock := lock.ReadSkillLock()
		canonicalBase := getCanonicalSkillsDir(true, cwd)
		for skillName, entry := range globalLock.Skills {
			r := doctorResult{
				Name:  skillName,
				Scope: "global",
				Path:  filepath.Join(canonicalBase, sanitizeName(skillName)),
			}
			diagnoseSkill(&r, entry.SkillFolderHash, true, cwd)
			results = append(results, r)
		}
	}

	// Directories and files already covered by skill/instruction checks; the
	// project-wide markdown walk skips these to avoid double-reporting.
	skipDirs := map[string]bool{}
	skipFiles := map[string]bool{}

	if checkProject {
		localLock := lock.ReadLocalLock(cwd)
		canonicalBase := getCanonicalSkillsDir(false, cwd)
		for skillName := range localLock.Skills {
			r := doctorResult{
				Name:  skillName,
				Scope: "project",
				Path:  filepath.Join(canonicalBase, sanitizeName(skillName)),
			}
			diagnoseSkill(&r, "", false, cwd)
			results = append(results, r)
		}

		buildProjectSkipPaths(cwd, canonicalBase, skipDirs, skipFiles)
	}

	var instrIssues []doctorIssue
	var unlinkedRulesIssues []doctorIssue
	var missingSkillLinkIssues []doctorIssue
	var mdIssues []doctorIssue
	var mdTruncated bool

	var readmeIssue *doctorIssue
	if checkProject {
		instrIssues = checkInstructionFiles(cwd)
		unlinkedRulesIssues = checkUnlinkedRulesAgents(cwd)
		missingSkillLinkIssues = checkMissingAgentSkillLinks(cwd)
		mdIssues, mdTruncated = checkProjectMarkdown(cwd, skipDirs, skipFiles)
		readmeIssue = checkProjectReadme(cwd)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Scope != results[j].Scope {
			return results[i].Scope < results[j].Scope
		}
		return results[i].Name < results[j].Name
	})

	printDoctorResults(results, instrIssues, unlinkedRulesIssues, missingSkillLinkIssues, mdIssues, mdTruncated, readmeIssue, checkProject, cwd)
}

func buildProjectSkipPaths(cwd, canonicalBase string, skipDirs, skipFiles map[string]bool) {
	if _, err := os.Stat(canonicalBase); err == nil {
		skipDirs[filepath.Clean(canonicalBase)] = true
	}
	for _, agentCfg := range agent.AllAgents {
		if agentCfg == nil {
			continue
		}
		agentSkillsDir := filepath.Clean(filepath.Join(cwd, agentCfg.SkillsDir))
		if _, err := os.Stat(agentSkillsDir); err == nil {
			skipDirs[agentSkillsDir] = true
		}
		if agentCfg.InstructionsFile != "" {
			skipFiles[filepath.Clean(filepath.Join(cwd, agentCfg.InstructionsFile))] = true
		}
	}
}

// ── Checks ─────────────────────────────────────────────────────────────────────

func diagnoseSkill(r *doctorResult, storedHash string, global bool, cwd string) {
	// 1. Skill directory must exist
	if _, err := os.Stat(r.Path); os.IsNotExist(err) {
		r.Issues = append(r.Issues, doctorIssue{
			Level:   "error",
			Message: "skill directory not found on disk — run `mdm skills install` to restore",
		})
		return
	}

	// 2. SKILL.md must exist and have valid frontmatter
	skillMdPath := filepath.Join(r.Path, "SKILL.md")
	if _, err := os.Stat(skillMdPath); os.IsNotExist(err) {
		r.Issues = append(r.Issues, doctorIssue{
			Level:   "error",
			Message: "SKILL.md not found in skill directory",
		})
	} else {
		sk, err := skill.ParseSkillMd(skillMdPath, true)
		if err != nil {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("SKILL.md could not be read: %s", err),
			})
		} else if sk == nil {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: "SKILL.md is missing required name or description fields",
			})
		}
	}

	// 3. Symlinks in agent-specific directories must resolve
	checkAgentLinks(r, global, cwd)

	// 5. Skill content must match the installed hash (global skills only)
	if storedHash != "" {
		current, err := lock.ComputeSkillFolderHash(r.Path)
		if err != nil {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("could not compute skill content hash — integrity check skipped: %s", err),
			})
		} else if current != storedHash {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: "skill content differs from installed version — run `mdm skills update` to sync",
			})
		}
	}

	// 6. Markdown files must not be too large for agent context windows
	checkLargeMarkdown(r)
}

// checkAgentLinks verifies that symlinks in non-universal agent directories
// point to an existing target.
func checkAgentLinks(r *doctorResult, global bool, cwd string) {
	sName := sanitizeName(r.Name)
	for agentName, agentCfg := range agent.AllAgents {
		if agentCfg == nil || agent.UsesSharedSkillsDir(agentName) {
			continue
		}
		var agentBase string
		if global {
			if agentCfg.GlobalSkillsDir == "" {
				continue
			}
			agentBase = agentCfg.GlobalSkillsDir
		} else {
			agentBase = filepath.Join(cwd, agentCfg.SkillsDir)
		}
		linkPath := filepath.Join(agentBase, sName)
		info, err := os.Lstat(linkPath)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue // not present or not a symlink
		}
		target, err := os.Readlink(linkPath)
		if err != nil {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("broken symlink in %s directory", agentCfg.DisplayName),
			})
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(linkPath), target)
		}
		if _, err := os.Stat(target); os.IsNotExist(err) {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("broken symlink in %s directory: target not found", agentCfg.DisplayName),
			})
		}
	}
}

// checkLargeMarkdown walks the skill directory and flags .md files that are
// large enough to threaten agent context windows. Common dependency/build
// directories (e.g. .git, node_modules, vendor) are skipped.
func checkLargeMarkdown(r *doctorResult) {
	_ = filepath.WalkDir(r.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if markdownSkipDirNames[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		rel, _ := filepath.Rel(r.Path, path)
		switch {
		case size >= fileSizeErrorBytes:
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("%s is %s — likely too large for agent context windows", rel, formatFileSize(size)),
			})
		case size >= fileSizeWarnBytes:
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is %s — may strain agent context windows", rel, formatFileSize(size)),
			})
		}
		return nil
	})
}

// checkUnlinkedRulesAgents finds configured agents that have a unique
// instructions file (e.g. CLAUDE.md, .cursorrules) which is not yet symlinked
// to AGENTS.md. This means the agent has been added via skills add or agents
// add but mdm rules link has not been run for it yet.
func checkUnlinkedRulesAgents(cwd string) []doctorIssue {
	configured := lock.GetConfiguredAgents(false, cwd)
	if len(configured) == 0 {
		return nil
	}
	agentsMDPath := filepath.Join(cwd, agentsMDFile)
	// Only relevant when AGENTS.md exists as the source of truth.
	if _, err := os.Stat(agentsMDPath); err != nil {
		return nil
	}

	var issues []doctorIssue
	for _, name := range configured {
		cfg := agent.AllAgents[name]
		if cfg == nil || cfg.NativeInstructions {
			continue
		}
		instrPath := filepath.Join(cwd, cfg.InstructionsFile)
		info, err := os.Lstat(instrPath)
		if err != nil {
			// File doesn't exist at all — not yet linked.
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s (%s) is configured but %s is missing — run `mdm rules link` to create it", cfg.DisplayName, name, cfg.InstructionsFile),
			})
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			// Real file, not a symlink to AGENTS.md.
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s (%s) is configured but %s is not linked to AGENTS.md — run `mdm rules link`", cfg.DisplayName, name, cfg.InstructionsFile),
			})
		}
		// If it is a symlink we assume it points to AGENTS.md (rules link created it).
	}
	return issues
}

// checkMissingAgentSkillLinks finds configured agents whose rules file is
// properly linked but whose agent-specific skills directory is missing symlinks
// for one or more installed project skills. This catches the case where rules
// link was run but skills add was never run for that agent.
func checkMissingAgentSkillLinks(cwd string) []doctorIssue {
	configured := lock.GetConfiguredAgents(false, cwd)
	if len(configured) == 0 {
		return nil
	}
	localLock := lock.ReadLocalLock(cwd)
	if len(localLock.Skills) == 0 {
		return nil
	}
	canonicalBase := getCanonicalSkillsDir(false, cwd)

	var issues []doctorIssue
	for _, name := range configured {
		cfg := agent.AllAgents[name]
		if cfg == nil || agent.UsesSharedSkillsDir(name) {
			// Shared-skills-dir agents don't need per-agent symlinks.
			continue
		}
		// Only flag agents whose rules file IS already linked (or they have no
		// rules file — e.g. a pure-skills-dir agent). Agents whose rules file
		// is missing are already reported by checkUnlinkedRulesAgents.
		if !cfg.NativeInstructions {
			instrPath := filepath.Join(cwd, cfg.InstructionsFile)
			info, err := os.Lstat(instrPath)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				continue // rules not linked yet — covered by the other check
			}
		}

		agentSkillsDir := filepath.Join(cwd, cfg.SkillsDir)
		var missing []string
		for skillName := range localLock.Skills {
			sName := sanitizeName(skillName)
			linkPath := filepath.Join(agentSkillsDir, sName)
			if _, err := os.Lstat(linkPath); os.IsNotExist(err) {
				canonicalPath := filepath.Join(canonicalBase, sName)
				// Only flag if the canonical skill dir actually exists.
				if _, err2 := os.Stat(canonicalPath); err2 == nil {
					missing = append(missing, skillName)
				}
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			for _, skillName := range missing {
				issues = append(issues, doctorIssue{
					Level:   "warn",
					Message: fmt.Sprintf("%s (%s) is configured but skill %q is not installed for it — run `mdm skills add` to include it", cfg.DisplayName, name, skillName),
				})
			}
		}
	}
	return issues
}

// checkInstructionFiles scans the project root for known agent instruction
// files (CLAUDE.md, AGENTS.md, .cursorrules, .github/copilot-instructions.md,
// etc.) and flags oversized ones.
func checkInstructionFiles(cwd string) []doctorIssue {
	seen := map[string]bool{}
	var issues []doctorIssue

	for _, agentCfg := range agent.AllAgents {
		if agentCfg == nil || agentCfg.InstructionsFile == "" {
			continue
		}
		fname := agentCfg.InstructionsFile
		if seen[fname] {
			continue
		}
		seen[fname] = true

		path := filepath.Join(cwd, fname)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		size := info.Size()
		switch {
		case size >= fileSizeErrorBytes:
			issues = append(issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("%s is %s — likely too large for agent context windows", fname, formatFileSize(size)),
			})
		case size >= fileSizeWarnBytes:
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is %s — may strain agent context windows", fname, formatFileSize(size)),
			})
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Message < issues[j].Message
	})
	return issues
}

// checkProjectReadme verifies that the project root contains a README file.
func checkProjectReadme(cwd string) *doctorIssue {
	for _, name := range []string{"README.md", "readme.md", "README", "README.txt"} {
		if _, err := os.Stat(filepath.Join(cwd, name)); err == nil {
			return nil
		}
	}
	return &doctorIssue{
		Level:   "warn",
		Message: "no README found in project root — consider adding a README.md",
	}
}

// checkProjectMarkdown walks the project tree and flags .md files that are
// large enough to strain agent context windows. It skips directories and files
// already covered by the skill and instruction-file checks, as well as common
// build/dependency directories. The walk stops after markdownWalkLimit
// filesystem entries to prevent hangs on very large repositories.
func checkProjectMarkdown(cwd string, skipDirs map[string]bool, skipFiles map[string]bool) (issues []doctorIssue, truncated bool) {
	walked := 0

	_ = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if walked >= markdownWalkLimit {
			truncated = true
			return fs.SkipAll
		}
		walked++

		if d.IsDir() {
			if markdownSkipDirNames[d.Name()] || skipDirs[filepath.Clean(path)] {
				return fs.SkipDir
			}
			return nil
		}

		if skipFiles[filepath.Clean(path)] {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		size := info.Size()
		rel, _ := filepath.Rel(cwd, path)

		switch {
		case size >= fileSizeErrorBytes:
			issues = append(issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("%s is %s — likely too large for agent context windows", rel, formatFileSize(size)),
			})
		case size >= fileSizeWarnBytes:
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is %s — may strain agent context windows", rel, formatFileSize(size)),
			})
		}
		return nil
	})

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].Message < issues[j].Message
	})
	return issues, truncated
}

// ── Output ─────────────────────────────────────────────────────────────────────

func printDoctorResults(results []doctorResult, instrIssues, unlinkedRulesIssues, missingSkillLinkIssues, mdIssues []doctorIssue, mdTruncated bool, readmeIssue *doctorIssue, scannedProject bool, cwd string) {
	fmt.Println()

	byScope := map[string][]doctorResult{}
	for _, r := range results {
		byScope[r.Scope] = append(byScope[r.Scope], r)
	}

	totalErrors, totalWarnings := 0, 0

	for _, scope := range []string{"project", "global"} {
		scopeResults, ok := byScope[scope]
		if !ok {
			continue
		}
		e, w := printDoctorSkillSection(scopeResults, scope, cwd)
		totalErrors += e
		totalWarnings += w
	}

	if len(instrIssues) > 0 {
		fmt.Printf("%sInstruction files:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(instrIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	if len(unlinkedRulesIssues) > 0 {
		fmt.Printf("%sRules linking:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(unlinkedRulesIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	if len(missingSkillLinkIssues) > 0 {
		fmt.Printf("%sSkill coverage:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(missingSkillLinkIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	e, w := printDoctorMarkdownSection(readmeIssue, mdIssues, mdTruncated)
	totalErrors += e
	totalWarnings += w

	printDoctorSummary(len(results), scannedProject, totalErrors, totalWarnings)
}

func printDoctorSkillSection(scopeResults []doctorResult, scope, cwd string) (errs, warns int) {
	scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
	fmt.Printf("%s%s skills:%s\n\n", ansiText, scopeTitle, ansiReset)
	for _, r := range scopeResults {
		errCount, warnCount := 0, 0
		for _, issue := range r.Issues {
			switch issue.Level {
			case "error":
				errCount++
			case "warn":
				warnCount++
			}
		}
		errs += errCount
		warns += warnCount
		statusIcon, statusColor := doctorStatusIcon(errCount, warnCount)
		fmt.Printf("  %s%s%s %s%s%s\n", statusColor, statusIcon, ansiReset, ansiBold, r.Name, ansiReset)
		if len(r.Issues) == 0 {
			fmt.Printf("    %s%s%s\n", ansiDim, shortenPath(r.Path, cwd), ansiReset)
		} else {
			for _, issue := range r.Issues {
				icon, color := doctorIssueIcon(issue.Level)
				fmt.Printf("    %s%s%s %s%s%s\n", color, icon, ansiReset, ansiDim, issue.Message, ansiReset)
			}
		}
		fmt.Println()
	}
	return
}

func printAndCountDoctorIssues(issues []doctorIssue) (errs, warns int) {
	for _, issue := range issues {
		icon, color := doctorIssueIcon(issue.Level)
		fmt.Printf("  %s%s%s %s%s%s\n", color, icon, ansiReset, ansiDim, issue.Message, ansiReset)
		if issue.Level == "error" {
			errs++
		} else {
			warns++
		}
	}
	return
}

func printDoctorMarkdownSection(readmeIssue *doctorIssue, mdIssues []doctorIssue, mdTruncated bool) (errs, warns int) {
	if readmeIssue == nil && len(mdIssues) == 0 && !mdTruncated {
		return
	}
	fmt.Printf("%sProject markdown:%s\n\n", ansiText, ansiReset)
	if readmeIssue != nil {
		icon, color := doctorIssueIcon(readmeIssue.Level)
		fmt.Printf("  %s%s%s %s%s%s\n", color, icon, ansiReset, ansiDim, readmeIssue.Message, ansiReset)
		if readmeIssue.Level == "error" {
			errs++
		} else {
			warns++
		}
	}
	e, w := printAndCountDoctorIssues(mdIssues)
	errs += e
	warns += w
	if mdTruncated {
		fmt.Printf("  %s▲%s %sscan stopped after %d entries — run from a subdirectory to check further%s\n",
			ansiYellow, ansiReset, ansiDim, markdownWalkLimit, ansiReset)
	}
	fmt.Println()
	return
}

func printDoctorSummary(total int, scannedProject bool, errs, warns int) {
	if total > 0 {
		fmt.Printf("%sDoctor complete:%s %d skill(s) checked", ansiText, ansiReset, total)
	} else {
		fmt.Printf("%sDoctor complete:%s no skills installed", ansiText, ansiReset)
	}
	if scannedProject {
		fmt.Printf(", project markdown scanned")
	}
	if errs > 0 {
		fmt.Printf(", %s%d error(s)%s", ansiRed, errs, ansiReset)
	}
	if warns > 0 {
		fmt.Printf(", %s%d warning(s)%s", ansiYellow, warns, ansiReset)
	}
	if errs == 0 && warns == 0 {
		fmt.Printf(", %sall clear%s", ansiGreen, ansiReset)
	}
	fmt.Println()
	fmt.Println()
}

func doctorStatusIcon(errors, warnings int) (icon, color string) {
	switch {
	case errors > 0:
		return "✗", ansiRed
	case warnings > 0:
		return "▲", ansiYellow
	default:
		return "✓", ansiGreen
	}
}

func doctorIssueIcon(level string) (icon, color string) {
	if level == "error" {
		return "✗", ansiRed
	}
	return "▲", ansiYellow
}

func formatFileSize(n int64) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.0fKB", float64(n)/1024)
}
