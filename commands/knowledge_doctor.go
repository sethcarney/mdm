package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/okf"
)

// checkKnowledgeBundles diagnoses installed knowledge bundles against
// knowledge-lock.json: missing directories, content drift since install, and
// OKF conformance errors. Only called when the knowledge experimental gate is
// enabled, so doctor output is unchanged for everyone else.
func checkKnowledgeBundles(cwd string) []doctorIssue {
	lk := lock.ReadKnowledgeLock(cwd)
	if len(lk.Bundles) == 0 {
		return nil
	}

	names := make([]string, 0, len(lk.Bundles))
	for name := range lk.Bundles {
		names = append(names, name)
	}
	sort.Strings(names)

	var issues []doctorIssue
	for _, name := range names {
		issues = append(issues, diagnoseKnowledgeBundle(name, lk.Bundles[name], cwd)...)
	}
	return issues
}

func diagnoseKnowledgeBundle(name string, entry lock.KnowledgeLockEntry, cwd string) []doctorIssue {
	dir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return []doctorIssue{{
			Level:   "error",
			Message: fmt.Sprintf("%s: bundle directory ./%s not found — run `mdm knowledge install` to restore", name, entry.InstallDir),
		}}
	}

	var issues []doctorIssue
	if entry.ContentHash != "" {
		if hash, err := okf.HashBundleDir(dir); err == nil && hash != entry.ContentHash {
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s: modified since install (content hash mismatch) — run `mdm knowledge update %s` to re-fetch", name, name),
			})
		}
	}
	if b, err := okf.LoadBundle(dir); err == nil {
		if conformance := okf.Validate(b); okf.HasErrors(conformance) {
			errs := 0
			for _, issue := range conformance {
				if issue.Severity == okf.SeverityError {
					errs++
				}
			}
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s: %d conformance error(s) — run `mdm knowledge validate ./%s`", name, errs, entry.InstallDir),
			})
		}
	}
	return issues
}
