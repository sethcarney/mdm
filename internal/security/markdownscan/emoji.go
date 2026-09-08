package markdownscan

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// UnicodeVersion is the Unicode release the vendored
// emoji-variation-sequences.txt was taken from. TestEmojiDataVersionMatches
// fails the build when the file's own "# Version:" header disagrees, so a
// refresh has to update both together (see unicodedata/README.md).
const UnicodeVersion = "17.0"

//go:embed unicodedata/emoji-variation-sequences.txt
var emojiVariationSequencesData []byte

// variationSequence is one "<base> <selector>" pair from the data file.
type variationSequence struct {
	base     rune
	selector rune
}

// emojiVariationTable is the parsed data file: every listed sequence keyed by
// its pair, with the character name the file records for the base. bases
// counts distinct base characters, which is what the file's
// "#Total sequences:" trailer reports (each base has a text and an emoji
// line).
type emojiVariationTable struct {
	sequences map[variationSequence]string
	bases     map[rune]struct{}
	version   string
	declared  int
}

var (
	emojiTableOnce sync.Once
	emojiTable     *emojiVariationTable
)

func loadEmojiVariationTable() *emojiVariationTable {
	emojiTableOnce.Do(func() {
		table, err := parseEmojiVariationSequences(emojiVariationSequencesData)
		if err != nil {
			// The file is embedded at build time and covered by tests, so a
			// parse failure is a build defect rather than a runtime condition.
			panic(fmt.Sprintf("markdownscan: embedded emoji-variation-sequences.txt: %v", err))
		}
		emojiTable = table
	})
	return emojiTable
}

// parseEmojiVariationSequences reads the Unicode data file format:
//
//	26A0 FE0F  ; emoji style; # (4.0) WARNING SIGN
//
// Comment lines carry the "# Version:" header and the "#Total sequences:"
// trailer, which are kept so tests can check the vendored copy for drift.
func parseEmojiVariationSequences(data []byte) (*emojiVariationTable, error) {
	table := &emojiVariationTable{
		sequences: make(map[variationSequence]string),
		bases:     make(map[rune]struct{}),
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			parseEmojiDataComment(table, line)
			continue
		}
		seq, name, err := parseEmojiDataLine(line)
		if err != nil {
			return nil, err
		}
		table.sequences[seq] = name
		table.bases[seq.base] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(table.sequences) == 0 {
		return nil, fmt.Errorf("no variation sequences found")
	}
	return table, nil
}

func parseEmojiDataComment(table *emojiVariationTable, line string) {
	if v, ok := strings.CutPrefix(line, "# Version:"); ok {
		table.version = strings.TrimSpace(v)
		return
	}
	if v, ok := strings.CutPrefix(line, "#Total sequences:"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			table.declared = n
		}
	}
}

func parseEmojiDataLine(line string) (variationSequence, string, error) {
	fields, comment, _ := strings.Cut(line, "#")
	parts := strings.Split(fields, ";")
	codepoints := strings.Fields(parts[0])
	if len(codepoints) != 2 {
		return variationSequence{}, "", fmt.Errorf("expected two codepoints, got %q", line)
	}
	base, err := parseCodepoint(codepoints[0])
	if err != nil {
		return variationSequence{}, "", fmt.Errorf("%q: %w", line, err)
	}
	selector, err := parseCodepoint(codepoints[1])
	if err != nil {
		return variationSequence{}, "", fmt.Errorf("%q: %w", line, err)
	}
	if selector != '\ufe0e' && selector != '\ufe0f' {
		return variationSequence{}, "", fmt.Errorf("%q: selector U+%04X is not VS15 or VS16", line, selector)
	}
	return variationSequence{base: base, selector: selector}, parseEmojiDataName(comment), nil
}

// parseEmojiDataName strips the "(1.1)" age marker the file puts before the
// character name in each trailing comment.
func parseEmojiDataName(comment string) string {
	comment = strings.TrimSpace(comment)
	if _, rest, ok := strings.Cut(comment, ")"); ok && strings.HasPrefix(comment, "(") {
		comment = strings.TrimSpace(rest)
	}
	return comment
}

func parseCodepoint(s string) (rune, error) {
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("bad codepoint %q", s)
	}
	return rune(n), nil
}

// isEmojiVariationSelector reports whether r is VS15 (text presentation) or
// VS16 (emoji presentation), the only two selectors the data file covers.
func isEmojiVariationSelector(r rune) bool {
	return r == '\ufe0e' || r == '\ufe0f'
}

// emojiVariationSequenceName returns the base character's name when
// base+selector is a sequence listed in emoji-variation-sequences.txt.
func emojiVariationSequenceName(base, selector rune) (string, bool) {
	name, ok := loadEmojiVariationTable().sequences[variationSequence{base: base, selector: selector}]
	return name, ok
}
