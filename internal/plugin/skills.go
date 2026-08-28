package plugin

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sethcarney/mdm/internal/skill"
)

// PluginSkill is one skill discovered inside a plugin's skills/ directory.
type PluginSkill struct {
	Name  string
	Dir   string // absolute path to the skill directory
	Skill *skill.Skill
}

// ListSkills discovers skills per the spec's fixed-location rule: each
// immediate child of <root>/skills containing a regular SKILL.md is one
// skill, with no deeper recursion. A missing skills/ directory is not an
// error; a skills entry that is not a directory invalidates the component.
// Invalid skills are skipped and reported without affecting the rest.
func ListSkills(root string) ([]PluginSkill, []Issue) {
	skillsPath := filepath.Join(root, SkillsDir)
	info, err := os.Stat(skillsPath)
	if err != nil {
		return nil, nil
	}
	if !info.IsDir() {
		return nil, []Issue{{
			File:     SkillsDir,
			Severity: SeverityError,
			Rule:     "skills-not-directory",
			Message:  "skills exists but is not a directory - skills component disabled",
		}}
	}
	entries, err := os.ReadDir(skillsPath)
	if err != nil {
		return nil, []Issue{{
			File:     SkillsDir,
			Severity: SeverityError,
			Rule:     "skills-unreadable",
			Message:  err.Error(),
		}}
	}
	var skills []PluginSkill
	var issues []Issue
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(skillsPath, entry.Name())
		if !skill.HasSkillMd(dir) {
			continue
		}
		if EscapesRoot(root, filepath.Join(dir, "SKILL.md")) {
			issues = append(issues, Issue{
				File:     filepath.ToSlash(filepath.Join(SkillsDir, entry.Name(), "SKILL.md")),
				Severity: SeverityWarning,
				Rule:     "skill-escapes-root",
				Message:  fmt.Sprintf("skill %q skipped: SKILL.md resolves outside the plugin root", entry.Name()),
			})
			continue
		}
		s, err := skill.ParseSkillMd(filepath.Join(dir, "SKILL.md"), false)
		if err != nil || s == nil {
			issues = append(issues, Issue{
				File:     filepath.ToSlash(filepath.Join(SkillsDir, entry.Name(), "SKILL.md")),
				Severity: SeverityWarning,
				Rule:     "invalid-skill",
				Message:  fmt.Sprintf("skill %q skipped: SKILL.md is missing a valid name or description", entry.Name()),
			})
			continue
		}
		skills = append(skills, PluginSkill{Name: s.Name, Dir: dir, Skill: s})
	}
	return skills, issues
}
