package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/lock"
)

// addProjectItself runs the install half of `mdm agents add .`: discovery on
// cwd, then an install of what it found into harnesses, with cwd as the
// local source. It returns critic's lock entry and fails when any harness
// did not receive a file.
func addProjectItself(t *testing.T, cwd string, mode InstallMode, harnesses ...string) lock.AgentLockEntry {
	t.Helper()
	agents, err := agentfile.DiscoverAgentFiles(cwd, "")
	if err != nil || len(agents) != 1 {
		t.Fatalf("discovery: err=%v agents=%d", err, len(agents))
	}
	baseEntry := lock.AgentLockEntry{Source: cwd, SourceType: "local"}
	installAgentsForHarnesses(agents, harnesses, false, mode, baseEntry, cwd, cwd)
	for _, h := range harnesses {
		if _, err := os.Lstat(agentHarnessTarget(h, cwd)); err != nil {
			t.Fatalf("setup: %s did not receive the definition: %v", h, err)
		}
	}
	return criticEntry(t, cwd)
}

// `mdm agents add .` discovers a hand-written .claude/agents/critic.md and
// installs it: the canonical file is written, the source becomes a link to
// it, and Codex's TOML and Copilot's copy are written beside it. Remove then
// found every one of those paths inside the local source - the project root
// - and kept them all, dropping the lock entry while the Codex and Copilot
// files stayed live. Only the file discovery found is the user's; the lock
// records which one that was, and remove keeps that one.
//
// Mutation this catches: recording no AgentPath for a local install, or
// falling back to "keep everything inside the source" when one is recorded.
func TestRemoveAfterAddOnTheProjectKeepsOnlyTheDiscoveredSource(t *testing.T) {
	cwd := t.TempDir()
	own, original := writeOwnCritic(t, "claude-code", cwd)
	entry := addProjectItself(t, cwd, InstallModeSymlink, "claude-code", "codex", "github-copilot")
	if entry.AgentPath != filepath.ToSlash(filepath.Join(".claude", "agents", "critic.md")) {
		t.Fatalf("AgentPath = %q, want the discovered file relative to the source", entry.AgentPath)
	}
	if fi, err := os.Lstat(own); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Skipf("the source was not adopted as a symlink (err=%v); nothing to un-adopt on this platform", err)
	}
	codexFile := agentHarnessTarget("codex", cwd)
	copilotFile := agentHarnessTarget("github-copilot", cwd)

	res, err := removeAgentFromDisk("critic", nil, entry, false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !res.fullyRemoved {
		t.Error("fullyRemoved = false")
	}
	if len(res.kept) != 1 || res.kept[0] != own {
		t.Errorf("kept = %v, want only the discovered source %s", res.kept, own)
	}
	fi, err := os.Lstat(own)
	if err != nil {
		t.Fatalf("the user's own definition is gone: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("the user's definition is still a symlink; it should be a real file again")
	}
	if got, _ := os.ReadFile(own); string(got) != original {
		t.Errorf("the user's definition changed:\n%s", got)
	}
	for _, p := range []string{codexFile, copilotFile, agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived: mdm wrote it beside the source and must delete it (stat err=%v)", p, err)
		}
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; ok {
		t.Error("lock entry survived a complete removal")
	}
}

// An entry written before the source file was recorded has no AgentPath for
// a local install. Nothing can tell the source from what mdm wrote beside
// it, so nothing inside the source directory is deleted, as before.
func TestRemoveWithoutARecordedSourcePathKeepsEverythingInsideTheSource(t *testing.T) {
	cwd := t.TempDir()
	writeOwnCritic(t, "claude-code", cwd)
	entry := addProjectItself(t, cwd, InstallModeCopy, "claude-code", "codex")
	entry.AgentPath = ""
	if err := lock.AddAgentToLocalLock("critic", entry, cwd); err != nil {
		t.Fatal(err)
	}

	res, err := removeAgentFromDisk("critic", nil, criticEntry(t, cwd), false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !res.fullyRemoved {
		t.Error("fullyRemoved = false")
	}
	if len(res.kept) != 3 {
		t.Errorf("kept = %v, want the Claude Code file, Codex's TOML and the canonical file", res.kept)
	}
	if _, err := os.Lstat(agentHarnessTarget("codex", cwd)); err != nil {
		t.Errorf("Codex's file was deleted although the entry cannot say it was not the source: %v", err)
	}
	if !strings.Contains(strings.Join(res.kept, "\n"), agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)) {
		t.Errorf("the canonical file inside the source was not kept: %v", res.kept)
	}
}
