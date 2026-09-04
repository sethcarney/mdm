package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/skill"
)

// skillBody is what the fixtures write into SKILL.md, so a test can tell "the
// content survived" from "the file exists but is empty".
const skillBody = "---\nname: demo\ndescription: d\n---\nbody\n"

func writeDemoSkill(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillBody), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertSkillIntact(t *testing.T, dir string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("%s should still hold the skill: %v", dir, err)
	}
	if string(got) != skillBody {
		t.Errorf("%s/SKILL.md = %q, want the original content", dir, got)
	}
}

// Guards the sameExistingDir check around cleanAndCreateDir/cp in
// performSymlinkInstall. `mdm skills add .` discovers mdm's own canonical
// copies under .agents/skills, so source and destination are one directory.
// cleanAndCreateDir is a RemoveAll and runs before any copying.
func TestInstallSkillFromItsOwnCanonicalDirIsNotDestructive(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	canonical := writeDemoSkill(t, filepath.Join(cwd, ".agents", "skills", "demo"))

	r := installSkillForHarness(&skill.Skill{Name: "demo", Path: canonical}, "claude-code", false, InstallModeSymlink)
	if !r.Success {
		t.Fatalf("reinstalling a skill that is already in place must not fail: %s", r.Error)
	}
	assertSkillIntact(t, canonical)
}

// The same defect on the copy-mode path, where the destination is the
// harness's own directory rather than the canonical one. Mutation: removing
// the sameExistingDir guard in the InstallModeCopy branch of
// installSkillForHarness.
func TestInstallSkillCopyModeFromItsOwnHarnessDirIsNotDestructive(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	harnessDir := writeDemoSkill(t, filepath.Join(cwd, ".claude", "skills", "demo"))

	r := installSkillForHarness(&skill.Skill{Name: "demo", Path: harnessDir}, "claude-code", false, InstallModeCopy)
	if !r.Success {
		t.Fatalf("reinstalling a skill that is already in place must not fail: %s", r.Error)
	}
	assertSkillIntact(t, harnessDir)
}

// Source and destination are reached by different paths: the discovered source
// is the harness's symlink, the destination is the canonical directory it
// points at. That is why the check is os.SameFile and not string comparison.
func TestInstallSkillThroughAHarnessSymlinkIsNotDestructive(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	canonical := writeDemoSkill(t, filepath.Join(cwd, ".agents", "skills", "demo"))
	link := filepath.Join(cwd, ".claude", "skills", "demo")
	symlinkOrSkip(t, canonical, link)

	r := installSkillForHarness(&skill.Skill{Name: "demo", Path: link}, "claude-code", false, InstallModeSymlink)
	if !r.Success {
		t.Fatalf("reinstalling a skill that is already in place must not fail: %s", r.Error)
	}
	assertSkillIntact(t, canonical)
}

// The guard must not turn a real install into a no-op: a source somewhere
// else on disk is still copied into the canonical directory, and the harness
// still gets its link.
func TestInstallSkillFromAnUnrelatedSourceStillInstalls(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	src := writeDemoSkill(t, filepath.Join(t.TempDir(), "demo"))

	r := installSkillForHarness(&skill.Skill{Name: "demo", Path: src}, "claude-code", false, InstallModeSymlink)
	if !r.Success {
		t.Fatalf("install failed: %s", r.Error)
	}
	assertSkillIntact(t, filepath.Join(cwd, ".agents", "skills", "demo"))
	if _, err := os.Stat(filepath.Join(cwd, ".claude", "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("claude-code should have the skill: %v", err)
	}
}
