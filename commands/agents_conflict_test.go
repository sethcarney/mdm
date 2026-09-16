package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/lock"
)

// writeNamedSource writes one definition with the given frontmatter name
// under a fresh source directory and returns the parsed file.
func writeNamedSource(t *testing.T, name, body string) *agentfile.AgentFile {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, strings.ToLower(name)+".md")
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: d\n---\n"+body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &agentfile.AgentFile{Name: name, Description: "d", Instructions: body, Path: path}
}

// The guard against two names sanitizing to one disk name lived inside one
// run. Across runs, `add srcA --harness cursor` (critic) and then `add srcB
// --harness claude-code` (Critic) silently repointed the canonical file and
// the lock entry at srcB, and Cursor's link served srcB's body. A recorded
// entry from another source is refused unless the add is forced.
//
// Mutation this catches: dropping the agentSourceConflict check, or making
// it ignore the source.
func TestInstallAgentsRefusesANameTheLockRecordsFromAnotherSource(t *testing.T) {
	cwd := t.TempDir()
	first := writeNamedSource(t, "critic", "FROM A")
	srcA := lock.AgentLockEntry{Source: "owner/a", SourceType: "github", Ref: "main"}
	installAgentsForHarnesses([]*agentfile.AgentFile{first}, []string{"cursor"}, false, InstallModeCopy, srcA, filepath.Dir(filepath.Dir(first.Path)), cwd)

	second := writeNamedSource(t, "Critic", "FROM B")
	srcB := lock.AgentLockEntry{Source: "owner/b", SourceType: "github", Ref: "main"}
	outcome := installAgentsForHarnesses([]*agentfile.AgentFile{second}, []string{"claude-code"}, false, InstallModeCopy, srcB, filepath.Dir(filepath.Dir(second.Path)), cwd)
	if outcome.installed != 0 || outcome.refused != 1 {
		t.Errorf("installed = %d, refused = %d; want 0 and 1", outcome.installed, outcome.refused)
	}
	// The wording, which names both sources and --force, is asserted through
	// the binary in tests/agents_conflict_test.go, where stderr is captured.

	canonical := agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)
	if got, _ := os.ReadFile(canonical); !strings.Contains(string(got), "FROM A") {
		t.Errorf("the canonical file was repointed at the second source:\n%s", got)
	}
	entry := criticEntry(t, cwd)
	if entry.Source != "owner/a" {
		t.Errorf("lock Source = %q, want owner/a", entry.Source)
	}
	if _, err := os.Lstat(agentHarnessTarget("claude-code", cwd)); !os.IsNotExist(err) {
		t.Errorf("Claude Code received the refused definition (stat err=%v)", err)
	}

	// Forced, the replacement is complete: the lock names srcB, and every
	// harness still serving srcA's definition now serves srcB's too.
	outcome = installAgents([]*agentfile.AgentFile{second}, []string{"claude-code"}, false, InstallModeCopy, srcB, filepath.Dir(filepath.Dir(second.Path)), cwd, agentInstallRun{force: true})
	if outcome.installed != 1 {
		t.Fatalf("forced install: installed = %d, want 1", outcome.installed)
	}
	if got, _ := os.ReadFile(canonical); !strings.Contains(string(got), "FROM B") {
		t.Errorf("the forced replacement left the canonical file at the first source:\n%s", got)
	}
	entry = criticEntry(t, cwd)
	if entry.Source != "owner/b" || strings.Join(entry.Harnesses, ",") != "claude-code,cursor" {
		t.Errorf("lock after a forced replacement = %+v, want Source owner/b held by claude-code and cursor", entry)
	}
	if got, _ := os.ReadFile(agentHarnessTarget("cursor", cwd)); !strings.Contains(string(got), "FROM B") {
		t.Errorf("Cursor still serves the first source after a forced replacement:\n%s", got)
	}
}

// The same source spelled two ways is one source, and re-adding it at
// another ref is a re-pin, not a collision. Only a different file of the same
// source - two files whose names sanitize alike - collides.
func TestAgentSourceConflictTellsSourcesApart(t *testing.T) {
	cwd := t.TempDir()
	prior := lock.AgentLockEntry{Source: "owner/repo", SourceType: "github", Ref: "main", AgentPath: "agents/critic.md"}

	same := lock.AgentLockEntry{Source: "https://github.com/owner/repo", SourceType: "github", Ref: "v2"}
	if got := agentSourceConflict(prior, same, "agents/critic.md", "critic", "", cwd); got != "" {
		t.Errorf("the same repository under another spelling and ref was reported as a conflict: %s", got)
	}
	if got := agentSourceConflict(prior, same, "custom/Critic.md", "critic", "", cwd); got == "" {
		t.Error("a different file of the same source, sanitizing to the installed name, was not reported")
	}
	other := lock.AgentLockEntry{Source: "owner/other", SourceType: "github", Ref: "main"}
	if got := agentSourceConflict(prior, other, "agents/critic.md", "critic", "", cwd); got == "" {
		t.Error("a different repository was not reported")
	}

	// An entry written before the path was recorded is compared by source alone.
	old := prior
	old.AgentPath = ""
	if got := agentSourceConflict(old, same, "custom/Critic.md", "critic", "", cwd); got != "" {
		t.Errorf("an entry with no recorded path was compared by path: %s", got)
	}

	// Local sources compare resolved: the lock keeps ./src, a fresh entry the absolute path.
	local := lock.AgentLockEntry{Source: "./src", SourceType: "local", AgentPath: "agents/critic.md"}
	fresh := lock.AgentLockEntry{Source: filepath.Join(cwd, "src"), SourceType: "local"}
	if got := agentSourceConflict(local, fresh, "agents/critic.md", "critic", "", cwd); got != "" {
		t.Errorf("the same local directory, relative and absolute, was reported as a conflict: %s", got)
	}
}

// A definition that moved within the same source is a relocation, not a
// conflict: when the prior file no longer exists under the fetched root, the
// re-add proceeds without --force. When it still exists, two files really do
// claim one name and the conflict stands.
func TestAgentSourceConflictAllowsAMoveWithinTheSameSource(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	prior := lock.AgentLockEntry{Source: "owner/repo", SourceType: "github", Ref: "main", AgentPath: "agents/critic.md"}
	incoming := lock.AgentLockEntry{Source: "owner/repo", SourceType: "github", Ref: "main"}

	// The prior path is gone from the source: the definition moved.
	if got := agentSourceConflict(prior, incoming, "custom-agents/critic.md", "critic", root, cwd); got != "" {
		t.Errorf("a move within the same source should not conflict, got: %s", got)
	}

	// The prior path still exists alongside the new one: a genuine duplicate.
	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agents", "critic.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := agentSourceConflict(prior, incoming, "custom-agents/critic.md", "critic", root, cwd); got == "" {
		t.Error("two files of the same source claiming one name should still conflict")
	}
}
