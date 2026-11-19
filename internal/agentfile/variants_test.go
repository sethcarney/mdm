package agentfile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDef(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const (
	reviewerMd   = "---\nname: reviewer\ndescription: d\n---\nreview\n"
	reviewerTOML = "name = \"reviewer\"\ndescription = \"d\"\ndeveloper_instructions = \"review natively\"\nmodel = \"gpt-5\"\n"
)

func discoverOne(t *testing.T, root string) *AgentFile {
	t.Helper()
	got, err := DiscoverAgentFiles(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("discovered %d definitions, want 1: a markdown and a TOML file under one name are one agent", len(got))
	}
	return got[0]
}

// Mutation this test catches: keeping the first occurrence of a name
// regardless of format, which drops the native Codex file before any harness
// is known.
func TestDiscoverPairsMarkdownAndTOMLUnderOneName(t *testing.T) {
	root := t.TempDir()
	writeDef(t, filepath.Join(root, "agents", "reviewer.md"), reviewerMd)
	writeDef(t, filepath.Join(root, "agents", "reviewer.toml"), reviewerTOML)

	a := discoverOne(t, root)
	if a.Format != FormatMarkdown {
		t.Errorf("primary format = %q, want markdown", a.Format)
	}
	if a.Alt == nil || a.Alt.Format != FormatTOML {
		t.Fatalf("Alt = %+v, want the TOML variant", a.Alt)
	}
	if a.Alt.Extra["model"] != "gpt-5" {
		t.Errorf("the TOML variant lost its optional attributes: %#v", a.Alt.Extra)
	}
	if got := a.VariantFor(FormatTOML); got != a.Alt {
		t.Errorf("VariantFor(toml) = %s, want the TOML file", got.Path)
	}
	if got := a.VariantFor(FormatMarkdown); got != a {
		t.Errorf("VariantFor(markdown) = %s, want the markdown file", got.Path)
	}
}

// The markdown file is the primary whichever directory yields it, so the
// result does not depend on discovery order: here the TOML is found first, in
// a conventional directory, and the markdown last, in the search path itself.
func TestDiscoverPairsAcrossDirectoriesWithMarkdownPrimary(t *testing.T) {
	root := t.TempDir()
	writeDef(t, filepath.Join(root, "agents", "reviewer.toml"), reviewerTOML)
	writeDef(t, filepath.Join(root, "reviewer.md"), reviewerMd)

	a := discoverOne(t, root)
	if a.Format != FormatMarkdown || a.Alt == nil || a.Alt.Format != FormatTOML {
		t.Fatalf("got primary %q with Alt %+v, want markdown primary with a TOML variant", a.Format, a.Alt)
	}
	if a.Alt.Alt != nil {
		t.Error("a variant must not carry a variant of its own")
	}
}

// Within one format the first occurrence still wins: a second markdown file
// of the same name is a duplicate, not a variant, and precedence across
// discovery directories is what it always was.
func TestDiscoverKeepsFirstOccurrenceWithinAFormat(t *testing.T) {
	root := t.TempDir()
	writeDef(t, filepath.Join(root, "agents", "reviewer.md"), reviewerMd)
	writeDef(t, filepath.Join(root, "agents", "reviewer.toml"), reviewerTOML)
	writeDef(t, filepath.Join(root, "subagents", "reviewer.md"), "---\nname: reviewer\ndescription: second\n---\nx\n")
	writeDef(t, filepath.Join(root, "subagents", "reviewer.toml"), "name = \"reviewer\"\ndescription = \"second\"\ndeveloper_instructions = \"x\"\n")

	a := discoverOne(t, root)
	if a.Description != "d" || a.Alt == nil || a.Alt.Description != "d" {
		t.Errorf("a later directory replaced an earlier one: primary %q, variant %+v", a.Description, a.Alt)
	}
}

// A source with one format is untouched: no variant, and VariantFor falls
// back to the only file there is, which the installer then converts.
func TestSingleFormatDefinitionHasNoVariant(t *testing.T) {
	root := t.TempDir()
	writeDef(t, filepath.Join(root, "agents", "reviewer.toml"), reviewerTOML)

	a := discoverOne(t, root)
	if a.Alt != nil {
		t.Fatalf("Alt = %+v, want none", a.Alt)
	}
	if got := a.VariantFor(FormatMarkdown); got != a {
		t.Error("VariantFor must fall back to the primary when no variant is in that format")
	}
}
