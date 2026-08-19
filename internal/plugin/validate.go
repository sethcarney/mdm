package plugin

import (
	"errors"
)

// Report is everything mdm knows about a plugin directory after loading it
// with the spec's resilience semantics.
type Report struct {
	Root     string
	Manifest *Manifest // nil when the plugin was rejected
	Skills   []PluginSkill
	// MCP is nil when mcp.json is absent or the MCP component is
	// disabled; MCPDisabled distinguishes the two.
	MCP         *MCPConfig
	MCPDisabled bool
	Issues      []Issue
}

// Inspect loads a plugin directory: manifest first (a fatal manifest error
// rejects the plugin), then skills and MCP servers, each failing
// independently per the spec's resilience rules.
func Inspect(root string) *Report {
	report := &Report{Root: root}
	manifest, issues, err := LoadManifest(root)
	report.Issues = issues
	if err != nil {
		report.Issues = append(report.Issues, Issue{
			File:     ManifestFile,
			Severity: SeverityError,
			Rule:     "invalid-manifest",
			Message:  err.Error(),
		})
		return report
	}
	report.Manifest = manifest

	skills, skillIssues := ListSkills(root)
	report.Skills = skills
	report.Issues = append(report.Issues, skillIssues...)

	mcp, mcpIssues, err := LoadMCPConfig(root)
	report.Issues = append(report.Issues, mcpIssues...)
	if errors.Is(err, ErrMCPDisabled) {
		report.MCPDisabled = true
	} else {
		report.MCP = mcp
	}

	if len(report.Skills) == 0 && report.MCP == nil {
		report.Issues = append(report.Issues, Issue{
			Severity: SeverityWarning,
			Rule:     "empty-plugin",
			Message:  "plugin declares no skills and no MCP servers",
		})
	}
	return report
}

// Validate checks a plugin directory and returns all findings. Errors
// indicate spec violations; warnings indicate ignored or skipped content.
func Validate(root string) []Issue {
	return Inspect(root).Issues
}
