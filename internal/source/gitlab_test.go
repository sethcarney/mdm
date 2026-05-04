package source

import (
	"fmt"
	"testing"
)

func TestParseGitLabURL(t *testing.T) {
	cases := []struct {
		input       string
		wantType    SourceType
		wantURL     string
		wantRef     string
		wantSubpath string
	}{
		{
			input:    "https://gitlab.com/acme/skills/-/tree/main",
			wantType: SourceTypeGitLab,
			wantURL:  "https://gitlab.com/acme/skills.git",
			wantRef:  "main",
		},
		{
			input:       "https://gitlab.com/acme/skills/-/tree/main/subdir",
			wantType:    SourceTypeGitLab,
			wantURL:     "https://gitlab.com/acme/skills.git",
			wantRef:     "main",
			wantSubpath: "subdir",
		},
		{
			input:    "https://gitlab.com/acme/skills",
			wantType: SourceTypeGitLab,
			wantURL:  "https://gitlab.com/acme/skills.git",
		},
		{
			input:    "https://gitlab.com/acme/skills.git",
			wantType: SourceTypeGitLab,
			wantURL:  "https://gitlab.com/acme/skills.git",
		},
		{
			// gitlab: shorthand resolves to gitlab.com HTTPS
			input:    "gitlab:acme/skills",
			wantType: SourceTypeGitLab,
			wantURL:  "https://gitlab.com/acme/skills.git",
		},
		{
			// SSH URLs pass through as generic git
			input:    "git@gitlab.com:acme/skills.git",
			wantType: SourceTypeGit,
			wantURL:  "git@gitlab.com:acme/skills.git",
		},
		{
			// Self-hosted GitLab with /-/tree/ browse URL
			input:    "https://gitlab.example.com/acme/skills/-/tree/main",
			wantType: SourceTypeGitLab,
			wantURL:  "https://gitlab.example.com/acme/skills.git",
			wantRef:  "main",
		},
		{
			// Self-hosted GitLab with subpath
			input:       "https://gitlab.example.com/acme/skills/-/tree/main/subdir",
			wantType:    SourceTypeGitLab,
			wantURL:     "https://gitlab.example.com/acme/skills.git",
			wantRef:     "main",
			wantSubpath: "subdir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			p := ParseSource(tc.input)
			if p.Type != tc.wantType {
				t.Errorf("Type: got %q, want %q", p.Type, tc.wantType)
			}
			if p.URL != tc.wantURL {
				t.Errorf("URL: got %q, want %q", p.URL, tc.wantURL)
			}
			if p.Ref != tc.wantRef {
				t.Errorf("Ref: got %q, want %q", p.Ref, tc.wantRef)
			}
			if p.Subpath != tc.wantSubpath {
				t.Errorf("Subpath: got %q, want %q", p.Subpath, tc.wantSubpath)
			}
			fmt.Printf("  %s -> type=%s url=%s ref=%q\n", tc.input, p.Type, p.URL, p.Ref)
		})
	}
}
