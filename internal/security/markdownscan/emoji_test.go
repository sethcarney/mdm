package markdownscan

import (
	"strings"
	"testing"
)

// TestEmojiDataVersionMatches is the drift guard for the vendored data file:
// the Go constant and the file's own header have to name the same Unicode
// release, and the parsed count has to match the trailer the Consortium
// writes, so a truncated or hand-edited copy fails the build.
func TestEmojiDataVersionMatches(t *testing.T) {
	table := loadEmojiVariationTable()
	if table.version != UnicodeVersion {
		t.Fatalf("emoji-variation-sequences.txt header says version %q, UnicodeVersion is %q; update both together (see unicodedata/README.md)", table.version, UnicodeVersion)
	}
	if table.declared == 0 {
		t.Fatal("data file is missing its '#Total sequences:' trailer")
	}
	if len(table.bases) != table.declared {
		t.Fatalf("parsed %d base characters, file declares %d", len(table.bases), table.declared)
	}
	if len(table.sequences) != 2*table.declared {
		t.Fatalf("parsed %d sequences, expected a text and an emoji line for each of %d bases", len(table.sequences), table.declared)
	}
}

func TestEmojiVariationSequenceLookup(t *testing.T) {
	cases := []struct {
		name     string
		base     rune
		selector rune
		listed   bool
		want     string
	}{
		{"warning sign emoji", 0x26a0, 0xfe0f, true, "WARNING SIGN"},
		{"warning sign text", 0x26a0, 0xfe0e, true, "WARNING SIGN"},
		{"heavy black heart", 0x2764, 0xfe0f, true, "HEAVY BLACK HEART"},
		{"keycap digit", '1', 0xfe0f, true, "DIGIT ONE"},
		{"latin letter", 'A', 0xfe0f, false, ""},
		{"no base", 0, 0xfe0f, false, ""},
		{"ivs selector", 0x26a0, 0xe0100, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, ok := emojiVariationSequenceName(tc.base, tc.selector)
			if ok != tc.listed {
				t.Fatalf("listed = %v, want %v", ok, tc.listed)
			}
			if name != tc.want {
				t.Fatalf("name = %q, want %q", name, tc.want)
			}
		})
	}
}

func TestParseEmojiVariationSequencesRejectsMalformedLines(t *testing.T) {
	cases := map[string]string{
		"one codepoint":    "26A0 ; emoji style; # WARNING SIGN\n",
		"bad hex":          "26A0 ZZZZ ; emoji style; # WARNING SIGN\n",
		"wrong selector":   "26A0 200D ; emoji style; # WARNING SIGN\n",
		"no sequences":     "# Version: 17.0\n",
		"only blank lines": "\n\n",
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseEmojiVariationSequences([]byte(data)); err == nil {
				t.Fatal("expected a parse error")
			}
		})
	}
}

func TestParseEmojiVariationSequencesReadsNamesAndTrailer(t *testing.T) {
	data := strings.Join([]string{
		"# Version: 99.0",
		"26A0 FE0E  ; text style;  # (4.0) WARNING SIGN",
		"26A0 FE0F  ; emoji style; # (4.0) WARNING SIGN",
		"",
		"#Total sequences: 1",
		"#EOF",
	}, "\n")
	table, err := parseEmojiVariationSequences([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if table.version != "99.0" || table.declared != 1 || len(table.sequences) != 2 || len(table.bases) != 1 {
		t.Fatalf("unexpected table %+v", table)
	}
	if got := table.sequences[variationSequence{base: 0x26a0, selector: 0xfe0f}]; got != "WARNING SIGN" {
		t.Fatalf("name = %q, want WARNING SIGN", got)
	}
}
