package commands

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestBuildBugURLFields(t *testing.T) {
	report := bugReport{
		Version: "1.93.0",
		OS:      "linux/amd64",
		Shell:   "/bin/zsh",
		Go:      "go1.26.6",
		Agents:  "claude-code, cursor",
		Command: "mdm skills add o/r",
		Logs:    "panic: boom",
	}
	u, err := url.Parse(buildBugURL(report))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("template") != BugTemplateName {
		t.Errorf("template = %q, want %q", q.Get("template"), BugTemplateName)
	}
	for _, id := range BugFieldIDs {
		if q.Get(id) == "" {
			t.Errorf("field %q missing from URL", id)
		}
	}
	// Empty fields are omitted, not sent as empty params.
	q2, _ := url.Parse(buildBugURL(bugReport{Version: "1.93.0"}))
	if q2.Query().Has("shell") || q2.Query().Has("logs") {
		t.Errorf("empty fields should be omitted: %s", q2.RawQuery)
	}
}

func TestBugURLStaysBounded(t *testing.T) {
	report := bugReport{Version: "1.93.0", Logs: strings.Repeat("x", 100_000)}
	if got := len(buildBugURL(report)); got > 4000 {
		t.Errorf("URL length %d exceeds the browser-safe bound", got)
	}
}

func TestScrubHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir in this environment")
	}
	in := home + "/projects/secret-client/main.go"
	out := scrubHome(in)
	if strings.Contains(out, home) {
		t.Errorf("home dir leaked: %q", out)
	}
	if !strings.HasPrefix(out, "~/") {
		t.Errorf("expected ~ substitution, got %q", out)
	}
}
