package agentfile

import "testing"

// A TOML definition carries the same three required ideas as a markdown one,
// with developer_instructions standing in for the body.
func TestParseAgentFileReadsTOML(t *testing.T) {
	got, err := ParseAgentFile("testdata/repo/agents/codex-style.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil, want a parsed definition")
	}
	if got.Name != "codex-style" {
		t.Errorf("Name = %q, want codex-style", got.Name)
	}
	if got.Format != FormatTOML {
		t.Errorf("Format = %q, want %q", got.Format, FormatTOML)
	}
	if got.Instructions == "" {
		t.Error("Instructions is empty; developer_instructions did not reach it")
	}
	if got.Extra["model"] != "gpt-5" {
		t.Errorf("Extra[model] = %v, want gpt-5; non-required keys must pass through", got.Extra["model"])
	}
	if _, ok := got.Extra["mcp_servers"]; !ok {
		t.Error("Extra[mcp_servers] missing; a nested table must survive")
	}
	if _, ok := got.Extra["name"]; ok {
		t.Error("Extra must not repeat name; it has its own field")
	}
}

// A TOML file with no name or no description is not a definition. That is
// normal in a source tree, matching ParseAgentMd's contract for markdown, so
// it must come back as (nil, nil) rather than an error.
func TestParseAgentFileTOMLSkipsNonDefinition(t *testing.T) {
	got, err := ParseAgentFile("testdata/repo/agents/no-name.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil for a TOML file with no name", got)
	}
}

// A markdown definition keeps working and reports its format.
func TestParseAgentFileReadsMarkdown(t *testing.T) {
	got, err := ParseAgentFile("testdata/repo/agents/critic.md")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil, want a parsed definition")
	}
	if got.Format != FormatMarkdown {
		t.Errorf("Format = %q, want %q", got.Format, FormatMarkdown)
	}
	if got.Instructions == "" {
		t.Error("Instructions is empty; the markdown body did not reach it")
	}
}
