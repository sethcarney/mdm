package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeOwnClaudeCritic writes a hand-written critic definition where Claude
// Code reads it, as a user who never ran mdm for that harness would.
func writeOwnClaudeCritic(t *testing.T, projectDir string) (path, content string) {
	t.Helper()
	path = filepath.Join(projectDir, ".claude", "agents", "critic.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content = "---\nname: critic\ndescription: mine, written by hand\n---\n\nMy own critic.\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, content
}

// assertUntouched fails when the file at path no longer holds content.
func assertUntouched(t *testing.T, step, path, content string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Errorf("%s touched the hand-written Claude Code definition (err=%v):\n%s", step, err, got)
	}
}

// setupCursorOnlyCritic lays out a hand-written Claude Code critic and
// installs a critic from ./src into Cursor only. It returns the isolated
// environment and the hand-written file's path and content.
func setupCursorOnlyCritic(t *testing.T, projectDir, stateDir string) (env []string, own, original string) {
	t.Helper()
	env = isolatedEnv(projectDir, stateDir)
	own, original = writeOwnClaudeCritic(t, projectDir)
	srcDir := filepath.Join(projectDir, "src", "agents")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	upstream := "---\nname: critic\ndescription: upstream v1\n---\nbody v1\n"
	if err := os.WriteFile(filepath.Join(srcDir, "critic.md"), []byte(upstream), 0o600); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", "./src", "--harness", "cursor", "--project", "-y"); code != 0 {
		t.Fatalf("mdm agents add exited %d:\n%s%s", code, stdout, stderr)
	}
	return env, own, original
}

// recordedHarnesses reads the harness list critic's lock entry records.
func recordedHarnesses(t *testing.T, projectDir string) []string {
	t.Helper()
	lockData, err := os.ReadFile(filepath.Join(projectDir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Agents map[string]struct {
			Harnesses []string `json:"harnesses"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(lockData, &doc); err != nil {
		t.Fatalf("lock is not JSON: %v\n%s", err, lockData)
	}
	return doc.Agents["critic"].Harnesses
}

// The lock recorded no harness list, so remove, update, install and list
// inferred one by stat-ing <harnessDir>/<name> for every harness. A
// hand-written .claude/agents/critic.md therefore counted as an install of a
// definition added with --harness cursor: `agents list` claimed Claude Code,
// `agents remove` deleted the user's file, `agents update` overwrote it with
// upstream content, and `agents install` restored into every capable harness,
// the committed .github/agents included. The list is recorded now, and every
// command follows it. (Update is exercised in commands/agents_held_test.go:
// planUpdates never re-fetches a local source, and only a local source can
// be edited by a test.)
func TestAgentsCommandsFollowTheRecordedHarnessList(t *testing.T) {
	projectDir := t.TempDir()
	env, own, original := setupCursorOnlyCritic(t, projectDir, t.TempDir())
	if got := strings.Join(recordedHarnesses(t, projectDir), ","); got != "cursor" {
		t.Fatalf("lock records harnesses %q, want cursor", got)
	}
	cursorFile := filepath.Join(projectDir, ".cursor", "agents", "critic.md")

	// list: Cursor holds it; the hand-written Claude Code file is not an install.
	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "list", "--project")
	if code != 0 {
		t.Fatalf("mdm agents list exited %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Cursor") || strings.Contains(stdout, "Claude Code") {
		t.Errorf("list should name Cursor and not Claude Code:\n%s", stdout)
	}

	// install: restores Cursor's copy, and only Cursor's.
	if err := os.Remove(cursorFile); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "install", "-y"); code != 0 {
		t.Fatalf("mdm agents install exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Lstat(cursorFile); err != nil {
		t.Errorf("install did not restore Cursor's copy: %v", err)
	}
	for _, p := range []string{
		filepath.Join(projectDir, ".github", "agents", "critic.agent.md"),
		filepath.Join(projectDir, ".codex", "agents", "critic.toml"),
	} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("install wrote into a harness the lock never named: %s (stat err=%v)", p, err)
		}
	}
	assertUntouched(t, "install", own, original)

	// remove: Cursor's copy goes; the user's file stays.
	stdout, stderr, code = runMdmInDir(t, projectDir, env, "agents", "remove", "critic", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Lstat(cursorFile); !os.IsNotExist(err) {
		t.Errorf("Cursor's copy survived the removal (stat err=%v)", err)
	}
	assertUntouched(t, "remove", own, original)
	if data, _ := os.ReadFile(filepath.Join(projectDir, lockName)); strings.Contains(string(data), `"critic"`) {
		t.Errorf("lock still records critic:\n%s", data)
	}
}

// dropRecordedHarnesses rewrites the lock as an earlier v2 build would have
// written it: the same entry with no harness list.
func dropRecordedHarnesses(t *testing.T, projectDir string) {
	t.Helper()
	lockPath := filepath.Join(projectDir, lockName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc["agents"].(map[string]any)["critic"].(map[string]any)
	delete(entry, "harnesses")
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// An entry written by an earlier v2 build has no harness list. The commands
// fall back to inferring one, but only from files mdm can show it wrote: the
// hand-written Claude Code file's bytes are neither the canonical bytes nor
// what mdm would have encoded for that harness, so it is left alone.
func TestAgentsRemoveWithoutARecordedListLeavesAHandWrittenFileAlone(t *testing.T) {
	projectDir := t.TempDir()
	env, own, original := setupCursorOnlyCritic(t, projectDir, t.TempDir())
	dropRecordedHarnesses(t, projectDir)

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "list", "--project")
	if code != 0 {
		t.Fatalf("mdm agents list exited %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Cursor") || strings.Contains(stdout, "Claude Code") {
		t.Errorf("list should infer Cursor only:\n%s", stdout)
	}

	stdout, stderr, code = runMdmInDir(t, projectDir, env, "agents", "remove", "critic", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".cursor", "agents", "critic.md")); !os.IsNotExist(err) {
		t.Errorf("Cursor's copy survived the removal (stat err=%v)", err)
	}
	assertUntouched(t, "remove", own, original)
}
