package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectLockPreservesUnknownKeys(t *testing.T) {
	cwd := t.TempDir()
	content := `{
  "version": 1,
  "skills": {
    "my-skill": {"source": "o/r", "sourceType": "github"}
  },
  "futureSection": {"anything": ["the", "codec", "cannot", "parse"]},
  "futureFlag": true
}
`
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// A skills write must round-trip the sections it does not understand.
	if err := AddSkillToLocalLock("other", LocalSkillLockEntry{Source: "a/b", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(GetProjectLockPath(cwd))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["futureFlag"]) != "true" {
		t.Errorf("futureFlag not preserved: %s", raw["futureFlag"])
	}
	var future map[string][]string
	if err := json.Unmarshal(raw["futureSection"], &future); err != nil || len(future["anything"]) != 4 {
		t.Errorf("futureSection not preserved verbatim: %s", raw["futureSection"])
	}
	lk := ReadProjectLock(cwd)
	if _, ok := lk.Skills["my-skill"]; !ok {
		t.Error("existing skill entry lost")
	}
	if _, ok := lk.Skills["other"]; !ok {
		t.Error("added skill entry missing")
	}
}

func TestProjectLockSectionsAreIsolated(t *testing.T) {
	cwd := t.TempDir()
	if err := AddBundleToKnowledgeLock("sales", KnowledgeLockEntry{Source: "x", SourceType: "local", InstallDir: "knowledge/sales", SpecVersion: "0.1"}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := AddPluginToLock("toolkit", PluginLockEntry{Source: "acme/toolkit", SourceType: "github", InstallDir: ".agents/plugins/toolkit", DataDir: ".agents/plugins-data/toolkit", SpecVersion: "1.0.0"}, cwd); err != nil {
		t.Fatal(err)
	}
	knowledgeBefore := ReadProjectLock(cwd).Knowledge["sales"]
	pluginsBefore := ReadProjectLock(cwd).Plugins["toolkit"]

	// A skills write must leave the other sections untouched.
	if err := AddSkillToLocalLock("my-skill", LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	lk := ReadProjectLock(cwd)
	if lk.Knowledge["sales"] != knowledgeBefore {
		t.Errorf("knowledge entry changed by a skills write: %+v", lk.Knowledge["sales"])
	}
	if got := lk.Plugins["toolkit"]; got.Source != pluginsBefore.Source || got.InstalledAt != pluginsBefore.InstalledAt {
		t.Errorf("plugin entry changed by a skills write: %+v", got)
	}
}

func TestProjectLockLegacyFallback(t *testing.T) {
	cwd := t.TempDir()
	writeJSON := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON("skills-lock.json", `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"}},"configuredAgents":["claude-code"]}`)
	writeJSON("knowledge-lock.json", `{"version":1,"bundles":{"k1":{"source":"x","sourceType":"local","installDir":"knowledge/k1","specVersion":"0.1","installedAt":"t","updatedAt":"t"}}}`)
	writeJSON("plugins-lock.json", `{"version":1,"plugins":{"p1":{"source":"a/b","sourceType":"github","installDir":".agents/plugins/p1","dataDir":".agents/plugins-data/p1","specVersion":"1.0.0","installedAt":"t","updatedAt":"t"}}}`)

	lk := ReadProjectLock(cwd)
	if _, ok := lk.Skills["s1"]; !ok {
		t.Error("legacy skills-lock.json not read")
	}
	if _, ok := lk.Knowledge["k1"]; !ok {
		t.Error("legacy knowledge-lock.json not read")
	}
	if _, ok := lk.Plugins["p1"]; !ok {
		t.Error("legacy plugins-lock.json not read")
	}
	if len(lk.ConfiguredAgents) != 1 || lk.ConfiguredAgents[0] != "claude-code" {
		t.Errorf("legacy configuredAgents not read: %v", lk.ConfiguredAgents)
	}

	// Once mdm-lock.json exists it wins; the legacy files are ignored but
	// left in place for `mdm migrate`.
	if err := WriteProjectLock(lk, cwd); err != nil {
		t.Fatal(err)
	}
	writeJSON("skills-lock.json", `{"version":1,"skills":{"ghost":{"source":"o/r","sourceType":"github"}}}`)
	lk = ReadProjectLock(cwd)
	if _, ok := lk.Skills["ghost"]; ok {
		t.Error("legacy file consulted even though mdm-lock.json exists")
	}
	if _, ok := lk.Skills["s1"]; !ok {
		t.Error("migrated entry missing from mdm-lock.json")
	}
	for _, name := range []string{"skills-lock.json", "knowledge-lock.json", "plugins-lock.json"} {
		if _, err := os.Stat(filepath.Join(cwd, name)); err != nil {
			t.Errorf("legacy %s should be left in place until mdm migrate: %v", name, err)
		}
	}
}

func TestProjectLockKeyOrderAndDeterminism(t *testing.T) {
	cwd := t.TempDir()
	lk := EmptyProjectLock()
	lk.ConfiguredAgents = []string{"claude-code"}
	lk.Skills["b"] = LocalSkillLockEntry{Source: "o/r", SourceType: "github"}
	lk.Skills["a"] = LocalSkillLockEntry{Source: "o/r", SourceType: "github"}
	lk.Knowledge["k"] = KnowledgeLockEntry{Source: "x", SourceType: "local", InstallDir: "knowledge/k", SpecVersion: "0.1"}
	if err := WriteProjectLock(lk, cwd); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(GetProjectLockPath(cwd))
	if err != nil {
		t.Fatal(err)
	}
	content := string(first)
	for _, ordered := range [][2]string{
		{`"version"`, `"configuredAgents"`},
		{`"configuredAgents"`, `"skills"`},
		{`"skills"`, `"knowledge"`},
	} {
		if strings.Index(content, ordered[0]) > strings.Index(content, ordered[1]) {
			t.Errorf("expected %s before %s in:\n%s", ordered[0], ordered[1], content)
		}
	}
	if !strings.HasSuffix(content, "\n") {
		t.Error("lock file should end with a newline")
	}
	if err := WriteProjectLock(ReadProjectLock(cwd), cwd); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(GetProjectLockPath(cwd))
	if string(first) != string(second) {
		t.Errorf("read/write round trip is not byte-stable:\n%s\nvs\n%s", first, second)
	}
}

func TestProjectLockEmptyWriteRemovesFile(t *testing.T) {
	cwd := t.TempDir()
	if err := AddSkillToLocalLock("s", LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSkillFromLocalLock("s", cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetProjectLockPath(cwd)); !os.IsNotExist(err) {
		t.Error("expected mdm-lock.json to be removed when nothing is left in it")
	}
	// But not when another section still has entries.
	if err := AddBundleToKnowledgeLock("k", KnowledgeLockEntry{Source: "x", SourceType: "local", InstallDir: "knowledge/k", SpecVersion: "0.1"}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := AddSkillToLocalLock("s", LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSkillFromLocalLock("s", cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetProjectLockPath(cwd)); err != nil {
		t.Errorf("mdm-lock.json should survive while the knowledge section has entries: %v", err)
	}
}

func TestProjectLockUnreadableErrors(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectLockE(cwd); err == nil || !strings.Contains(err.Error(), "could not be parsed") {
		t.Errorf("corrupt lock should error, got %v", err)
	}
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte(`{"version":99,"skills":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProjectLockE(cwd); err == nil || !strings.Contains(err.Error(), "newer version of mdm") {
		t.Errorf("newer lock should error with an upgrade pointer, got %v", err)
	}
	// Version 0 (malformed-but-old) still reads as empty, matching v1.
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte(`{"version":0,"skills":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if lk, err := readProjectLockE(cwd); err != nil || !lk.isEmpty() {
		t.Errorf("version 0 lock should read as empty, got %+v err=%v", lk, err)
	}
}

func TestProjectLockToleratesLegacyTombstone(t *testing.T) {
	cwd := t.TempDir()
	// Only the tombstone exists: a fresh clone after a --no-commit mishap,
	// or a v2 binary revisiting a migrated project whose mdm-lock.json was
	// deleted. It must read as empty, never abort.
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(SkillsTombstone), 0600); err != nil {
		t.Fatal(err)
	}
	lk, err := readProjectLockE(cwd)
	if err != nil || !lk.isEmpty() {
		t.Errorf("tombstone-only project should read as empty, got %+v err=%v", lk, err)
	}
}
