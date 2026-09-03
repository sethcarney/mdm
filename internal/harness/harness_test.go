package harness

import (
	"path/filepath"
	"testing"
)

// SkillsInstallDir is the single resolution the installer, the scope
// re-materializer and install-mode inference all read, so its shape is
// pinned here rather than in any one of them.
func TestSkillsInstallDirProjectScope(t *testing.T) {
	cwd := t.TempDir()
	cfg := AllHarnesses["claude-code"]
	if cfg == nil || cfg.SharedSkillsDir {
		t.Skip("fixture harness no longer has its own project skills directory")
	}
	want := filepath.Join(cwd, cfg.SkillsDir)
	if got := SkillsInstallDir("claude-code", false, cwd); got != want {
		t.Errorf("SkillsInstallDir = %q, want %q", got, want)
	}
}

// A shared-skills-dir harness has no directory of its own: it reads the
// canonical directory, which is what callers have to recognize before
// treating a path as convertible evidence of an install mode.
func TestSkillsInstallDirSharedHarnessIsCanonical(t *testing.T) {
	cwd := t.TempDir()
	if !UsesSharedSkillsDir("amp") {
		t.Skip("fixture harness no longer uses the shared skills directory")
	}
	want := CanonicalSkillsDir(false, cwd)
	if got := SkillsInstallDir("amp", false, cwd); got != want {
		t.Errorf("SkillsInstallDir = %q, want the canonical directory %q", got, want)
	}
	if want != filepath.Join(cwd, SharedRootDir, SkillsSubdir) {
		t.Errorf("canonical directory = %q, want %q", want, filepath.Join(cwd, SharedRootDir, SkillsSubdir))
	}
}

func TestSkillsInstallDirGlobalScopeIgnoresCwd(t *testing.T) {
	cfg := AllHarnesses["claude-code"]
	if cfg == nil || cfg.GlobalSkillsDir == "" {
		t.Skip("fixture harness no longer supports global installs")
	}
	if got := SkillsInstallDir("claude-code", true, t.TempDir()); got != cfg.GlobalSkillsDir {
		t.Errorf("SkillsInstallDir = %q, want %q", got, cfg.GlobalSkillsDir)
	}
}

func TestSkillsInstallDirUnknownHarness(t *testing.T) {
	if got := SkillsInstallDir("no-such-harness", false, t.TempDir()); got != "" {
		t.Errorf("SkillsInstallDir = %q, want empty for an unknown harness", got)
	}
}
