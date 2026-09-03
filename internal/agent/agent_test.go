package agent

import (
	"path/filepath"
	"testing"
)

// SkillsInstallDir is the single resolution the installer, the scope
// re-materializer and install-mode inference all read, so its shape is
// pinned here rather than in any one of them.
func TestSkillsInstallDirProjectScope(t *testing.T) {
	cwd := t.TempDir()
	cfg := AllAgents["claude-code"]
	if cfg == nil || cfg.SharedSkillsDir {
		t.Skip("fixture agent no longer has its own project skills directory")
	}
	want := filepath.Join(cwd, cfg.SkillsDir)
	if got := SkillsInstallDir("claude-code", false, cwd); got != want {
		t.Errorf("SkillsInstallDir = %q, want %q", got, want)
	}
}

// A shared-skills-dir agent has no directory of its own: it reads the
// canonical directory, which is what callers have to recognize before
// treating a path as convertible evidence of an install mode.
func TestSkillsInstallDirSharedAgentIsCanonical(t *testing.T) {
	cwd := t.TempDir()
	if !UsesSharedSkillsDir("amp") {
		t.Skip("fixture agent no longer uses the shared skills directory")
	}
	want := CanonicalSkillsDir(false, cwd)
	if got := SkillsInstallDir("amp", false, cwd); got != want {
		t.Errorf("SkillsInstallDir = %q, want the canonical directory %q", got, want)
	}
	if want != filepath.Join(cwd, AgentsDir, SkillsSubdir) {
		t.Errorf("canonical directory = %q, want %q", want, filepath.Join(cwd, AgentsDir, SkillsSubdir))
	}
}

func TestSkillsInstallDirGlobalScopeIgnoresCwd(t *testing.T) {
	cfg := AllAgents["claude-code"]
	if cfg == nil || cfg.GlobalSkillsDir == "" {
		t.Skip("fixture agent no longer supports global installs")
	}
	if got := SkillsInstallDir("claude-code", true, t.TempDir()); got != cfg.GlobalSkillsDir {
		t.Errorf("SkillsInstallDir = %q, want %q", got, cfg.GlobalSkillsDir)
	}
}

func TestSkillsInstallDirUnknownAgent(t *testing.T) {
	if got := SkillsInstallDir("no-such-agent", false, t.TempDir()); got != "" {
		t.Errorf("SkillsInstallDir = %q, want empty for an unknown agent", got)
	}
}
