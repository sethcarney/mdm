package okf

import (
	"fmt"
	"path"
	"strings"
	"time"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one validation finding, anchored to a document when applicable.
type Issue struct {
	File     string   `json:"file,omitempty"`
	Line     int      `json:"line,omitempty"`
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	Message  string   `json:"message"`
}

// Validate checks a bundle against OKF v0.1 conformance rules plus link
// integrity. Errors indicate spec violations; warnings indicate likely
// mistakes that the spec does not forbid.
func Validate(b *Bundle) []Issue {
	if len(b.Docs) == 0 {
		return []Issue{{
			Severity: SeverityError,
			Rule:     "empty-bundle",
			Message:  "no markdown documents found in bundle",
		}}
	}
	var issues []Issue
	issues = append(issues, checkFrontmatter(b)...)
	issues = append(issues, checkLinks(b)...)
	issues = append(issues, checkOrphans(b)...)
	return issues
}

// HasErrors reports whether any issue has error severity.
func HasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			return true
		}
	}
	return false
}

// checkFrontmatter enforces the required `type` field on concept documents
// (reserved navigational files are exempt) and warns on malformed timestamps.
func checkFrontmatter(b *Bundle) []Issue {
	var issues []Issue
	for _, doc := range b.Docs {
		if doc.Type == "" && !doc.Reserved() {
			issues = append(issues, Issue{
				File:     doc.RelPath,
				Severity: SeverityError,
				Rule:     "missing-type",
				Message:  "frontmatter is missing the required `type` field",
			})
		}
		if doc.Timestamp != "" {
			if _, err := time.Parse(time.RFC3339, doc.Timestamp); err != nil {
				issues = append(issues, Issue{
					File:     doc.RelPath,
					Severity: SeverityWarning,
					Rule:     "invalid-timestamp",
					Message:  fmt.Sprintf("timestamp %q is not ISO 8601 / RFC 3339", doc.Timestamp),
				})
			}
		}
	}
	return issues
}

// isExternalLink reports whether the target points outside the bundle by
// scheme rather than by path (URLs, mail links, in-page anchors).
func isExternalLink(target string) bool {
	return strings.Contains(target, "://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "#")
}

// resolveLink normalizes a link target to a slash-separated path relative to
// the bundle root. Targets with a leading slash are root-relative per the OKF
// examples; everything else resolves against the linking document's directory.
// The second return is false when the target escapes the bundle root.
func resolveLink(fromRelPath, target string) (string, bool) {
	target = strings.SplitN(target, "#", 2)[0]
	var resolved string
	if strings.HasPrefix(target, "/") {
		resolved = path.Clean(strings.TrimPrefix(target, "/"))
	} else {
		resolved = path.Join(path.Dir(fromRelPath), target)
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return resolved, false
	}
	return resolved, true
}

// checkLinks verifies that every markdown-to-markdown link resolves to a
// document inside the bundle.
func checkLinks(b *Bundle) []Issue {
	docSet := make(map[string]bool, len(b.Docs))
	for _, doc := range b.Docs {
		docSet[doc.RelPath] = true
	}
	var issues []Issue
	for _, doc := range b.Docs {
		for _, link := range doc.Links {
			if isExternalLink(link.Target) || !strings.HasSuffix(strings.SplitN(link.Target, "#", 2)[0], ".md") {
				continue
			}
			resolved, inside := resolveLink(doc.RelPath, link.Target)
			if !inside {
				issues = append(issues, Issue{
					File:     doc.RelPath,
					Line:     link.Line,
					Severity: SeverityError,
					Rule:     "link-escapes-bundle",
					Message:  fmt.Sprintf("link %q resolves outside the bundle", link.Target),
				})
				continue
			}
			if !docSet[resolved] {
				issues = append(issues, Issue{
					File:     doc.RelPath,
					Line:     link.Line,
					Severity: SeverityError,
					Rule:     "broken-link",
					Message:  fmt.Sprintf("link %q does not resolve to a document in the bundle", link.Target),
				})
			}
		}
	}
	return issues
}

// checkOrphans warns about concept documents that no other document links to.
// Reserved navigational files are exempt: index.md files are entry points and
// log.md files are append-only histories.
func checkOrphans(b *Bundle) []Issue {
	if len(b.Docs) == 1 {
		return nil
	}
	incoming := map[string]bool{}
	for _, doc := range b.Docs {
		for _, link := range doc.Links {
			if isExternalLink(link.Target) {
				continue
			}
			if resolved, inside := resolveLink(doc.RelPath, link.Target); inside && resolved != doc.RelPath {
				incoming[resolved] = true
			}
		}
	}
	var issues []Issue
	for _, doc := range b.Docs {
		if doc.Reserved() || incoming[doc.RelPath] {
			continue
		}
		issues = append(issues, Issue{
			File:     doc.RelPath,
			Severity: SeverityWarning,
			Rule:     "orphaned-document",
			Message:  "no other document links to this concept",
		})
	}
	return issues
}
