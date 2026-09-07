package agentfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseAgentMd(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantNil  bool
		wantName string
	}{
		{"valid frontmatter", "testdata/repo/agents/critic.md", false, "critic"},
		{"no frontmatter is not an agent", "testdata/repo/agents/notes.md", true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAgentMd(tc.path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Fatalf("got %+v, want nil for a file that is not an agent", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil, want a parsed agent")
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
		})
	}
}

// A manifest ships inside the source being installed, which is third-party.
// A directory that escapes the search root must be dropped, not followed.
func TestDiscoverRejectsUnsafeManifestDirs(t *testing.T) {
	got, err := DiscoverAgentFiles("testdata/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		abs, err := filepath.Abs(a.Path)
		if err != nil {
			t.Fatal(err)
		}
		root, err := filepath.Abs("testdata/repo")
		if err != nil {
			t.Fatal(err)
		}
		if rel, err := filepath.Rel(root, abs); err != nil || rel == ".." || len(rel) > 2 && rel[:3] == ".."+string(filepath.Separator) {
			t.Errorf("discovered %q outside the search root", a.Path)
		}
	}
	// Explicitly check that the agent in the escaped directory was not discovered.
	for _, a := range got {
		if a.Name == "leaked" {
			t.Error("leaked agent from escaped directory was discovered despite guard")
		}
	}
}

// A manifest directory is searched before the conventional ones, and a name
// found first wins, so a source can override where its agents come from.
func TestDiscoverFindsManifestAndConventionalDirs(t *testing.T) {
	got, err := DiscoverAgentFiles("testdata/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, a := range got {
		if names[a.Name] {
			t.Errorf("duplicate name %q; the first occurrence must win", a.Name)
		}
		names[a.Name] = true
	}
	for _, want := range []string{"critic", "mechanic", "shadow"} {
		if !names[want] {
			t.Errorf("missing agent %q; found %v", want, names)
		}
	}
	if names["notes"] {
		t.Error("a file without name/description frontmatter was treated as an agent")
	}
}

// Discovery must consider .toml files, not just .md, or a Codex-authored
// source is invisible to it even though ParseAgentFile can read it fine.
func TestDiscoverFindsTOMLDefinition(t *testing.T) {
	got, err := DiscoverAgentFiles("testdata/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.Name == "codex-style" {
			if a.Format != FormatTOML {
				t.Errorf("Format = %q, want %q", a.Format, FormatTOML)
			}
			return
		}
	}
	t.Fatal("codex-style.toml was not discovered")
}

// A legitimate, safely-contained directory can still hold a symlinked TOML
// file pointing outside the root, the same way a .md file can (see
// TestDiscoverRejectsSymlinkedFileEscape). The containment check must apply
// to .toml exactly as it does to .md, not just to the extension that existed
// first.
func TestDiscoverRejectsSymlinkedTOMLFileEscape(t *testing.T) {
	root := t.TempDir()

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.toml")
	agentToml := "name = \"leaked-toml\"\ndescription = \"Should never be discovered\"\ndeveloper_instructions = \"x\"\n"
	if err := os.WriteFile(outsideFile, []byte(agentToml), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(root, "repo")
	agentsDir := filepath.Join(repo, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(agentsDir, "linked.toml")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("cannot create a file symlink on this system: %v", err)
	}

	got, err := DiscoverAgentFiles(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.Name == "leaked-toml" {
			t.Fatalf("discovered %q through a symlinked TOML file that escapes the search root", a.Name)
		}
	}
}

// ParseAgentMd's (nil, nil) return means "not an agent definition". A genuine
// read failure must not collapse into the same result. A directory makes
// os.ReadFile fail with something other than "not found" on every OS, without
// chmod, which does not model "permission denied" the same way on NTFS.
func TestParseAgentMdReadError(t *testing.T) {
	got, err := ParseAgentMd("testdata/repo/agents")
	if err == nil {
		t.Fatalf("got (%+v, nil), want a non-nil error for an unreadable path", got)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil result alongside the error", got)
	}
}

// isSafeRelDir is the boundary that filters agentsDirs, which arrives from
// an untrusted marketplace.json. Test it directly, not just through the
// fixtures, since a fixture's absence would let a broken check pass anyway
// (see TestDiscover fixtures: nonexistent "../escape" and "/abs" entries
// fail at os.ReadDir regardless of what isSafeRelDir decided).
func TestIsSafeRelDir(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{"normal relative dir", "custom-agents", true},
		{"nested relative dir", "a/b", true},
		{"parent traversal", "..", false},
		{"traversal past root", "a/../../escape", false},
		{"current dir is not a declared subdirectory", ".", false},
		{"empty", "", false},
		{"absolute unix path", "/etc/passwd", false},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name string
			dir  string
			want bool
		}{"windows drive-absolute path", `C:\x`, false})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSafeRelDir(tc.dir); got != tc.want {
				t.Errorf("isSafeRelDir(%q) = %v, want %v", tc.dir, got, tc.want)
			}
		})
	}
}

// DiscoverAgentFiles searches manifest-declared directories before conventional
// ones, and the first occurrence of a name wins.
// testdata/repo/custom-agents/shared.md and testdata/repo/agents/shared.md share
// the name "shared" with different descriptions to pin that order down.
func TestDiscoverManifestDirWinsOverConventional(t *testing.T) {
	got, err := DiscoverAgentFiles("testdata/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	var found *AgentFile
	for _, a := range got {
		if a.Name == "shared" {
			found = a
			break
		}
	}
	if found == nil {
		t.Fatal("agent \"shared\" not discovered at all")
	}
	const wantDesc = "From the manifest-declared dir, must win"
	if found.Description != wantDesc {
		t.Errorf("Description = %q, want %q (the conventional-dir copy won instead)", found.Description, wantDesc)
	}
}

// A source can ship a directory entry that lexically looks like a plain
// subdirectory but is a symlink pointing outside the search root. isSafeRelDir
// only inspects the declared name. Built with t.TempDir rather than a committed
// fixture, since a symlink materializing depends on the checkout environment.
func TestDiscoverRejectsSymlinkedDirEscape(t *testing.T) {
	root := t.TempDir()

	// Outside the search root entirely. If the symlink below is followed,
	// this is what leaks onto the victim's install.
	outside := t.TempDir()
	agentMd := "---\nname: leaked-via-symlink\ndescription: Should never be discovered\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(outside, "leaked.md"), []byte(agentMd), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(repo, "escaped-agents")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create a directory symlink on this system: %v", err)
	}

	manifest := `{"agentsDirs":["escaped-agents"]}`
	if err := os.WriteFile(filepath.Join(repo, ".claude-plugin", "marketplace.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DiscoverAgentFiles(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.Name == "leaked-via-symlink" {
			t.Fatalf("discovered %q through a symlinked directory that escapes the search root", a.Name)
		}
	}
}

// A legitimate, safely-contained directory can still hold a symlinked FILE
// pointing outside the root. The directory-level containment check alone
// does not catch that; each discovered .md file needs its own resolution.
func TestDiscoverRejectsSymlinkedFileEscape(t *testing.T) {
	root := t.TempDir()

	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.md")
	agentMd := "---\nname: leaked-file\ndescription: Should never be discovered\n---\n\nBody.\n"
	if err := os.WriteFile(outsideFile, []byte(agentMd), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(root, "repo")
	agentsDir := filepath.Join(repo, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(agentsDir, "linked.md")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("cannot create a file symlink on this system: %v", err)
	}

	got, err := DiscoverAgentFiles(repo, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a.Name == "leaked-file" {
			t.Fatalf("discovered %q through a symlinked file that escapes the search root", a.Name)
		}
	}
}

// .claude-plugin itself can be a symlink pointing outside the search root,
// making manifestAgentDirs parse a marketplace.json from outside the source.
// Declared agentsDirs are still joined against searchPath, so this is not an
// exfiltration path, but the read is refused. Only a direct manifestAgentDirs
// call can observe that: DiscoverAgentFiles resolves against the repo root.
func TestManifestAgentDirsRejectsSymlinkedPluginDir(t *testing.T) {
	root := t.TempDir()

	outside := t.TempDir()
	outsidePlugin := filepath.Join(outside, ".claude-plugin")
	if err := os.MkdirAll(outsidePlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"agentsDirs":["outside-agents"]}`
	if err := os.WriteFile(filepath.Join(outsidePlugin, "marketplace.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(repo, ".claude-plugin")
	if err := os.Symlink(outsidePlugin, link); err != nil {
		t.Skipf("cannot create a directory symlink on this system: %v", err)
	}

	resolvedRoot, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}

	got := manifestAgentDirs(repo, resolvedRoot)
	if got != nil {
		t.Fatalf("manifestAgentDirs(%q) = %v, want nil for a .claude-plugin symlink escaping the search root", repo, got)
	}
}

// A file that does not parse is skipped, not fatal: one malformed .toml in a
// source must not stop every valid definition beside it from installing.
// Markdown gets this for free, because skill.ParseFrontmatter falls back to
// "no frontmatter" when the YAML will not unmarshal; TOML's decoder reports
// the syntax error instead, and DiscoverAgentFiles used to return it.
//
// Mutation this detects: put back the fatal branch in DiscoverAgentFiles
// (`return nil, fmt.Errorf("agentfile: reading %s: %w", filePath, err)` in
// place of the note-and-skip). Discovery then returns an error and "good" is
// never found.
func TestDiscoverSkipsAnUnparseableFile(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		content []byte
	}{
		{"malformed toml", "junk.toml", []byte("this is not toml at all [[[\n")},
		{"binary toml", "binary.toml", []byte{0x00, 0x01, 0xFF, 0xFE, 0x00, 0x00}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			agentsDir := filepath.Join(repo, "agents")
			if err := os.MkdirAll(agentsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			good := "---\nname: good\ndescription: A valid definition\n---\n\nBody.\n"
			if err := os.WriteFile(filepath.Join(agentsDir, "good.md"), []byte(good), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(agentsDir, tc.file), tc.content, 0o644); err != nil {
				t.Fatal(err)
			}

			var noted []string
			restore := noteSkippedFile
			noteSkippedFile = func(path string, err error) {
				noted = append(noted, path)
			}
			t.Cleanup(func() { noteSkippedFile = restore })

			got, err := DiscoverAgentFiles(repo, "")
			if err != nil {
				t.Fatalf("DiscoverAgentFiles returned %v; an unparseable file must be skipped, not fatal", err)
			}
			var names []string
			for _, a := range got {
				names = append(names, a.Name)
			}
			if len(names) != 1 || names[0] != "good" {
				t.Fatalf("discovered %v, want exactly [good]", names)
			}
			if len(noted) != 1 || filepath.Base(noted[0]) != tc.file {
				t.Errorf("noted %v, want a single note naming %q: a silent skip leaves the user with no reason", noted, tc.file)
			}
		})
	}
}
