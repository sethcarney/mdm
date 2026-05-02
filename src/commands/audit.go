package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

type AuditOptions struct {
	Global  bool
	Project bool
	JSON    bool
}

type auditSkillResult struct {
	Name          string          `json:"name"`
	Scope         string          `json:"scope"`
	SourceType    string          `json:"sourceType"`
	Source        string          `json:"source"`
	InstalledAt   string          `json:"installedAt,omitempty"`
	UpdatedAt     string          `json:"updatedAt,omitempty"`
	SyncStatus    string          `json:"syncStatus"`
	AdvisoryCount int             `json:"advisoryCount"`
	MaxSeverity   string          `json:"maxSeverity,omitempty"`
	Advisories    []auditAdvisory `json:"advisories,omitempty"`
}

type auditAdvisory struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"`
}

func buildAuditCmd() *cobra.Command {
	var opts AuditOptions

	cmd := &cobra.Command{
		Use:   "audit [skills...]",
		Short: "Audit installed skills for updates and security advisories",
		Long: fmt.Sprintf(`Audit installed skills for sync status and known security advisories.

%sExamples:%s
  mdm skills audit
  mdm skills audit -g
  mdm skills audit --json`, ansiBold, ansiReset),
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runAudit(args, opts)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opts.Global, "global", "g", false, "Audit global skills only")
	f.BoolVarP(&opts.Project, "project", "p", false, "Audit project skills only")
	f.BoolVar(&opts.JSON, "json", false, "Output as JSON")

	return cmd
}

func runAudit(skillFilter []string, opts AuditOptions) {
	global := opts.Global
	project := opts.Project
	if !global && !project {
		global = true
		project = true
	}

	var results []auditSkillResult

	if global {
		l := lock.ReadSkillLock()
		for sName, entry := range l.Skills {
			if !matchesFilter(sName, entry.PluginName, skillFilter) {
				continue
			}
			results = append(results, auditEntry(sName, "global", entry))
		}
	}

	if project {
		cwd, _ := os.Getwd()
		localLock := lock.ReadLocalLock(cwd)
		for sName, entry := range localLock.Skills {
			if !matchesFilterSimple(sName, skillFilter) {
				continue
			}
			// Convert LocalSkillLockEntry to SkillLockEntry for uniform handling
			globalEntry := lock.SkillLockEntry{
				Source:     entry.Source,
				SourceType: entry.SourceType,
				Ref:        entry.Ref,
			}
			results = append(results, auditEntry(sName, "project", globalEntry))
		}
	}

	if len(results) == 0 {
		if opts.JSON {
			fmt.Println("[]")
			return
		}
		fmt.Printf("%sNo skills to audit.%s\n", ansiDim, ansiReset)
		return
	}

	// Enrich with sync status and security info
	if !opts.JSON {
		fmt.Printf("\n%sAuditing %d skill(s)...%s\n\n", ansiDim, len(results), ansiReset)
	}

	for i := range results {
		r := &results[i]
		var spin *ui.Spinner
		if !opts.JSON {
			spin = ui.NewSpinner(fmt.Sprintf("Checking %s...", r.Name))
		}
		enrichAuditResult(r)
		if spin != nil {
			spin.Stop("")
		}
	}

	if opts.JSON {
		out, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(out))
		return
	}

	printAuditResults(results)
}

func auditEntry(name, scope string, entry lock.SkillLockEntry) auditSkillResult {
	return auditSkillResult{
		Name:        name,
		Scope:       scope,
		SourceType:  entry.SourceType,
		Source:      entry.Source,
		InstalledAt: entry.InstalledAt,
		UpdatedAt:   entry.UpdatedAt,
		SyncStatus:  "unknown",
	}
}

func enrichAuditResult(r *auditSkillResult) {
	isGitSource := r.SourceType == string(source.SourceTypeGitHub) ||
		r.SourceType == string(source.SourceTypeGitLab) ||
		r.SourceType == string(source.SourceTypeGit)

	if !isGitSource {
		r.SyncStatus = "local"
		return
	}

	parsed := source.ParseSource(r.Source)
	ownerRepo := source.GetOwnerRepo(parsed)

	// Sync check
	entry := lock.SkillLockEntry{
		Source:          r.Source,
		SourceType:      r.SourceType,
		SkillFolderHash: "", // will cause false if empty, checked inside checkSkillUpToDate
	}
	_ = entry

	// Re-read the full entry from lock to get SkillFolderHash and SkillPath
	globalLock := lock.ReadSkillLock()
	if e, ok := globalLock.Skills[r.Name]; ok {
		upToDate, err := checkSkillUpToDate(r.Name, e)
		if err != nil {
			r.SyncStatus = "unknown"
		} else if upToDate {
			r.SyncStatus = "up-to-date"
		} else {
			r.SyncStatus = "outdated"
		}
	} else {
		// Project-scope skill: no hash stored, mark as unchecked
		r.SyncStatus = "unchecked"
	}

	// Security check
	if ownerRepo != "" {
		osvResult := registry.FetchOSVAdvisories(ownerRepo, 5000)
		if osvResult != nil {
			r.AdvisoryCount = osvResult.Count
			if osvResult.Count > 0 {
				r.MaxSeverity = string(osvResult.MaxSeverity)
				for _, a := range osvResult.Advisories {
					r.Advisories = append(r.Advisories, auditAdvisory{
						ID:       a.ID,
						Summary:  a.Summary,
						Severity: string(a.Severity),
					})
				}
			}
		}
	}
}

func printAuditResults(results []auditSkillResult) {
	// Group by scope
	byScope := map[string][]auditSkillResult{}
	for _, r := range results {
		byScope[r.Scope] = append(byScope[r.Scope], r)
	}

	for _, scope := range []string{"project", "global"} {
		scopeResults, ok := byScope[scope]
		if !ok {
			continue
		}
		scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
		fmt.Printf("%s%s skills:%s\n\n", ansiText, scopeTitle, ansiReset)

		for _, r := range scopeResults {
			// Name + sync badge
			syncBadge, syncColor := syncBadge(r.SyncStatus)
			fmt.Printf("  %s%s%s  %s%s%s\n", ansiBold, r.Name, ansiReset, syncColor, syncBadge, ansiReset)

			// Source info
			fmt.Printf("    %ssource:%s %s%s%s\n", ansiDim, ansiReset, ansiDim, r.Source, ansiReset)

			// Dates
			if r.UpdatedAt != "" {
				fmt.Printf("    %supdated:%s %s%s%s\n", ansiDim, ansiReset, ansiDim, formatDate(r.UpdatedAt), ansiReset)
			}

			// Security
			secStr, secColor := securityBadge(r.AdvisoryCount, r.MaxSeverity)
			fmt.Printf("    %ssecurity:%s %s%s%s\n", ansiDim, ansiReset, secColor, secStr, ansiReset)

			// Advisory details
			if r.AdvisoryCount > 0 {
				for _, a := range r.Advisories {
					sevColor := severityColor(a.Severity)
					summary := a.Summary
					if len(summary) > 80 {
						summary = summary[:77] + "..."
					}
					fmt.Printf("      %s%s%s %s(%s)%s %s%s%s\n",
						ansiDim, a.ID, ansiReset,
						sevColor, a.Severity, ansiReset,
						ansiDim, summary, ansiReset)
				}
			}
			fmt.Println()
		}
	}

	// Summary line
	total := len(results)
	outdated := 0
	advisories := 0
	for _, r := range results {
		if r.SyncStatus == "outdated" {
			outdated++
		}
		if r.AdvisoryCount > 0 {
			advisories++
		}
	}
	fmt.Printf("%sAudit complete:%s %d skill(s)", ansiText, ansiReset, total)
	if outdated > 0 {
		fmt.Printf(", %s%d outdated%s", ansiYellow, outdated, ansiReset)
	}
	if advisories > 0 {
		fmt.Printf(", %s%d with advisories%s", ansiRed, advisories, ansiReset)
	}
	if outdated == 0 && advisories == 0 {
		fmt.Printf(", %sall clear%s", ansiGreen, ansiReset)
	}
	fmt.Println()
	fmt.Println()
}

func syncBadge(status string) (string, string) {
	switch status {
	case "up-to-date":
		return "✓ up to date", ansiGreen
	case "outdated":
		return "↑ outdated", ansiYellow
	case "local":
		return "~ local", ansiDim
	case "unchecked":
		return "? unchecked", ansiDim
	default:
		return "? unknown", ansiDim
	}
}

func securityBadge(count int, maxSeverity string) (string, string) {
	if count == 0 {
		return "no advisories", ansiGreen
	}
	label := fmt.Sprintf("%d advisor%s", count, map[bool]string{true: "y", false: "ies"}[count == 1])
	if maxSeverity != "" {
		label += " (" + maxSeverity + ")"
	}
	return label, severityColor(maxSeverity)
}

func severityColor(sev string) string {
	switch strings.ToUpper(sev) {
	case "CRITICAL", "HIGH":
		return ansiRed
	case "MEDIUM":
		return ansiYellow
	default:
		return ansiDim
	}
}

func formatDate(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("2006-01-02")
}

func matchesFilter(name, pluginName string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if strings.EqualFold(name, f) || strings.EqualFold(pluginName, f) {
			return true
		}
	}
	return false
}

func matchesFilterSimple(name string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if strings.EqualFold(name, f) {
			return true
		}
	}
	return false
}

// fetchSkillsShInfo queries skills.sh for description/stars enrichment.
func fetchSkillsShInfo(name string) *FindSkillResult {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := findAPIURL + "?q=" + name
	body, status, err := registry.HttpGetText(ctx, url)
	if err != nil || status != 200 {
		return nil
	}

	var wrapped struct {
		Skills []FindSkillResult `json:"skills"`
	}
	if err := json.Unmarshal([]byte(body), &wrapped); err != nil {
		return nil
	}
	for _, s := range wrapped.Skills {
		if strings.EqualFold(s.Name, name) {
			return &s
		}
	}
	return nil
}
