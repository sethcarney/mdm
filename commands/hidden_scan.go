package commands

import (
	"fmt"
	"path/filepath"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/security/markdownscan"
	"github.com/sethcarney/mdm/internal/skill"
)

// checkSkillMarkdownForHiddenChars reports every finding and decides whether
// the install may continue. Only blocking (error-severity) findings gate the
// install; warnings such as a variation selector completing a valid emoji
// sequence are printed for the audit trail and never need --allow-hidden-chars.
func checkSkillMarkdownForHiddenChars(skillName string, findings []markdownscan.Finding, allow bool) bool {
	if len(findings) == 0 {
		return true
	}
	if !markdownscan.HasBlocking(findings) {
		printHiddenCharWarnings(skillName, findings)
		return true
	}
	printHiddenCharFindings(skillName, findings, allow)
	return allow
}

func checkDiskSkillMarkdownForHiddenChars(sk *skill.Skill, allow bool) bool {
	findings, err := markdownscan.ScanMarkdownFiles(sk.Path)
	if err != nil {
		fmt.Printf("%sHidden character scan failed for %s: %s%s\n", ansiRed, sk.Name, err, ansiReset)
		return false
	}
	return checkSkillMarkdownForHiddenChars(sk.Name, findings, allow)
}

func checkDiskSkillsMarkdownForHiddenChars(skills []*skill.Skill, allow bool) bool {
	ok := true
	for _, sk := range skills {
		if !checkDiskSkillMarkdownForHiddenChars(sk, allow) {
			ok = false
		}
	}
	return ok
}

func checkBlobSkillMarkdownForHiddenChars(sk *blob.BlobSkill, allow bool) bool {
	files := make([]markdownscan.NamedContent, 0, len(sk.Files))
	for _, f := range sk.Files {
		files = append(files, markdownscan.NamedContent{Path: f.Path, Contents: f.Contents})
	}
	return checkSkillMarkdownForHiddenChars(sk.Name, markdownscan.ScanMarkdownPayloads(files), allow)
}

func checkBlobSkillsMarkdownForHiddenChars(skills []*blob.BlobSkill, allow bool) bool {
	ok := true
	for _, sk := range skills {
		if !checkBlobSkillMarkdownForHiddenChars(sk, allow) {
			ok = false
		}
	}
	return ok
}

func checkWellKnownSkillMarkdownForHiddenChars(sk *registry.WellKnownSkill, allow bool) bool {
	files := make([]markdownscan.NamedContent, 0, len(sk.Files))
	for path, contents := range sk.Files {
		files = append(files, markdownscan.NamedContent{Path: path, Contents: contents})
	}
	return checkSkillMarkdownForHiddenChars(sk.Name, markdownscan.ScanMarkdownPayloads(files), allow)
}

func checkWellKnownSkillsMarkdownForHiddenChars(skills []*registry.WellKnownSkill, allow bool) bool {
	ok := true
	for _, sk := range skills {
		if !checkWellKnownSkillMarkdownForHiddenChars(sk, allow) {
			ok = false
		}
	}
	return ok
}

// printHiddenCharWarnings handles a report with no blocking findings.
func printHiddenCharWarnings(skillName string, findings []markdownscan.Finding) {
	fmt.Printf("%sHidden character warnings in %s:%s\n\n", ansiYellow, skillName, ansiReset)
	printHiddenCharFindingLines(findings)
	fmt.Printf("%sWarnings do not block installation.%s\n\n", ansiDim, ansiReset)
}

func printHiddenCharFindings(skillName string, findings []markdownscan.Finding, allow bool) {
	if allow {
		fmt.Printf("%sHidden character warnings in %s:%s\n\n", ansiYellow, skillName, ansiReset)
	} else {
		fmt.Printf("%sHidden character scan failed for %s:%s\n\n", ansiRed, skillName, ansiReset)
	}
	printHiddenCharFindingLines(findings)
	if allow {
		fmt.Printf("%sContinuing because --allow-hidden-chars was provided.%s\n\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sInstallation blocked.%s Remove the hidden characters or pass %s--allow-hidden-chars%s to install anyway.\n\n",
		ansiRed, ansiReset, ansiText, ansiReset)
}

// printHiddenCharFindingLines prints one line per finding, led by its
// severity so a mixed report shows which lines actually block.
func printHiddenCharFindingLines(findings []markdownscan.Finding) {
	for _, f := range findings {
		severityColor := ansiRed
		if f.Severity == markdownscan.SeverityWarning {
			severityColor = ansiYellow
		}
		fmt.Printf("  %s%-7s%s  %s%s%s:%d:%d  %s%s%s  %s%s%s",
			severityColor, f.Severity, ansiReset,
			ansiText, filepath.ToSlash(f.File), ansiReset,
			f.Line, f.Column,
			ansiYellow, f.Category, ansiReset,
			ansiDim, formatCodepoint(f.Rune), ansiReset)
		if f.Detail != "" {
			fmt.Printf("  %s%s%s", ansiDim, f.Detail, ansiReset)
		}
		fmt.Println()
	}
	fmt.Println()
}

func formatCodepoint(r rune) string {
	if r < 0 {
		return "U+FFFD"
	}
	return fmt.Sprintf("U+%04X", r)
}
