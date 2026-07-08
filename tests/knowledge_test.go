package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// okfFixturePath returns the path to a fixture bundle in internal/okf/testdata.
func okfFixturePath(t *testing.T, name string) string {
	t.Helper()
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("finding module root: %v", err)
	}
	return filepath.Join(root, "internal", "okf", "testdata", name)
}

func TestKnowledgeInitValidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")

	_, stderr, code := runMdmInDir(t, dir, env, "knowledge", "init", "my-bundle")
	if code != 0 {
		t.Fatalf("knowledge init exited %d: %s", code, stderr)
	}

	stdout, stderr, code := runMdmInDir(t, dir, env, "knowledge", "validate", "my-bundle")
	if code != 0 {
		t.Fatalf("scaffolded bundle should validate cleanly, exited %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "no issues") {
		t.Errorf("expected 'no issues' in validate output, got: %q", stdout)
	}
}

func TestKnowledgeValidateFailsOnBrokenBundle(t *testing.T) {
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")
	stdout, stderr, code := runMdmInDir(t, t.TempDir(), env, "knowledge", "validate", okfFixturePath(t, "broken-link"))
	if code == 0 {
		t.Fatal("expected non-zero exit for bundle with broken links")
	}
	if !strings.Contains(stdout+stderr, "broken-link") {
		t.Errorf("expected broken-link rule in output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestKnowledgeValidateJSON(t *testing.T) {
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")
	stdout, stderr, code := runMdmInDir(t, t.TempDir(), env, "knowledge", "validate", "--json", okfFixturePath(t, "valid-bundle"))
	if code != 0 {
		t.Fatalf("validate --json exited %d: %s", code, stderr)
	}
	var report struct {
		Documents int `json:"documents"`
		Errors    int `json:"errors"`
		Issues    []struct {
			Rule string `json:"rule"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	if report.Documents != 3 || report.Errors != 0 || len(report.Issues) != 0 {
		t.Errorf("unexpected report: %+v", report)
	}
}

// writeSourceBundle creates a small valid OKF bundle to install from.
func writeSourceBundle(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "concepts"), 0755); err != nil {
		t.Fatal(err)
	}
	index := "---\ntitle: Fixture\n---\n\n- [Thing](concepts/thing.md)\n"
	concept := "---\ntype: Concept\ntitle: Thing\n---\n\nA thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(index), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "concepts", "thing.md"), []byte(concept), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeAddListRemoveLocal(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "src-bundle")
	writeSourceBundle(t, src)
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")

	// add
	stdout, stderr, code := runMdmInDir(t, project, env, "knowledge", "add", "./src-bundle", "-y")
	if code != 0 {
		t.Fatalf("knowledge add exited %d:\n%s%s", code, stdout, stderr)
	}
	installed := filepath.Join(project, "knowledge", "src-bundle", "index.md")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("expected installed bundle at %s: %v", installed, err)
	}
	lockPath := filepath.Join(project, "knowledge-lock.json")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("expected knowledge-lock.json: %v", err)
	}
	for _, want := range []string{"src-bundle", "specVersion", "contentHash", "knowledge/src-bundle"} {
		if !strings.Contains(string(lockData), want) {
			t.Errorf("expected %q in knowledge-lock.json, got:\n%s", want, lockData)
		}
	}

	// list
	stdout, stderr, code = runMdmInDir(t, project, env, "knowledge", "list")
	if code != 0 {
		t.Fatalf("knowledge list exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "src-bundle") || !strings.Contains(stdout, "2 document(s)") {
		t.Errorf("unexpected list output: %q", stdout)
	}

	// remove
	_, stderr, code = runMdmInDir(t, project, env, "knowledge", "remove", "src-bundle", "-y")
	if code != 0 {
		t.Fatalf("knowledge remove exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "knowledge", "src-bundle")); !os.IsNotExist(err) {
		t.Error("expected installed bundle directory to be removed")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected knowledge-lock.json to be removed with the last bundle")
	}
}

func TestKnowledgeAddDryRunWritesNothing(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "src-bundle")
	writeSourceBundle(t, src)
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")

	stdout, stderr, code := runMdmInDir(t, project, env, "knowledge", "add", "./src-bundle", "-y", "--dry-run")
	if code != 0 {
		t.Fatalf("knowledge add --dry-run exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "knowledge")); !os.IsNotExist(err) {
		t.Error("dry run must not create the knowledge directory")
	}
	if _, err := os.Stat(filepath.Join(project, "knowledge-lock.json")); !os.IsNotExist(err) {
		t.Error("dry run must not write knowledge-lock.json")
	}
}

func TestKnowledgeAddBlocksHiddenChars(t *testing.T) {
	project := t.TempDir()
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")
	root, err := findModRoot()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "tests", "testdata", "hidden-knowledge")

	stdout, stderr, code := runMdmInDir(t, project, env, "knowledge", "add", fixture, "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected hidden character scan to block install:\n%s", combined)
	}
	if !strings.Contains(combined, "Hidden character") {
		t.Errorf("expected hidden character finding, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(project, "knowledge-lock.json")); !os.IsNotExist(err) {
		t.Error("blocked install must not write knowledge-lock.json")
	}
}

func TestKnowledgeLockSurvivesSkillsOperations(t *testing.T) {
	project := t.TempDir()
	src := filepath.Join(project, "src-bundle")
	writeSourceBundle(t, src)
	env := freshEnv(t, "MDM_EXPERIMENTAL=knowledge")

	if _, stderr, code := runMdmInDir(t, project, env, "knowledge", "add", "./src-bundle", "-y"); code != 0 {
		t.Fatalf("knowledge add exited %d: %s", code, stderr)
	}
	lockPath := filepath.Join(project, "knowledge-lock.json")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	// A skills operation that rewrites skills-lock.json must leave the
	// knowledge lock byte-identical.
	skillDir := filepath.Join(project, "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillMd := "---\nname: my-skill\ndescription: fixture skill\n---\n\n# my-skill\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMd), 0644); err != nil {
		t.Fatal(err)
	}
	if stdout, stderr, code := runMdmInDir(t, project, env, "skills", "add", "./my-skill", "-p", "-y", "-a", "claude-code"); code != 0 {
		t.Fatalf("skills add exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(project, "skills-lock.json")); err != nil {
		t.Fatalf("expected skills add to write skills-lock.json: %v", err)
	}

	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("knowledge-lock.json changed after a skills operation:\nbefore: %s\nafter: %s", before, after)
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
