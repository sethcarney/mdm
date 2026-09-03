package commands

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
// each agent once, however many skills fell back for it, and the summary line
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
		r := installSkillForAgent(&skill.Skill{Name: name, Path: src}, "claude-code", false, InstallModeSymlink)
		if !r.Success {
			t.Fatalf("install of %s failed: %s", name, r.Error)
		}
		if !r.SymlinkFailed {
			t.Fatalf("install of %s did not report the fallback", name)
		}
		fallbacks.note("claude-code", r)
	}
	// A second agent, recorded through the same path a real loop would use.
	fallbacks.note("other-agent", InstallResult{Success: true, SymlinkFailed: true})
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
		printInstallSummary(2, false, []string{"claude-code", "other-agent"}, InstallModeSymlink, &fallbacks)
	})
	for _, want := range []string{
		"symlink mode, copied where symlinks failed",
		"Could not create symlinks for Claude Code, other-agent",
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
		t.Errorf("an agent that did not fall back is named in the warning:\n%s", out)
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
