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
	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/ui"
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
// no directory recorded forces every install in the batch to fail, harmlessly
// and without needing a filesystem fault.
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

// blockAgentsDir puts a regular file where harnessName's agents directory
// belongs, so the os.MkdirAll every harness write starts with fails. It is the
// cheapest write failure that happens after the canonical copy rather than
// before it, which is the window this fixture exists to open.
func blockAgentsDir(t *testing.T, harnessName, cwd string) {
	t.Helper()
	dir := harness.AgentsInstallDirFor(harnessName, false, cwd)
	if dir == "" {
		t.Fatalf("%s has no project agents directory", harnessName)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// installAgentFile writes the canonical copy before the first harness write, so
// a definition that fails for every harness after that point leaves a file at
// .agents/agents/<name> that no lock entry names. `agents list` and `agents
// remove` read the lock and cannot see it, `mdm agents add .` rediscovers it as
// a source, and checkAgentFormatCollision then refuses the name in the other
// format forever, pointing at a remove that reports the name is not installed.
//
// Mutation this test catches: dropping the rollback() call from the
// !installedAny arm of installAgentsForHarnesses.
func TestInstallAgentsForHarnessesLeavesNoOrphanCanonicalFile(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(t.TempDir(), "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}
	blockAgentsDir(t, "claude-code", cwd)

	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github", Ref: "main"}
	outcome := installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)
	if outcome.installed != 0 {
		t.Fatalf("installed = %d, want 0; the blocked directory did not fail the write", outcome.installed)
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; ok {
		t.Fatal("lock records the failed install; this test cannot tell an orphan from a normal install")
	}

	canonicalPath := agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("canonical file left at %s (stat err = %v) with no lock entry naming it", canonicalPath, err)
	}
}

// The rollback undoes this run's canonical write, not somebody else's file. A
// canonical already on disk belongs to an earlier install the lock still names,
// or is the source itself for `mdm agents add .`; deleting it would turn one
// failed harness write into the loss of a definition every other harness holds.
//
// Mutation this test catches: rolling back unconditionally, i.e. dropping
// canonicalRollback's os.Stat check on the path.
func TestInstallAgentsForHarnessesKeepsACanonicalItDidNotCreate(t *testing.T) {
	cwd := t.TempDir()
	canonicalPath := agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonicalPath, []byte("---\nname: critic\ndescription: d\n---\nold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\nnew\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}
	blockAgentsDir(t, "claude-code", cwd)

	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github", Ref: "main"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)

	if _, err := os.Stat(canonicalPath); err != nil {
		t.Errorf("canonical file that predates the run was deleted: %v", err)
	}
}

// Guards the `entry.AgentPath = agentFileRepoPath(...)` assignment in
// installAgentsForHarnesses, which a later update needs to find the file in its
// source. It also catches writing the entry to any key besides mdm.lock's
// "agents" section.
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

	fullyRemoved, err := removeAgentFromDisk("critic", []string{"claude-code"}, agentfile.FormatMarkdown, false, cwd)
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

	fullyRemoved, err := removeAgentFromDisk("critic", nil, agentfile.FormatMarkdown, false, cwd)
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

	fullyRemoved, err := removeAgentFromDisk("critic", []string{"claude-code"}, agentfile.FormatMarkdown, false, cwd)
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
	st := agentStatusFor("critic", agentfile.FormatMarkdown, false, cwd)
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

	st := agentStatusFor("critic", agentfile.FormatMarkdown, false, cwd)
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

// An `agents update` that refreshes only the canonical file, or that resolves
// harnesses from the scope's configured-harness list instead of
// agentInstalledHarnesses, leaves a copy-mode harness install stale. This
// installs "critic" to claude-code in copy mode, so a real file and not a
// symlink, and asserts both copies pick up an upstream change.
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
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, false, &stats)

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
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, false, &stats)

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

// Guards the `if !installedAny { continue }` check in runAgentUpdateGroups.
// Without it, an update where every installed harness fails still writes the
// lock entry. The failure is forced by replacing claude-code's installed file
// with a directory of the same name: os.MkdirAll then succeeds and copyFile's
// O_CREATE|O_TRUNC open fails, while os.Lstat still counts it as installed.
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
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, false, &stats)

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
		runAgentUpdateGroups([]updateGroup{group}, false, cwd, false, &stats)
	})

	if !strings.Contains(out, "ghost") {
		t.Errorf("expected a warning naming the missing definition \"ghost\", got:\n%s", out)
	}
	if !strings.Contains(out, sourceDir) {
		t.Errorf("expected the warning to name the source %q, got:\n%s", sourceDir, out)
	}
}

// The summary must report what landed, not what was asked for. Reporting
// len(selected) printed "✓ Installed 1 agent definition" after writing no
// file and no lock entry, and named the harness the install had just been
// skipped by.
func TestInstallAgentsForHarnessesCountsWhatActuallyInstalled(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(cwd, "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

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

	// Every target skipped: nothing installed, no harness to report.
	outcome := installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{without}, false, InstallModeCopy, baseEntry, "", cwd)
	if outcome.installed != 0 {
		t.Errorf("installed = %d, want 0 when every harness skipped", outcome.installed)
	}
	if len(outcome.harnesses) != 0 {
		t.Errorf("harnesses = %v, want none: no harness received anything", outcome.harnesses)
	}

	// Mixed: the count is 1, and only the harness that took it is named.
	outcome = installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{without, "claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)
	if outcome.installed != 1 {
		t.Errorf("installed = %d, want 1", outcome.installed)
	}
	if len(outcome.harnesses) != 1 || outcome.harnesses[0] != "claude-code" {
		t.Errorf("harnesses = %v, want [claude-code]", outcome.harnesses)
	}
}

// writeNamedAgent lays down a minimal definition source whose frontmatter
// name is rawName and returns the parsed struct pointing at it.
func writeNamedAgent(t *testing.T, rawName string) *agentfile.AgentFile {
	t.Helper()
	src := filepath.Join(t.TempDir(), "agent.md")
	body := "---\nname: " + rawName + "\ndescription: d\n---\n"
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return &agentfile.AgentFile{Name: rawName, Description: "d", Path: src}
}

// Mutation this test catches: removing the name-pattern warning entirely.
// "Code Reviewer" has a space and capitals, which Claude Code's docs say it
// will not load, and the install must still happen despite the warning.
func TestAgentInstallWarnsWhenNameWontSatisfyClaudeCode(t *testing.T) {
	cwd := t.TempDir()
	a := writeNamedAgent(t, "Code Reviewer")
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

	out := captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)
	})

	if !strings.Contains(out, "Code Reviewer") {
		t.Errorf("the warning does not name the definition:\n%s", out)
	}
	if !strings.Contains(out, "Claude Code") {
		t.Errorf("the warning does not name the harness:\n%s", out)
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["code-reviewer"]; !ok {
		t.Error("the install did not happen despite the warning")
	}
}

// Mutation this test catches: warning unconditionally, regardless of whether
// the name actually satisfies the harness's rule. "code-reviewer" is already
// lowercase letters and hyphens, so Claude Code has nothing to complain about.
func TestAgentInstallNoWarningWhenNameAlreadySatisfiesHarness(t *testing.T) {
	cwd := t.TempDir()
	a := writeNamedAgent(t, "code-reviewer")
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

	out := captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)
	})

	if strings.Contains(out, "will not satisfy") {
		t.Errorf("a name that already satisfies claude-code's rule should not warn:\n%s", out)
	}
}

// Mutation this test catches: treating an empty AgentNamePattern as "matches
// nothing" instead of "no documented rule to check". OpenCode takes its name
// from the filename and documents no naming constraint, so even a bad name
// must not warn against it.
func TestAgentInstallNoWarningForHarnessWithNoDocumentedPattern(t *testing.T) {
	cwd := t.TempDir()
	a := writeNamedAgent(t, "Code Reviewer")
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

	out := captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"opencode"}, false, InstallModeCopy, baseEntry, "", cwd)
	})

	if strings.Contains(out, "will not satisfy") {
		t.Errorf("opencode documents no naming pattern, so nothing should warn:\n%s", out)
	}
}

// Mutation this test catches: emitting the warning once per failing harness
// instead of once per definition. The same bad name fails both claude-code's
// and gemini-cli's documented rule; the two harnesses must be named together
// on a single line, not on two separate warning lines.
func TestAgentInstallNameWarningNamesEachFailingHarnessOnce(t *testing.T) {
	cwd := t.TempDir()
	a := writeNamedAgent(t, "Code Reviewer")
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

	out := captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code", "gemini-cli"}, false, InstallModeCopy, baseEntry, "", cwd)
	})

	warnLines := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "will not satisfy") {
			continue
		}
		warnLines++
		if !strings.Contains(line, "Claude Code") || !strings.Contains(line, "Gemini CLI") {
			t.Errorf("the single warning line must name both harnesses:\n%s", line)
		}
	}
	if warnLines != 1 {
		t.Errorf("got %d warning line(s) for the name-pattern violation, want exactly 1:\n%s", warnLines, out)
	}
}

// writeParsedAgent lays down a definition source in the format its extension
// names and returns it parsed, so Format is set the way discovery sets it.
func writeParsedAgent(t *testing.T, ext string) *agentfile.AgentFile {
	t.Helper()
	body := "---\nname: critic\ndescription: d\n---\nbody\n"
	if ext == ".toml" {
		body = "name = \"critic\"\ndescription = \"d\"\ndeveloper_instructions = \"be critical\"\n"
	}
	src := filepath.Join(t.TempDir(), "critic"+ext)
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing %s: a=%v err=%v", src, a, err)
	}
	return a
}

// Mutation this test catches: hardcoding ".md" in agentCanonicalPath again.
// The canonical file mirrors its source, so a TOML source must not be parked
// under a name that says markdown.
func TestCanonicalFileMirrorsTheSourceFormat(t *testing.T) {
	cases := []struct{ ext, harnessName string }{
		{".md", "claude-code"},
		{".toml", "codex"},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			cwd := t.TempDir()
			a := writeParsedAgent(t, tc.ext)

			res := installAgentFile(a, tc.harnessName, false, cwd, InstallModeCopy)
			if !res.Success {
				t.Fatalf("install failed: %s", res.Error)
			}

			want := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic"+tc.ext)
			if res.CanonicalPath != want {
				t.Errorf("canonical path = %q, want %q", res.CanonicalPath, want)
			}
			entries, err := os.ReadDir(harness.CanonicalAgentsDir(false, cwd))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "critic"+tc.ext {
				t.Errorf("canonical directory holds %v, want only critic%s", entries, tc.ext)
			}
		})
	}
}

// The lock has to say which format the canonical file is in, because every
// command that reaches for that file now has two extensions to choose from.
func TestInstallAgentsForHarnessesRecordsTheCanonicalFormat(t *testing.T) {
	cwd := t.TempDir()
	a := writeParsedAgent(t, ".toml")

	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"codex"}, false, InstallModeCopy, baseEntry, "", cwd)

	entry, ok := lock.ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("expected an agents lock entry for critic")
	}
	if entry.Format != string(agentfile.FormatTOML) {
		t.Errorf("Format = %q, want %q", entry.Format, agentfile.FormatTOML)
	}
}

// Mutation this test catches: removing the collision check, which lets the
// install succeed and leaves one name with two canonical files in two
// formats — and silently changes the format every harness gets.
func TestAddingTheSameNameInTheOtherFormatIsRefused(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code"})

	canonicalDir := harness.CanonicalAgentsDir(false, cwd)
	existing := filepath.Join(canonicalDir, "critic.md")
	before, err := os.ReadFile(existing)
	if err != nil {
		t.Fatalf("setup: no markdown canonical at %s: %v", existing, err)
	}

	a := writeParsedAgent(t, ".toml")
	res := installAgentFile(a, "codex", false, cwd, InstallModeCopy)
	if res.Success {
		t.Fatalf("install succeeded; %s and %s now both claim the name critic", existing, filepath.Join(canonicalDir, "critic.toml"))
	}
	if !strings.Contains(res.Error, existing) {
		t.Errorf("error = %q; it must name the canonical file that already exists (%s)", res.Error, existing)
	}
	if !strings.Contains(res.Error, a.Path) {
		t.Errorf("error = %q; it must name the incoming source (%s)", res.Error, a.Path)
	}

	after, err := os.ReadFile(existing)
	if err != nil || string(after) != string(before) {
		t.Errorf("the existing canonical file changed (err=%v): %q, want %q", err, after, before)
	}
	if _, err := os.Stat(filepath.Join(canonicalDir, "critic.toml")); !os.IsNotExist(err) {
		t.Errorf("a second canonical file was written despite the refusal (stat err=%v)", err)
	}
}

// Mutation this test catches: reading an absent lock format as anything but
// markdown. Every canonical file mdm wrote before it recorded the format is
// a .md, so those entries have to keep resolving.
func TestLockEntryWithoutFormatResolvesTheMarkdownCanonical(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code"})

	entry := lock.ReadProjectLock(cwd).Agents["critic"]
	entry.Format = ""
	if err := lock.AddAgentToLocalLock("critic", entry, cwd); err != nil {
		t.Fatal(err)
	}
	format := lockedAgentFormat(entry)

	if st := agentStatusFor("critic", format, false, cwd); st.CanonicalMissing {
		t.Error("a lock entry with no format reports its canonical file missing")
	}

	fullyRemoved, err := removeAgentFromDisk("critic", nil, format, false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !fullyRemoved {
		t.Error("removal did not complete for a lock entry with no format")
	}
	canonical := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Errorf("the markdown canonical survived the removal at %s (stat err=%v)", canonical, err)
	}
}

// reasonLine returns the single line of out holding needle, with ANSI codes
// and indentation stripped, so a test can assert on the reason alone.
func reasonLine(t *testing.T, out, needle string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		line = strings.ReplaceAll(line, ansiDim, "")
		return strings.TrimSpace(strings.ReplaceAll(line, ansiReset, ""))
	}
	t.Fatalf("no line of the output holds %q:\n%s", needle, out)
	return ""
}

// Mutation this test catches: discarding InstallResult.Error again, so the run
// prints only the harness name. Encode computes the offending key path
// precisely so the user knows what to change; a summary that drops it just
// relocates the confusion.
func TestInstallAgentsForHarnessesPrintsWhyAnInstallFailed(t *testing.T) {
	cwd := t.TempDir()
	a := writeUnencodableAgent(t)
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

	out := captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)
	})
	if !strings.Contains(out, "failed for: claude-code") {
		t.Fatalf("the failure itself is not reported:\n%s", out)
	}
	if !strings.Contains(out, "standup") {
		t.Fatalf("the output never names the offending key, so there is nothing to act on:\n%s", out)
	}

	// The reason sits under a line that already names the definition, and it
	// is read by a person, not a Go caller.
	reason := reasonLine(t, out, "standup")
	if strings.Contains(reason, a.Name) {
		t.Errorf("the reason repeats the definition name from the line above it: %q", reason)
	}
	if strings.Contains(reason, "agentfile:") {
		t.Errorf("the reason carries a Go package prefix: %q", reason)
	}
	if !strings.Contains(reason, "cannot be re-encoded") {
		t.Errorf("the reason does not explain why the key cannot be converted: %q", reason)
	}

	// One reason shared by two harnesses is stated once, not per harness.
	other := t.TempDir()
	out = captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code", "cursor"}, false, InstallModeCopy, baseEntry, "", other)
	})
	if !strings.Contains(out, "failed for: claude-code, cursor") {
		t.Errorf("both harnesses should be named on the failure line:\n%s", out)
	}
	if got := strings.Count(out, "standup"); got != 1 {
		t.Errorf("the same reason is printed %d times, want once:\n%s", got, out)
	}
}

// Mutation this test catches: discarding InstallResult.Error for the
// format-collision refusal. The refusal is only useful if the user can see
// which file already holds the name and which source was rejected.
func TestInstallAgentsForHarnessesPrintsBothFilesOnAFormatCollision(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code"})
	existing := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")

	a := writeParsedAgent(t, ".toml")
	out := captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"codex"}, false, InstallModeCopy, lock.AgentLockEntry{Source: "o/r", SourceType: "github"}, "", cwd)
	})
	if !strings.Contains(out, existing) {
		t.Errorf("the output does not name the canonical file that already exists (%s):\n%s", existing, out)
	}
	if !strings.Contains(out, a.Path) {
		t.Errorf("the output does not name the rejected source (%s):\n%s", a.Path, out)
	}
}

// The reason lines are the agent path's own reporting. The skills installer
// still reports a failed harness by name and nothing else, pinned here by
// exact text so a later change to the agent path cannot drift it.
func TestSkillsInstallFailureOutputIsUnchanged(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	src := filepath.Join(cwd, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: s1\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() {
		installSkillsForHarnesses([]*skill.Skill{{Name: "s1", Path: src}}, []string{"no-such-harness"},
			false, InstallModeCopy, lock.SkillLockEntry{Source: "o/r", SourceType: "github"}, cwd, "")
	})

	want := "  " + ui.Text + "!" + ui.Reset + " s1 (failed for: no-such-harness)\n"
	if !strings.Contains(out, want) {
		t.Errorf("the skills failure line changed.\ngot:\n%q\nwant it to contain:\n%q", out, want)
	}
	if strings.Contains(out, "unknown harness") {
		t.Errorf("the skills path started printing install error text:\n%s", out)
	}
}

// Mutation this test catches: removing the claimed-name guard in
// installAgentsForHarnesses. Two definitions whose frontmatter names sanitize
// to one disk name write one canonical file, so the second silently replaces
// the first while the summary counts both as installed.
func TestInstallAgentsRefusesTwoDefinitionsThatClaimOneDiskName(t *testing.T) {
	cwd := t.TempDir()
	src := t.TempDir()

	first := filepath.Join(src, "first.md")
	if err := os.WriteFile(first, []byte("---\nname: Code Reviewer\ndescription: d\n---\nFIRST\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(src, "second.md")
	if err := os.WriteFile(second, []byte("---\nname: code-reviewer\ndescription: d\n---\nSECOND\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	agents := []*agentfile.AgentFile{
		{Name: "Code Reviewer", Description: "d", Instructions: "FIRST", Path: first},
		{Name: "code-reviewer", Description: "d", Instructions: "SECOND", Path: second},
	}
	baseEntry := lock.AgentLockEntry{Source: "o/r", SourceType: "github"}
	outcome := installAgentsForHarnesses(agents, []string{"claude-code"}, false, InstallModeCopy, baseEntry, "", cwd)

	if outcome.installed != 1 {
		t.Errorf("installed = %d, want 1: both definitions claim the name code-reviewer, so only one can be installed", outcome.installed)
	}

	canonical := agentCanonicalPath("code-reviewer", agentfile.FormatMarkdown, false, cwd)
	body, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "FIRST") {
		t.Errorf("canonical file holds %q, want the first definition to keep the name it claimed", string(body))
	}
}

// Mutation this test catches: removing the claimed-name guard from
// runAgentUpdateGroups. A lock key is matched against discovered definitions
// by sanitized name, so one installed name matches every source definition
// that sanitizes to it. Without the guard both are written to one canonical
// file and both are counted as updated.
func TestUpdateRefusesASecondDefinitionClaimingAnInstalledName(t *testing.T) {
	cwd := t.TempDir()
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

	// Upstream adds a second definition whose name sanitizes to the installed
	// one. Both now match the single lock key "critic".
	dupe := filepath.Join(agentsDir, "zz-dupe.md")
	if err := os.WriteFile(dupe, []byte("---\nname: Critic\ndescription: d\n---\nDUPE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	group := updateGroup{source: sourceDir, skills: []string{"critic"}, names: []string{"critic"}}
	var stats updateStats
	runAgentUpdateGroups([]updateGroup{group}, false, cwd, false, &stats)

	if stats.updated != 1 {
		t.Errorf("updated = %d, want 1: two source definitions claim the name critic, and only one canonical file exists", stats.updated)
	}
}
