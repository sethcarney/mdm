// Package plugin implements the vendor-neutral Agent Plugins format
// (https://agent-plugins.org, spec v1.0.0): closed-schema plugin.json
// manifest parsing, skill discovery under skills/, and mcp.json server
// configuration.
//
// Validation is performed locally against the rules below — the spec
// forbids retrieving schemas during plugin loading.
package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const (
	// SpecVersion is the Agent Plugins spec revision this build implements.
	SpecVersion = "1.0.0"

	// ManifestSchema is the canonical schema identifier a supported
	// plugin.json must declare in $schema.
	ManifestSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"

	// MCPSchema is the canonical schema identifier a supported mcp.json
	// must declare in $schema.
	MCPSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

	ManifestFile = "plugin.json"
	MCPFile      = "mcp.json"
	SkillsDir    = "skills"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one validation finding, anchored to a file when applicable.
type Issue struct {
	File     string   `json:"file,omitempty"`
	Severity Severity `json:"severity"`
	Rule     string   `json:"rule"`
	Message  string   `json:"message"`
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

type Author struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Manifest is a parsed plugin.json.
type Manifest struct {
	Schema      string
	Name        string
	Version     string
	Description string
	Author      *Author
	Homepage    string
	Repository  string
	License     string
	Keywords    []string
	// Extensions holds client-specific data keyed by reverse-domain
	// namespace. Contents of unimplemented namespaces are opaque.
	Extensions map[string]json.RawMessage
}

// LoadManifest parses <root>/plugin.json with the spec's closed-schema
// semantics. A non-nil error means the plugin must be rejected (missing or
// unparseable manifest, invalid name, wrong-typed known field). The issues
// carry the spec's non-fatal reports: unknown top-level fields and a
// non-object extensions field, both ignored but surfaced.
func LoadManifest(root string) (*Manifest, []Issue, error) {
	manifestPath := filepath.Join(root, ManifestFile)
	if EscapesRoot(root, manifestPath) {
		return nil, nil, fmt.Errorf("%s resolves outside the plugin root", ManifestFile)
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", ManifestFile, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("%s is not a JSON object: %w", ManifestFile, err)
	}

	m := &Manifest{}
	var issues []Issue
	for _, key := range sortedKeys(raw) {
		if err := setManifestField(m, key, raw[key], &issues); err != nil {
			return nil, nil, err
		}
	}
	if m.Schema == "" {
		return nil, nil, fmt.Errorf("%s is missing the required $schema field", ManifestFile)
	}
	if m.Schema != ManifestSchema {
		return nil, nil, fmt.Errorf("unsupported $schema %q — this build supports %s", m.Schema, ManifestSchema)
	}
	if m.Name == "" {
		return nil, nil, fmt.Errorf("%s is missing the required name field", ManifestFile)
	}
	if err := ValidateName(m.Name); err != nil {
		return nil, nil, fmt.Errorf("invalid plugin name %q: %w", m.Name, err)
	}
	return m, issues, nil
}

// setManifestField applies one top-level plugin.json field to m. Wrong-typed
// known fields are fatal; unknown fields and a non-object extensions value
// are reported as issues and ignored, per the spec.
func setManifestField(m *Manifest, key string, value json.RawMessage, issues *[]Issue) error {
	var dst *string
	switch key {
	case "$schema":
		dst = &m.Schema
	case "name":
		dst = &m.Name
	case "version":
		dst = &m.Version
	case "description":
		dst = &m.Description
	case "homepage":
		dst = &m.Homepage
	case "repository":
		dst = &m.Repository
	case "license":
		dst = &m.License
	case "keywords":
		if err := json.Unmarshal(value, &m.Keywords); err != nil {
			return fmt.Errorf("field keywords must be an array of strings")
		}
		return nil
	case "author":
		author, err := parseAuthor(value)
		if err != nil {
			return err
		}
		m.Author = author
		return nil
	case "extensions":
		if err := json.Unmarshal(value, &m.Extensions); err != nil {
			*issues = append(*issues, Issue{
				File:     ManifestFile,
				Severity: SeverityWarning,
				Rule:     "extensions-not-object",
				Message:  "extensions is not an object — ignored",
			})
			m.Extensions = nil
		}
		return nil
	default:
		*issues = append(*issues, Issue{
			File:     ManifestFile,
			Severity: SeverityWarning,
			Rule:     "unknown-field",
			Message:  fmt.Sprintf("unknown top-level field %q — ignored", key),
		})
		return nil
	}
	if err := json.Unmarshal(value, dst); err != nil {
		return fmt.Errorf("field %s must be a string", key)
	}
	return nil
}

// parseAuthor decodes the author object, which may contain only the
// optional name, email, and url string fields.
func parseAuthor(value json.RawMessage) (*Author, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(value, &raw); err != nil {
		return nil, fmt.Errorf("field author must be an object")
	}
	author := &Author{}
	for _, key := range sortedKeys(raw) {
		var dst *string
		switch key {
		case "name":
			dst = &author.Name
		case "email":
			dst = &author.Email
		case "url":
			dst = &author.URL
		default:
			return nil, fmt.Errorf("author contains unknown field %q", key)
		}
		if err := json.Unmarshal(raw[key], dst); err != nil {
			return nil, fmt.Errorf("author.%s must be a string", key)
		}
	}
	return author, nil
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
