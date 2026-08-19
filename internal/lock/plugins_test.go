package lock

import (
	"os"
	"testing"
)

func TestPluginsLockRoundTrip(t *testing.T) {
	cwd := t.TempDir()

	if lk := ReadPluginsLock(cwd); len(lk.Plugins) != 0 || lk.Version != pluginsLockVersion {
		t.Fatalf("missing lock should read as empty, got %+v", lk)
	}

	entry := PluginLockEntry{
		Source:      "acme/toolkit",
		SourceType:  "github",
		Ref:         "main",
		InstallDir:  ".agents/plugins/toolkit",
		DataDir:     ".agents/plugins-data/toolkit",
		SpecVersion: "1.0.0",
		Skills:      []string{"alpha"},
		SkillAgents: []string{"claude-code"},
		MCP:         map[string][]string{"claude-code": {"toolkit--example"}},
	}
	if err := AddPluginToLock("toolkit", entry, cwd); err != nil {
		t.Fatal(err)
	}

	lk := ReadPluginsLock(cwd)
	got, ok := lk.Plugins["toolkit"]
	if !ok || got.Source != "acme/toolkit" || got.InstalledAt == "" || got.UpdatedAt == "" {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if got.MCP["claude-code"][0] != "toolkit--example" {
		t.Errorf("MCP map not preserved: %+v", got.MCP)
	}

	// Re-adding preserves InstalledAt.
	firstInstall := got.InstalledAt
	if err := AddPluginToLock("toolkit", entry, cwd); err != nil {
		t.Fatal(err)
	}
	if again := ReadPluginsLock(cwd).Plugins["toolkit"]; again.InstalledAt != firstInstall {
		t.Errorf("InstalledAt changed on update: %q vs %q", again.InstalledAt, firstInstall)
	}

	// Removing the last entry deletes the file.
	if err := RemovePluginFromLock("toolkit", cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(GetProjectLockPath(cwd)); !os.IsNotExist(err) {
		t.Error("lock file should be deleted when the last plugin is removed")
	}
	if err := RemovePluginFromLock("absent", cwd); err != nil {
		t.Errorf("removing a missing entry should be a no-op, got %v", err)
	}
}
