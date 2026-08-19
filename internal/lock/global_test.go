package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// isolateGlobal points the global state at a fresh temp dir so tests never
// read or write the developer's real state file.
func isolateGlobal(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
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
