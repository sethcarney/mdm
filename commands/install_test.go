package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/lock"
)

// Guards two mutations: dropping the restoreAgentsHook call, and swapping the
// two calls in runInstallFromLock. Neither restore step leaves an on-disk trace
// to assert on, so both are swapped for recorders. Not parallel-safe.
func TestRunInstallFromLockRestoresSkillsThenAgents(t *testing.T) {
	origSkills, origAgents := restoreSkillsHook, restoreAgentsHook
	t.Cleanup(func() { restoreSkillsHook, restoreAgentsHook = origSkills, origAgents })

	var order []string
	restoreSkillsHook = func(opts restoreOptions, cwd string) { order = append(order, "skills") }
	restoreAgentsHook = func(opts restoreOptions) { order = append(order, "agents") }

	runInstallFromLock(restoreOptions{yes: true})

	want := []string{"skills", "agents"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("restore order = %v, want %v", order, want)
	}
}

// The order test above still passes when restoreAgentsHook is called with the
// wrong opts. This pins that the exact opts value flows through unchanged.
func TestRunInstallFromLockPassesOptsToAgentRestore(t *testing.T) {
	origSkills, origAgents := restoreSkillsHook, restoreAgentsHook
	t.Cleanup(func() { restoreSkillsHook, restoreAgentsHook = origSkills, origAgents })

	var gotSkillsOpts restoreOptions
	var gotAgentsOpts restoreOptions
	restoreSkillsHook = func(opts restoreOptions, cwd string) { gotSkillsOpts = opts }
	restoreAgentsHook = func(opts restoreOptions) { gotAgentsOpts = opts }

	want := restoreOptions{yes: true, allowHiddenChars: true, copy: true}
	runInstallFromLock(want)

	if gotSkillsOpts != want {
		t.Errorf("skill restore opts = %+v, want %+v", gotSkillsOpts, want)
	}
	if gotAgentsOpts != want {
		t.Errorf("agent restore opts = %+v, want %+v", gotAgentsOpts, want)
	}
}

// ── An agents-only lock ────────────────────────────────────────────────────

// seedLockedAgent installs and records one "critic" definition from src into
// cwd in symlink mode, the state a project that already holds one is in
// before a restore runs.
func seedLockedAgent(t *testing.T, cwd, src string) {
	t.Helper()
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: filepath.Join(src, "agents", "critic.md")}
	entry := lock.AgentLockEntry{Source: src, SourceType: "local"}
	captureStdout(t, func() {
		installAgentsForHarnesses([]*agentfile.AgentFile{a}, []string{"claude-code"}, false, InstallModeSymlink, entry, "", cwd)
	})
	if _, ok := lock.ReadProjectLock(cwd).Agents["critic"]; !ok {
		t.Fatal("setup: the install recorded no agent lock entry")
	}
}

// seedAgentsOnlyLock is seedLockedAgent plus the guarantee the scope holds no
// skills, which is the project shape the two tests below turn on.
func seedAgentsOnlyLock(t *testing.T, cwd, src string) {
	t.Helper()
	seedLockedAgent(t, cwd, src)
	if n := len(lock.ReadLocalLock(cwd).Skills); n != 0 {
		t.Fatalf("setup: the project holds %d skill(s), want none", n)
	}
}

// Mutation this test catches: computing the presence check from the skills
// sections alone. A project whose lock holds only agent definitions is then
// told there is no lock, and pointed at `mdm skills add` — which is not the
// command that would fix anything for it.
func TestSkillRestoreOnAnAgentsOnlyLockDoesNotDenyTheLock(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()
	seedAgentsOnlyLock(t, cwd, agentSourceDir(t))

	out := captureStdout(t, func() {
		restoreSkillsFromCurrentLock(restoreOptions{yes: true}, cwd)
	})

	if strings.Contains(out, "No "+lockName+" found") {
		t.Errorf("the skill step denies a populated %s exists:\n%s", lockName, out)
	}
	if strings.Contains(out, "mdm skills add") {
		t.Errorf("a project with no skills is told to add a skill:\n%s", out)
	}
}

// Mutation this test catches: dropping Copy and Symlink from the AgentOptions
// restoreAgentsMap builds. The definitions are restored either way, so only
// the recorded mode shows whether the flag reached the agent path at all.
func TestInstallFromLockCopyRecordsTheModeOnAnAgentsOnlyLock(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	setConfigured(t, cwd, "claude-code")
	seedAgentsOnlyLock(t, cwd, agentSourceDir(t))
	assertRecordedMode(t, cwd, "")

	captureStdout(t, func() { runInstallFromLock(restoreOptions{yes: true, copy: true}) })

	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	target := agentHarnessTarget("claude-code", cwd)
	if isSymlink(t, target) {
		t.Errorf("%s is still a symlink after --copy", target)
	}
}
