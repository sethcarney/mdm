package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

// installCriticTo installs a minimal "critic" definition to every named
// harness in project scope (copy mode, so no symlink support is needed in
// the test environment), and returns the base lock entry it recorded.
func installCriticTo(t *testing.T, cwd string, harnesses []string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, harnesses, false, InstallModeCopy, baseEntry, "", cwd)
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; !ok {
		t.Fatal("setup: install did not record a lock entry")
	}
}

func agentHarnessTarget(harnessName, cwd string) string {
	dir := harness.AgentsInstallDirFor(harnessName, false, cwd)
	return filepath.Join(dir, "critic"+harness.AgentFileExt(harnessName))
}

// Mutation this test catches: deleting the `if !installedAny { ...continue
// }` guard in installAgentsForHarnesses (i.e. always writing the lock entry
// regardless of whether any harness actually got the file). A harness with
// no agent concept is a guaranteed, harmless way to force every install in
// the batch to fail, without needing a filesystem fault.
func TestInstallAgentsForHarnessesSkipsLockOnTotalFailure(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(t.TempDir(), "critic.md")
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

	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github", Ref: "main"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{without}, false, InstallModeCopy, baseEntry, "", cwd)

	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; ok {
		t.Error("lock records an install that failed on every requested harness")
	}
}

// Mutation this test catches: dropping the `entry.AgentPath =
// agentFileRepoPath(...)` assignment in installAgentsForHarnesses, which
// would leave AgentPath empty and strand a later update with no way to find
// the file inside its source again. Also catches writing the entry to any
// key besides mdm.lock's "agents" section (ReadProjectLock().Agents reads
// through the same decoder that a "wrong key" mutation would starve).
func TestInstallAgentsForHarnessesRecordsAgentPathOnSuccess(t *testing.T) {
	cwd := t.TempDir()
	cloneDir := t.TempDir()
	agentsDir := filepath.Join(cloneDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentsDir, "critic.md")
	if err := os.WriteFile(agentPath, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: agentPath}

	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github", Ref: "main"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, cloneDir, cwd)

	entry, ok := lock.ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("expected an agents lock entry for critic")
	}
	if entry.AgentPath != "agents/critic.md" {
		t.Errorf("AgentPath = %q, want %q", entry.AgentPath, "agents/critic.md")
	}
	if entry.Source != "o/r" || entry.SourceType != "github" || entry.Ref != "main" {
		t.Errorf("source fields not carried through: %+v", entry)
	}
}

// Mutation this test catches: recording the lock entry even when only SOME
// of several requested harnesses succeeded is the desired behavior (mirrors
// skills), so this pins that down against an over-eager fix that gates on
// "every harness succeeded" instead of "at least one did".
func TestInstallAgentsForHarnessesRecordsOnPartialSuccess(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(t.TempDir(), "critic.md")
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

	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{without, "claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)

	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; !ok {
		t.Error("lock should still record the install when at least one harness succeeded")
	}
}

// Mutation this test catches: dropping the agentInstalledSomewhere gate and
// unconditionally deleting the canonical file and lock entry after the
// per-harness loop (the pre-fix-round-1 behavior). `--harness claude-code`
// must leave cursor's copy, the canonical file, and the lock entry alone,
// since cursor still needs them.
func TestRemoveAgentScopedToHarnessLeavesOtherHarnessAndLockIntact(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code", "cursor"})

	fullyRemoved, err := removeAgentFromDisk("critic", []string{"claude-code"}, false, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fullyRemoved {
		t.Error("fullyRemoved = true; cursor still has a copy, so this must be false")
	}

	if _, statErr := os.Lstat(agentHarnessTarget("claude-code", cwd)); !os.IsNotExist(statErr) {
		t.Errorf("claude-code's copy should be gone, stat err = %v", statErr)
	}
	if _, statErr := os.Lstat(agentHarnessTarget("cursor", cwd)); statErr != nil {
		t.Errorf("cursor's copy must survive a scoped removal: %v", statErr)
	}
	canonical := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
	if _, statErr := os.Stat(canonical); statErr != nil {
		t.Errorf("canonical file must survive while another harness still needs it: %v", statErr)
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; !ok {
		t.Error("lock entry must survive a scoped removal that left the definition installed elsewhere")
	}
}

// A removal with no --harness filter targets every harness, so once nothing
// is left installed the canonical file and lock entry must go too.
func TestRemoveAgentWithNoFilterRemovesEverything(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code", "cursor"})

	fullyRemoved, err := removeAgentFromDisk("critic", nil, false, cwd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fullyRemoved {
		t.Error("fullyRemoved = false; nothing should be left installed anywhere")
	}
	for _, h := range []string{"claude-code", "cursor"} {
		if _, statErr := os.Lstat(agentHarnessTarget(h, cwd)); !os.IsNotExist(statErr) {
			t.Errorf("%s's copy should be gone, stat err = %v", h, statErr)
		}
	}
	canonical := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
	if _, statErr := os.Stat(canonical); !os.IsNotExist(statErr) {
		t.Errorf("canonical file should be gone, stat err = %v", statErr)
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; ok {
		t.Error("lock entry should be gone once nothing is installed anywhere")
	}
}

// Mutation this test catches: ignoring removeFileFn's error and reporting
// success (and dropping the lock entry) anyway. A permission error or a
// locked file must leave the lock describing what is actually on disk, so a
// retry can find the definition again.
func TestRemoveAgentDeletionFailureKeepsLockEntry(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code"})

	target := agentHarnessTarget("claude-code", cwd)
	orig := removeFileFn
	removeFileFn = func(path string) error {
		if path == target {
			return errors.New("permission denied (simulated)")
		}
		return orig(path)
	}
	defer func() { removeFileFn = orig }()

	fullyRemoved, err := removeAgentFromDisk("critic", []string{"claude-code"}, false, cwd)
	if err == nil {
		t.Fatal("expected an error when the per-harness deletion fails")
	}
	if fullyRemoved {
		t.Error("fullyRemoved = true despite a deletion failure")
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; !ok {
		t.Error("lock entry must survive a failed deletion")
	}
	canonical := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
	if _, statErr := os.Stat(canonical); statErr != nil {
		t.Errorf("canonical file must survive a failed per-harness deletion: %v", statErr)
	}
}

// Mutation this test catches: `mdm agents list` trusting the canonical file
// alone. A lock entry with no canonical file on disk must be flagged, not
// reported as healthy.
func TestAgentStatusForFlagsMissingCanonical(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.AddAgentToLocalLock("critic", lock.AgentLockEntry{Source: "o/r", SourceType: "github", AgentPath: "a.md"}, cwd); err != nil {
		t.Fatal(err)
	}
	st := agentStatusFor("critic", false, cwd)
	if !st.CanonicalMissing {
		t.Error("CanonicalMissing = false; no canonical file exists on disk")
	}
}

// Mutation this test catches: agentInstalledHarnesses (and so
// agentStatusFor) reporting a harness as installed after its own copy was
// deleted directly on disk, without going through mdm — e.g. hardcoding
// InstalledIn from the harnesses requested at install time instead of
// checking the filesystem.
func TestAgentStatusForReportsPerHarnessBreakage(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code", "cursor"})

	// Simulate a harness losing its copy without mdm's involvement.
	if err := os.Remove(agentHarnessTarget("claude-code", cwd)); err != nil {
		t.Fatal(err)
	}

	st := agentStatusFor("critic", false, cwd)
	if st.CanonicalMissing {
		t.Error("canonical file untouched by this scenario; CanonicalMissing must be false")
	}
	found := map[string]bool{}
	for _, h := range st.InstalledIn {
		found[h] = true
	}
	if found["claude-code"] {
		t.Error("claude-code's copy was deleted directly; it must not be reported as installed")
	}
	if !found["cursor"] {
		t.Error("cursor's copy is untouched; it must still be reported as installed")
	}
}
