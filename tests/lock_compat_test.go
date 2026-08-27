package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A lock file written by a newer mdm must abort the command with a clear
// error, never read as empty — the v1 line's empty fallback made
// `mdm skills install` in a newer-format project a silent no-op that
// exits 0 in CI.
func TestNewerLockVersionAborts(t *testing.T) {
	dir := t.TempDir()
	lockContent := `{"version": 99, "skills": {}}`
	if err := os.WriteFile(filepath.Join(dir, lockName), []byte(lockContent), 0600); err != nil {
		t.Fatal(err)
	}

	env := freshEnv(t)
	stdout, stderr, code := runMdmInDir(t, dir, env, "skills", "install", "-y")
	if code == 0 {
		t.Fatalf("expected non-zero exit for a newer lock version, got 0:\n%s%s", stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "newer version of mdm") || !strings.Contains(combined, "mdm upgrade") {
		t.Errorf("expected an upgrade pointer, got stdout=%q stderr=%q", stdout, stderr)
	}

	// Write paths must abort too, so this binary cannot clobber the file.
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
	after, err := os.ReadFile(filepath.Join(dir, lockName))
	if err != nil || string(after) != lockContent {
		t.Errorf("newer lock file must be left untouched, got: %s (err=%v)", after, err)
	}
}

func TestCorruptLockAborts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lockName), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "skills", "install", "-y")
	if code == 0 {
		t.Fatalf("expected non-zero exit for a corrupt lock, got 0:\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "could not be parsed") {
		t.Errorf("expected a parse error pointer, got stdout=%q stderr=%q", stdout, stderr)
	}
}

// The migration tombstone carries a deliberately newer version so patched
// v1 binaries refuse it — but v2 itself must keep treating it as an empty
// legacy file, not abort on it.
func TestTombstoneDoesNotAbortV2(t *testing.T) {
	dir := t.TempDir()
	tomb := `{"version": 2, "skills": {}, "_moved": "` + lockName + `"}`
	if err := os.WriteFile(filepath.Join(dir, "skills-lock.json"), []byte(tomb), 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "skills", "install", "-y")
	if code != 0 {
		t.Fatalf("v2 must tolerate its own tombstone, exited %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "No "+lockName+" found") {
		t.Errorf("tombstone-only project should read as empty, got: %q", stdout)
	}
}
