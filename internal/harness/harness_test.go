package harness

import (
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
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

// All five confirmed registry entries populate both AgentsInstallDir and
// GlobalAgentsInstallDir, so nothing there reaches "global scope asked of a
// harness with a project dir but no global dir", which must return "". A
// synthetic HarnessConfig reaches it. Not parallel: it replaces AllHarnesses.
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

// The registry has to say what a harness reads, not just what extension it
// wants. Copilot is markdown with a custom extension; Codex is a different
// format. Conflating them installs a file the harness ignores.
func TestAgentFormatPerHarness(t *testing.T) {
	tests := []struct {
		harness string
		want    agentfile.Format
	}{
		{"claude-code", agentfile.FormatMarkdown},
		{"github-copilot", agentfile.FormatMarkdown},
		{"codex", agentfile.FormatTOML},
		{"no-such-harness", agentfile.FormatMarkdown},
	}
	for _, tc := range tests {
		if got := AgentFormat(tc.harness); got != tc.want {
			t.Errorf("AgentFormat(%q) = %q, want %q", tc.harness, got, tc.want)
		}
	}
}

// Codex reads .toml files from its own directory, confirmed 2026-09-05.
func TestCodexHasAgentDirectories(t *testing.T) {
	h := AllHarnesses["codex"]
	if h == nil {
		t.Fatal("codex missing from the registry")
	}
	if h.AgentsInstallDir != ".codex/agents" {
		t.Errorf("AgentsInstallDir = %q, want .codex/agents", h.AgentsInstallDir)
	}
	if h.AgentFileSuffix != ".toml" {
		t.Errorf("AgentFileSuffix = %q, want .toml", h.AgentFileSuffix)
	}
	if h.GlobalAgentsInstallDir == "" {
		t.Error("GlobalAgentsInstallDir is empty; ~/.codex/agents is documented")
	}
}

// The name warning used to build its regex by matching words like "digit" in
// the prose, so a harness worded differently silently got no check. The rule
// is now a regexp beside the prose, and the two travel together.
func TestAgentNameRegexpAccompaniesEveryPattern(t *testing.T) {
	for name, h := range AllHarnesses {
		if (h.AgentNamePattern == "") != (h.AgentNameRegexp == nil) {
			t.Errorf("%s: AgentNamePattern %q and AgentNameRegexp %v must be set together", name, h.AgentNamePattern, h.AgentNameRegexp)
		}
	}
	for _, tc := range []struct {
		harness, name string
		ok            bool
	}{
		{"claude-code", "code-reviewer", true},
		{"claude-code", "Code Reviewer", false},
		{"claude-code", "CodeReviewer", false},
		{"claude-code", "a:b", false},
		{"gemini-cli", "review_2", true},
		{"gemini-cli", "Review", false},
	} {
		h := AllHarnesses[tc.harness]
		if h == nil || h.AgentNameRegexp == nil {
			t.Fatalf("%s has no name regexp", tc.harness)
		}
		if got := h.AgentNameRegexp.MatchString(tc.name); got != tc.ok {
			t.Errorf("%s: %q matches = %v, want %v", tc.harness, tc.name, got, tc.ok)
		}
	}
}
