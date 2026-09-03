package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/agent"
)

// isolateGlobal points the global state AND every user-level directory the
// agent registry resolves at a fresh temp home, so a test that touches
// global scope can never read or write the developer's real files.
//
// The state file alone is not enough: global install paths are built from
// the user's home directory, so install-mode inference walking every agent
// the scope supports would otherwise Lstat (and a conversion would rewrite)
// real directories under the developer's home. That has already happened
// once during development, which is why this is not left to each test.
//
// agent.Reload is what makes the redirect stick: the registry resolves every
// global path once, at package init. Its cleanup is registered before the
// t.Setenv calls so it runs after them, rebuilding the registry from the
// restored environment. Tests using this must not run in parallel.
func isolateGlobal(t *testing.T) string {
	t.Helper()
	// agent.Reload below replaces AllAgents wholesale, so calling this after
	// registerTestAgent would discard the injected agent and leave the test
	// asserting against the real registry. Say so here rather than let the
	// test pass for the wrong reason.
	if len(injectedTestAgents) > 0 {
		t.Fatalf("isolateGlobal called after registerTestAgent registered %v: reloading the registry would discard them, so isolate the home first", injectedTestAgentNames())
	}
	t.Cleanup(agent.Reload)

	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere; set
	// both so the isolation does not depend on which platform runs the test.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	agent.Reload()

	return dir
}

func TestGlobalStateRoundTrip(t *testing.T) {
	isolateGlobal(t)

	if s := ReadGlobalState(); len(s.Skills) != 0 {
		t.Fatalf("expected empty state, got %+v", s.Skills)
	}
	if err := AddSkillToGlobalState("my-skill", SkillLockEntry{Source: "o/r", SourceType: "github", SourceURL: "https://github.com/o/r"}); err != nil {
		t.Fatal(err)
	}
	got := ReadGlobalState().Skills["my-skill"]
	if got.Source != "o/r" || got.InstalledAt == "" || got.UpdatedAt == "" {
		t.Fatalf("unexpected entry: %+v", got)
	}

	// Re-adding preserves InstalledAt.
	if err := AddSkillToGlobalState("my-skill", SkillLockEntry{Source: "o/r", SourceType: "github", SourceURL: "https://github.com/o/r", Ref: "v2"}); err != nil {
		t.Fatal(err)
	}
	again := ReadGlobalState().Skills["my-skill"]
	if again.InstalledAt != got.InstalledAt || again.Ref != "v2" {
		t.Errorf("update lost fields: %+v", again)
	}

	if err := RemoveSkillFromGlobalState("my-skill"); err != nil {
		t.Fatal(err)
	}
	if len(ReadGlobalState().Skills) != 0 {
		t.Error("expected no skills after remove")
	}
}

func TestGlobalStatePreservesUnknownKeys(t *testing.T) {
	isolateGlobal(t)
	path := GetGlobalStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"version":1,"skills":{},"futureKey":{"nested":[1,2,3]}}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DismissPrompt("findSkillsPrompt"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["futureKey"]; !ok {
		t.Errorf("futureKey not preserved across a write: %s", data)
	}
	if !IsPromptDismissed("findSkillsPrompt") {
		t.Error("dismissed prompt not recorded")
	}
}

func TestGlobalStateLegacyFallback(t *testing.T) {
	stateDir := isolateGlobal(t)
	legacy := filepath.Join(stateDir, "skills", "skills-lock.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{
  "version": 3,
  "skills": {"old-skill": {"source": "o/r", "sourceType": "github", "sourceUrl": "u", "installedAt": "t", "updatedAt": "t"}},
  "dismissed": {"findSkillsPrompt": true},
  "configuredAgents": ["claude-code"],
  "experimental": ["knowledge"]
}`
	if err := os.WriteFile(legacy, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	s := ReadGlobalState()
	if _, ok := s.Skills["old-skill"]; !ok {
		t.Error("legacy global skills-lock.json not read")
	}
	if !s.Dismissed.FindSkillsPrompt || len(s.ConfiguredAgents) != 1 || len(s.Experimental) != 1 {
		t.Errorf("legacy sections not carried over: %+v", s)
	}

	// A write lands in the new location and takes precedence; the legacy
	// file stays for `mdm migrate` to retire.
	if err := WriteGlobalState(s); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetGlobalStatePath()); err != nil {
		t.Fatalf("expected mdm state file after write: %v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Errorf("legacy file should be left in place until mdm migrate: %v", err)
	}
	if err := os.Remove(legacy); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadGlobalState().Skills["old-skill"]; !ok {
		t.Error("state not readable from the new location")
	}
}

func TestGlobalStateUnreadableErrors(t *testing.T) {
	isolateGlobal(t)
	path := GetGlobalStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":99,"skills":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGlobalStateE(); err == nil {
		t.Error("newer state file should error with an upgrade pointer")
	}
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readGlobalStateE(); err == nil {
		t.Error("corrupt state file should error")
	}
}
