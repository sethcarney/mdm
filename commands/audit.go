package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

const auditAPIBase = "https://skills.sh/api/v1/skills"

// ── API types ──────────────────────────────────────────────

type skillsShAuditResponse struct {
	ID     string          `json:"id"`
	Source string          `json:"source"`
	Slug   string          `json:"slug"`
	Audits []skillsShAudit `json:"audits"`
}

type skillsShAudit struct {
	Provider   string   `json:"provider"`
	Slug       string   `json:"slug"`
	Status     string   `json:"status"` // pass / warn / fail
	Summary    string   `json:"summary"`
	AuditedAt  string   `json:"auditedAt"`
	RiskLevel  string   `json:"riskLevel,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// ── Result types ───────────────────────────────────────────

type auditSkillResult struct {
	Name             string          `json:"name"`
	Scope            string          `json:"scope"`
	SourceType       string          `json:"sourceType"`
	Source           string          `json:"source"`
	UpdatedAt        string          `json:"updatedAt,omitempty"`
	SyncStatus       string          `json:"syncStatus"` // up-to-date / outdated / unknown / local / unchecked
	Audits           []auditProvider `json:"audits,omitempty"`
	SkillID          string          `json:"skillId,omitempty"`
	RegistryError    bool            `json:"registryError,omitempty"` // true when lookup failed (network/server error)
}

type auditProvider struct {
	Provider  string `json:"provider"`
	Slug      string `json:"slug,omitempty"`
	Status    string `json:"status"`
	RiskLevel string `json:"riskLevel,omitempty"`
	Summary   string `json:"summary,omitempty"`
	AuditedAt string `json:"auditedAt,omitempty"`
}

// ── Command ────────────────────────────────────────────────

type AuditOptions struct {
	Global  bool
	Project bool
	JSON    bool
}

func buildAuditCmd() *cobra.Command {
	var opts AuditOptions

	cmd := &cobra.Command{
		Use:   "audit [skills...]",
		Short: "Audit installed skills for updates and security advisories",
		Long: fmt.Sprintf(`Audit installed skills for sync status and security audit results.

Security data is sourced from skills.sh, which aggregates results from
Gen Agent Trust Hub, Socket, Snyk, Runlayer, and ZeroLeaks.

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

// ── Run ────────────────────────────────────────────────────

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
			r := auditEntryFromGlobal(sName, entry)
			results = append(results, r)
		}
	}

	if project {
		cwd, _ := os.Getwd()
		localLock := lock.ReadLocalLock(cwd)
		for sName, entry := range localLock.Skills {
			if !matchesFilter(sName, "", skillFilter) {
				continue
			}
			r := auditEntryFromLocal(sName, entry)
			results = append(results, r)
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

	if !opts.JSON {
		fmt.Printf("\n%sAuditing %d skill(s)...%s\n\n", ansiDim, len(results), ansiReset)
	}

	for i := range results {
		r := &results[i]
		var spin *ui.Spinner
		if !opts.JSON {
			spin = ui.NewSpinner(fmt.Sprintf("Checking %s...", r.Name))
		}
		enrichResult(r)
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

	// Offer summaries if any security data exists and stdout is a TTY
	hasSecurity := false
	for _, r := range results {
		if len(r.Audits) > 0 {
			hasSecurity = true
			break
		}
	}
	if hasSecurity && term.IsTerminal(os.Stdout.Fd()) {
		fmt.Printf("%s[s]%s show audit summaries  %s[any key]%s exit  ", ansiText, ansiReset, ansiDim, ansiReset)
		if pressedS() {
			fmt.Println()
			printAuditSummaries(results)
		} else {
			fmt.Println()
		}
	}
}

// ── Entry builders ─────────────────────────────────────────

func auditEntryFromGlobal(name string, entry lock.SkillLockEntry) auditSkillResult {
	r := auditSkillResult{
		Name:       name,
		Scope:      "global",
		SourceType: entry.SourceType,
		Source:     entry.Source,
		UpdatedAt:  entry.UpdatedAt,
		SyncStatus: "unknown",
	}
	if entry.PluginName != "" {
		parsed := source.ParseSource(entry.Source)
		ownerRepo := source.GetOwnerRepo(parsed)
		if ownerRepo != "" {
			r.SkillID = ownerRepo + "/" + blob.ToSkillSlug(entry.PluginName)
		}
	}
	return r
}

func auditEntryFromLocal(name string, entry lock.LocalSkillLockEntry) auditSkillResult {
	return auditSkillResult{
		Name:       name,
		Scope:      "project",
		SourceType: entry.SourceType,
		Source:     entry.Source,
		SyncStatus: "unknown",
	}
}

// ── Enrichment ─────────────────────────────────────────────

func enrichResult(r *auditSkillResult) {
	isGitSource := r.SourceType == string(source.SourceTypeGitHub) ||
		r.SourceType == string(source.SourceTypeGitLab) ||
		r.SourceType == string(source.SourceTypeGit)

	if !isGitSource {
		r.SyncStatus = "local"
		return
	}

	// Sync status: only works for global GitHub skills that have a stored hash
	if r.Scope == "global" && r.SourceType == string(source.SourceTypeGitHub) {
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
		}
	} else {
		r.SyncStatus = "unchecked"
	}

	// Security: query skills.sh audit endpoint
	if r.SkillID != "" {
		audits, registryErr := fetchSkillAudits(r.SkillID)
		r.Audits = audits
		r.RegistryError = registryErr
	} else if r.Scope == "project" {
		// Try to derive a skill ID for project skills
		parsed := source.ParseSource(r.Source)
		ownerRepo := source.GetOwnerRepo(parsed)
		if ownerRepo != "" {
			r.SkillID = ownerRepo + "/" + blob.ToSkillSlug(r.Name)
			audits, registryErr := fetchSkillAudits(r.SkillID)
			r.Audits = audits
			r.RegistryError = registryErr
		}
	}
}

func fetchSkillAudits(skillID string) ([]auditProvider, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	url := auditAPIBase + "/audit/" + skillID
	body, status, err := registry.HttpGetText(ctx, url)
	if err != nil {
		// Network / timeout error — not a definitive "not found"
		return nil, true
	}
	if status == 200 {
		var resp skillsShAuditResponse
		if jsonErr := json.Unmarshal([]byte(body), &resp); jsonErr != nil {
			return nil, true
		}
		if len(resp.Audits) > 0 {
			var providers []auditProvider
			for _, a := range resp.Audits {
				providers = append(providers, auditProvider{
					Provider:  a.Provider,
					Slug:      a.Slug,
					Status:    a.Status,
					RiskLevel: a.RiskLevel,
					Summary:   a.Summary,
					AuditedAt: a.AuditedAt,
				})
			}
			return providers, false
		}
		// 200 but empty audits — not in registry
		return nil, false
	}
	if status >= 500 {
		// Server-side error — treat as a lookup failure, not "not in registry"
		return nil, true
	}

	// Fallback: query OSV for GitHub advisories (on non-200/non-5xx, e.g. 404)
	// skillID format is "owner/repo/slug" — we need just "owner/repo"
	parts := strings.SplitN(skillID, "/", 3)
	if len(parts) < 2 {
		return nil, false
	}
	ownerRepo := parts[0] + "/" + parts[1]
	osvResult := registry.FetchOSVAdvisories(ownerRepo, 5000)
	if osvResult == nil || osvResult.Count == 0 {
		return nil, false
	}

	status2 := "pass"
	if osvResult.MaxSeverity == registry.OSVHigh || osvResult.MaxSeverity == registry.OSVCritical {
		status2 = "fail"
	} else if osvResult.MaxSeverity == registry.OSVMedium || osvResult.MaxSeverity == registry.OSVLow {
		status2 = "warn"
	}
	summary := fmt.Sprintf("%d advisory(s) found via OSV", osvResult.Count)
	if len(osvResult.Advisories) > 0 && osvResult.Advisories[0].Summary != "" {
		summary = osvResult.Advisories[0].Summary
	}
	return []auditProvider{{
		Provider:  "OSV",
		Status:    status2,
		RiskLevel: string(osvResult.MaxSeverity),
		Summary:   summary,
	}}, false
}

// ── Output ─────────────────────────────────────────────────

func printAuditResults(results []auditSkillResult) {
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
			syncStr, syncColor := syncBadge(r.SyncStatus)
			fmt.Printf("  %s%s%s\n", ansiBold, r.Name, ansiReset)
			fmt.Printf("    %ssync:%s     %s%s%s\n", ansiDim, ansiReset, syncColor, syncStr, ansiReset)

			if len(r.Audits) == 0 {
				if r.RegistryError {
					fmt.Printf("    %ssecurity:%s %slookup unavailable%s\n", ansiDim, ansiReset, ansiDim, ansiReset)
				} else if r.SourceType == string(source.SourceTypeGitHub) || r.SourceType == string(source.SourceTypeGitLab) {
					fmt.Printf("    %ssecurity:%s %snot in registry%s\n", ansiDim, ansiReset, ansiDim, ansiReset)
				}
			} else {
				fmt.Printf("    %ssecurity:%s %s\n", ansiDim, ansiReset, formatAuditLine(r.Audits))
			}

			if r.UpdatedAt != "" {
				fmt.Printf("    %ssource:%s   %s%s%s\n", ansiDim, ansiReset, ansiDim, r.Source, ansiReset)
				fmt.Printf("    %supdated:%s  %s%s%s\n", ansiDim, ansiReset, ansiDim, formatDate(r.UpdatedAt), ansiReset)
			} else {
				fmt.Printf("    %ssource:%s   %s%s%s\n", ansiDim, ansiReset, ansiDim, r.Source, ansiReset)
			}
			fmt.Println()
		}
	}

	// Summary
	total := len(results)
	outdated, warned, failed := 0, 0, 0
	for _, r := range results {
		if r.SyncStatus == "outdated" {
			outdated++
		}
		for _, a := range r.Audits {
			if a.Status == "fail" {
				failed++
				break
			} else if a.Status == "warn" {
				warned++
				break
			}
		}
	}

	fmt.Printf("%sAudit complete:%s %d skill(s)", ansiText, ansiReset, total)
	if outdated > 0 {
		fmt.Printf(", %s%d outdated%s", ansiYellow, outdated, ansiReset)
	}
	if failed > 0 {
		fmt.Printf(", %s%d security fail%s", ansiRed, failed, ansiReset)
	}
	if warned > 0 {
		fmt.Printf(", %s%d security warn%s", ansiYellow, warned, ansiReset)
	}
	if outdated == 0 && failed == 0 && warned == 0 {
		fmt.Printf(", %sall clear%s", ansiGreen, ansiReset)
	}
	fmt.Println()
	fmt.Println()
}

func printAuditSummaries(results []auditSkillResult) {
	fmt.Printf("%sSecurity summaries:%s\n\n", ansiBold, ansiReset)
	any := false
	for _, r := range results {
		if len(r.Audits) == 0 {
			continue
		}
		any = true
		fmt.Printf("  %s%s%s\n", ansiBold, r.Name, ansiReset)
		for _, a := range r.Audits {
			statusColor := auditStatusColor(a.Status)
			badge := auditStatusBadge(a.Status)
			rl := ""
			if a.RiskLevel != "" && a.RiskLevel != "NONE" {
				rl = fmt.Sprintf("  %s[%s]%s", riskLevelColor(a.RiskLevel), a.RiskLevel, ansiReset)
			}
			fmt.Printf("    %s%s%s %s%-20s%s%s\n",
				statusColor, badge, ansiReset,
				ansiDim, a.Provider, ansiReset,
				rl)
			if a.Summary != "" {
				fmt.Printf("         %s%s%s\n", ansiDim, a.Summary, ansiReset)
			}
			if a.Slug != "" && r.SkillID != "" {
				url := fmt.Sprintf("https://skills.sh/%s/security/%s", r.SkillID, a.Slug)
				fmt.Printf("         %s%s%s\n", ansiDim, url, ansiReset)
			}
			if a.AuditedAt != "" {
				fmt.Printf("         %saudited: %s%s\n", ansiDim, formatDate(a.AuditedAt), ansiReset)
			}
		}
		fmt.Println()
	}
	if !any {
		fmt.Printf("%sNo security data available.%s\n\n", ansiDim, ansiReset)
	}
}

// formatAuditLine renders a compact one-line summary of all provider results.
func formatAuditLine(audits []auditProvider) string {
	var parts []string
	for _, a := range audits {
		color := auditStatusColor(a.Status)
		badge := auditStatusBadge(a.Status)
		name := shortProviderName(a.Provider)
		rl := ""
		if a.RiskLevel != "" && a.RiskLevel != "NONE" {
			rl = fmt.Sprintf(" %s[%s]%s", riskLevelColor(a.RiskLevel), a.RiskLevel, ansiReset)
		}
		parts = append(parts, fmt.Sprintf("%s%s%s %s%s%s%s", color, badge, ansiReset, ansiDim, name, ansiReset, rl))
	}
	return strings.Join(parts, "  ")
}

func shortProviderName(full string) string {
	switch full {
	case "Gen Agent Trust Hub":
		return "TrustHub"
	case "Socket":
		return "Socket"
	case "Snyk":
		return "Snyk"
	case "Runlayer":
		return "Runlayer"
	case "ZeroLeaks":
		return "ZeroLeaks"
	}
	return full
}

func auditStatusBadge(status string) string {
	switch status {
	case "pass":
		return "✓"
	case "warn":
		return "▲"
	case "fail":
		return "✗"
	}
	return "?"
}

func auditStatusColor(status string) string {
	switch status {
	case "pass":
		return ansiGreen
	case "warn":
		return ansiYellow
	case "fail":
		return ansiRed
	}
	return ansiDim
}

func riskLevelColor(level string) string {
	switch strings.ToUpper(level) {
	case "CRITICAL", "HIGH":
		return ansiRed
	case "MEDIUM":
		return ansiYellow
	}
	return ansiDim
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

func formatDate(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Format("2006-01-02")
}

// ── Filter helpers ─────────────────────────────────────────

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

// ── Key press ──────────────────────────────────────────────

// pressedS reads a single keypress in raw mode and returns true if it was 's'/'S'.
func pressedS() bool {
	state, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		// fallback: buffered read (requires Enter)
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		return b[0] == 's' || b[0] == 'S'
	}
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
	_ = term.Restore(os.Stdin.Fd(), state)
	return b[0] == 's' || b[0] == 'S'
}
