package markdownscan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

type Severity string

const (
	SeverityError Severity = "error"
)

type Category string

const (
	CategoryUnicodeTag      Category = "unicode-tag"
	CategoryBidirectional   Category = "bidirectional-control"
	CategoryZeroWidth       Category = "zero-width"
	CategoryVariation       Category = "variation-selector"
	CategorySoftHyphen      Category = "soft-hyphen"
	CategoryReplacementRune Category = "invalid-utf8"
)

type Finding struct {
	File     string
	Line     int
	Column   int
	Rune     rune
	Category Category
	Severity Severity
	Detail   string
}

type NamedContent struct {
	Path     string
	Contents string
}

type scanPosition struct {
	line   int
	column int
	offset int
	prevCR bool
}

var skipDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
}

func ScanMarkdownFiles(root string) ([]Finding, error) {
	var findings []Finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDirNames[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !isMarkdownPath(path) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		findings = append(findings, ScanMarkdownText(rel, string(data))...)
		return nil
	})
	sortFindings(findings)
	return findings, err
}

func ScanMarkdownPayloads(files []NamedContent) []Finding {
	var findings []Finding
	for _, f := range files {
		if !isMarkdownPath(f.Path) {
			continue
		}
		findings = append(findings, ScanMarkdownText(filepath.ToSlash(f.Path), f.Contents)...)
	}
	sortFindings(findings)
	return findings
}

func ScanMarkdownText(path, content string) []Finding {
	var findings []Finding
	pos := scanPosition{line: 1, column: 1}

	for pos.offset < len(content) {
		r, size := utf8.DecodeRuneInString(content[pos.offset:])
		if isInvalidUTF8Rune(content, pos.offset, r, size) {
			findings = append(findings, newFinding(path, pos.line, pos.column, r, CategoryReplacementRune, "invalid UTF-8 byte sequence"))
			advanceColumn(&pos, size)
			continue
		}
		if advanceNewline(&pos, r, size) {
			continue
		}

		if isUnicodeTag(r) {
			findings = append(findings, scanTagSequence(path, content, &pos, r, size))
			continue
		}

		if pos.offset == 0 && r == '\ufeff' {
			advanceColumn(&pos, size)
			continue
		}
		if cat, detail, ok := classifyRune(r); ok {
			findings = append(findings, newFinding(path, pos.line, pos.column, r, cat, detail))
		}
		advanceColumn(&pos, size)
	}
	return findings
}

func isInvalidUTF8Rune(content string, offset int, r rune, size int) bool {
	return r == utf8.RuneError && size == 1 && !utf8.ValidString(content[offset:offset+size])
}

func advanceColumn(pos *scanPosition, size int) {
	pos.offset += size
	pos.column++
	pos.prevCR = false
}

func advanceNewline(pos *scanPosition, r rune, size int) bool {
	switch r {
	case '\n':
		if !pos.prevCR {
			pos.line++
			pos.column = 1
		}
		pos.offset += size
		pos.prevCR = false
		return true
	case '\r':
		pos.line++
		pos.column = 1
		pos.offset += size
		pos.prevCR = true
		return true
	default:
		pos.prevCR = false
		return false
	}
}

func scanTagSequence(path, content string, pos *scanPosition, firstRune rune, firstSize int) Finding {
	startOffset, startLine, startColumn := pos.offset, pos.line, pos.column
	decoded := strings.Builder{}
	for pos.offset < len(content) {
		next, nextSize := utf8.DecodeRuneInString(content[pos.offset:])
		if !isUnicodeTag(next) {
			break
		}
		if printable, ok := decodeTagRune(next); ok {
			decoded.WriteRune(printable)
		}
		advanceColumn(pos, nextSize)
	}
	detail := "Unicode tag characters can hide ASCII instructions"
	if decoded.Len() > 0 {
		detail = fmt.Sprintf("decoded tag text: %q", truncate(decoded.String(), 80))
	}
	if pos.offset == startOffset {
		advanceColumn(pos, firstSize)
	}
	return newFinding(path, startLine, startColumn, firstRune, CategoryUnicodeTag, detail)
}

func newFinding(path string, line, col int, r rune, cat Category, detail string) Finding {
	return Finding{
		File:     path,
		Line:     line,
		Column:   col,
		Rune:     r,
		Category: cat,
		Severity: SeverityError,
		Detail:   detail,
	}
}

func classifyRune(r rune) (Category, string, bool) {
	switch {
	case isBidirectionalControl(r):
		return CategoryBidirectional, "bidirectional controls can visually reorder markdown text", true
	case isZeroWidth(r):
		return CategoryZeroWidth, "zero-width or invisible format character", true
	case isVariationSelector(r):
		return CategoryVariation, "variation selectors can hide content in rendered text", true
	case r == '\u00ad':
		return CategorySoftHyphen, "soft hyphen is normally invisible unless line-wrapped", true
	default:
		return "", "", false
	}
}

func isMarkdownPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".md")
}

func isUnicodeTag(r rune) bool {
	return r >= 0xe0001 && r <= 0xe007f
}

func decodeTagRune(r rune) (rune, bool) {
	if r >= 0xe0020 && r <= 0xe007e {
		return r - 0xe0000, true
	}
	return 0, false
}

func isBidirectionalControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
}

func isZeroWidth(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u200e', '\u200f', '\u2060', '\ufeff':
		return true
	default:
		return false
	}
}

func isVariationSelector(r rune) bool {
	return (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0xe0100 && r <= 0xe01ef)
}

func truncate(s string, maxRunes int) string {
	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	return string(rs[:maxRunes]) + "..."
}

func sortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Column < findings[j].Column
	})
}
