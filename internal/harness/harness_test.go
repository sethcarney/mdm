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

// AgentsInstallDirFor's scope handling is exercised in production only by
// the confirmed registry entries, and all five of those populate both
// AgentsInstallDir and GlobalAgentsInstallDir — so nothing in the registry
// exercises "global scope asked of a harness with a project dir but no
// global dir" (must return "", not fall back to the project dir). A
// synthetic HarnessConfig, not a real registry entry, is needed to reach
// that combination without waiting for a harness that happens to have it.
//
// Not run in parallel: it replaces the package-level AllHarnesses map for
// its duration, and Reload's own doc comment says such tests must not run
// alongside anything else that reads or replaces it.
func TestAgentsInstallDirForScopes(t *testing.T) {
	orig := AllHarnesses
	defer func() { AllHarnesses = orig }()
	AllHarnesses = map[string]*HarnessConfig{
		"both-scopes": {
			AgentsInstallDir:       ".both/agents",
			GlobalAgentsInstallDir: filepath.Join("global", "both", "agents"),
		},
		"project-only": {
			AgentsInstallDir: ".projectonly/agents",
			// GlobalAgentsInstallDir intentionally left empty.
		},
	}

	cwd := filepath.Join("some", "project")

	tests := []struct {
		name        string
		harnessName string
		global      bool
		want        string
	}{
		{"project scope with a project dir", "both-scopes", false, filepath.Join(cwd, ".both", "agents")},
		{"global scope with a global dir", "both-scopes", true, filepath.Join("global", "both", "agents")},
		{"global scope, no global dir despite a project dir", "project-only", true, ""},
		{"unknown harness", "no-such-harness", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AgentsInstallDirFor(tc.harnessName, tc.global, cwd); got != tc.want {
				t.Errorf("AgentsInstallDirFor(%q, global=%v) = %q, want %q", tc.harnessName, tc.global, got, tc.want)
			}
		})
	}
}

// DeepAgents is a product name, not an instance of the concept this branch
// renamed. The blanket agent→harness rename caught it and produced "Deep
// Harnesses", which is a name no user has ever seen. Proper nouns do not
// get renamed by a concept rename.
func TestDeepAgentsKeepsItsProductName(t *testing.T) {
	h, ok := AllHarnesses["deepagents"]
	if !ok {
		t.Fatal("deepagents is missing from AllHarnesses")
	}
	if h.DisplayName != "Deep Agents" {
		t.Errorf("DisplayName = %q, want %q", h.DisplayName, "Deep Agents")
	}
}
