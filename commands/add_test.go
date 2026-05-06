package commands

import (
	"path/filepath"
	"runtime"
	"testing"
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
