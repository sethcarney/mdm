package agentfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// A definition can carry a frontmatter key literally named name, description
// or developer_instructions. Each encoder writes the definition's own fields
// under some of those keys and then loops over Extra into the same table, so
// such a key would silently replace what the definition itself says: a
// frontmatter developer_instructions would reach Codex as the whole of the
// agent's instructions with the real body dropped.
//
// TOML reserves all three. Markdown reserves name and description: the body
// is not a frontmatter key there, so developer_instructions sits beside it
// and nothing is lost. encodeMarkdown used to seed name and let the Extra
// loop overwrite it, and the old test only checked that the body survived.
//
// Mutation this detects: delete the reserved-key check from either encoder.
// The refusing subtests then succeed, and the emitted name or
// developer_instructions is the frontmatter value rather than the definition's.
func TestEncodeRefusesAReservedKeyInExtra(t *testing.T) {
	for _, key := range []string{"developer_instructions", "name", "description"} {
		t.Run(key, func(t *testing.T) {
			a := &AgentFile{
				Name:         "collide",
				Description:  "d",
				Instructions: "THE REAL BODY",
				Format:       FormatMarkdown,
				Extra:        map[string]any{key: "FROM THE FRONTMATTER KEY"},
			}

			got, err := Encode(a, FormatTOML)
			if err == nil {
				t.Fatalf("Encode to TOML succeeded and wrote %q, want a refusal naming %q", string(got), key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q does not name the offending key %q", err.Error(), key)
			}

			md, err := Encode(a, FormatMarkdown)
			if key == "developer_instructions" {
				if err != nil {
					t.Fatalf("Encode to markdown: %v; the body is not a frontmatter key, so nothing collides", err)
				}
				if !strings.Contains(string(md), "THE REAL BODY") || !strings.Contains(string(md), "developer_instructions: FROM THE FRONTMATTER KEY") {
					t.Errorf("markdown output should keep both the body and the key:\n%s", md)
				}
				return
			}
			if err == nil {
				t.Fatalf("Encode to markdown succeeded and wrote:\n%s\nwant a refusal naming %q", md, key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q does not name the offending key %q", err.Error(), key)
			}
		})
	}
}

// yaml.v3 resolves an unquoted `since: 2024-01-15` to a time.Time. TOML then
// writes it as the offset datetime 2024-01-15T00:00:00Z, and YAML writes it
// back as an RFC 3339 timestamp: either way the file no longer says what the
// source said, which is the change the TOML local-date guard refuses in the
// other direction. Quoting the value in the source keeps it a string.
//
// Mutation this detects: delete the markdown-source temporal check from
// Encode. Both subtests then succeed and the output carries T00:00:00Z.
func TestEncodeRefusesAYAMLDate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.md")
	src := "---\nname: dated\ndescription: has a date\nsince: 2024-01-15\n---\nBody.\n"
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ParseAgentFile(p)
	if err != nil || a == nil {
		t.Fatalf("fixture did not parse: %v", err)
	}
	if _, ok := a.Extra["since"].(time.Time); !ok {
		t.Skipf("yaml.v3 decoded the date as %T, not time.Time; nothing to refuse", a.Extra["since"])
	}
	for _, f := range []Format{FormatTOML, FormatMarkdown} {
		t.Run(string(f), func(t *testing.T) {
			out, err := Encode(a, f)
			if err == nil {
				t.Fatalf("Encode succeeded; output:\n%s", out)
			}
			if !strings.Contains(err.Error(), `"since"`) {
				t.Errorf("error should name the key since, got: %v", err)
			}
		})
	}
}

// tomlLocalZoneNames relies on the synthetic time.Location names BurntSushi
// gives a decoded local date, local time and local date-time. They are not
// part of the library's documented API, so a minor bump could rename them and
// local values would silently round-trip as UTC instants. This pins them.
//
// The decode target matters: into a time.Time struct field the library hands
// back "Local", and the names appear only when it decodes into interface{},
// which is what parseAgentTOML does. The test decodes the same way.
func TestBurntSushiLocalZoneNamesStillHold(t *testing.T) {
	var doc map[string]any
	if _, err := toml.Decode("d = 1979-05-27\nt = 07:32:00\ndt = 1979-05-27T07:32:00\n", &doc); err != nil {
		t.Fatal(err)
	}
	zone := func(key string) string {
		v, ok := doc[key].(time.Time)
		if !ok {
			t.Fatalf("%s decoded as %T, want time.Time", key, doc[key])
		}
		return v.Location().String()
	}
	for name, got := range map[string]string{
		"date-local":     zone("d"),
		"time-local":     zone("t"),
		"datetime-local": zone("dt"),
	} {
		if got != name {
			t.Errorf("BurntSushi names the %s location %q; tomlLocalZoneNames expects %q", name, got, name)
		}
		if !tomlLocalZoneNames[got] {
			t.Errorf("tomlLocalZoneNames does not list %q", got)
		}
	}
}

// developer_instructions is written as a multi-line basic string, so a
// multi-paragraph body is reviewable in a diff, and it has to read back byte
// for byte through BurntSushi's decoder whatever the body holds: quotes,
// three quotes in a row, backslashes, a trailing backslash, tabs, CRLF, a
// leading newline, a control character.
//
// Mutation this detects: drop the escape of the third consecutive quote, or
// of the backslash; the matching body then fails to parse or comes back
// changed.
func TestEncodeTOMLWritesTheBodyMultilineAndRoundTripsIt(t *testing.T) {
	bodies := []string{
		"Be critical.\n\nSecond paragraph.\n",
		"\nstarts with a newline",
		"has \"quotes\" and \"\"\"three\"\"\" and ends with two \"\"",
		"a backslash \\ in the middle and one at the end \\",
		"tab\tseparated\r\nwindows lines\r\n",
		"bell \x07 and delete \x7f inside",
		"",
	}
	for _, body := range bodies {
		a := &AgentFile{Name: "critic", Description: "d", Instructions: body, Format: FormatMarkdown, Extra: map[string]any{"model": "gpt-5", "mcp_servers": map[string]any{"docs": map[string]any{"command": "x"}}}}
		out, err := Encode(a, FormatTOML)
		if err != nil {
			t.Fatalf("Encode(%q): %v", body, err)
		}
		if !strings.Contains(string(out), "developer_instructions = \"\"\"\n") {
			t.Errorf("body is not written as a multi-line string:\n%s", out)
		}
		got := parseBytes(t, out, ".toml")
		if got.Instructions != body {
			t.Errorf("body changed through TOML:\n got: %q\nwant: %q\nfile:\n%s", got.Instructions, body, out)
		}
		if got.Extra["model"] != "gpt-5" {
			t.Errorf("Extra lost after the body: %#v\n%s", got.Extra, out)
		}
	}
}
