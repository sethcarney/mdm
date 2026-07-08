package commands

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/okf"
)

func buildKnowledgeValidateCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate an OKF bundle",
		Long: fmt.Sprintf(`Validate a knowledge bundle against OKF v%s conformance rules.

Checks the required frontmatter (every concept document needs a "type"
field), cross-link integrity (every markdown link must resolve to a
document inside the bundle), and warns about orphaned documents and
malformed timestamps.

Exits non-zero when errors are found; warnings alone do not fail
validation.

%sExamples:%s
  mdm knowledge validate                # validate the current directory
  mdm knowledge validate ./knowledge/sales
  mdm knowledge validate ./knowledge/sales --json`, knowledgeSpecVersion, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path := "."
			if len(args) > 0 {
				path = args[0]
			}
			runKnowledgeValidate(path, jsonOut)
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Print the validation report as JSON")
	return cmd
}

type knowledgeValidateReport struct {
	Bundle      string      `json:"bundle"`
	SpecVersion string      `json:"specVersion"`
	Documents   int         `json:"documents"`
	Errors      int         `json:"errors"`
	Warnings    int         `json:"warnings"`
	Issues      []okf.Issue `json:"issues"`
}

func countKnowledgeIssues(issues []okf.Issue) (errs, warns int) {
	for _, issue := range issues {
		if issue.Severity == okf.SeverityError {
			errs++
		} else {
			warns++
		}
	}
	return errs, warns
}

func runKnowledgeValidate(path string, jsonOut bool) {
	bundle, err := okf.LoadBundle(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err)
		os.Exit(1)
	}
	issues := okf.Validate(bundle)
	errs, warns := countKnowledgeIssues(issues)

	if jsonOut {
		report := knowledgeValidateReport{
			Bundle:      path,
			SpecVersion: knowledgeSpecVersion,
			Documents:   len(bundle.Docs),
			Errors:      errs,
			Warnings:    warns,
			Issues:      issues,
		}
		if report.Issues == nil {
			report.Issues = []okf.Issue{}
		}
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
	} else {
		printKnowledgeValidateReport(bundle, issues, errs, warns)
	}

	if okf.HasErrors(issues) {
		os.Exit(1)
	}
}

func printKnowledgeValidateReport(bundle *okf.Bundle, issues []okf.Issue, errs, warns int) {
	fmt.Println()
	for _, issue := range issues {
		sevColor := ansiYellow
		if issue.Severity == okf.SeverityError {
			sevColor = ansiRed
		}
		loc := issue.File
		if issue.Line > 0 {
			loc = fmt.Sprintf("%s:%d", issue.File, issue.Line)
		}
		if loc == "" {
			loc = bundle.Root
		}
		fmt.Printf("  %s%-7s%s %s%s%s  %s%s%s  %s\n",
			sevColor, issue.Severity, ansiReset,
			ansiText, loc, ansiReset,
			ansiDim, issue.Rule, ansiReset,
			issue.Message)
	}
	if len(issues) > 0 {
		fmt.Println()
	}
	switch {
	case errs > 0:
		fmt.Printf("%s✗%s %d document(s) — %d error(s), %d warning(s)\n\n", ansiRed, ansiReset, len(bundle.Docs), errs, warns)
	case warns > 0:
		fmt.Printf("%s✓%s %d document(s) valid — %d warning(s)\n\n", ansiGreen, ansiReset, len(bundle.Docs), warns)
	default:
		fmt.Printf("%s✓%s %d document(s) valid — no issues\n\n", ansiGreen, ansiReset, len(bundle.Docs))
	}
}
