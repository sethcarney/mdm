package tests_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/sethcarney/mdm/internal/agentfile"
)

// Exercise the real commands: new installs and restores must apply the field
// restriction, including replacement of an older generated Codex file.
func TestAgentsCodexCoreFieldsAcrossAddAndInstall(t *testing.T) {
	projectDir := t.TempDir()
	env := isolatedEnv(projectDir, t.TempDir())
	srcRoot := t.TempDir()
	src := filepath.Join(srcRoot, "critic.md")
	markdown := "---\nname: critic\ndescription: Reviews code\nmodel-tier: Frontier-1\nmodel: some-model\n---\nBe critical.\n"
	if err := os.WriteFile(src, []byte(markdown), 0o600); err != nil {
		t.Fatal(err)
	}
	codexFile := filepath.Join(projectDir, ".codex", "agents", "critic.toml")
	for _, command := range []string{"add", "install"} {
		t.Run(command, func(t *testing.T) {
			args := []string{"agents", command, "-y"}
			if command == "add" {
				args = append(args, srcRoot, "--harness", "codex", "claude-code", "--copy", "--project")
			}
			stdout, stderr, code := runMdmInDir(t, projectDir, env, args...)
			if code != 0 {
				t.Fatalf("agents %s exited %d:\n%s%s", command, code, stdout, stderr)
			}
			var got map[string]any
			if _, err := toml.DecodeFile(codexFile, &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"name": "critic", "description": "Reviews code",
				"developer_instructions": "Be critical.\n",
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Codex fields = %#v, want %#v", got, want)
			}
			for _, path := range []string{src,
				filepath.Join(projectDir, ".agents", "agents", "critic.md"),
				filepath.Join(projectDir, ".claude", "agents", "critic.md"),
			} {
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(content) != markdown {
					t.Errorf("Markdown at %s was modified: %s", path, content)
				}
			}
		})
		if t.Failed() {
			return
		}
		if command == "add" {
			// Recreate output from the old converter, before restore runs.
			a, err := agentfile.ParseAgentFile(src)
			if err != nil || a == nil {
				t.Fatalf("parsing source: agent=%v error=%v", a, err)
			}
			legacy, err := agentfile.Encode(a, agentfile.FormatTOML)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(codexFile, legacy, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
