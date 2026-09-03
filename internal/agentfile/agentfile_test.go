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

// ParseAgentMd's (nil, nil) return means "not an agent definition", which is
// a routine thing to find. A genuine read failure is different and must not
// collapse into the same result. A directory is a reliable, portable way to
// make os.ReadFile fail with something other than "not found" on every OS,
// without relying on chmod (which does not model "permission denied" the
// same way on Windows/NTFS).
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

// The doc comment on DiscoverAgentFiles claims manifest-declared directories
// are searched before conventional ones and the first occurrence of a name
// wins. testdata/repo/custom-agents/shared.md (manifest-declared) and
// testdata/repo/agents/shared.md (conventional) share the name "shared"
// with different descriptions specifically to pin that order down; every
// other fixture uses distinct names, so this is the only test that would
// fail if the order were reversed.
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
// subdirectory (e.g. "escaped-agents" in agentsDirs) but is actually a
// symlink pointing outside the search root. isSafeRelDir only inspects the
// declared name and cannot see that; only resolving the entry on disk
// reveals it. Built at test time with t.TempDir rather than as a committed
// fixture, since a symlink materializing correctly depends on the checkout
// environment, not just this repo's own core.symlinks setting.
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
// making manifestAgentDirs read and parse a marketplace.json that never
// lived inside the source being installed. Declared agentsDirs are still
// joined against searchPath (not against wherever the manifest actually
// lives), so this is not an exfiltration path — but the read itself should
// still be refused rather than silently followed, which is only observable
// by calling manifestAgentDirs directly: DiscoverAgentFiles would resolve
// any declared directory against the (uncompromised) repo root either way,
// so it can't tell the two cases apart from the outside.
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
