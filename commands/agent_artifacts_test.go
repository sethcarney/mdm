package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// ─── agents update ──────────────────────────────────────────────────────────────

// writeCriticSource (re)writes the on-disk source for "critic" so a later
// runAgentUpdateGroups call re-fetches a changed file.
func writeCriticSource(t *testing.T, path, description string) {
	t.Helper()
	content := "---\nname: critic\ndescription: " + description + "\n---\nbody\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Mutation this test catches: an `agents update` that refreshes only the
// canonical file (or that resolves harnesses from the scope's
// configured-harness list instead of agentInstalledHarnesses, so it misses
// a harness never added to that list) leaves a copy-mode harness install
// stale. This installs "critic" to claude-code in copy mode — a real file,
// not a symlink, so nothing here can pass just because the canonical file
// changed underneath a link — changes the upstream source, then asserts
// BOTH the canonical file and the harness's own copy picked up the change.
func TestAgentsUpdateRefreshesCopyModeHarnessInstalls(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.SetInstallMode(lock.InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	agentsDir := filepath.Join(sourceDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentsDir, "critic.md")
	writeCriticSource(t, agentPath, "v1")

	a := &agentfile.AgentFile{Name: "critic", Description: "v1", Path: agentPath}
	baseEntry := lock.AgentLockEntry{Source: sourceDir, SourceType: "local"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)

	harnessTarget := agentHarnessTarget("claude-code", cwd)
	canonicalPath := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")

	before, err := os.ReadFile(harnessTarget)
	if err != nil {
		t.Fatalf("setup: could not read harness copy: %v", err)
	}
	if !strings.Contains(string(before), "v1") {
		t.Fatalf("setup: harness copy does not hold v1 content: %s", before)
	}

	// Simulate an upstream change to the source.
	writeCriticSource(t, agentPath, "v2")

	group := updateGroup{source: sourceDir, skills: []string{"critic"}, names: []string{"critic"}}
	var stats updateStats
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, &stats)

	canonicalAfter, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("could not read canonical file after update: %v", err)
	}
	if !strings.Contains(string(canonicalAfter), "v2") {
		t.Errorf("canonical file not refreshed by update: %s", canonicalAfter)
	}

	harnessAfter, err := os.ReadFile(harnessTarget)
	if err != nil {
		t.Fatalf("could not read harness copy after update: %v", err)
	}
	if !strings.Contains(string(harnessAfter), "v2") {
		t.Errorf("harness copy left stale by update (still: %s)", harnessAfter)
	}
	if stats.updated != 1 {
		t.Errorf("stats.updated = %d, want 1", stats.updated)
	}
}

// The symmetric case of the copy-mode test above: a symlink-mode scope must
// still have a real symlink at the harness path after an update, not a copy
// — mode correctness runs both directions.
func TestAgentsUpdateKeepsSymlinkModeHarnessInstallsAsSymlinks(t *testing.T) {
	cwd := t.TempDir()
	// No lock.SetInstallMode call: the default (unrecorded) mode is symlink.

	sourceDir := t.TempDir()
	agentsDir := filepath.Join(sourceDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentsDir, "critic.md")
	writeCriticSource(t, agentPath, "v1")

	a := &agentfile.AgentFile{Name: "critic", Description: "v1", Path: agentPath}
	baseEntry := lock.AgentLockEntry{Source: sourceDir, SourceType: "local"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeSymlink, baseEntry, "", cwd)

	harnessTarget := agentHarnessTarget("claude-code", cwd)
	info, err := os.Lstat(harnessTarget)
	if err != nil {
		t.Fatalf("setup: could not lstat harness target: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Skip("symlinks unavailable on this host; installAgentFile fell back to a copy")
	}

	writeCriticSource(t, agentPath, "v2")

	group := updateGroup{source: sourceDir, skills: []string{"critic"}, names: []string{"critic"}}
	var stats updateStats
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, &stats)

	infoAfter, err := os.Lstat(harnessTarget)
	if err != nil {
		t.Fatalf("could not lstat harness target after update: %v", err)
	}
	if infoAfter.Mode()&os.ModeSymlink == 0 {
		t.Error("harness install is no longer a symlink after an update in symlink mode")
	}

	content, err := os.ReadFile(harnessTarget) // follows the symlink
	if err != nil {
		t.Fatalf("could not read harness target after update: %v", err)
	}
	if !strings.Contains(string(content), "v2") {
		t.Errorf("harness symlink target not refreshed: %s", content)
	}
	if stats.updated != 1 {
		t.Errorf("stats.updated = %d, want 1", stats.updated)
	}
}

// Mutation this test catches: dropping the `if !installedAny { continue }`
// guard added to runAgentUpdateGroups (mirroring installAgentsForHarnesses'
// own guard) — without it, an update where every currently-installed
// harness fails still writes the lock entry and counts as updated, even
// though nothing on disk changed.
//
// The failure is forced by replacing claude-code's installed FILE (not its
// containing directory — agentInstalledHarnesses must still count it as
// installed, via os.Lstat, or the test would only exercise the older
// "not installed in any harness, skipping" branch instead of the new
// guard) with a directory of the same name: installAgentFile's
// os.MkdirAll(harnessDir, ...) then succeeds (the directory is already
// there), but copyFile's os.OpenFile(..., O_CREATE|O_TRUNC, ...) fails
// deterministically against a path that is now a directory.
func TestAgentsUpdateLeavesLockUnchangedWhenEveryHarnessInstallFails(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.SetInstallMode(lock.InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	agentsDir := filepath.Join(sourceDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentsDir, "critic.md")
	writeCriticSource(t, agentPath, "v1")

	a := &agentfile.AgentFile{Name: "critic", Description: "v1", Path: agentPath}
	baseEntry := lock.AgentLockEntry{Source: sourceDir, SourceType: "local"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)

	lockPath := lock.GetProjectLockPath(cwd)
	infoBefore, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("setup: could not stat %s: %v", lockPath, err)
	}
	entryBefore, ok := lock.ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("setup: expected a lock entry after the initial install")
	}

	harnessTarget := agentHarnessTarget("claude-code", cwd)
	if _, err := os.Lstat(harnessTarget); err != nil {
		t.Fatalf("setup: expected an installed copy at %s: %v", harnessTarget, err)
	}
	if err := os.Remove(harnessTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(harnessTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	// Confirm the swap still reads as "installed" (Lstat-based), so the
	// test actually exercises the new guard and not the pre-existing
	// "not installed anywhere" early exit.
	if got := agentInstalledHarnesses("critic", false, cwd); len(got) != 1 || got[0] != "claude-code" {
		t.Fatalf("setup: expected agentInstalledHarnesses to still report claude-code, got %v", got)
	}

	// An upstream change, so a naive implementation has something it might
	// wrongly record as a completed update.
	writeCriticSource(t, agentPath, "v2")

	group := updateGroup{source: sourceDir, skills: []string{"critic"}, names: []string{"critic"}}
	var stats updateStats
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, &stats)

	if stats.updated != 0 {
		t.Errorf("stats.updated = %d, want 0: every installed harness failed", stats.updated)
	}

	entryAfter, ok := lock.ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("lock entry disappeared entirely")
	}
	if entryAfter != entryBefore {
		t.Errorf("lock entry changed despite every harness install failing: before=%+v after=%+v", entryBefore, entryAfter)
	}

	// Content equality alone cannot prove the lock file was never
	// rewritten (a local source's recorded fields happen to be identical
	// whether written once or twice), so also check the file was never
	// touched at all.
	infoAfter, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("could not stat %s after update: %v", lockPath, err)
	}
	if !infoAfter.ModTime().Equal(infoBefore.ModTime()) || infoAfter.Size() != infoBefore.Size() {
		t.Errorf("%s was rewritten despite every harness install failing (mtime %v -> %v, size %d -> %d)",
			lockPath, infoBefore.ModTime(), infoAfter.ModTime(), infoBefore.Size(), infoAfter.Size())
	}
}

// Mutation this test catches: dropping the "not found upstream" warning
// added ahead of the install loop in runAgentUpdateGroups. "ghost" is
// recorded in the lock but has no corresponding file in the source tree
// (only "critic" does); the fix must name it and its source rather than
// silently doing nothing.
func TestAgentsUpdateWarnsWhenDefinitionMissingUpstream(t *testing.T) {
	cwd := t.TempDir()
	sourceDir := t.TempDir()
	agentsDir := filepath.Join(sourceDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCriticSource(t, filepath.Join(agentsDir, "critic.md"), "v1")

	group := updateGroup{source: sourceDir, skills: []string{"critic", "ghost"}, names: []string{"critic", "ghost"}}
	var stats updateStats
	out := captureStdout(t, func() {
		runAgentUpdateGroups([]updateGroup{group}, false, cwd, &stats)
	})

	if !strings.Contains(out, "ghost") {
		t.Errorf("expected a warning naming the missing definition \"ghost\", got:\n%s", out)
	}
	if !strings.Contains(out, sourceDir) {
		t.Errorf("expected the warning to name the source %q, got:\n%s", sourceDir, out)
	}
}
