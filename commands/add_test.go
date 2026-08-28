package commands

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/skill"
)

func TestToRelSourcePath(t *testing.T) {
	cases := []struct {
		name    string
		absPath string
		cwd     string
		want    string // expected stored value
	}{
		{
			name:    "sibling dir",
			absPath: "/home/user/skills/my-skill",
			cwd:     "/home/user/project",
			want:    "../skills/my-skill",
		},
		{
			name:    "nested inside project",
			absPath: "/home/user/project/local-skills/my-skill",
			cwd:     "/home/user/project",
			want:    "./local-skills/my-skill",
		},
		{
			name:    "same dir as project",
			absPath: "/home/user/project",
			cwd:     "/home/user/project",
			want:    ".",
		},
		{
			name:    "two levels up",
			absPath: "/home/skills/my-skill",
			cwd:     "/home/user/project",
			want:    "../../skills/my-skill",
		},
	}

	if runtime.GOOS == "windows" {
		t.Skip("unix path cases skipped on Windows")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toRelSourcePath(tc.absPath, tc.cwd)
			if got != tc.want {
				t.Errorf("toRelSourcePath(%q, %q) = %q, want %q", tc.absPath, tc.cwd, got, tc.want)
			}
		})
	}
}

func TestBlobSkillNameMatches(t *testing.T) {
	cases := []struct {
		skillName, filter string
		want              bool
	}{
		// Exact and case-insensitive matches.
		{"my-skill", "my-skill", true},
		{"My-Skill", "my-skill", true},
		// Lock files store sanitizeName(name); dots survive sanitizeName but
		// not ToSkillSlug, so both normalizations must be accepted.
		{"Web v1.2 Tool", "web-v1.2-tool", true}, // sanitizeName form
		{"Web v1.2 Tool", "web-v12-tool", true},  // ToSkillSlug form
		{"my-skill", "other-skill", false},
	}
	for _, c := range cases {
		if got := blobSkillNameMatches(c.skillName, c.filter); got != c.want {
			t.Errorf("blobSkillNameMatches(%q, %q) = %v, want %v", c.skillName, c.filter, got, c.want)
		}
	}
}

// Explicitly named skills must install without an interactive picker on the
// blob fast path, matching the clone path - this is what keeps
// `mdm skills install` and `mdm skills update` non-interactive.
func TestSelectBlobSkillsExplicitNamesSkipPrompt(t *testing.T) {
	skills := []*blob.BlobSkill{
		{Skill: skill.Skill{Name: "foo"}},
		{Skill: skill.Skill{Name: "bar"}},
	}
	// No TTY in tests: reaching the picker would return not-ok and fail this.
	selected, ok := selectBlobSkills(skills, AddOptions{Skills: []string{"foo", "bar"}})
	if !ok || len(selected) != 2 {
		t.Fatalf("expected 2 skills without prompting, got ok=%v len=%d", ok, len(selected))
	}
}

func TestSelectWellKnownSkillsExplicitNamesSkipPrompt(t *testing.T) {
	skills := []*registry.WellKnownSkill{
		{Name: "foo", InstallName: "foo"},
		{Name: "bar", InstallName: "bar"},
	}
	selected, ok := selectWellKnownSkills(skills, AddOptions{Skills: []string{"foo", "bar"}})
	if !ok || len(selected) != 2 {
		t.Fatalf("expected 2 skills without prompting, got ok=%v len=%d", ok, len(selected))
	}
}

func TestToRelSourcePathForwardSlashes(t *testing.T) {
	// Stored paths must use forward slashes so lock files are portable.
	cwd := filepath.FromSlash("/home/user/project")
	abs := filepath.FromSlash("/home/user/project/local-skills/my-skill")
	got := toRelSourcePath(abs, cwd)
	for _, ch := range got {
		if ch == '\\' {
			t.Errorf("toRelSourcePath returned backslash in %q", got)
		}
	}
}
