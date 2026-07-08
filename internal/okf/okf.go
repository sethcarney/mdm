// Package okf parses and validates Open Knowledge Format bundles —
// directories of markdown documents with YAML frontmatter where each file is
// one concept and documents cross-link with ordinary markdown links.
//
// This implementation tracks OKF spec v0.1:
// https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf
package okf

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sethcarney/mdm/internal/skill"
)

// Reserved filenames per the OKF spec: index.md provides hierarchical
// navigation, log.md holds chronological change history. Both are
// navigational and exempt from the required `type` frontmatter field.
var reservedFilenames = map[string]bool{
	"index.md": true,
	"log.md":   true,
}

// Link is a markdown link found in a document body.
type Link struct {
	Target string // raw link target as written
	Line   int    // 1-indexed line in the file
}

// Document is one markdown file in a bundle. RelPath is slash-separated and
// relative to the bundle root; per the spec it is the concept's identity.
type Document struct {
	RelPath     string
	Type        string
	Title       string
	Description string
	Resource    string
	Tags        []string
	Timestamp   string
	Links       []Link
	Body        string
}

// Reserved reports whether the document is a reserved navigational file.
func (d *Document) Reserved() bool {
	return reservedFilenames[path.Base(d.RelPath)]
}

// Bundle is a parsed OKF bundle.
type Bundle struct {
	Root string
	Docs []*Document
}

var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"__pycache__":  true,
}

// LoadBundle parses every markdown document under root into a Bundle.
func LoadBundle(root string) (*Bundle, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}

	b := &Bundle{Root: root}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		doc, err := parseDocument(p, filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b.Docs = append(b.Docs, doc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(b.Docs, func(i, j int) bool { return b.Docs[i].RelPath < b.Docs[j].RelPath })
	return b, nil
}

// linkRe matches inline markdown links and images: [text](target) and
// ![alt](target), capturing the target up to whitespace or the closing paren.
// Reference-style links are not resolved.
var linkRe = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

func parseDocument(fullPath, relPath string) (*Document, error) {
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	data, body := skill.ParseFrontmatter(string(content))

	doc := &Document{
		RelPath:     relPath,
		Type:        stringField(data, "type"),
		Title:       stringField(data, "title"),
		Description: stringField(data, "description"),
		Resource:    stringField(data, "resource"),
		Tags:        stringSliceField(data, "tags"),
		Timestamp:   timestampField(data),
		Body:        body,
	}

	// Frontmatter lines precede the body; link line numbers must be
	// file-relative, so offset by the number of lines the frontmatter consumed.
	offset := strings.Count(string(content), "\n") - strings.Count(body, "\n")
	for i, line := range strings.Split(body, "\n") {
		for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
			doc.Links = append(doc.Links, Link{Target: m[1], Line: offset + i + 1})
		}
	}
	return doc, nil
}

func stringField(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// timestampField returns the timestamp frontmatter value as an RFC 3339
// string. yaml.v3 decodes unquoted ISO 8601 values into time.Time, so both
// quoted and unquoted timestamps normalize to the same representation.
func timestampField(data map[string]interface{}) string {
	if v, ok := data["timestamp"]; ok {
		if t, ok := v.(time.Time); ok {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return stringField(data, "timestamp")
}

func stringSliceField(data map[string]interface{}, key string) []string {
	v, ok := data[key]
	if !ok {
		return nil
	}
	items, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
