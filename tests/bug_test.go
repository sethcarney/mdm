package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sethcarney/mdm/commands"
)

// The issue-form field ids `mdm bug` prefills must exist in the template
// it targets, or the prefill silently drops those values on GitHub's side.
func TestBugTemplateFieldsMatch(t *testing.T) {
	root, err := findModRoot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "ISSUE_TEMPLATE", commands.BugTemplateName))
	if err != nil {
		t.Fatalf("template %s missing: %v", commands.BugTemplateName, err)
	}
	var form struct {
		Body []struct {
			Type string `yaml:"type"`
			ID   string `yaml:"id"`
		} `yaml:"body"`
	}
	if err := yaml.Unmarshal(data, &form); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, f := range form.Body {
		if f.ID != "" {
			ids[f.ID] = true
		}
	}
	for _, id := range commands.BugFieldIDs {
		if !ids[id] {
			t.Errorf("mdm bug prefills field %q, but %s has no field with that id", id, commands.BugTemplateName)
		}
	}
}

func TestBugPrint(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	env := isolatedEnv(home, t.TempDir())

	stdout, stderr, code := runMdmInDir(t, dir, env, "bug", "--print", "--command", "mdm skills add "+home+"/repo")
	if code != 0 {
		t.Fatalf("mdm bug --print exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "issues/new?") || !strings.Contains(stdout, "template="+commands.BugTemplateName) {
		t.Errorf("expected a prefill URL, got: %q", stdout)
	}
	for _, want := range []string{"version=", "os="} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in URL, got: %q", want, stdout)
		}
	}
	// $HOME must be scrubbed to ~ everywhere.
	if strings.Contains(stdout, home) {
		t.Errorf("home directory leaked into the report: %q", stdout)
	}
	// --print must not try to open anything; output is reviewable text.
	if !strings.Contains(stdout, "nothing has been sent") {
		t.Errorf("expected reviewable body header, got: %q", stdout)
	}
}
