package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

// installedDemo is the InstalledSkill a removal is handed for a skill whose
// canonical directory is .agents/skills/demo. Harnesses is what
// listInstalledSkills reports, which is already narrowed by --harness, so a
// scoped removal must not use it to answer "who else has this?".
func installedDemo(cwd string, harnesses ...string) *InstalledSkill {
	return &InstalledSkill{
		Name:          "demo",
		Path:          filepath.Join(cwd, ".agents", "skills", "demo"),
		CanonicalPath: filepath.Join(cwd, ".agents", "skills", "demo"),
		Scope:         "project",
		Harnesses:     harnesses,
	}
}

func canonicalDemoSkillMd(cwd string) string {
	return filepath.Join(cwd, ".agents", "skills", "demo", "SKILL.md")
}

func hasLockEntry(t *testing.T, cwd, name string) bool {
	t.Helper()
	_, ok := lock.ReadLocalLock(cwd).Skills[name]
	return ok
}

// Guards the `if len(retained) > 0 { return }` gate in removeSkillFromDisk.
// Removing a skill from one harness must not delete it for the others: Roo
// Code's install is a symlink into the canonical directory, and the lock entry
// is what lets `mdm skills list` or `mdm doctor` notice.
func TestRemoveSkillScopedToOneHarnessKeepsCanonicalAndLock(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	linkSkillInto(t, cwd, ".claude", "demo")
	_, rooLink := linkSkillInto(t, cwd, ".roo", "demo")
	lockSkill(t, cwd, "demo")

	retained, err := removeSkillFromDisk(installedDemo(cwd, "claude-code"), []string{"claude-code"}, false, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(retained, "roo") {
		t.Errorf("retained = %v, want it to name roo - roo still has the skill", retained)
	}

	if _, statErr := os.Lstat(filepath.Join(cwd, ".claude", "skills", "demo")); !os.IsNotExist(statErr) {
		t.Errorf("claude-code's install should be gone, stat err = %v", statErr)
	}
	// os.Stat follows the link, so this fails if the canonical directory was
	// deleted out from under it - the dangling-symlink damage, not just the
	// absence of the link itself.
	if _, statErr := os.Stat(filepath.Join(rooLink, "SKILL.md")); statErr != nil {
		t.Errorf("roo's install must still resolve to a real skill: %v", statErr)
	}
	if _, statErr := os.Stat(canonicalDemoSkillMd(cwd)); statErr != nil {
		t.Errorf("canonical directory must survive while another harness needs it: %v", statErr)
	}
	if !hasLockEntry(t, cwd, "demo") {
		t.Error("lock entry must survive a scoped removal that left the skill installed elsewhere")
	}
}

// Guards the UsesSharedSkillsDir skip in the per-harness deletion loop. For a
// shared-directory harness, "delete this harness's copy" is "delete the
// canonical copy", which breaks Claude Code's symlink into it.
func TestRemoveSkillScopedToSharedDirHarnessKeepsTheSharedCopy(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	if !harness.UsesSharedSkillsDir("cursor") {
		t.Skip("cursor no longer reads the shared skills directory")
	}
	_, claudeLink := linkSkillInto(t, cwd, ".claude", "demo")
	lockSkill(t, cwd, "demo")

	retained, err := removeSkillFromDisk(installedDemo(cwd, "cursor"), []string{"cursor"}, false, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(retained, "claude-code") {
		t.Errorf("retained = %v, want it to name claude-code", retained)
	}
	if _, statErr := os.Stat(filepath.Join(claudeLink, "SKILL.md")); statErr != nil {
		t.Errorf("claude-code's install must still resolve to a real skill: %v", statErr)
	}
	if _, statErr := os.Stat(canonicalDemoSkillMd(cwd)); statErr != nil {
		t.Errorf("the shared directory is claude-code's copy too, so it must survive: %v", statErr)
	}
	if !hasLockEntry(t, cwd, "demo") {
		t.Error("lock entry must survive while the skill is still installed")
	}
}

// The other half of the contract: with no --harness filter the skill is being
// removed outright, so every harness's install, the canonical directory and
// the lock entry all go. This is what stops an over-eager retention check from
// making `mdm skills remove demo` a no-op.
func TestRemoveSkillWithNoHarnessFilterRemovesEverything(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	linkSkillInto(t, cwd, ".claude", "demo")
	linkSkillInto(t, cwd, ".roo", "demo")
	lockSkill(t, cwd, "demo")

	// Harnesses is deliberately empty: an undetected harness is missing from
	// it, and an unfiltered removal still has to clean that harness up rather
	// than leaving it linked to a canonical directory that is about to go.
	retained, err := removeSkillFromDisk(installedDemo(cwd), nil, false, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(retained) != 0 {
		t.Errorf("retained = %v, want none - the skill was removed everywhere", retained)
	}
	for _, dir := range []string{".claude", ".roo"} {
		if _, statErr := os.Lstat(filepath.Join(cwd, dir, "skills", "demo")); !os.IsNotExist(statErr) {
			t.Errorf("%s's install should be gone, stat err = %v", dir, statErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(cwd, ".agents", "skills", "demo")); !os.IsNotExist(statErr) {
		t.Errorf("canonical directory should be gone, stat err = %v", statErr)
	}
	if hasLockEntry(t, cwd, "demo") {
		t.Error("lock entry should be gone once the skill is installed nowhere")
	}
}

// Mutation this test catches: discarding the deletion error (the old `_ =
// os.RemoveAll(...)`) and dropping the lock entry anyway. The lock is the
// record of what is on disk; if the files are still there, the entry has to
// stay, or every later command believes a skill that exists was removed.
func TestRemoveSkillKeepsLockEntryWhenDeletionFails(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	isolateHome(t)

	// A copy-mode install: a real directory, so the deletion goes through
	// removeAllFn rather than the symlink branch.
	copied := writeCopiedSkill(t, cwd, "demo")
	writeSkillDir(t, filepath.Join(cwd, ".agents", "skills", "demo"))
	lockSkill(t, cwd, "demo")

	orig := removeAllFn
	removeAllFn = func(path string) error {
		if path == copied {
			return errors.New("permission denied (simulated)")
		}
		return orig(path)
	}
	t.Cleanup(func() { removeAllFn = orig })

	if _, err := removeSkillFromDisk(installedDemo(cwd, "claude-code"), []string{"claude-code"}, false, cwd); err == nil {
		t.Fatal("expected an error when the per-harness deletion fails")
	}
	if !hasLockEntry(t, cwd, "demo") {
		t.Error("lock entry must survive a failed deletion")
	}
	if _, statErr := os.Stat(canonicalDemoSkillMd(cwd)); statErr != nil {
		t.Errorf("canonical directory must survive a failed per-harness deletion: %v", statErr)
	}
}
