package commands

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
)

// ──────────────────────────────────────────────────────────
// mdm bug — prefilled issue reporting
//
// The command constructs a GitHub issue-form prefill URL and hands it
// over. It does no network I/O, no authentication, and no telemetry —
// nothing leaves the machine until the user submits the form themselves.
// Prior art: `npm bugs`, rustc's ICE handler, `brew gist-logs`.
// ──────────────────────────────────────────────────────────

const bugRepoURL = "https://github.com/sethcarney/mdm"

// BugTemplateName is the issue form the prefill URL targets.
const BugTemplateName = "agent-issue.yml"

// BugFieldIDs are the issue-form field ids `mdm bug` prefills. They must
// match the `id:` values in .github/ISSUE_TEMPLATE/agent-issue.yml —
// tests/bug_template_test.go fails the build when they drift.
var BugFieldIDs = []string{"version", "os", "shell", "go", "agents", "command", "logs"}

// bugLogsLimit caps the error-output query param. Browsers and GitHub
// both truncate very long URLs silently; anything bulky belongs in the
// form's attachment box, not the URL.
const bugLogsLimit = 1500

type bugReport struct {
	Version string
	OS      string
	Shell   string
	Go      string
	Agents  string
	Command string
	Logs    string
}

func buildBugCmd(ver string) *cobra.Command {
	var printOnly bool
	var failedCommand string

	cmd := &cobra.Command{
		Use:   "bug",
		Short: "Report a bug with environment details prefilled",
		Long: fmt.Sprintf(`Open a prefilled GitHub issue form for reporting an mdm bug.

Collects the mdm version, OS and architecture, shell, Go runtime, and
the agent tools detected on this machine, then builds an issue-form URL
with those fields already filled in. Nothing is sent anywhere — the
command only constructs a URL and (when a browser is available) opens
it; you review and submit the form yourself.

Paths are scrubbed ($HOME becomes ~) before entering the URL, and no
repository names or usernames are included.

%sExamples:%s
  mdm bug
  mdm bug --command "mdm skills add owner/repo"
  mdm bug --print`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runBug(collectBugReport(ver, failedCommand), printOnly)
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "Print the issue body and URL without opening a browser")
	cmd.Flags().StringVar(&failedCommand, "command", "", "The mdm command that failed, to include in the report")
	return cmd
}

func collectBugReport(ver, failedCommand string) bugReport {
	shell := os.Getenv("SHELL")
	if shell == "" && runtime.GOOS == "windows" {
		shell = os.Getenv("ComSpec")
	}
	detected := agent.DetectInstalledAgents()
	if len(detected) > 8 {
		detected = append(detected[:8], fmt.Sprintf("(+%d more)", len(detected)-8))
	}
	return bugReport{
		Version: ver,
		OS:      runtime.GOOS + "/" + runtime.GOARCH,
		Shell:   scrubHome(shell),
		Go:      runtime.Version(),
		Agents:  strings.Join(detected, ", "),
		Command: scrubHome(failedCommand),
	}
}

func runBug(report bugReport, printOnly bool) {
	issueURL := buildBugURL(report)

	if printOnly {
		fmt.Print(renderBugBody(report))
		fmt.Printf("\n%s\n", issueURL)
		return
	}

	// The URL is printed unconditionally: headless environments (SSH,
	// containers, WSL2 without a browser bridge) still get something usable
	// even when opening fails silently.
	fmt.Printf("\n%sOpening a prefilled issue form — review before submitting:%s\n", ansiText, ansiReset)
	fmt.Printf("%s\n\n", issueURL)
	openInBrowser(issueURL)
}

// buildBugURL constructs the issue-form prefill URL. Field ids must stay
// in sync with BugFieldIDs and the template.
func buildBugURL(report bugReport) string {
	values := url.Values{}
	values.Set("template", BugTemplateName)
	set := func(id, v string) {
		if v != "" {
			values.Set(id, v)
		}
	}
	set("version", report.Version)
	set("os", report.OS)
	set("shell", report.Shell)
	set("go", report.Go)
	set("agents", report.Agents)
	set("command", report.Command)
	set("logs", truncateForURL(report.Logs))
	return bugRepoURL + "/issues/new?" + values.Encode()
}

func renderBugBody(report bugReport) string {
	var b strings.Builder
	b.WriteString("mdm bug report (nothing has been sent — review, then submit via the URL below)\n\n")
	write := func(label, v string) {
		if v != "" {
			fmt.Fprintf(&b, "  %-10s %s\n", label+":", v)
		}
	}
	write("version", report.Version)
	write("os", report.OS)
	write("shell", report.Shell)
	write("go", report.Go)
	write("agents", report.Agents)
	write("command", report.Command)
	write("logs", report.Logs)
	return b.String()
}

// scrubHome replaces the user's home directory with ~ so no local paths
// leak into the URL or report body.
func scrubHome(s string) string {
	if s == "" {
		return s
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return s
	}
	return strings.ReplaceAll(s, home, "~")
}

func truncateForURL(s string) string {
	if len(s) <= bugLogsLimit {
		return s
	}
	return s[:bugLogsLimit] + "\n… (truncated — attach the full output to the form)"
}

func openInBrowser(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		if _, err := exec.LookPath("xdg-open"); err != nil {
			return // headless — the URL is already printed
		}
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// ──────────────────────────────────────────────────────────
// Panic hook
// ──────────────────────────────────────────────────────────

// HandlePanic is deferred from main. On a panic it writes the full panic
// output to a temp file (bulky data stays out of the URL), prints a
// prefilled bug URL carrying the failing command and the panic's first
// line, and exits non-zero. This catches people at the moment they are
// annoyed enough to report but not enough to fill out a form by hand.
func HandlePanic(ver string) {
	r := recover()
	if r == nil {
		return
	}
	stack := make([]byte, 64*1024)
	stack = stack[:runtime.Stack(stack, false)]
	full := fmt.Sprintf("panic: %v\n\n%s", r, stack)

	fmt.Fprintf(os.Stderr, "\n%smdm crashed.%s This is a bug in mdm, not in your project.\n\n", ansiYellow, ansiReset)

	logsPath := writePanicLog(full)
	if logsPath != "" {
		fmt.Fprintf(os.Stderr, "Full crash output written to %s — attach it to the report.\n\n", scrubHome(logsPath))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n\n", full)
	}

	report := collectBugReport(ver, strings.Join(os.Args, " "))
	report.Logs = scrubHome(firstLine(fmt.Sprintf("panic: %v", r)))
	fmt.Fprintf(os.Stderr, "Report it with this prefilled issue form:\n%s\n", buildBugURL(report))
	os.Exit(2)
}

func writePanicLog(content string) string {
	f, err := os.CreateTemp("", "mdm-crash-*.log")
	if err != nil {
		return ""
	}
	_, werr := f.WriteString(content)
	if cerr := f.Close(); werr != nil || cerr != nil {
		return ""
	}
	return filepath.Clean(f.Name())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
