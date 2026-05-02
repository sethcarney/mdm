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
)

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
  • Skills modified since install (hash mismatch)
  • Markdown files large enough to strain agent context windows
  • Oversized project instruction files (CLAUDE.md, AGENTS.md, etc.)

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
	}

	// Instruction-file check is project-level only; skip if --global
	var instrIssues []doctorIssue
	if checkProject {
		instrIssues = checkInstructionFiles(cwd)
	}

	if len(results) == 0 && len(instrIssues) == 0 {
		fmt.Printf("%sNo skills found.%s\n", ansiDim, ansiReset)
		return
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Scope != results[j].Scope {
			return results[i].Scope < results[j].Scope
		}
		return results[i].Name < results[j].Name
	})

	printDoctorResults(results, instrIssues, cwd)
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
		if err != nil || sk == nil {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: "SKILL.md is missing required name or description fields",
			})
		}
	}

	// 3. Symlinks in agent-specific directories must resolve
	checkAgentLinks(r, global, cwd)

	// 4. Skill content must match the installed hash (global skills only)
	if storedHash != "" {
		current, err := lock.ComputeSkillFolderHash(r.Path)
		if err == nil && current != storedHash {
			r.Issues = append(r.Issues, doctorIssue{
				Level:   "warn",
				Message: "skill content differs from installed version — run `mdm skills update` to sync",
			})
		}
	}

	// 5. Markdown files must not be too large for agent context windows
	checkLargeMarkdown(r)
}

// checkAgentLinks verifies that symlinks in non-universal agent directories
// point to an existing target.
func checkAgentLinks(r *doctorResult, global bool, cwd string) {
	sName := sanitizeName(r.Name)
	for agentName, agentCfg := range agent.AllAgents {
		if agentCfg == nil || agent.IsUniversalAgent(agentName) {
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
// large enough to threaten agent context windows.
func checkLargeMarkdown(r *doctorResult) {
	_ = filepath.WalkDir(r.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
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

// checkInstructionFiles scans the project root for known agent instruction
// files (CLAUDE.md, AGENTS.md, etc.) and flags oversized ones.
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

// ── Output ─────────────────────────────────────────────────────────────────────

func printDoctorResults(results []doctorResult, instrIssues []doctorIssue, cwd string) {
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
			totalErrors += errCount
			totalWarnings += warnCount

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
	}

	// Instruction files section
	if len(instrIssues) > 0 {
		fmt.Printf("%sProject files:%s\n\n", ansiText, ansiReset)
		for _, issue := range instrIssues {
			icon, color := doctorIssueIcon(issue.Level)
			fmt.Printf("  %s%s%s %s%s%s\n", color, icon, ansiReset, ansiDim, issue.Message, ansiReset)
			if issue.Level == "error" {
				totalErrors++
			} else {
				totalWarnings++
			}
		}
		fmt.Println()
	}

	// Summary
	total := len(results)
	fmt.Printf("%sDoctor complete:%s %d skill(s) checked", ansiText, ansiReset, total)
	if totalErrors > 0 {
		fmt.Printf(", %s%d error(s)%s", ansiRed, totalErrors, ansiReset)
	}
	if totalWarnings > 0 {
		fmt.Printf(", %s%d warning(s)%s", ansiYellow, totalWarnings, ansiReset)
	}
	if totalErrors == 0 && totalWarnings == 0 {
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
