// The `--json` output contract for `mdm plugins list`, `mdm knowledge list`
// and `mdm agents list`.
//
// These drive the built binary rather than the commands package because the
// contract is about what reaches a caller's stdout: an array at the top level,
// `[]` and exit 0 when nothing is installed, no ANSI escapes, and no human
// text mixed in. A unit test on the item structs asserts none of that.
package tests_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// decodeJSONList fails the test unless stdout is exactly one JSON array, with
// no ANSI escapes, and returns it decoded into v.
func decodeJSONList(t *testing.T, stdout string, v any) {
	t.Helper()
	if strings.Contains(stdout, "\x1b[") {
		t.Errorf("--json output must carry no ANSI escapes, got: %q", stdout)
	}
	trimmed := strings.TrimSpace(stdout)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("--json output must be a top-level array, got: %q", stdout)
	}
	if err := json.Unmarshal([]byte(trimmed), v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
}

// assertEmptyJSONList checks the case the human path returns early on: with
// nothing installed a caller still needs `[]` and exit 0, so that "nothing
// installed" is distinguishable from "the command failed".
func assertEmptyJSONList(t *testing.T, args ...string) {
	t.Helper()
	dir := t.TempDir()
	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), args...)
	if code != 0 {
		t.Fatalf("mdm %s exited %d: %s%s", strings.Join(args, " "), code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("mdm %s with nothing installed must print [], got: %q", strings.Join(args, " "), stdout)
	}
}

func TestPluginsListJSON(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha", "beta")
	mustRunPlugins(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "--harness", "claude-code", "-y")

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "list", "--json")
	if code != 0 {
		t.Fatalf("plugins list --json exited %d: %s", code, stderr)
	}

	var items []struct {
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Source      string   `json:"source"`
		SpecVersion string   `json:"specVersion"`
		InstallDir  string   `json:"installDir"`
		Skills      []string `json:"skills"`
		Harnesses   []string `json:"harnesses"`
		MCPServers  int      `json:"mcpServers"`
		Valid       bool     `json:"valid"`
	}
	decodeJSONList(t, stdout, &items)

	if len(items) != 1 {
		t.Fatalf("expected one plugin, got %+v", items)
	}
	got := items[0]
	if got.Name != "toolkit" || got.Version != "1.0.0" || got.SpecVersion != "1.0.0" {
		t.Errorf("unexpected identity fields: %+v", got)
	}
	if got.InstallDir != ".agents/plugins/toolkit" {
		t.Errorf("installDir should be slash-separated and relative, got %q", got.InstallDir)
	}
	if strings.Join(got.Skills, ",") != "alpha,beta" {
		t.Errorf("expected both skills, got %v", got.Skills)
	}
	if strings.Join(got.Harnesses, ",") != "claude-code" {
		t.Errorf("harnesses should be the canonical names the skills were installed for, got %v", got.Harnesses)
	}
	if got.MCPServers != 0 {
		t.Errorf("the fixture wires no MCP servers, got %d", got.MCPServers)
	}
	if !got.Valid {
		t.Error("a freshly installed plugin should load off the disk")
	}
}

func TestPluginsListJSONEmpty(t *testing.T) {
	assertEmptyJSONList(t, "plugins", "list", "--json")
}

func TestKnowledgeListJSON(t *testing.T) {
	project := t.TempDir()
	writeSourceBundle(t, filepath.Join(project, "src-bundle"))
	env := freshEnv(t)

	if stdout, stderr, code := runMdmInDir(t, project, env, "knowledge", "add", "./src-bundle", "-y"); code != 0 {
		t.Fatalf("knowledge add exited %d:\n%s%s", code, stdout, stderr)
	}

	stdout, stderr, code := runMdmInDir(t, project, env, "knowledge", "list", "--json")
	if code != 0 {
		t.Fatalf("knowledge list --json exited %d: %s", code, stderr)
	}

	var items []struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		SpecVersion string `json:"specVersion"`
		InstallDir  string `json:"installDir"`
		Documents   int    `json:"documents"`
		Present     bool   `json:"present"`
	}
	decodeJSONList(t, stdout, &items)

	if len(items) != 1 {
		t.Fatalf("expected one bundle, got %+v", items)
	}
	got := items[0]
	if got.Name != "src-bundle" || got.SpecVersion == "" {
		t.Errorf("unexpected identity fields: %+v", got)
	}
	if got.InstallDir != "knowledge/src-bundle" {
		t.Errorf("installDir should be slash-separated and relative, got %q", got.InstallDir)
	}
	// The same count the text output prints as "2 document(s)".
	if got.Documents != 2 {
		t.Errorf("expected 2 documents, got %d", got.Documents)
	}
	if !got.Present {
		t.Error("a freshly installed bundle should load off the disk")
	}
}

func TestKnowledgeListJSONEmpty(t *testing.T) {
	assertEmptyJSONList(t, "knowledge", "list", "--json")
}

func TestAgentsListJSON(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)
	src := writeAgentSource(t, "code-reviewer", "code-reviewer")

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("agents add exited %d:\n%s%s", code, stdout, stderr)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "list", "--json")
	if code != 0 {
		t.Fatalf("agents list --json exited %d: %s", code, stderr)
	}

	var items []struct {
		Name             string   `json:"name"`
		Scope            string   `json:"scope"`
		Source           string   `json:"source"`
		CanonicalMissing bool     `json:"canonicalMissing"`
		InstalledIn      []string `json:"installedIn"`
		MissingFrom      []string `json:"missingFrom"`
	}
	decodeJSONList(t, stdout, &items)

	if len(items) != 1 {
		t.Fatalf("expected one agent definition, got %+v", items)
	}
	got := items[0]
	if got.Name != "code-reviewer" {
		t.Errorf("expected the sanitized lock key as the name, got %q", got.Name)
	}
	// Scope is a field rather than a grouping wrapper, matching skills list.
	if got.Scope != "project" {
		t.Errorf("expected project scope, got %q", got.Scope)
	}
	if got.CanonicalMissing {
		t.Error("the canonical file should be on disk after an add")
	}
	// Canonical harness names, not the "Claude Code" display name the text
	// output prints: the canonical name is the stable identifier.
	if strings.Join(got.InstalledIn, ",") != "claude-code" {
		t.Errorf("expected the canonical harness name, got %v", got.InstalledIn)
	}
	// An absent list is [], never null - a caller should not have to
	// special-case the two.
	if got.MissingFrom == nil {
		t.Error("missingFrom should marshal as [] rather than null")
	}
}

func TestAgentsListJSONEmpty(t *testing.T) {
	assertEmptyJSONList(t, "agents", "list", "--json")
}
