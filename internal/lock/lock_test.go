package lock

import (
	"os"
	"strings"
	"testing"
)

func TestInstallModeAccessorsProjectScope(t *testing.T) {
	cwd := t.TempDir()
	if got := GetInstallMode(false, cwd); got != "" {
		t.Fatalf("default mode = %q, want empty", got)
	}
	if err := SetInstallMode(InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}
	if got := GetInstallMode(false, cwd); got != InstallModeCopy {
		t.Fatalf("mode = %q, want %q", got, InstallModeCopy)
	}
}

func TestSetInstallModePreservesOtherSections(t *testing.T) {
	cwd := t.TempDir()
	content := `{"version":2,"skills":{"s":{"source":"o/r","sourceType":"github"}},"futureFlag":true}`
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := SetInstallMode(InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}
	lk := ReadProjectLock(cwd)
	if _, ok := lk.Skills["s"]; !ok {
		t.Error("skills section lost")
	}
	data, _ := os.ReadFile(GetProjectLockPath(cwd))
	if !strings.Contains(string(data), "futureFlag") {
		t.Error("unknown key lost")
	}
}

func TestInstallModeAccessorsGlobalScope(t *testing.T) {
	isolateGlobal(t)
	if err := SetInstallMode(InstallModeCopy, true, ""); err != nil {
		t.Fatal(err)
	}
	if got := GetInstallMode(true, ""); got != InstallModeCopy {
		t.Fatalf("mode = %q, want %q", got, InstallModeCopy)
	}
}

// Mutation this test catches: dropping Format from AgentLockEntry's write
// path. A lock that does not say which format a definition's canonical file
// is in leaves every later command guessing the extension, which works only
// until both extensions exist.
func TestAgentLockEntryRecordsTheCanonicalFormat(t *testing.T) {
	cwd := t.TempDir()
	if err := AddAgentToLocalLock("critic", AgentLockEntry{
		Source: "o/r", SourceType: "github", AgentPath: "agents/critic.toml", Format: "toml",
	}, cwd); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(GetProjectLockPath(cwd))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"format"`) {
		t.Errorf("mdm.lock has no \"format\" key:\n%s", data)
	}

	entry, ok := ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("agent entry did not round-trip back through ReadProjectLock")
	}
	if entry.Format != "toml" {
		t.Errorf("Format = %q, want toml", entry.Format)
	}
}

// An entry written before mdm recorded the format has no format key. It must
// read back as the empty string, which the commands layer resolves to
// markdown, and a fresh markdown entry must not start writing one.
func TestAgentLockEntryWithoutFormatRoundTripsEmpty(t *testing.T) {
	cwd := t.TempDir()
	content := `{"version":2,"agents":{"critic":{"source":"o/r","sourceType":"github","agentPath":"agents/critic.md"}}}`
	if err := os.WriteFile(GetProjectLockPath(cwd), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	entry, ok := ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("no agents entry read back")
	}
	if entry.Format != "" {
		t.Errorf("Format = %q, want empty for a lock entry written before the format existed", entry.Format)
	}

	if err := AddAgentToLocalLock("other", AgentLockEntry{Source: "o/r", SourceType: "github", AgentPath: "a.md"}, cwd); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(GetProjectLockPath(cwd))
	if strings.Contains(string(data), `"format"`) {
		t.Errorf("an entry with no format wrote one, so omitempty is missing:\n%s", data)
	}
}
