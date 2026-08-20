package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A lock file written by a newer mdm must abort the command with a clear
// error, never read as empty — the old fallback made `mdm skills install`
// in a newer-format project a silent no-op that exits 0 in CI.
func TestNewerLockVersionAborts(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()
	lockContent := `{"version": 2, "skills": {}, "_moved": "mdm-lock.json"}`
	if err := os.WriteFile(filepath.Join(dir, "skills-lock.json"), []byte(lockContent), 0600); err != nil {
		t.Fatal(err)
	}

	env := isolatedEnv(dir, stateDir)
	stdout, stderr, code := runMdmInDir(t, dir, env, "skills", "install", "-y")
	if code == 0 {
		t.Fatalf("expected non-zero exit for a newer lock version, got 0:\n%s%s", stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "newer version of mdm") || !strings.Contains(combined, "mdm upgrade") {
		t.Errorf("expected an upgrade pointer, got stdout=%q stderr=%q", stdout, stderr)
	}

	// Write paths must abort too, so an old binary cannot clobber the file.
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\ndescription: d\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, code = runMdmInDir(t, dir, env, "skills", "add", "./my-skill", "-p", "-y", "-a", "claude-code")
	if code == 0 {
		t.Fatal("expected skills add to abort instead of overwriting a newer lock")
	}
	after, err := os.ReadFile(filepath.Join(dir, "skills-lock.json"))
	if err != nil || string(after) != lockContent {
		t.Errorf("newer lock file must be left untouched, got: %s (err=%v)", after, err)
	}
}

func TestCorruptLockAborts(t *testing.T) {
	dir := t.TempDir()
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skills-lock.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, isolatedEnv(dir, stateDir), "skills", "install", "-y")
	if code == 0 {
		t.Fatalf("expected non-zero exit for a corrupt lock, got 0:\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "could not be parsed") {
		t.Errorf("expected a parse error pointer, got stdout=%q stderr=%q", stdout, stderr)
	}
}
