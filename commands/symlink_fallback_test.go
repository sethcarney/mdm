package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/skill"
)

// captureStdout runs fn and returns what it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	os.Stdout = orig
	_ = w.Close()
	return <-done
}

// When a symlink cannot be created, the install is copied instead and the
// result says so. Nothing is recorded about it. The one-per-run warning names
// each harness once, however many skills fell back for it, and the summary line
// stops claiming a clean symlink install.
func TestInstallReportsSymlinkFallbackOnce(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	src := filepath.Join(cwd, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: s1\ndescription: d\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}

	orig := symlinkFn
	symlinkFn = func(_, _ string) error { return errors.New("forced symlink failure") }
	t.Cleanup(func() { symlinkFn = orig })

	var fallbacks symlinkFallbacks
	for _, name := range []string{"s1", "s2"} {
		r := installSkillForHarness(&skill.Skill{Name: name, Path: src}, "claude-code", false, InstallModeSymlink)
		if !r.Success {
			t.Fatalf("install of %s failed: %s", name, r.Error)
		}
		if !r.SymlinkFailed {
			t.Fatalf("install of %s did not report the fallback", name)
		}
		fallbacks.note("claude-code", r)
	}
	// A second harness, recorded through the same path a real loop would use.
	fallbacks.note("other-harness", InstallResult{Success: true, SymlinkFailed: true})
	// A result without a fallback is ignored.
	fallbacks.note("cursor", InstallResult{Success: true})

	installed := filepath.Join(cwd, ".claude", "skills", "s1")
	info, err := os.Lstat(installed)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("fallback did not leave a real directory at %s", installed)
	}
	if _, err := os.Stat(filepath.Join(installed, "SKILL.md")); err != nil {
		t.Errorf("fallback copy is missing its content: %v", err)
	}

	out := captureStdout(t, func() {
		printInstallSummary(2, false, []string{"claude-code", "other-harness"}, InstallModeSymlink, &fallbacks)
	})
	for _, want := range []string{
		"symlink mode, copied where symlinks failed",
		"Could not create symlinks for Claude Code, other-harness",
		"still in symlink mode",
		"run with --copy once to record it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary output missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "Could not create symlinks"); n != 1 {
		t.Errorf("warning printed %d times, want once:\n%s", n, out)
	}
	if strings.Contains(out, "Cursor") {
		t.Errorf("a harness that did not fall back is named in the warning:\n%s", out)
	}
}

// Without a fallback the summary is unchanged, and a nil collector is safe,
// so callers that never install anything need not build one.
func TestInstallSummaryIsQuietWithoutFallback(t *testing.T) {
	var empty symlinkFallbacks
	for _, fb := range []*symlinkFallbacks{nil, &empty} {
		out := captureStdout(t, func() {
			printInstallSummary(1, false, []string{"claude-code"}, InstallModeSymlink, fb)
		})
		if !strings.Contains(out, "symlink mode)") {
			t.Errorf("summary line changed without a fallback:\n%s", out)
		}
		if strings.Contains(out, "Could not create symlinks") {
			t.Errorf("warning printed without a fallback:\n%s", out)
		}
	}
}

// The materialized note and the fallback warning describe different causes
// with different remedies. A run whose only real files were written by design
// must not tell the user a symlink failed, nor point at --copy, which cannot
// change a decision the harness itself forces.
func TestAgentSummarySeparatesMaterializedFromSymlinkFailure(t *testing.T) {
	var materialized materializedInstalls
	materialized.note("github-copilot", InstallResult{Success: true, Materialized: true})
	materialized.note("codex", InstallResult{Success: true, Materialized: true})
	// A plain success is ignored, so the note names only what it explains.
	materialized.note("claude-code", InstallResult{Success: true})

	outcome := agentInstallOutcome{
		installed:    1,
		harnesses:    []string{"github-copilot", "codex"},
		fallbacks:    &symlinkFallbacks{},
		materialized: &materialized,
	}
	out := captureStdout(t, func() {
		printAgentInstallSummary(outcome, false, InstallModeSymlink)
	})

	for _, unwanted := range []string{"Could not create symlinks", "--copy", "symlinks failed", "those skills were copied"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a by-design materialization printed %q:\n%s", unwanted, out)
		}
	}
	for _, want := range []string{
		"received a real file rather than a symlink",
		"GitHub Copilot: its agents directory is committed to the repository.",
		"Codex: it reads TOML.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Claude Code") {
		t.Errorf("a harness that took a symlink is named in the materialized note:\n%s", out)
	}
}

// The symlink-failure path is real and unchanged: it is what fires on Windows
// without developer mode. Routing it through the materialized note would drop
// the one piece of advice that does help.
func TestAgentSummaryKeepsTheSymlinkFailureMessage(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(t.TempDir(), "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}

	orig := symlinkFn
	symlinkFn = func(_, _ string) error { return errors.New("forced symlink failure") }
	t.Cleanup(func() { symlinkFn = orig })

	res := installAgentFile(a, "claude-code", false, cwd, InstallModeSymlink)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}
	if !res.SymlinkFailed {
		t.Fatal("the forced failure was not reported as a fallback")
	}
	if res.Materialized {
		t.Error("a symlink that was attempted and refused was reported as a by-design real file")
	}

	var fallbacks symlinkFallbacks
	var materialized materializedInstalls
	fallbacks.note("claude-code", res)
	materialized.note("claude-code", res)
	outcome := agentInstallOutcome{
		installed:    1,
		harnesses:    []string{"claude-code"},
		fallbacks:    &fallbacks,
		materialized: &materialized,
	}
	out := captureStdout(t, func() {
		printAgentInstallSummary(outcome, false, InstallModeSymlink)
	})
	for _, want := range []string{
		"symlink mode, copied where symlinks failed",
		"Could not create symlinks for Claude Code",
		"still in symlink mode",
		"run with --copy once to record it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "received a real file") {
		t.Errorf("a genuine symlink failure was described as a by-design materialization:\n%s", out)
	}
}

// Both causes in one run stay two lists. Merging them would name a harness in
// a warning that does not apply to it.
func TestAgentSummaryReportsBothCausesSeparately(t *testing.T) {
	var fallbacks symlinkFallbacks
	var materialized materializedInstalls
	fallbacks.note("claude-code", InstallResult{Success: true, SymlinkFailed: true})
	materialized.note("codex", InstallResult{Success: true, Materialized: true})

	outcome := agentInstallOutcome{
		installed:    1,
		harnesses:    []string{"claude-code", "codex"},
		fallbacks:    &fallbacks,
		materialized: &materialized,
	}
	out := captureStdout(t, func() {
		printAgentInstallSummary(outcome, false, InstallModeSymlink)
	})
	if !strings.Contains(out, "Could not create symlinks for Claude Code") {
		t.Errorf("the symlink failure lost its own message:\n%s", out)
	}
	if !strings.Contains(out, "Codex: it reads TOML.") {
		t.Errorf("the materialization lost its own message:\n%s", out)
	}
	if strings.Contains(out, "Could not create symlinks for Claude Code, Codex") ||
		strings.Contains(out, "Could not create symlinks for Codex") {
		t.Errorf("Codex is named in the symlink-failure warning:\n%s", out)
	}
}

// The skills installer's wording is shared with the agent path and must not
// drift when the agent path gains a message. Asserted as exact lines, so an
// edit to the shared function is caught here rather than by a user.
func TestSkillsSymlinkFallbackWordingIsUnchanged(t *testing.T) {
	var fallbacks symlinkFallbacks
	fallbacks.note("claude-code", InstallResult{Success: true, SymlinkFailed: true})
	out := captureStdout(t, fallbacks.warn)

	want := "▲ Could not create symlinks for Claude Code; those skills were copied instead.\n" +
		"  The scope is still in symlink mode, so mdm skills install and mdm skills update will try to symlink again.\n" +
		"  If copies are what you want on this machine, run with --copy once to record it.\n"
	got := stripANSI(out)
	if !strings.Contains(got, want) {
		t.Errorf("the shared symlink-fallback wording changed.\ngot:\n%q\nwant it to contain:\n%q", got, want)
	}
}

// stripANSI removes the color escapes the summary writes, so a test can assert
// on the words a user reads rather than on the styling around them.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
