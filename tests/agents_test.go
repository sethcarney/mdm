package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the built binary rather than the commands package,
// because every defect they cover was reachable only through the real
// command wiring: a scan that is never called, a name that diverges between
// two writers, a summary line and an exit code. A unit test on the helper
// each of those uses passes whether or not the command calls it.

// writeAgentSource lays out a source tree holding one agent definition and
// returns its path.
func writeAgentSource(t *testing.T, name, frontmatterName string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + frontmatterName + "\ndescription: a test agent definition\n---\n\nYou are " + frontmatterName + ".\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func hiddenAgentFixturePath(t *testing.T) string {
	t.Helper()
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("finding module root: %v", err)
	}
	return filepath.Join(root, "tests", "testdata", "hidden-agent")
}

// A definition's frontmatter name is third-party text and is routinely not a
// legal file name. It has to be sanitized once, because it becomes BOTH the
// file name and the lock key: when the two disagreed, `mdm agents remove`
// dropped the lock entry, found no file at the name it recorded, reported
// success, and left the definition live in the harness with nothing left to
// find it by. This test installs "Code Reviewer" and then removes it, which
// is the only sequence that catches the divergence end to end.
func TestAgentsAddSanitizesTheNameOnDiskAndInTheLock(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "code-reviewer", "Code Reviewer")

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents add exited %d:\n%s%s", code, stdout, stderr)
	}

	canonical := filepath.Join(projectDir, ".agents", "agents", "code-reviewer.md")
	harnessFile := filepath.Join(projectDir, ".claude", "agents", "code-reviewer.md")
	for _, p := range []string{canonical, harnessFile} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("expected %s to exist, stat err=%v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".agents", "agents", "Code Reviewer.md")); err == nil {
		t.Error("the raw frontmatter name was used as a file name")
	}

	lockData, err := os.ReadFile(filepath.Join(projectDir, lockName))
	if err != nil {
		t.Fatalf("reading the lock: %v", err)
	}
	if !strings.Contains(string(lockData), `"code-reviewer"`) {
		t.Fatalf("lock does not record the sanitized name:\n%s", lockData)
	}

	// The name in the lock must find the files on disk.
	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "remove", "code-reviewer", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}
	for _, p := range []string{canonical, harnessFile} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("remove reported success but %s survives (stat err=%v)", p, err)
		}
	}
}

// An agent definition is third-party markdown installed specifically to
// become a persona the model adopts, so it gets the same pre-install scan
// every other install path in mdm runs — and the same escape hatch.
func TestAgentsAddBlocksHiddenMarkdownCharacters(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", hiddenAgentFixturePath(t), "--harness", "claude-code", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected the hidden character scan to block the install, got code 0:\n%s", combined)
	}
	if !strings.Contains(combined, "Hidden character scan failed") || !strings.Contains(combined, "zero-width") {
		t.Fatalf("expected a hidden character finding in the output, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".agents", "agents", "hidden-agent.md")); !os.IsNotExist(err) {
		t.Errorf("expected nothing to be written, stat err=%v", err)
	}
}

func TestAgentsAddAllowsHiddenMarkdownCharactersWithFlag(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", hiddenAgentFixturePath(t), "--harness", "claude-code", "--project", "-y", "--allow-hidden-chars")
	combined := stdout + stderr
	if code != 0 {
		t.Fatalf("expected --allow-hidden-chars to let the install through, got code %d:\n%s", code, combined)
	}
	if !strings.Contains(combined, "Continuing because --allow-hidden-chars was provided") {
		t.Fatalf("expected the allow notice in the output, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".agents", "agents", "hidden-agent.md")); err != nil {
		t.Errorf("expected the definition to be installed, stat err=%v", err)
	}
}

// `mdm agents add .` is one keystroke from the documented `mdm agents add
// ./my-agents`, and .agents/agents is itself a conventional agents
// directory, so discovery finds mdm's own canonical copies. Copying one onto
// itself used to empty it — copyFile opens the destination O_TRUNC before
// reading the source — and print a checkmark over a 0-byte file with a
// harness symlink pointing at nothing.
func TestAgentsAddOnTheProjectItselfLeavesTheDefinitionIntact(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")

	env := isolatedEnv(projectDir, stateDir)
	if _, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup install exited %d: %s", code, stderr)
	}

	canonical := filepath.Join(projectDir, ".agents", "agents", "critic.md")
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("setup produced an empty canonical file")
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", ".", "--harness", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents add . exited %d:\n%s%s", code, stdout, stderr)
	}

	after, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatalf("reading the canonical file back: %v", err)
	}
	if len(after) == 0 {
		t.Fatal("the canonical file was truncated to zero bytes by re-adding the project itself")
	}
	if string(after) != string(before) {
		t.Errorf("the canonical file changed:\nbefore: %q\nafter:  %q", before, after)
	}
	// The harness's own file has to still resolve to real content.
	harnessContent, err := os.ReadFile(filepath.Join(projectDir, ".claude", "agents", "critic.md"))
	if err != nil {
		t.Fatalf("reading the harness file: %v", err)
	}
	if len(harnessContent) == 0 {
		t.Error("the harness file resolves to nothing")
	}
}

// A harness with no agent concept is a skip, not a failure — but a run that
// installed nothing anywhere is still a failed run. Printing "✓ Installed 1
// agent definition" and exiting 0 after writing no files and no lock entry
// tells a CI script the opposite of what happened.
func TestAgentsAddSkipsAHarnessWithNoAgentConceptAndFailsWhenNothingLands(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "codex", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected a non-zero exit when nothing was installed, got 0:\n%s", combined)
	}
	if strings.Contains(combined, "Installed 1 agent definition") {
		t.Errorf("a success line was printed for an install that wrote nothing:\n%s", combined)
	}
	if !strings.Contains(combined, "skipped") || !strings.Contains(combined, "no agent concept") {
		t.Errorf("expected a skip with its reason, got:\n%s", combined)
	}
	if strings.Contains(combined, "failed for") {
		t.Errorf("a skip was reported as a failure:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, lockName)); !os.IsNotExist(err) {
		t.Errorf("a lock file was written for an install that wrote nothing, stat err=%v", err)
	}

	// The same definition aimed at a harness that DOES take one still
	// succeeds, and the summary names only the harness that received it.
	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "codex", "--harness", "claude-code", "--project", "-y")
	combined = stdout + stderr
	if code != 0 {
		t.Fatalf("mixed harness install exited %d:\n%s", code, combined)
	}
	if !strings.Contains(combined, "Installed 1 agent definition") {
		t.Errorf("expected a success line, got:\n%s", combined)
	}
	if strings.Contains(combined, "Harnesses: Codex") || strings.Contains(combined, ", Codex") {
		t.Errorf("the summary names a harness that installed nothing:\n%s", combined)
	}
}
