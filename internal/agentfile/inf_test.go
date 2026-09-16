package agentfile

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A TOML `inf` is a float64 infinity, and math.Trunc(±Inf) == ±Inf, so
// wholeFloatNode treated it as a whole number and emitted "!!float +Inf.0",
// which yaml.v3 cannot parse. skill.ParseFrontmatter then fell back to "no
// frontmatter", the installed markdown definition had no name, and the
// harness never loaded it while mdm reported success. yaml.v3 writes an
// infinity as ".inf" on its own when handed the bare float.
//
// Mutation this detects: drop the math.IsInf check from wholeFloatNode.
func TestEncodeMarkdownWritesAnInfinityYAMLCanReadBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  float64
	}{
		{"positive", math.Inf(1)},
		{"negative", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &AgentFile{
				Name:         "limits",
				Description:  "d",
				Instructions: "body",
				Format:       FormatTOML,
				Extra:        map[string]any{"ceiling": tc.val},
			}
			out, err := Encode(a, FormatMarkdown)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), "Inf.0") {
				t.Errorf("encoded markdown spells the infinity as a whole float, which YAML cannot read:\n%s", out)
			}

			p := filepath.Join(t.TempDir(), "a.md")
			if err := os.WriteFile(p, out, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ParseAgentFile(p)
			if err != nil {
				t.Fatalf("re-parse failed: %v", err)
			}
			if got == nil {
				t.Fatalf("re-parse found no definition: the frontmatter did not parse\n%s", out)
			}
			if got.Name != "limits" {
				t.Errorf("Name = %q after the round trip, want limits", got.Name)
			}
			v, ok := got.Extra["ceiling"].(float64)
			if !ok || v != tc.val {
				t.Errorf("Extra[ceiling] = %v (%T), want %v", got.Extra["ceiling"], got.Extra["ceiling"], tc.val)
			}
		})
	}
}
