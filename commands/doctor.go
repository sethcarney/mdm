package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/skill"
)

const (
	fileSizeWarnBytes  = 20 * 1024  // 20 KB - may strain context windows
	fileSizeErrorBytes = 100 * 1024 // 100 KB - likely too large

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
  • Broken symlinks in harness skill directories
  • Skills modified since install (hash mismatch; global installs with a recorded hash only)
  • Markdown files inside skill directories that are too large
  • Oversized harness instruction files (CLAUDE.md, AGENTS.md, .cursorrules, etc.)
  • Configured harnesses whose instruction file is not yet linked to AGENTS.md
  • Configured harnesses with linked rules but missing skill symlinks
  • Missing README in the project root
  • Legacy v1 lock files, and an install mode a scope uses but has not recorded
  • All other .md files in the project that may strain harness context windows

Exits 1 when any error-level issue is found (warnings alone exit 0), so
doctor can gate CI.

%sExamples:%s
  mdm doctor
  mdm doctor -g
  mdm doctor -p`, ansiBold, ansiReset),
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
	vlog(verboseFlag, "doctor: checkGlobal=%v checkProject=%v cwd=%s", checkGlobal, checkProject, cwd)

	var results []doctorResult

	if checkGlobal {
		globalLock := lock.ReadGlobalState()
		canonicalBase := getCanonicalSkillsDir(true, cwd)
		for skillName := range globalLock.Skills {
			r := doctorResult{
				Name:  skillName,
				Scope: "global",
				Path:  filepath.Join(canonicalBase, sanitizeName(skillName)),
			}
			diagnoseSkill(&r, true, cwd)
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
			diagnoseSkill(&r, false, cwd)
			results = append(results, r)
		}

		buildProjectSkipPaths(cwd, canonicalBase, skipDirs, skipFiles)
	}

	var instrIssues []doctorIssue
	var unlinkedRulesIssues []doctorIssue
	var missingSkillLinkIssues []doctorIssue
	var knowledgeIssues []doctorIssue
	var pluginIssues []doctorIssue
	var migrationIssues []doctorIssue
	var mdIssues []doctorIssue
	var mdTruncated bool

	var readmeIssue *doctorIssue
	var agentIssues []doctorIssue
	var agentsChecked int
	if checkProject {
		instrIssues = checkInstructionFiles(cwd)
		unlinkedRulesIssues = checkUnlinkedRulesHarnesses(cwd)
		missingSkillLinkIssues = checkMissingHarnessSkillLinks(cwd)
		knowledgeIssues = checkKnowledgeBundles(cwd)
		pluginIssues = checkInstalledPlugins(cwd)
		// Project scope only, like checkKnowledgeBundles and checkInstalledPlugins:
		// a global read aborts the process on an unreadable global state file, and
		// `mdm doctor -p` must not fail over a problem outside the project.
		agentIssues, agentsChecked = checkAgentInstalls(cwd)
		migrationIssues = checkProjectMigration(cwd)
		mdIssues, mdTruncated = checkProjectMarkdown(cwd, skipDirs, skipFiles)
		if mdTruncated {
			vlog(verboseFlag, "project markdown walk hit the %d-entry limit; results truncated", markdownWalkLimit)
		}
		readmeIssue = checkProjectReadme(cwd)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Scope != results[j].Scope {
			return results[i].Scope < results[j].Scope
		}
		return results[i].Name < results[j].Name
	})

	migrationIssues = append(migrationIssues, checkGlobalMigration(checkGlobal)...)

	errs := printDoctorResults(results, instrIssues, unlinkedRulesIssues, missingSkillLinkIssues, knowledgeIssues, pluginIssues, agentIssues, migrationIssues, mdIssues, mdTruncated, readmeIssue, checkProject, agentsChecked, cwd)
	if errs > 0 {
		os.Exit(1)
	}
}

// checkProjectMigration flags v1 project lock files that mdm migrate should
// fold into mdm.lock, and an install mode the project is using but has not
// recorded.
func checkProjectMigration(cwd string) []doctorIssue {
	plan, err := lock.PlanProjectMigration(cwd)
	if err != nil {
		return []doctorIssue{{
			Level:   "error",
			Message: fmt.Sprintf("legacy lock file could not be read: %v", err),
		}}
	}
	var issues []doctorIssue
	for _, fname := range lock.LegacyProjectLockNames {
		if _, ok := plan.Legacy[fname]; ok {
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is a v1 lock file - fold it into %s with `mdm migrate`", fname, lockName),
			})
		}
	}
	// Copies on disk with no recorded mode get re-symlinked by the next restore.
	// Both lock shapes need this: mdm.lock has no legacy files to prompt a
	// migration, and a v1 lock's warning names the file but not the copies.
	if plan.InstallModeBackfill != "" {
		msg := fmt.Sprintf("skills here are installed in %s mode but %s does not record it: run `mdm migrate` so installs and updates keep it", plan.InstallModeBackfill, lockName)
		if !plan.TargetExists {
			msg = fmt.Sprintf("skills here are installed in %s mode and no lock records it: run `mdm migrate` first, or the next install replaces them with symlinks", plan.InstallModeBackfill)
		}
		issues = append(issues, doctorIssue{Level: "warn", Message: msg})
	}
	return issues
}

// checkGlobalMigration flags the v1 global skills-lock.json, and an install mode
// the global scope uses but has not recorded. checkGlobal only sets the level of
// the unreadable-state issue: error in global scope, warning otherwise, so
// `mdm doctor -p` as a CI gate does not exit 1 over a machine-global file.
func checkGlobalMigration(checkGlobal bool) []doctorIssue {
	var issues []doctorIssue
	if path, ok := lock.LegacyGlobalLockExists(); ok {
		issues = append(issues, doctorIssue{
			Level:   "warn",
			Message: fmt.Sprintf("%s is the v1 global state file - move it to %s with `mdm migrate`", path, lock.GetGlobalStatePath()),
		})
	}
	plan, err := lock.PlanGlobalMigration()
	if err != nil {
		level := "warn"
		if checkGlobal {
			level = "error"
		}
		return append(issues, doctorIssue{
			Level:   level,
			Message: fmt.Sprintf("global state could not be read: %v", err),
		})
	}
	// Both state shapes need the warning, as in checkProjectMigration.
	if plan.InstallModeBackfill != "" {
		msg := fmt.Sprintf("global skills are installed in %s mode but %s does not record it: run `mdm migrate` so installs and updates keep it", plan.InstallModeBackfill, lock.GetGlobalStatePath())
		if !plan.TargetExists {
			msg = fmt.Sprintf("global skills are installed in %s mode and no state file records it: run `mdm migrate` first, or the next install replaces them with symlinks", plan.InstallModeBackfill)
		}
		issues = append(issues, doctorIssue{Level: "warn", Message: msg})
	}
	return issues
}

func buildProjectSkipPaths(cwd, canonicalBase string, skipDirs, skipFiles map[string]bool) {
	if _, err := os.Stat(canonicalBase); err == nil {
		skipDirs[filepath.Clean(canonicalBase)] = true
	}
	for _, harnessCfg := range harness.AllHarnesses {
		if harnessCfg == nil {
			continue
		}
		harnessSkillsDir := filepath.Clean(filepath.Join(cwd, harnessCfg.SkillsDir))
		if _, err := os.Stat(harnessSkillsDir); err == nil {
			skipDirs[harnessSkillsDir] = true
		}
		if harnessCfg.InstructionsFile != "" {
			skipFiles[filepath.Clean(filepath.Join(cwd, harnessCfg.InstructionsFile))] = true
		}
	}
}

// ── Checks ─────────────────────────────────────────────────────────────────────

func diagnoseSkill(r *doctorResult, global bool, cwd string) {
	// 1. Skill directory must exist
	if _, err := os.Stat(r.Path); os.IsNotExist(err) {
		r.Issues = append(r.Issues, doctorIssue{
			Level:   "error",
			Message: "skill directory not found on disk - run `mdm skills install` to restore",
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

	// 3. Symlinks in harness-specific directories must resolve
	checkHarnessLinks(r, global, cwd)

	// 4. Markdown files must not be too large for harness context windows
	checkLargeMarkdown(r)
}

// checkHarnessLinks verifies that symlinks in non-universal harness directories
// point to an existing target.
func checkHarnessLinks(r *doctorResult, global bool, cwd string) {
	sName := sanitizeName(r.Name)
	for harnessName, harnessCfg := range harness.AllHarnesses {
		if harnessCfg == nil || harness.UsesSharedSkillsDir(harnessName) {
			continue
		}
		var harnessBase string
		if global {
			if harnessCfg.GlobalSkillsDir == "" {
				continue
			}
			harnessBase = harnessCfg.GlobalSkillsDir
		} else {
			harnessBase = filepath.Join(cwd, harnessCfg.SkillsDir)
		}
		linkPath := filepath.Join(harnessBase, sName)
		info, err := os.Lstat(linkPath)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue // not present or not a symlink
		}
		target, err := os.Readlink(linkPath)
		if err != nil {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("broken symlink in %s directory", harnessCfg.DisplayName),
			})
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(linkPath), target)
		}
		if _, err := os.Stat(target); os.IsNotExist(err) {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("broken symlink in %s directory: target not found", harnessCfg.DisplayName),
			})
		}
	}
}

// checkLargeMarkdown walks the skill directory and flags .md files large enough
// to threaten harness context windows. It skips .git, node_modules, and vendor.
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
				Message: fmt.Sprintf("%s is %s — likely too large for harness context windows", rel, formatFileSize(size)),
			})
		case size >= fileSizeWarnBytes:
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is %s — may strain harness context windows", rel, formatFileSize(size)),
			})
		}
		return nil
	})
}

// checkUnlinkedRulesHarnesses finds configured harnesses with a unique
// instructions file (CLAUDE.md, .cursorrules) not yet symlinked to AGENTS.md.
func checkUnlinkedRulesHarnesses(cwd string) []doctorIssue {
	configured := lock.GetConfiguredHarnesses(false, cwd)
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
		cfg := harness.AllHarnesses[name]
		if cfg == nil || cfg.NativeInstructions {
			continue
		}
		instrPath := filepath.Join(cwd, cfg.InstructionsFile)
		info, err := os.Lstat(instrPath)
		if err != nil {
			// File doesn't exist at all - not yet linked.
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s (%s) is configured but %s is missing - run `mdm rules link` to create it", cfg.DisplayName, name, cfg.InstructionsFile),
			})
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			// Real file, not a symlink to AGENTS.md.
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s (%s) is configured but %s is not linked to AGENTS.md - run `mdm rules link`", cfg.DisplayName, name, cfg.InstructionsFile),
			})
		}
		// If it is a symlink we assume it points to AGENTS.md (rules link created it).
	}
	return issues
}

// checkMissingHarnessSkillLinks finds configured harnesses whose rules file is
// linked but whose skills directory lacks symlinks for installed project skills.
func checkMissingHarnessSkillLinks(cwd string) []doctorIssue {
	configured := lock.GetConfiguredHarnesses(false, cwd)
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
		cfg := harness.AllHarnesses[name]
		if cfg == nil || harness.UsesSharedSkillsDir(name) {
			// Shared-skills-dir harnesses don't need per-harness symlinks.
			continue
		}
		// Harnesses whose rules file is missing are already reported by
		// checkUnlinkedRulesHarnesses. A pure-skills-dir harness has no rules file.
		if !cfg.NativeInstructions {
			instrPath := filepath.Join(cwd, cfg.InstructionsFile)
			info, err := os.Lstat(instrPath)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				continue // rules not linked yet - covered by the other check
			}
		}

		harnessSkillsDir := filepath.Join(cwd, cfg.SkillsDir)
		var missing []string
		for skillName := range localLock.Skills {
			sName := sanitizeName(skillName)
			linkPath := filepath.Join(harnessSkillsDir, sName)
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
					Message: fmt.Sprintf("%s (%s) is configured but skill %q is not installed for it - run `mdm skills add` to include it", cfg.DisplayName, name, skillName),
				})
			}
		}
	}
	return issues
}

// checkAgentInstalls reports the health of every project-scoped locked agent
// definition. It separates "installed in no harness" from "a harness entry is a
// symlink whose target is gone". agentInstalledSomewhere uses os.Lstat, so a
// dangling symlink counts as installed there and `mdm agents list` calls a
// broken install healthy. Doctor is where that surfaces.
func checkAgentInstalls(cwd string) (issues []doctorIssue, checked int) {
	names, agents := agentLockEntries(false, cwd)
	for _, name := range names {
		issues = append(issues, diagnoseAgentInstall(name, agents[name], cwd)...)
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Message < issues[j].Message })
	return issues, len(names)
}

// diagnoseAgentInstall checks one locked project-scoped agent definition: the
// canonical file, and every harness that has something on disk for it.
func diagnoseAgentInstall(name string, entry lock.AgentLockEntry, cwd string) []doctorIssue {
	var issues []doctorIssue

	canonical := agentCanonicalPath(name, lockedAgentFormat(entry), false, cwd)
	if _, err := os.Stat(canonical); err != nil {
		issues = append(issues, doctorIssue{
			Level:   "error",
			Message: fmt.Sprintf("agent %q: canonical file missing — run `mdm agents install` to restore", name),
		})
	}

	installedAnywhere := false
	for harnessName := range harness.AllHarnesses {
		target := agentHarnessPath(name, harnessName, false, cwd)
		if target == "" {
			continue
		}
		info, err := os.Lstat(target)
		if err != nil {
			// A definition need not be installed to every harness that could
			// take it.
			continue
		}
		installedAnywhere = true
		if info.Mode()&os.ModeSymlink == 0 {
			continue // a real file (copy-mode install) — healthy
		}
		if _, statErr := os.Stat(target); statErr != nil {
			cfg := harness.AllHarnesses[harnessName]
			displayName := harnessName
			if cfg != nil {
				displayName = cfg.DisplayName
			}
			issues = append(issues, doctorIssue{
				Level:   "error",
				Message: fmt.Sprintf("agent %q: broken symlink in %s — target missing, run `mdm agents update %s` to repair", name, displayName, name),
			})
		}
	}

	if !installedAnywhere {
		issues = append(issues, doctorIssue{
			Level:   "warn",
			Message: fmt.Sprintf("agent %q is not installed in any harness — run `mdm agents install` to restore", name),
		})
	}

	return issues
}

// checkInstructionFiles scans the project root for known harness instruction
// files (CLAUDE.md, AGENTS.md, .cursorrules) and flags oversized ones.
func checkInstructionFiles(cwd string) []doctorIssue {
	seen := map[string]bool{}
	var issues []doctorIssue

	for _, harnessCfg := range harness.AllHarnesses {
		if harnessCfg == nil || harnessCfg.InstructionsFile == "" {
			continue
		}
		fname := harnessCfg.InstructionsFile
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
				Message: fmt.Sprintf("%s is %s — likely too large for harness context windows", fname, formatFileSize(size)),
			})
		case size >= fileSizeWarnBytes:
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is %s — may strain harness context windows", fname, formatFileSize(size)),
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
		Message: "no README found in project root - consider adding a README.md",
	}
}

// checkProjectMarkdown walks the project tree and flags .md files large enough
// to strain harness context windows. It skips what the skill and instruction
// checks cover plus build directories, and stops after markdownWalkLimit entries.
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
				Message: fmt.Sprintf("%s is %s — likely too large for harness context windows", rel, formatFileSize(size)),
			})
		case size >= fileSizeWarnBytes:
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s is %s — may strain harness context windows", rel, formatFileSize(size)),
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

func printDoctorResults(results []doctorResult, instrIssues, unlinkedRulesIssues, missingSkillLinkIssues, knowledgeIssues, pluginIssues, agentIssues, migrationIssues, mdIssues []doctorIssue, mdTruncated bool, readmeIssue *doctorIssue, scannedProject bool, agentsChecked int, cwd string) int {
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

	if len(knowledgeIssues) > 0 {
		fmt.Printf("%sKnowledge bundles:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(knowledgeIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	if len(pluginIssues) > 0 {
		fmt.Printf("%sPlugins:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(pluginIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	if len(agentIssues) > 0 {
		fmt.Printf("%sAgent definitions:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(agentIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	if len(migrationIssues) > 0 {
		fmt.Printf("%sMigration:%s\n\n", ansiText, ansiReset)
		e, w := printAndCountDoctorIssues(migrationIssues)
		totalErrors += e
		totalWarnings += w
		fmt.Println()
	}

	e, w := printDoctorMarkdownSection(readmeIssue, mdIssues, mdTruncated)
	totalErrors += e
	totalWarnings += w

	printDoctorSummary(len(results), agentsChecked, scannedProject, totalErrors, totalWarnings)
	return totalErrors
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
		fmt.Printf("  %s▲%s %sscan stopped after %d entries - run from a subdirectory to check further%s\n",
			ansiYellow, ansiReset, ansiDim, markdownWalkLimit, ansiReset)
	}
	fmt.Println()
	return
}

func printDoctorSummary(total, agents int, scannedProject bool, errs, warns int) {
	// Naming only skills told a project holding agent definitions that
	// nothing was installed, right after doctor had checked them.
	var checked []string
	if total > 0 {
		checked = append(checked, fmt.Sprintf("%d skill(s)", total))
	}
	if agents > 0 {
		checked = append(checked, fmt.Sprintf("%d agent definition(s)", agents))
	}
	if len(checked) == 0 {
		fmt.Printf("%sDoctor complete:%s nothing installed", ansiText, ansiReset)
	} else {
		fmt.Printf("%sDoctor complete:%s %s checked", ansiText, ansiReset, strings.Join(checked, " and "))
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
