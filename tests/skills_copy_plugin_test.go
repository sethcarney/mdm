package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLocalSkill lays out a one-skill source directory and returns its path.
func writeLocalSkill(t *testing.T, root, dir, name, body string) string {
	t.Helper()
	skillDir := filepath.Join(root, dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: d\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, dir)
}

// S1: copy mode must materialize the canonical directory too, so doctor - which
// checks the canonical path - does not report the skill missing on disk.
func TestSkillsCopyModeWritesCanonicalDirectory(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writeLocalSkill(t, dir, "src", "myskill", "hi")

	_, stderr, code := runMdmInDir(t, dir, env, "skills", "add", src, "--harness", "claude-code", "--copy", "--project", "-y")
	if code != 0 {
		t.Fatalf("copy add exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "myskill", "SKILL.md")); err != nil {
		t.Errorf("copy mode should write the canonical directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills", "myskill", "SKILL.md")); err != nil {
		t.Errorf("copy mode should also write the harness copy: %v", err)
	}
	// doctor checks the canonical path; with it present the run is clean.
	_, _, dcode := runMdmInDir(t, dir, env, "doctor")
	if dcode != 0 {
		t.Errorf("doctor should exit 0 after a copy-mode install, got %d", dcode)
	}
}

// S2/S3: an add refused by every harness (a plugin owns the name) must report no
// success, exit non-zero, write no orphan lock entry, and - in copy mode - not
// smuggle the impostor's bytes into the harness directory.
func TestSkillsAddRefusedEverywhereFailsAndWritesNoLockEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		copy bool
	}{{"symlink", false}, {"copy", true}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			env := freshEnv(t)

			if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "init", "myplugin"); code != 0 {
				t.Fatalf("plugins init: %d %s", code, stderr)
			}
			if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./myplugin", "--harness", "claude-code", "-y"); code != 0 {
				t.Fatalf("plugins add: %d %s", code, stderr)
			}

			// An impostor standalone skill sharing the plugin skill's name.
			imp := writeLocalSkill(t, dir, "imp", "example-skill", "IMPOSTOR")
			args := []string{"skills", "add", imp, "--harness", "claude-code", "--project", "-y"}
			if tc.copy {
				args = append(args, "--copy")
			}
			stdout, stderr, code := runMdmInDir(t, dir, env, args...)
			if code == 0 {
				t.Fatalf("add refused for every harness should exit non-zero:\n%s%s", stdout, stderr)
			}

			// No orphan skills entry in the lock.
			data, err := os.ReadFile(filepath.Join(dir, lockName))
			if err != nil {
				t.Fatal(err)
			}
			var lk struct {
				Skills map[string]json.RawMessage `json:"skills"`
			}
			if err := json.Unmarshal(data, &lk); err != nil {
				t.Fatal(err)
			}
			if _, ok := lk.Skills["example-skill"]; ok {
				t.Errorf("a refused install must not write a skills lock entry:\n%s", data)
			}

			// In copy mode the impostor bytes must not reach the harness dir.
			got, _ := os.ReadFile(filepath.Join(dir, ".claude", "skills", "example-skill", "SKILL.md"))
			if strings.Contains(string(got), "IMPOSTOR") {
				t.Errorf("impostor bytes leaked into the harness directory:\n%s", got)
			}
		})
	}
}
