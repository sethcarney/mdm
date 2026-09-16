package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S5: adding a harness to a copy-mode scope must give it a real copy, not a
// symlink - otherwise the new harness alone disagrees with every other skill.
func TestHarnessesAddHonorsCopyMode(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writeLocalSkill(t, dir, "src", "myskill", "hi")

	if _, stderr, code := runMdmInDir(t, dir, env, "skills", "add", src, "--harness", "claude-code", "--copy", "--project", "-y"); code != 0 {
		t.Fatalf("copy add failed: %d %s", code, stderr)
	}
	if _, stderr, code := runMdmInDir(t, dir, env, "harnesses", "add", "roo"); code != 0 {
		t.Fatalf("harnesses add failed: %d %s", code, stderr)
	}
	rooSkill := filepath.Join(dir, ".roo", "skills", "myskill")
	fi, err := os.Lstat(rooSkill)
	if err != nil {
		t.Fatalf("roo skill not installed: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("copy-mode scope should give a new harness a real copy, not a symlink")
	}
}

// S6: a skill installed globally for a shared-dir harness lands in the shared
// ~/.agents/skills directory; list must attribute it to that harness, not show
// it owned by nobody because it probed the harness's empty per-harness dir.
func TestSkillsListGlobalAttributesSharedDirHarness(t *testing.T) {
	home := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(home, stateDir)
	// A separate cwd for the source; the install is global.
	proj := t.TempDir()
	writeLocalSkill(t, proj, "src", "myskill", "hi")

	if _, stderr, code := runMdmInDir(t, proj, env, "skills", "add", "./src", "-g", "--harness", "cursor", "-y"); code != 0 {
		t.Fatalf("global install failed: %d %s", code, stderr)
	}
	// Cursor must be detected for list to check it at all.
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, proj, env, "skills", "list", "-g")
	if code != 0 {
		t.Fatalf("list -g failed: %d %s", code, stderr)
	}
	if !strings.Contains(stdout, "Cursor") {
		t.Errorf("global list should attribute the skill to Cursor:\n%s", stdout)
	}
}

// A7: `agents remove --harness X` when X's file was deleted by hand must still
// drop X from the lock's harness list, or list keeps reporting it missing and
// the next install reinstalls there.
func TestAgentsRemoveHarnessDropsHandDeletedFromLock(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	srcDir := filepath.Join(dir, "src", "agents")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "critic.md"), []byte("---\nname: critic\ndescription: c\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runMdmInDir(t, dir, env, "agents", "add", "./src", "--harness", "claude-code", "cursor", "--project", "-y"); code != 0 {
		t.Fatalf("setup add failed: %d %s", code, stderr)
	}

	harnessesOf := func() []string {
		data, _ := os.ReadFile(filepath.Join(dir, lockName))
		var lk struct {
			Agents map[string]struct {
				Harnesses []string `json:"harnesses"`
			} `json:"agents"`
		}
		_ = json.Unmarshal(data, &lk)
		return lk.Agents["critic"].Harnesses
	}
	before := strings.Join(harnessesOf(), ",")
	if !strings.Contains(before, "cursor") || !strings.Contains(before, "claude-code") {
		t.Fatalf("setup: expected both harnesses in the lock, got %q", before)
	}

	// Delete cursor's file by hand.
	_ = os.Remove(filepath.Join(dir, ".cursor", "rules", "critic.md"))
	_ = os.Remove(filepath.Join(dir, ".cursor", "agents", "critic.md"))

	if _, stderr, code := runMdmInDir(t, dir, env, "agents", "remove", "critic", "--harness", "cursor", "--project", "-y"); code != 0 {
		t.Fatalf("remove --harness cursor failed: %d %s", code, stderr)
	}
	after := harnessesOf()
	for _, h := range after {
		if h == "cursor" {
			t.Errorf("cursor should be gone from the lock after an explicit remove, got %v", after)
		}
	}
	if strings.Join(after, ",") != "claude-code" {
		t.Errorf("claude-code should remain, got %v", after)
	}
}
