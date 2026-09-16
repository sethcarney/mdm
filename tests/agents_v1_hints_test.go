package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `mdm agents remove <name> -y` for a name the lock does not hold printed
// "No matching agent definitions found." and exited 0, so a script read a
// removal that never happened as done. An explicit name that matches nothing
// is a failed removal; only "*" and an empty selection may find nothing.
func TestAgentsRemoveExitsNonZeroWhenAnExplicitNameMatchesNothing(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")
	env := isolatedEnv(projectDir, stateDir)

	// With no lock at all.
	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "ghost", "--project", "-y")
	if code == 0 {
		t.Errorf("exit code = 0 for a name that matches nothing in an empty scope:\n%s%s", stdout, stderr)
	}

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup: agents add exited %d:\n%s%s", code, stdout, stderr)
	}

	// With a lock that holds something else.
	stdout, stderr, code = runMdmInDir(t, projectDir, env, "agents", "remove", "ghost", "--project", "-y")
	if code == 0 {
		t.Errorf("exit code = 0 for a name that matches no installed definition:\n%s%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "No matching agent definitions found") {
		t.Errorf("expected the no-match message, got:\n%s%s", stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "critic.md")); err != nil {
		t.Errorf("a failed removal disturbed the installed definition: %v", err)
	}

	// "*" asks for whatever is there and may legitimately find nothing.
	if _, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "*", "--project", "-y"); code != 0 {
		t.Errorf("remove * exited %d: %s", code, stderr)
	}
	if _, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "*", "--project", "-y"); code != 0 {
		t.Errorf("remove * on an empty scope exited %d: %s", code, stderr)
	}
}

// `mdm agents remove cursor` dropped a harness before this release. It is
// now a lock-key lookup that finds nothing, and the exit alone cannot say
// why. `mdm agents add cursor` already explains where harness management
// went; remove says the same and exits 1.
func TestAgentsRemoveHintsWhenTheNameIsAHarness(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "cursor", "--project", "-y")
	combined := stdout + stderr
	if code != 1 {
		t.Errorf("exit code = %d, want 1:\n%s", code, combined)
	}
	for _, want := range []string{"cursor is a harness", "mdm harnesses remove cursor"} {
		if !strings.Contains(combined, want) {
			t.Errorf("output missing %q:\n%s", want, combined)
		}
	}
	if strings.Contains(combined, "No matching agent definitions") {
		t.Errorf("the hint should replace the no-match message, not follow it:\n%s", combined)
	}
}

// A definition can be named after a harness. When the lock holds one, the
// name is a definition and the removal proceeds without the hint.
func TestAgentsRemoveDoesNotHintForADefinitionNamedAfterAHarness(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "cursor", "cursor")
	env := isolatedEnv(projectDir, stateDir)
	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup: agents add exited %d:\n%s%s", code, stdout, stderr)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "cursor", "--project", "-y")
	combined := stdout + stderr
	if code != 0 {
		t.Fatalf("exit code = %d, want 0:\n%s", code, combined)
	}
	if strings.Contains(combined, "is a harness") {
		t.Errorf("hinted about a harness for a definition the lock holds:\n%s", combined)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "cursor.md")); !os.IsNotExist(err) {
		t.Errorf("the definition named cursor was not removed (stat err=%v)", err)
	}
}

// `mdm agents add claude-code cursor` was the v1 form for configuring two
// harnesses. cobra answered "accepts 1 arg(s), received 2", which says nothing
// about where that went; a list made only of harness names gets the same
// hint the single-name form already prints.
func TestAgentsAddHintsWhenEveryArgIsAHarness(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "add", "claude-code", "cursor")
	combined := stdout + stderr
	if code != 1 {
		t.Errorf("exit code = %d, want 1:\n%s", code, combined)
	}
	if strings.Contains(combined, "accepts 1 arg") {
		t.Errorf("cobra's bare arity error was printed instead of the hint:\n%s", combined)
	}
	for _, want := range []string{"claude-code cursor are harnesses", "mdm harnesses add claude-code cursor"} {
		if !strings.Contains(combined, want) {
			t.Errorf("output missing %q:\n%s", want, combined)
		}
	}

	// Two arguments that are not all harness names still get the arity error.
	_, stderr, code = runMdmInDir(t, projectDir, env, "agents", "add", "owner/repo", "cursor")
	if code == 0 {
		t.Errorf("two arguments were accepted")
	}
	if !strings.Contains(stderr, "accepts 1 arg") {
		t.Errorf("expected cobra's arity error for a mixed list, got:\n%s", stderr)
	}
}
