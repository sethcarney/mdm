package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `mdm agents add ./src` where ./src carries its own `critic` must not silently
// overwrite a hand-written .claude/agents/critic.md the user wrote themselves.
// Before the guard, the install replaced it with a symlink into the gitignored
// canonical directory, so a clone lost the hand-written file entirely. Unlike
// the adoption case (add . on the file itself), here the source is a different
// file, so there is nothing to adopt - the destination is refused, and the
// hand-written file is left exactly as it was. --force is the opt-in to replace.
func TestAgentsAddForeignSourceDoesNotOverwriteHandWrittenFile(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)

	own, original := writeOwnClaudeCritic(t, projectDir)

	// A different source with its own critic definition.
	srcDir := filepath.Join(projectDir, "src", "agents")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcBody := "---\nname: critic\ndescription: from src\n---\n\nThe src body, not the user's.\n"
	if err := os.WriteFile(filepath.Join(srcDir, "critic.md"), []byte(srcBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without --force: the add is refused for that harness and exits non-zero.
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", "./src", "--harness", "claude-code", "--project", "-y")
	if code == 0 {
		t.Fatalf("add of a foreign critic over a hand-written file should exit non-zero:\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "not written by mdm") {
		t.Errorf("expected a 'not written by mdm' refusal:\n%s%s", stdout, stderr)
	}

	// The hand-written file is untouched: still a real file, still its content.
	fi, err := os.Lstat(own)
	if err != nil {
		t.Fatalf("the hand-written file is gone: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the hand-written file was turned into a symlink")
	}
	if got, _ := os.ReadFile(own); string(got) != original {
		t.Errorf("the hand-written file was overwritten:\n%s", got)
	}

	// --force is the explicit opt-in to replace it.
	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "add", "./src", "--harness", "claude-code", "--project", "-y", "--force")
	if code != 0 {
		t.Fatalf("--force add should succeed, got %d:\n%s%s", code, stdout, stderr)
	}
	if got, _ := os.ReadFile(own); !strings.Contains(string(got), "src body") {
		t.Errorf("--force should have replaced the file with the src definition:\n%s", got)
	}
}
