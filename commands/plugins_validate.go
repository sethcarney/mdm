package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/plugin"
)

func buildPluginsValidateCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate an Agent Plugin",
		Long: fmt.Sprintf(`Validate a plugin directory against Agent Plugins spec v%s.

Checks the plugin.json manifest (closed schema, name rules), skill
discovery under skills/, and the mcp.json server configuration
(transport union, command and cwd rules, URL constraints).

Exits non-zero when errors are found; warnings alone do not fail
validation.

%sExamples:%s
  mdm plugins validate                # validate the current directory
  mdm plugins validate ./my-plugin
  mdm plugins validate ./my-plugin --json`, pluginsSpecVersion, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}
			runPluginsValidate(path, jsonOut)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the validation report as JSON")
	return cmd
}

type pluginsValidateReport struct {
	Plugin      string         `json:"plugin"`
	SpecVersion string         `json:"specVersion"`
	Skills      int            `json:"skills"`
	Servers     int            `json:"servers"`
	Errors      int            `json:"errors"`
	Warnings    int            `json:"warnings"`
	Issues      []plugin.Issue `json:"issues"`
}

func countPluginIssues(issues []plugin.Issue) (errs, warns int) {
	for _, issue := range issues {
		if issue.Severity == plugin.SeverityError {
			errs++
		} else {
			warns++
		}
	}
	return errs, warns
}

func runPluginsValidate(path string, jsonOut bool) {
	report := plugin.Inspect(path)
	errs, warns := countPluginIssues(report.Issues)
	servers := 0
	if report.MCP != nil {
		servers = len(report.MCP.Servers)
	}
	name := path
	if report.Manifest != nil {
		name = report.Manifest.Name
	}

	if jsonOut {
		out := pluginsValidateReport{
			Plugin:      name,
			SpecVersion: pluginsSpecVersion,
			Skills:      len(report.Skills),
			Servers:     servers,
			Errors:      errs,
			Warnings:    warns,
			Issues:      report.Issues,
		}
		if out.Issues == nil {
			out.Issues = []plugin.Issue{}
		}
		data, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(data))
	} else {
		printPluginsValidateReport(name, report, errs, warns, servers)
	}

	if plugin.HasErrors(report.Issues) {
		os.Exit(1)
	}
}

func printPluginsValidateReport(name string, report *plugin.Report, errs, warns, servers int) {
	fmt.Println()
	for _, issue := range report.Issues {
		sevColor := ansiYellow
		if issue.Severity == plugin.SeverityError {
			sevColor = ansiRed
		}
		loc := issue.File
		if loc == "" {
			loc = report.Root
		}
		fmt.Printf("  %s%-7s%s %s%s%s  %s%s%s  %s\n",
			sevColor, issue.Severity, ansiReset,
			ansiText, loc, ansiReset,
			ansiDim, issue.Rule, ansiReset,
			issue.Message)
	}
	if len(report.Issues) > 0 {
		fmt.Println()
	}
	summary := fmt.Sprintf("%s - %d skill(s), %d MCP server(s)", name, len(report.Skills), servers)
	switch {
	case errs > 0:
		fmt.Printf("%s✗%s %s - %d error(s), %d warning(s)\n\n", ansiRed, ansiReset, summary, errs, warns)
	case warns > 0:
		fmt.Printf("%s✓%s %s valid - %d warning(s)\n\n", ansiGreen, ansiReset, summary, warns)
	default:
		fmt.Printf("%s✓%s %s valid - no issues\n\n", ansiGreen, ansiReset, summary)
	}
}
