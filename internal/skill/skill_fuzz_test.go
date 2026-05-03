package skill

import "testing"

func FuzzParseFrontmatter(f *testing.F) {
	seeds := []string{
		// Valid frontmatter
		"---\nname: my-skill\ndescription: A test skill\n---\n# Content",
		"---\r\nname: my-skill\r\ndescription: A test skill\r\n---\r\n# Content",
		"---\nname: skill\ndescription: desc\nmetadata:\n  internal: true\n---\nbody",
		// No frontmatter
		"# Just markdown content",
		"plain text",
		"",
		// Malformed frontmatter
		"---\n",
		"---\nno closing delimiter",
		"---\n---\n",
		"---\ninvalid: yaml: :\n---\n",
		"---\nnull\n---\n",
		"---\n[invalid\n---\n",
		// Edge cases
		"---",
		"---\n---",
		"-",
		"--",
		"\x00",
		"\n---\n",
		"---\n" + string([]byte{0x00}) + "\n---\n",
		// Large-ish YAML
		"---\nname: n\ndescription: d\ntags:\n  - a\n  - b\n  - c\n---\nbody",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		ParseFrontmatter(input) // must not panic
	})
}
