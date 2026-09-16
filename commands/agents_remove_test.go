package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/lock"
)

// A harness file mdm cannot show it wrote is left where it is - so the
// definition is still installed there. The removal used to count such a
// harness as cleared: the canonical file went, the lock entry went, "Removed
// critic" printed, the command exited 0, and the file it had just declined to
// touch was left with nothing able to manage it.
//
// The same computation took the new harness list off the harnesses found on
// disk, so Gemini, whose file the user deleted by hand, was de-registered by a
// removal that never touched it. The list comes off the recorded one now,
// minus what this run actually cleared.
//
// Mutations this catches: computing the harnesses still holding the definition
// from the ones the removal tried rather than the ones it cleared; writing the
// new list from `held` rather than from the entry's recorded list.
func TestRemoveKeepsWhatItCouldNotClearAndDeregistersOnlyWhatItDid(t *testing.T) {
	cwd := t.TempDir()
	installCriticTo(t, cwd, []string{"claude-code", "cursor", "gemini-cli"})
	edited := "---\nname: critic\ndescription: d\n---\nEDITED BY HAND\n"
	if err := os.WriteFile(agentHarnessTarget("cursor", cwd), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(agentHarnessTarget("gemini-cli", cwd)); err != nil {
		t.Fatal(err)
	}

	res, err := removeAgentFromDisk("critic", nil, criticEntry(t, cwd), false, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if res.fullyRemoved {
		t.Error("fullyRemoved = true although Cursor's copy was left in place")
	}
	if got, _ := os.ReadFile(agentHarnessTarget("cursor", cwd)); string(got) != edited {
		t.Errorf("Cursor's edited copy was deleted or changed:\n%s", got)
	}
	if _, err := os.Lstat(agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)); err != nil {
		t.Errorf("the canonical file was deleted while Cursor still holds the definition: %v", err)
	}
	entry, ok := lock.ReadProjectLock(cwd).Agents["critic"]
	if !ok {
		t.Fatal("the lock entry was dropped, so nothing can manage the copy left behind")
	}
	if strings.Join(entry.Harnesses, ",") != "cursor,gemini-cli" {
		t.Errorf("Harnesses = %v, want [cursor gemini-cli]: only Claude Code was cleared", entry.Harnesses)
	}

	// A removal that could not complete must not report success either, or a
	// script reads a definition left behind as gone.
	var reported bool
	out := captureStdout(t, func() {
		reported = removeOneAgent("critic", nil, criticEntry(t, cwd), false, cwd)
	})
	if reported {
		t.Error("removeOneAgent reported success although the definition is still installed")
	}
	if strings.Contains(out, "Removed critic") {
		t.Errorf("a removal that left the definition in place printed a clean success:\n%s", out)
	}
}
