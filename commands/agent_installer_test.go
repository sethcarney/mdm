package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
)

// Copilot loads only .agent.md. Writing plain .md there installs a file the
// harness never reads, which fails silently at use time rather than here.
func TestAgentFileExtHonorsTheHarnessSuffix(t *testing.T) {
	if got := harness.AgentFileExt("github-copilot"); got != ".agent.md" {
		t.Errorf("github-copilot ext = %q, want .agent.md", got)
	}
	if got := harness.AgentFileExt("claude-code"); got != ".md" {
		t.Errorf("claude-code ext = %q, want .md", got)
	}
	if got := harness.AgentFileExt("no-such-harness"); got != ".md" {
		t.Errorf("unknown harness ext = %q, want the .md default", got)
	}
}

// A harness with no agent concept has nowhere to put a definition. It is
// skipped, not failed: installing to five harnesses where one has no agent
// support is a normal thing to do.
func TestInstallAgentFileSkipsAHarnessWithNoAgentDir(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(cwd, "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}

	var without string
	for name, h := range harness.AllHarnesses {
		if h.AgentsInstallDir == "" {
			without = name
			break
		}
	}
	if without == "" {
		t.Skip("every harness supports agent definitions")
	}
	res := installAgentFile(a, without, false, cwd, InstallModeSymlink)
	if res.Success {
		t.Errorf("installing to %q reported success; want a skip", without)
	}

	// The skip has to happen BEFORE the canonical copy is written, not just
	// report failure afterward. If the skip check were moved to run after
	// that write, Success would still be false here and this test would
	// still look like it passed, while a stray canonical file was left on
	// disk with no harness pointing at it.
	canonicalPath := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("canonical file exists at %q despite the skip (stat err=%v); the skip must run before any write", canonicalPath, err)
	}
}

// AgentFileExt is only meaningful if installAgentFile actually names the
// file it writes after it. A test that calls AgentFileExt in isolation
// would keep passing even if installAgentFile ignored the suffix and wrote
// "<name>.md" unconditionally — Copilot would then silently never read any
// installed agent. This test looks at the harness directory on disk instead
// of trusting the InstallResult.
func TestInstallAgentFileWritesTheHarnessRequiredFilename(t *testing.T) {
	tests := []struct {
		harnessName  string
		wantFileName string
		wrongName    string
	}{
		{"github-copilot", "critic.agent.md", "critic.md"},
		{"claude-code", "critic.md", "critic.agent.md"},
	}
	for _, tc := range tests {
		t.Run(tc.harnessName, func(t *testing.T) {
			cwd := t.TempDir()
			src := filepath.Join(t.TempDir(), "critic.md")
			if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}

			res := installAgentFile(a, tc.harnessName, false, cwd, InstallModeCopy)
			if !res.Success {
				t.Fatalf("install to %q failed: %s", tc.harnessName, res.Error)
			}

			harnessDir := harness.AgentsInstallDirFor(tc.harnessName, false, cwd)
			wantPath := filepath.Join(harnessDir, tc.wantFileName)
			if _, err := os.Stat(wantPath); err != nil {
				t.Errorf("%s: expected file at %q, stat error: %v", tc.harnessName, wantPath, err)
			}
			wrongPath := filepath.Join(harnessDir, tc.wrongName)
			if _, err := os.Stat(wrongPath); err == nil {
				t.Errorf("%s: found %q; this harness only reads %q, so this file is silently never seen", tc.harnessName, wrongPath, tc.wantFileName)
			}

			canonicalPath := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
			if _, err := os.Stat(canonicalPath); err != nil {
				t.Errorf("%s: expected canonical copy at %q, stat error: %v", tc.harnessName, canonicalPath, err)
			}
		})
	}
}
