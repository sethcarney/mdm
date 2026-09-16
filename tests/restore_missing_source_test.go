package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A restore whose source still exists but no longer yields the recorded
// definition must report the missing name and exit non-zero, not print "Done."
// and exit 0 as though it had installed everything.
func TestAgentsInstallReportsDefinitionMissingFromSource(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	src := filepath.Join(dir, "src", "agents")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "critic.md"), []byte("---\nname: critic\ndescription: c\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runMdmInDir(t, dir, env, "agents", "add", "./src", "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup add failed: %d %s", code, stderr)
	}

	// Simulate a fresh clone: the gitignored install trees are gone, and the
	// source no longer carries the definition the lock still records.
	os.RemoveAll(filepath.Join(dir, ".agents"))
	os.RemoveAll(filepath.Join(dir, ".claude"))
	os.Remove(filepath.Join(src, "critic.md"))

	stdout, stderr, code := runMdmInDir(t, dir, env, "agents", "install", "-y")
	if code == 0 {
		t.Fatalf("install should exit non-zero when a recorded definition is gone:\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "critic") || !strings.Contains(stdout+stderr, "not found") {
		t.Errorf("install should name the missing definition:\n%s%s", stdout, stderr)
	}
}

// A skills restore must survive a source group that no longer yields its skill:
// the other groups still restore, the missing one is reported, and the run
// exits non-zero. Before, the empty source exited the process mid-loop.
func TestSkillsInstallSurvivesAMissingSourceAndReportsIt(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	writeLocalSkill(t, dir, "srcA", "good", "hi")
	writeLocalSkill(t, dir, "srcB", "gone", "bye")
	for _, s := range []string{"./srcA", "./srcB"} {
		if _, stderr, code := runMdmInDir(t, dir, env, "skills", "add", s, "--harness", "claude-code", "--project", "-y"); code != 0 {
			t.Fatalf("setup add %s failed: %d %s", s, code, stderr)
		}
	}

	os.RemoveAll(filepath.Join(dir, ".agents"))
	os.RemoveAll(filepath.Join(dir, ".claude"))
	os.RemoveAll(filepath.Join(dir, "srcB", "gone"))

	stdout, stderr, code := runMdmInDir(t, dir, env, "skills", "install", "-y")
	if code == 0 {
		t.Fatalf("install should exit non-zero when a recorded skill is gone:\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "gone") || !strings.Contains(stdout+stderr, "not found") {
		t.Errorf("install should name the missing skill:\n%s%s", stdout, stderr)
	}
	// The reachable source still restored, so the loop was not aborted.
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "good")); err != nil {
		t.Errorf("the reachable skill should still have been restored: %v", err)
	}
}
