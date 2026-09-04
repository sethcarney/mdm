package commands

import (
	"reflect"
	"testing"
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
