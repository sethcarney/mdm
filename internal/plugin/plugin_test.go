package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlugin(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, contents := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func manifestJSON(extra string) string {
	base := fmt.Sprintf(`"$schema": %q, "name": "my-plugin"`, ManifestSchema)
	if extra != "" {
		base += ", " + extra
	}
	return "{" + base + "}"
}

func TestValidateName(t *testing.T) {
	valid := []string{"a", "my-plugin", "a.b-c", "plugin2", "0valid", strings.Repeat("a", 64)}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{
		"", strings.Repeat("a", 65), "My-Plugin", "has space", "under_score",
		"-leading", "trailing-", ".leading", "trailing.", "double--hyphen", "double..period", "ünïcode",
	}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

func TestLoadManifestMinimal(t *testing.T) {
	root := writePlugin(t, map[string]string{ManifestFile: manifestJSON("")})
	m, issues, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("issues = %v, want none", issues)
	}
	if m.Name != "my-plugin" || m.Schema != ManifestSchema {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestLoadManifestFullMetadata(t *testing.T) {
	root := writePlugin(t, map[string]string{ManifestFile: manifestJSON(
		`"version": "1.2.3", "description": "d", "homepage": "https://example.com",
		 "repository": "https://github.com/x/y", "license": "MIT", "keywords": ["a", "b"],
		 "author": {"name": "Ada", "email": "ada@example.com", "url": "https://ada.dev"},
		 "extensions": {"com.example.client": {"anything": true}}`)})
	m, issues, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("issues = %v, want none", issues)
	}
	if m.Version != "1.2.3" || m.License != "MIT" || len(m.Keywords) != 2 {
		t.Errorf("unexpected manifest: %+v", m)
	}
	if m.Author == nil || m.Author.Name != "Ada" {
		t.Errorf("unexpected author: %+v", m.Author)
	}
	if _, ok := m.Extensions["com.example.client"]; !ok {
		t.Errorf("extensions not preserved: %+v", m.Extensions)
	}
}

func TestLoadManifestNonFatalIssues(t *testing.T) {
	// Unknown top-level fields and a non-object extensions value are
	// reported and ignored; the plugin still loads.
	root := writePlugin(t, map[string]string{ManifestFile: manifestJSON(
		`"unknownField": 1, "extensions": "not-an-object"`)})
	m, issues, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if m == nil || m.Extensions != nil {
		t.Errorf("manifest = %+v, want loaded with nil extensions", m)
	}
	rules := map[string]bool{}
	for _, issue := range issues {
		if issue.Severity != SeverityWarning {
			t.Errorf("issue %v should be a warning", issue)
		}
		rules[issue.Rule] = true
	}
	if !rules["unknown-field"] || !rules["extensions-not-object"] {
		t.Errorf("missing expected issues, got %v", issues)
	}
}

func TestLoadManifestFatal(t *testing.T) {
	cases := map[string]string{
		"missing-schema":     `{"name": "my-plugin"}`,
		"unsupported-schema": `{"$schema": "https://agent-plugins.org/schemas/9.0.0/plugin.schema.json", "name": "my-plugin"}`,
		"missing-name":       fmt.Sprintf(`{"$schema": %q}`, ManifestSchema),
		"invalid-name":       fmt.Sprintf(`{"$schema": %q, "name": "Bad Name"}`, ManifestSchema),
		"wrong-typed-field":  manifestJSON(`"description": 42`),
		"wrong-typed-array":  manifestJSON(`"keywords": "not-an-array"`),
		"author-not-object":  manifestJSON(`"author": "just a string"`),
		"author-unknown-key": manifestJSON(`"author": {"name": "x", "nickname": "y"}`),
		"not-json":           `not json at all`,
		"array-not-object":   `[1, 2]`,
	}
	for label, contents := range cases {
		root := writePlugin(t, map[string]string{ManifestFile: contents})
		if _, _, err := LoadManifest(root); err == nil {
			t.Errorf("%s: LoadManifest = nil error, want fatal", label)
		}
	}
	if _, _, err := LoadManifest(t.TempDir()); err == nil {
		t.Error("missing plugin.json: want fatal error")
	}
}

const validSkillMd = `---
name: %s
description: A test skill.
---
# Body
`

func TestListSkills(t *testing.T) {
	root := writePlugin(t, map[string]string{
		ManifestFile:                        manifestJSON(""),
		"skills/alpha/SKILL.md":             fmt.Sprintf(validSkillMd, "alpha"),
		"skills/beta/SKILL.md":              fmt.Sprintf(validSkillMd, "beta"),
		"skills/broken/SKILL.md":            "no frontmatter here",
		"skills/not-a-skill/README.md":      "just a file",
		"skills/nested/deep/child/SKILL.md": fmt.Sprintf(validSkillMd, "child"),
		"skills/stray-file.md":              "ignored",
	})
	skills, issues := ListSkills(root)
	var names []string
	for _, s := range skills {
		names = append(names, s.Name)
	}
	if len(skills) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("skills = %v, want [alpha beta] - no recursion into nested dirs", names)
	}
	if len(issues) != 1 || issues[0].Rule != "invalid-skill" {
		t.Errorf("issues = %v, want one invalid-skill warning", issues)
	}
}

func TestListSkillsMissingAndInvalidComponent(t *testing.T) {
	skills, issues := ListSkills(writePlugin(t, map[string]string{ManifestFile: manifestJSON("")}))
	if skills != nil || issues != nil {
		t.Errorf("missing skills dir should be a no-op, got %v %v", skills, issues)
	}
	root := writePlugin(t, map[string]string{ManifestFile: manifestJSON(""), "skills": "a file, not a dir"})
	skills, issues = ListSkills(root)
	if skills != nil || len(issues) != 1 || issues[0].Rule != "skills-not-directory" {
		t.Errorf("file-typed skills should disable the component, got %v %v", skills, issues)
	}
}

func TestInspectResilience(t *testing.T) {
	// A broken mcp.json disables MCP but never the plugin's skills.
	root := writePlugin(t, map[string]string{
		ManifestFile:            manifestJSON(""),
		"skills/alpha/SKILL.md": fmt.Sprintf(validSkillMd, "alpha"),
		MCPFile:                 `{"$schema": "wrong", "mcpServers": {}}`,
	})
	report := Inspect(root)
	if report.Manifest == nil || len(report.Skills) != 1 {
		t.Fatalf("skills should survive a broken mcp.json: %+v", report)
	}
	if !report.MCPDisabled || report.MCP != nil {
		t.Errorf("MCP should be disabled: %+v", report)
	}
	if !HasErrors(report.Issues) {
		t.Errorf("expected error issues, got %v", report.Issues)
	}
}

func TestInspectRejectedManifest(t *testing.T) {
	report := Inspect(writePlugin(t, map[string]string{
		ManifestFile:            `{"name": "no-schema"}`,
		"skills/alpha/SKILL.md": fmt.Sprintf(validSkillMd, "alpha"),
	}))
	if report.Manifest != nil || report.Skills != nil {
		t.Errorf("rejected plugin must not discover components: %+v", report)
	}
	if !HasErrors(report.Issues) {
		t.Errorf("expected fatal issue, got %v", report.Issues)
	}
}

func TestFindPluginRoots(t *testing.T) {
	rootWins := writePlugin(t, map[string]string{
		ManifestFile:             manifestJSON(""),
		"nested/" + ManifestFile: manifestJSON(""),
	})
	if roots := FindPluginRoots(rootWins); len(roots) != 1 || roots[0] != rootWins {
		t.Errorf("root manifest should win, got %v", roots)
	}

	mono := writePlugin(t, map[string]string{
		"plugins/one/" + ManifestFile:      manifestJSON(""),
		"plugins/two/" + ManifestFile:      manifestJSON(""),
		"plugins/too/deep/" + ManifestFile: manifestJSON(""),
		"node_modules/pkg/" + ManifestFile: manifestJSON(""),
	})
	roots := FindPluginRoots(mono)
	want := []string{filepath.Join(mono, "plugins", "one"), filepath.Join(mono, "plugins", "two")}
	if len(roots) != 2 || roots[0] != want[0] || roots[1] != want[1] {
		t.Errorf("FindPluginRoots = %v, want %v", roots, want)
	}

	if roots := FindPluginRoots(t.TempDir()); roots != nil {
		t.Errorf("empty dir should have no roots, got %v", roots)
	}
}

func TestHashPluginDirDeterministic(t *testing.T) {
	files := map[string]string{ManifestFile: manifestJSON(""), "skills/a/SKILL.md": fmt.Sprintf(validSkillMd, "a")}
	h1, err := HashPluginDir(writePlugin(t, files))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashPluginDir(writePlugin(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 || !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("hashes differ or malformed: %s vs %s", h1, h2)
	}
	files["extra.txt"] = "x"
	h3, err := HashPluginDir(writePlugin(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Error("hash should change when contents change")
	}
}
