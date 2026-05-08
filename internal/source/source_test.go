package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSourceExistingRelativePath(t *testing.T) {
	root := t.TempDir()
	localSkill := filepath.Join(root, "tests", "testdata", "hidden-skill")
	if err := os.MkdirAll(localSkill, 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	parsed := ParseSource("tests/testdata/hidden-skill")
	if parsed.Type != SourceTypeLocal {
		t.Fatalf("Type = %q, want %q", parsed.Type, SourceTypeLocal)
	}
	if parsed.LocalPath != localSkill {
		t.Fatalf("LocalPath = %q, want %q", parsed.LocalPath, localSkill)
	}
}

func TestParseSourceMissingRelativePathRemainsGitHubShorthand(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	parsed := ParseSource("owner/repo")
	if parsed.Type != SourceTypeGitHub {
		t.Fatalf("Type = %q, want %q", parsed.Type, SourceTypeGitHub)
	}
}
