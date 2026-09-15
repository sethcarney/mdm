package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

// writeOwnCritic puts a hand-written critic definition at harnessName's own
// path, as a user who never ran mdm for that harness would, and returns the
// path and its bytes.
func writeOwnCritic(t *testing.T, harnessName, cwd string) (string, string) {
	t.Helper()
	own := agentHarnessTarget(harnessName, cwd)
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: critic\ndescription: mine, written by hand\n---\n\nMy own critic.\n"
	if err := os.WriteFile(own, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return own, content
}

// An install records which harnesses received the definition. Before it did,
// every later command inferred the list by stat-ing <harnessDir>/<name> for
// every harness, so a hand-written .claude/agents/critic.md counted as an
// install of a definition added with --harness cursor - and `agents remove`
// deleted it.
//
// Mutation this catches: dropping entry.Harnesses from installAgents.
func TestInstallAgentsRecordsTheHarnessesThatReceivedTheDefinition(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"cursor", "claude-code"})
	entry := criticEntry(t, cwd)
	if len(entry.Harnesses) != 2 || entry.Harnesses[0] != "claude-code" || entry.Harnesses[1] != "cursor" {
		t.Errorf("Harnesses = %v, want [claude-code cursor]", entry.Harnesses)
	}

	// A later add of the same definition to one more harness joins the list
	// rather than replacing it: the first two still hold the file.
	src := filepath.Join(t.TempDir(), "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"gemini-cli"}, false, InstallModeCopy, lock.AgentLockEntry{Source: "o/r", SourceType: "github"}, "", cwd)
	entry = criticEntry(t, cwd)
	if strings.Join(entry.Harnesses, ",") != "claude-code,cursor,gemini-cli" {
		t.Errorf("Harnesses after a second add = %v, want [claude-code cursor gemini-cli]", entry.Harnesses)
	}
}

// The repro: a hand-written Claude Code definition, then the same name
// installed to Cursor only. Remove must delete Cursor's copy and nothing
// else; list must name Cursor and not Claude Code.
func TestRemoveLeavesAHandWrittenFileInAHarnessTheLockDoesNotName(t *testing.T) {
	cwd := t.TempDir()
	own, original := writeOwnCritic(t, "claude-code", cwd)
	installCriticTo(t, cwd, []string{"cursor"})

	st := agentStatusFor("critic", criticEntry(t, cwd), false, cwd)
	if strings.Join(st.InstalledIn, ",") != "cursor" {
		t.Errorf("InstalledIn = %v, want [cursor]: the hand-written Claude Code file is not an install", st.InstalledIn)
	}

	res, err := removeAgentFromDisk("critic", nil, criticEntry(t, cwd), false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !res.fullyRemoved {
		t.Error("fullyRemoved = false; Cursor was the only harness holding it")
	}
	got, err := os.ReadFile(own)
	if err != nil {
		t.Fatalf("the hand-written Claude Code definition was deleted: %v", err)
	}
	if string(got) != original {
		t.Errorf("the hand-written definition changed:\n%s", got)
	}
	if _, err := os.Lstat(agentHarnessTarget("cursor", cwd)); !os.IsNotExist(err) {
		t.Errorf("Cursor's copy survived, stat err = %v", err)
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; ok {
		t.Error("lock entry survived a complete removal")
	}
}

// An entry written by an earlier build has no harness list, so the harnesses
// are inferred - but only from files mdm can show it wrote. The hand-written
// file's bytes are neither the canonical bytes nor what mdm would have
// encoded for that harness, so it is the user's and stays, with a note.
//
// Mutation this catches: inferring from os.Lstat alone, as before.
func TestRemoveInfersOnlyOwnedFilesForAnEntryWithoutAHarnessList(t *testing.T) {
	cwd := t.TempDir()
	own, original := writeOwnCritic(t, "claude-code", cwd)
	installCriticTo(t, cwd, []string{"cursor"})
	entry := criticEntry(t, cwd)
	entry.Harnesses = nil
	if err := lock.AddAgentToLocalLock("critic", entry, cwd); err != nil {
		t.Fatal(err)
	}
	entry = criticEntry(t, cwd)
	if len(entry.Harnesses) != 0 {
		t.Fatal("setup: the entry still records a harness list")
	}

	if got := agentInstalledIn("critic", entry, false, cwd); strings.Join(got, ",") != "cursor" {
		t.Errorf("inferred harnesses = %v, want [cursor]", got)
	}

	res, err := removeAgentFromDisk("critic", nil, entry, false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !res.fullyRemoved {
		t.Error("fullyRemoved = false; Cursor was the only harness mdm wrote to")
	}
	if got, err := os.ReadFile(own); err != nil || string(got) != original {
		t.Errorf("the hand-written Claude Code definition was touched (err=%v):\n%s", err, got)
	}
	if _, err := os.Lstat(agentHarnessTarget("cursor", cwd)); !os.IsNotExist(err) {
		t.Errorf("Cursor's copy survived, stat err = %v", err)
	}
}

// A copy the user edited after the install is theirs. Remove leaves it and
// says so; the lock entry still goes, since mdm no longer has anything there.
func TestRemoveLeavesAnEditedCopyAloneWithANote(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code"})
	target := agentHarnessTarget("claude-code", cwd)
	edited := "---\nname: critic\ndescription: d\n---\nEDITED BY HAND\n"
	if err := os.WriteFile(target, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := removeAgentFromDisk("critic", nil, criticEntry(t, cwd), false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.foreign) != 1 || !strings.Contains(res.foreign[0], target) {
		t.Errorf("foreign = %v, want the edited copy's path", res.foreign)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != edited {
		t.Errorf("the edited copy was deleted or changed (err=%v):\n%s", err, got)
	}
	if !res.fullyRemoved {
		t.Error("fullyRemoved = false; nothing mdm owns is left")
	}
}

// Update refreshes the harnesses the lock names, and only the files mdm
// wrote there. A copy the user edited is left alone and named in the output;
// a hand-written file in a harness the lock does not name is never looked at.
func TestAgentsUpdateLeavesFilesMdmDidNotWriteAlone(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.SetInstallMode(lock.InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}
	own, original := writeOwnCritic(t, "gemini-cli", cwd)

	sourceDir := t.TempDir()
	agentsDir := filepath.Join(sourceDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentsDir, "critic.md")
	writeCriticSource(t, agentPath, "v1")
	a := &agentfile.AgentFile{Name: "critic", Description: "v1", Path: agentPath}
	installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code", "cursor"}, false, InstallModeCopy, lock.AgentLockEntry{Source: sourceDir, SourceType: "local"}, "", cwd)

	cursorTarget := agentHarnessTarget("cursor", cwd)
	edited := "---\nname: critic\ndescription: v1\n---\nEDITED BY HAND\n"
	if err := os.WriteFile(cursorTarget, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	writeCriticSource(t, agentPath, "v2")
	group := updateGroup{source: sourceDir, skills: []string{"critic"}, names: []string{"critic"}}
	var stats updateStats
	out := captureStdout(t, func() {
		runAgentUpdateGroups([]updateGroup{group}, false, cwd, false, &stats)
	})

	if got, _ := os.ReadFile(agentHarnessTarget("claude-code", cwd)); !strings.Contains(string(got), "v2") {
		t.Errorf("Claude Code's copy, which mdm wrote, was not refreshed:\n%s", got)
	}
	if got, _ := os.ReadFile(cursorTarget); string(got) != edited {
		t.Errorf("Cursor's hand-edited copy was overwritten:\n%s", got)
	}
	if !strings.Contains(out, "Cursor") || !strings.Contains(out, "not the one mdm wrote") {
		t.Errorf("the update did not say it left Cursor's edited copy alone:\n%s", out)
	}
	if got, _ := os.ReadFile(own); string(got) != original {
		t.Errorf("the hand-written Gemini definition, in a harness the lock never named, was overwritten:\n%s", got)
	}
	if stats.updated != 1 {
		t.Errorf("stats.updated = %d, want 1", stats.updated)
	}
	if entry := criticEntry(t, cwd); strings.Join(entry.Harnesses, ",") != "claude-code,cursor" {
		t.Errorf("Harnesses after update = %v, want the recorded list kept", entry.Harnesses)
	}
}

// A restore puts each definition back into the harnesses its lock entry
// names. An entry with no list is inferred from the files mdm wrote, and one
// with nothing on disk either is the only kind the user is asked about.
func TestRestoreTargetsFollowTheLockNotEveryCapableHarness(t *testing.T) {
	cwd := t.TempDir()
	writeOwnCritic(t, "claude-code", cwd)
	installCriticTo(t, cwd, []string{"cursor"})
	recorded := criticEntry(t, cwd)

	inferred := recorded
	inferred.Harnesses = nil
	fresh := lock.AgentLockEntry{Source: "o/r", SourceType: "github", AgentPath: "agents/ghost.md"}

	entries := map[string]lock.AgentLockEntry{"critic": recorded, "ghost": fresh}
	harnessesFor, unlisted := restoreTargets(entries, false, cwd)
	if strings.Join(harnessesFor["critic"], ",") != "cursor" {
		t.Errorf("recorded entry restores to %v, want [cursor]", harnessesFor["critic"])
	}
	if strings.Join(unlisted, ",") != "ghost" {
		t.Errorf("unlisted = %v, want [ghost]: only an entry with no list and nothing on disk needs the user asked", unlisted)
	}

	// Without the list, the hand-written Claude Code file must not count.
	harnessesFor, unlisted = restoreTargets(map[string]lock.AgentLockEntry{"critic": inferred}, false, cwd)
	if strings.Join(harnessesFor["critic"], ",") != "cursor" {
		t.Errorf("inferred entry restores to %v, want [cursor]", harnessesFor["critic"])
	}
	if len(unlisted) != 0 {
		t.Errorf("unlisted = %v, want none", unlisted)
	}
}

// mustWrite writes content at path, creating its directory.
func mustWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// codexEncoding returns the canonical definition at path re-encoded as TOML,
// which is what Codex's file holds after an install.
func codexEncoding(t *testing.T, path string) []byte {
	t.Helper()
	parsed, err := agentfile.ParseAgentFile(path)
	if err != nil || parsed == nil {
		t.Fatalf("canonical did not parse: %v", err)
	}
	encoded, err := agentfile.Encode(parsed, agentfile.FormatTOML)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

// A symlink whose canonical file has gone still names it, so a removal after
// the canonical directory was deleted by hand recognizes its own links; a
// copy in that state cannot be checked and is not mdm's to delete.
func TestMdmOwnsAgentFileRecognizesItsOwnLinksAndCopies(t *testing.T) {
	cwd := t.TempDir()
	canonical := agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)
	body := "---\nname: critic\ndescription: d\n---\nbody\n"
	mustWrite(t, canonical, []byte(body))
	harnessDir := harness.AgentsInstallDirFor("claude-code", false, cwd)
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(harnessDir, "critic.md")
	if err := os.Symlink(canonical, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	copyPath := filepath.Join(harnessDir, "copy.md")
	mustWrite(t, copyPath, []byte(body))
	other := filepath.Join(harnessDir, "other.md")
	mustWrite(t, other, []byte(body+"more\n"))
	codexPath := filepath.Join(harness.AgentsInstallDirFor("codex", false, cwd), "critic.toml")
	mustWrite(t, codexPath, codexEncoding(t, canonical))

	if !mdmOwnsAgentFile(link, "claude-code", canonical) {
		t.Error("a symlink to the canonical file is not recognized as mdm's")
	}
	if !mdmOwnsAgentFile(copyPath, "claude-code", canonical) {
		t.Error("a byte copy of the canonical file is not recognized as mdm's")
	}
	if !mdmOwnsAgentFile(codexPath, "codex", canonical) {
		t.Error("the canonical definition encoded for Codex is not recognized as mdm's")
	}
	if mdmOwnsAgentFile(other, "claude-code", canonical) {
		t.Error("a file with different bytes is claimed as mdm's")
	}

	if err := os.Remove(canonical); err != nil {
		t.Fatal(err)
	}
	if !mdmOwnsAgentFile(link, "claude-code", canonical) {
		t.Error("a link left dangling by deleting the canonical file is not recognized as mdm's")
	}
	if mdmOwnsAgentFile(copyPath, "claude-code", canonical) {
		t.Error("a copy that can no longer be compared to anything is claimed as mdm's")
	}
}
