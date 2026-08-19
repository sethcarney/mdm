package skill

import "testing"

func TestSetFrontmatterName(t *testing.T) {
	cases := []struct {
		desc string
		raw  string
		name string
		want string
		ok   bool
	}{
		{
			desc: "rewrites only the name line",
			raw:  "---\nname: code-review\ndescription: Review a diff.\nversion: 1.0.0\n---\n\n# Body\n",
			name: "our-review",
			want: "---\nname: our-review\ndescription: Review a diff.\nversion: 1.0.0\n---\n\n# Body\n",
			ok:   true,
		},
		{
			desc: "preserves CRLF line endings",
			raw:  "---\r\nname: a\r\ndescription: b\r\n---\r\nbody\r\n",
			name: "c",
			want: "---\r\nname: c\r\ndescription: b\r\n---\r\nbody\r\n",
			ok:   true,
		},
		{
			desc: "ignores an indented name key",
			raw:  "---\nmetadata:\n  name: nested\ndescription: b\nname: real\n---\n",
			name: "renamed",
			want: "---\nmetadata:\n  name: nested\ndescription: b\nname: renamed\n---\n",
			ok:   true,
		},
		{
			desc: "quotes a value that would not survive unquoted",
			raw:  "---\nname: a\ndescription: b\n---\n",
			name: "ours: the fork",
			want: "---\nname: \"ours: the fork\"\ndescription: b\n---\n",
			ok:   true,
		},
		{
			desc: "reports failure when there is no frontmatter",
			raw:  "# Just a document\n",
			name: "x",
			want: "# Just a document\n",
		},
		{
			desc: "reports failure when the block is unterminated",
			raw:  "---\nname: a\ndescription: b\n",
			name: "x",
			want: "---\nname: a\ndescription: b\n",
		},
		{
			desc: "reports failure when there is no name key",
			raw:  "---\ndescription: b\n---\nbody\n",
			name: "x",
			want: "---\ndescription: b\n---\nbody\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, ok := SetFrontmatterName(tc.raw, tc.name)
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

// A rewritten document must still parse, and parse as the new name.
func TestSetFrontmatterNameStaysParseable(t *testing.T) {
	raw := "---\nname: code-review\ndescription: Review a diff.\n---\nbody\n"
	rewritten, ok := SetFrontmatterName(raw, "our-review")
	if !ok {
		t.Fatal("expected the rename to apply")
	}
	data, body := ParseFrontmatter(rewritten)
	if data["name"] != "our-review" {
		t.Errorf("name = %v, want our-review", data["name"])
	}
	if data["description"] != "Review a diff." {
		t.Errorf("description = %v, want it preserved", data["description"])
	}
	if body != "body\n" {
		t.Errorf("body = %q, want it preserved", body)
	}
}
