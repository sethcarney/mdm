package source

import (
	"fmt"
	"testing"
)

func TestParseBitbucketURL(t *testing.T) {
	cases := []struct {
		input       string
		wantType    SourceType
		wantURL     string
		wantRef     string
		wantSubpath string
	}{
		{
			input:    "https://bitbucket.org/acme/skills/src/main",
			wantType: SourceTypeGit,
			wantURL:  "https://bitbucket.org/acme/skills.git",
			wantRef:  "main",
		},
		{
			input:       "https://bitbucket.org/acme/skills/src/main/subdir",
			wantType:    SourceTypeGit,
			wantURL:     "https://bitbucket.org/acme/skills.git",
			wantRef:     "main",
			wantSubpath: "subdir",
		},
		{
			input:    "https://bitbucket.org/acme/skills",
			wantType: SourceTypeGit,
			wantURL:  "https://bitbucket.org/acme/skills.git",
		},
		{
			input:    "https://bitbucket.org/acme/skills.git",
			wantType: SourceTypeGit,
			wantURL:  "https://bitbucket.org/acme/skills.git",
		},
		{
			// SSH URLs still work
			input:    "git@bitbucket.org:acme/skills.git",
			wantType: SourceTypeGit,
			wantURL:  "git@bitbucket.org:acme/skills.git",
		},
		{
			// Non-bitbucket custom host still treated as well-known
			input:    "https://example.com/agent-skills",
			wantType: SourceTypeWellKnown,
			wantURL:  "https://example.com/agent-skills",
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
