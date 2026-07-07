package tests_test

import (
	"strings"
	"testing"
)

// freshEnv builds an isolated environment with a throwaway HOME and
// XDG_STATE_HOME so tests never read or write the developer's real global
// lock file. Extra entries are appended as-is.
func freshEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	return append(isolatedEnv(t.TempDir(), t.TempDir()), extra...)
}

func TestKnowledgeHiddenWhenGateOff(t *testing.T) {
	dir := t.TempDir()
	stdout, _, code := runMdmInDir(t, dir, freshEnv(t), "--help")
	if code != 0 {
		t.Fatalf("mdm --help exited %d", code)
	}
	if strings.Contains(stdout, "knowledge") {
		t.Errorf("knowledge should be hidden from --help while the experimental gate is off, got: %q", stdout)
	}
}

func TestKnowledgeRefusesWhenGateOff(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "knowledge")
	if code == 0 {
		t.Fatal("expected non-zero exit while the experimental gate is off")
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "mdm experimental enable knowledge") {
		t.Errorf("expected refusal to point at the enable command, got stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(combined, "MDM_EXPERIMENTAL") {
		t.Errorf("expected refusal to mention the env var, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestKnowledgeEnabledViaEnv(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")

	_, stderr, code := runMdmInDir(t, dir, env, "knowledge")
	if code != 0 {
		t.Fatalf("mdm knowledge exited %d with gate on: %s", code, stderr)
	}
	if !strings.Contains(stderr, "experimental") {
		t.Errorf("expected experimental banner on stderr, got: %q", stderr)
	}

	stdout, _, code := runMdmInDir(t, dir, env, "--help")
	if code != 0 {
		t.Fatalf("mdm --help exited %d", code)
	}
	if !strings.Contains(stdout, "knowledge") {
		t.Errorf("expected knowledge in --help with gate on, got: %q", stdout)
	}
}

func TestExperimentalEnableDisableRoundTrip(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	_, stderr, code := runMdmInDir(t, dir, env, "experimental", "enable", "knowledge")
	if code != 0 {
		t.Fatalf("experimental enable exited %d: %s", code, stderr)
	}

	_, stderr, code = runMdmInDir(t, dir, env, "knowledge")
	if code != 0 {
		t.Fatalf("mdm knowledge should run after enable, exited %d: %s", code, stderr)
	}

	_, stderr, code = runMdmInDir(t, dir, env, "experimental", "disable", "knowledge")
	if code != 0 {
		t.Fatalf("experimental disable exited %d: %s", code, stderr)
	}

	_, _, code = runMdmInDir(t, dir, env, "knowledge")
	if code == 0 {
		t.Fatal("expected refusal after disable")
	}
}

func TestExperimentalEnableUnknownFeature(t *testing.T) {
	dir := t.TempDir()
	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "experimental", "enable", "nonsense")
	if code == 0 {
		t.Fatal("expected non-zero exit for unknown feature")
	}
	if !strings.Contains(stdout+stderr, "unknown experimental feature") {
		t.Errorf("expected unknown-feature error, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestExperimentalList(t *testing.T) {
	dir := t.TempDir()
	stdout, _, code := runMdmInDir(t, dir, freshEnv(t), "experimental", "list")
	if code != 0 {
		t.Fatalf("experimental list exited %d", code)
	}
	if !strings.Contains(stdout, "knowledge") {
		t.Errorf("expected knowledge in experimental list, got: %q", stdout)
	}
}
