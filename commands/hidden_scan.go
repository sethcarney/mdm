package commands

import (
	"fmt"
	"path/filepath"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/security/markdownscan"
	"github.com/sethcarney/mdm/internal/skill"
)

func checkSkillMarkdownForHiddenChars(skillName string, findings []markdownscan.Finding, allow bool) bool {
	if len(findings) == 0 {
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

func printHiddenCharFindings(skillName string, findings []markdownscan.Finding, allow bool) {
	if allow {
		fmt.Printf("%sHidden character warnings in %s:%s\n\n", ansiYellow, skillName, ansiReset)
	} else {
		fmt.Printf("%sHidden character scan failed for %s:%s\n\n", ansiRed, skillName, ansiReset)
	}
	for _, f := range findings {
		fmt.Printf("  %s%s%s:%d:%d  %s%s%s  %s%s%s",
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
	if allow {
		fmt.Printf("%sContinuing because --allow-hidden-chars was provided.%s\n\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sInstallation blocked.%s Remove the hidden characters or pass %s--allow-hidden-chars%s to install anyway.\n\n",
		ansiRed, ansiReset, ansiText, ansiReset)
}

func formatCodepoint(r rune) string {
	if r < 0 {
		return "U+FFFD"
	}
	return fmt.Sprintf("U+%04X", r)
}
