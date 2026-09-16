package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/lock"
)

// "A forced replacement is a replacement everywhere" read the prior entry's
// harness list raw, so an entry written before that list existed - every
// v2.0.0-beta entry - unioned with nothing. The forced add reached only the
// harnesses it named and recorded only those, leaving Cursor serving the
// replaced source with no lock entry naming it: exactly the entries the
// ownership mechanism exists to protect were the ones it skipped.
//
// Mutation this catches: reading prior.Harnesses, or recorded[name].Harnesses
// for the new list, instead of resolving the entry's harnesses through the
// owned-file inference.
func TestAddResolvesTheHarnessesOfAnEntryWithNoList(t *testing.T) {
	cwd := t.TempDir()
	first := writeNamedSource(t, "critic", "FROM A")
	srcA := lock.AgentLockEntry{Source: "owner/a", SourceType: "github", Ref: "main"}
	installAgentsForHarnesses([]*agentfile.AgentFile{first}, []string{"claude-code", "cursor"}, false, InstallModeCopy, srcA, filepath.Dir(filepath.Dir(first.Path)), cwd)

	// What a build that predates the harness list wrote: no list at all.
	entry := criticEntry(t, cwd)
	entry.Harnesses = nil
	if err := lock.AddAgentToLocalLock("critic", entry, cwd); err != nil {
		t.Fatal(err)
	}

	second := writeNamedSource(t, "Critic", "FROM B")
	srcB := lock.AgentLockEntry{Source: "owner/b", SourceType: "github", Ref: "main"}
	rootB := filepath.Dir(filepath.Dir(second.Path))
	outcome := installAgents([]*agentfile.AgentFile{second}, []string{"claude-code"}, false, InstallModeCopy, srcB, rootB, cwd, agentInstallRun{force: true})
	if outcome.installed != 1 {
		t.Fatalf("forced install: installed = %d, want 1", outcome.installed)
	}
	if got, _ := os.ReadFile(agentHarnessTarget("cursor", cwd)); !strings.Contains(string(got), "FROM B") {
		t.Errorf("Cursor still serves the replaced source after a forced replacement:\n%s", got)
	}
	if entry := criticEntry(t, cwd); strings.Join(entry.Harnesses, ",") != "claude-code,cursor" {
		t.Errorf("Harnesses = %v, want [claude-code cursor]: a forced replacement replaces everywhere", entry.Harnesses)
	}

	// The same nil made an ordinary narrowing re-add de-register the harness
	// it did not name, so the new list is resolved the same way.
	listless := criticEntry(t, cwd)
	listless.Harnesses = nil
	if err := lock.AddAgentToLocalLock("critic", listless, cwd); err != nil {
		t.Fatal(err)
	}
	installAgentsForHarnesses([]*agentfile.AgentFile{second}, []string{"claude-code"}, false, InstallModeCopy, srcB, rootB, cwd)
	if entry := criticEntry(t, cwd); strings.Join(entry.Harnesses, ",") != "claude-code,cursor" {
		t.Errorf("Harnesses after a narrowing re-add = %v, want [claude-code cursor]: Cursor still holds it", entry.Harnesses)
	}
}
