package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// symlinkInto writes contents to a file outside root and links it at
// <root>/<rel>, modeling a package file that escapes the plugin root.
func symlinkInto(t *testing.T, root, rel, contents string) {
	t.Helper()
	outside := filepath.Join(t.TempDir(), filepath.Base(rel))
	if err := os.WriteFile(outside, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
}

func TestManifestSymlinkEscapeRejectsPlugin(t *testing.T) {
	root := t.TempDir()
	symlinkInto(t, root, ManifestFile, manifestJSON(""))
	if _, _, err := LoadManifest(root); err == nil {
		t.Fatal("an escaping plugin.json symlink must reject the plugin")
	}
}

func TestMCPSymlinkEscapeDisablesMCPOnly(t *testing.T) {
	root := writePlugin(t, map[string]string{
		ManifestFile:           manifestJSON(""),
		"skills/keep/SKILL.md": fmt.Sprintf(validSkillMd, "keep"),
	})
	symlinkInto(t, root, MCPFile, mcpJSON(`"a": {"type": "stdio", "command": "x"}`))
	report := Inspect(root)
	if report.Manifest == nil {
		t.Fatal("an escaping mcp.json must not reject the plugin")
	}
	if !report.MCPDisabled || report.MCP != nil {
		t.Errorf("MCP should be disabled, got MCPDisabled=%v MCP=%v", report.MCPDisabled, report.MCP)
	}
	if len(report.Skills) != 1 {
		t.Errorf("skills must still load, got %d", len(report.Skills))
	}
}

func TestSkillMdSymlinkEscapeSkipsSkill(t *testing.T) {
	root := writePlugin(t, map[string]string{
		ManifestFile:           manifestJSON(""),
		"skills/good/SKILL.md": fmt.Sprintf(validSkillMd, "good"),
	})
	symlinkInto(t, root, "skills/sneaky/SKILL.md", fmt.Sprintf(validSkillMd, "sneaky"))
	skills, issues := ListSkills(root)
	if len(skills) != 1 || skills[0].Name != "good" {
		t.Fatalf("only the contained skill should load, got %v", skills)
	}
	found := false
	for _, issue := range issues {
		if issue.Rule == "skill-escapes-root" {
			found = true
		}
	}
	if !found {
		t.Error("the escaping skill should be reported as skipped")
	}
}

func TestSkillMdSymlinkWithinRootIsAllowed(t *testing.T) {
	root := writePlugin(t, map[string]string{
		ManifestFile:      manifestJSON(""),
		"shared/SKILL.md": fmt.Sprintf(validSkillMd, "shared"),
	})
	if err := os.MkdirAll(filepath.Join(root, "skills", "shared"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "shared", "SKILL.md"), filepath.Join(root, "skills", "shared", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	skills, _ := ListSkills(root)
	if len(skills) != 1 || skills[0].Name != "shared" {
		t.Errorf("a within-root symlinked SKILL.md should count, got %v", skills)
	}
}

func TestEscapesRootMissingTarget(t *testing.T) {
	root := t.TempDir()
	if EscapesRoot(root, filepath.Join(root, "absent.json")) {
		t.Error("a nonexistent path has nothing to read and must not report as escaping")
	}
	if _, _, err := LoadMCPConfig(root); !errors.Is(err, nil) {
		t.Errorf("missing mcp.json must stay a valid absence, got %v", err)
	}
}
