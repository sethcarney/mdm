package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two sources, two frontmatter names - "critic" and "Critic" - one disk
// name. The second add used to repoint the canonical file and the lock entry
// at its own source without a word, and Cursor's link then served the second
// source's body. It is refused now, naming both, and --force is the override.
func TestAgentsAddRefusesACrossRunNameCollisionUnlessForced(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)
	srcA := writeAgentSource(t, "critic", "critic")
	srcB := writeAgentSource(t, "critic", "Critic")

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", srcA, "--harness", "cursor", "--project", "-y"); code != 0 {
		t.Fatalf("first add exited %d:\n%s%s", code, stdout, stderr)
	}
	canonical := filepath.Join(projectDir, ".agents", "agents", "critic.md")
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", srcB, "--harness", "claude-code", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("the colliding add exited 0:\n%s", combined)
	}
	for _, want := range []string{"already installed", "--force"} {
		if !strings.Contains(combined, want) {
			t.Errorf("refusal missing %q:\n%s", want, combined)
		}
	}
	// The lock keeps a local source cwd-relative, and the refusal names the
	// recorded one as recorded; the incoming one is named as typed.
	recordedA := "../" + filepath.Base(srcA)
	if !strings.Contains(combined, recordedA) || !strings.Contains(combined, srcB) {
		t.Errorf("refusal does not name both sources (%s and %s):\n%s", recordedA, srcB, combined)
	}
	if after, _ := os.ReadFile(canonical); string(after) != string(before) {
		t.Errorf("the canonical file changed despite the refusal:\nbefore: %q\nafter:  %q", before, after)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "critic.md")); !os.IsNotExist(err) {
		t.Errorf("Claude Code received the refused definition (stat err=%v)", err)
	}

	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "add", srcB, "--harness", "claude-code", "--project", "-y", "--force")
	if code != 0 {
		t.Fatalf("the forced add exited %d:\n%s%s", code, stdout, stderr)
	}
	if after, _ := os.ReadFile(canonical); !strings.Contains(string(after), "You are Critic.") {
		t.Errorf("the forced add did not replace the canonical file:\n%s", after)
	}
	lockData, _ := os.ReadFile(filepath.Join(projectDir, lockName))
	recordedB := `"source": "../` + filepath.Base(srcB) + `"`
	if !strings.Contains(string(lockData), recordedB) || strings.Contains(string(lockData), recordedA) {
		t.Errorf("lock does not point at the forced source %s:\n%s", recordedB, lockData)
	}

	// A refusal is a failure even when another definition in the same add
	// went in: the exit code counted installs only, so a batch with one
	// refused name read, to a script, like one where every name landed.
	srcC := writeAgentSourceWith(t, "critic", "helper")
	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "add", srcC, "--harness", "claude-code", "--project", "-y")
	combined = stdout + stderr
	if code == 0 {
		t.Errorf("an add that refused critic but installed helper exited 0:\n%s", combined)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "helper.md")); err != nil {
		t.Errorf("helper, which collided with nothing, was not installed: %v", err)
	}
}
