package commands

import (
	"reflect"
	"testing"
)

// Mutations this test catches:
//  1. `mdm install` restores skills but silently skips agents (the call to
//     restoreAgentsHook is dropped, or never wired in) — the recorded order
//     would be missing "agents" entirely, failing the length/content check.
//  2. Agent restore runs BEFORE skills instead of after (the two calls in
//     runInstallFromLock are swapped) — the recorded order would be
//     ["agents", "skills"], failing the exact-order check.
//
// Neither restoreSkillsFromCurrentLock nor restoreAgentsFromLock has an
// isolated on-disk side effect a test can assert on without a real network
// fetch or an interactive prompt (both eventually call runAdd/runAgentAdd),
// so this swaps them for recorders — the same seam-var pattern already used
// for copyDirFn/copyFileFn/renameFn/removeFileFn in install_mode.go and
// agent_artifacts.go. Not parallel-safe, for the same reason those aren't.
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

// A narrower regression guard than the order test above: even with agent
// restore correctly wired in, a mutation that calls restoreAgentsHook with
// the wrong opts value (rather than dropping or reordering the call
// entirely) would still pass the order test. Pin down that the exact opts
// value flows through unchanged.
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
