package blob

import "testing"

func TestSkillDirPrefix(t *testing.T) {
	cases := map[string]string{
		"SKILL.md":                "",
		"skills/foo/SKILL.md":     "skills/foo/",
		"a/b/c/SKILL.md":          "a/b/c/",
		"packages/x/y/z/SKILL.md": "packages/x/y/z/",
	}
	for in, want := range cases {
		if got := skillDirPrefix(in); got != want {
			t.Errorf("skillDirPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSkillRelPath(t *testing.T) {
	type tc struct {
		dir, entry string
		wantRel    string
		wantOK     bool
	}
	cases := []tc{
		// Subdirectory skill claims everything beneath it, paths relative to root.
		{"skills/foo/", "skills/foo/SKILL.md", "SKILL.md", true},
		{"skills/foo/", "skills/foo/scripts/run.sh", "scripts/run.sh", true},
		{"skills/foo/", "skills/foo/refs/a/b.md", "refs/a/b.md", true},
		// Files outside the skill dir are excluded.
		{"skills/foo/", "skills/bar/SKILL.md", "", false},
		{"skills/foo/", "README.md", "", false},
		// Root-level skill only claims SKILL.md, not the whole repo.
		{"", "SKILL.md", "SKILL.md", true},
		{"", "src/main.go", "", false},
		{"", "package.json", "", false},
	}
	for _, c := range cases {
		gotRel, gotOK := skillRelPath(c.dir, c.entry)
		if gotRel != c.wantRel || gotOK != c.wantOK {
			t.Errorf("skillRelPath(%q, %q) = (%q, %v), want (%q, %v)",
				c.dir, c.entry, gotRel, gotOK, c.wantRel, c.wantOK)
		}
	}
}

func TestIsRateLimited(t *testing.T) {
	if !IsRateLimited(&APIError{Status: 403, RateLimited: true}) {
		t.Error("expected rate-limited APIError to be detected")
	}
	if IsRateLimited(&APIError{Status: 404}) {
		t.Error("404 APIError should not be treated as rate-limited")
	}
	if IsRateLimited(nil) {
		t.Error("nil error should not be rate-limited")
	}
	if IsRateLimited(ErrTreeTruncated) {
		t.Error("truncation error should not be rate-limited")
	}
}

func TestAPIErrorMessage(t *testing.T) {
	if got := (&APIError{RateLimited: true}).Error(); got == "" {
		t.Error("rate-limited APIError should have a non-empty message")
	}
	if got := (&APIError{Status: 404}).Error(); got == "" {
		t.Error("status APIError should have a non-empty message")
	}
}

func TestFindSkillMdPaths(t *testing.T) {
	tree := &RepoTree{Tree: []TreeEntry{
		{Path: "SKILL.md", Type: "blob"},
		{Path: "skills/a/SKILL.md", Type: "blob"},
		{Path: "skills/b/SKILL.md", Type: "blob"},
		{Path: "skills/a", Type: "tree"},          // dirs ignored
		{Path: "docs/SKILL.md.bak", Type: "blob"}, // not a real SKILL.md
	}}

	all := findSkillMdPaths(tree, "")
	if len(all) != 3 {
		t.Fatalf("expected 3 SKILL.md paths, got %d: %v", len(all), all)
	}

	scoped := findSkillMdPaths(tree, "skills/a")
	if len(scoped) != 1 || scoped[0] != "skills/a/SKILL.md" {
		t.Fatalf("subpath filter failed, got %v", scoped)
	}
}
