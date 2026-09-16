package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `mdm agents add .` on a project holding a hand-written
// .claude/agents/critic.md adopts it and writes Codex's TOML and Copilot's
// copy beside it. `mdm agents remove critic` then found every one of those
// inside the local source - the project root - and kept them all, so the
// generated Codex and Copilot files stayed live with no lock entry naming
// them. Only the file discovery found is the user's.
func TestAgentsRemoveAfterAddDotDeletesWhatMdmWroteBesideTheSource(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)
	own, original := writeOwnClaudeCritic(t, projectDir)

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", ".", "--harness", "claude-code", "codex", "github-copilot", "--project", "-y"); code != 0 {
		t.Fatalf("mdm agents add . exited %d:\n%s%s", code, stdout, stderr)
	}
	codexFile := filepath.Join(projectDir, ".codex", "agents", "critic.toml")
	copilotFile := filepath.Join(projectDir, ".github", "agents", "critic.agent.md")
	canonical := filepath.Join(projectDir, ".agents", "agents", "critic.md")
	for _, p := range []string{codexFile, copilotFile, canonical} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("setup: %s was not written: %v", p, err)
		}
	}
	lockData, err := os.ReadFile(filepath.Join(projectDir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lockData), `"agentPath": ".claude/agents/critic.md"`) {
		t.Errorf("lock does not record which file was the source:\n%s", lockData)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "critic", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}
	for _, p := range []string{codexFile, copilotFile, canonical} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived the removal; mdm wrote it and must delete it (stat err=%v)", p, err)
		}
	}
	fi, err := os.Lstat(own)
	if err != nil {
		t.Fatalf("the user's own definition is gone after remove: %v\n%s%s", err, stdout, stderr)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("the user's definition is still a symlink after remove")
	}
	if got, _ := os.ReadFile(own); string(got) != original {
		t.Errorf("the user's definition changed:\n%s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(projectDir, lockName)); strings.Contains(string(data), `"critic"`) {
		t.Errorf("lock still records critic:\n%s", data)
	}
}
