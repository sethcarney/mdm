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

// ── `mdm skills install` restores skills and agent definitions ────────────

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

// The pointer is the only thing telling a user of `mdm skills install` that
// their lock holds sections it does not restore. Agent definitions are not
// among them - naming those here would tell the user to go restore something
// this very command just restored.
func TestSkillsInstallPointsAtTheUmbrellaForUnrestoredSections(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()
	seedAllSections(t, cwd)

	out := captureStdout(t, func() { hintOtherSections(cwd) })

	if !strings.Contains(out, "knowledge bundle(s)") || !strings.Contains(out, "plugin(s)") {
		t.Errorf("the pointer does not name the sections left unrestored:\n%s", out)
	}
	if !strings.Contains(out, "mdm install") {
		t.Errorf("the pointer does not name the command that restores them:\n%s", out)
	}
	if strings.Contains(out, "agent definition(s)") {
		t.Errorf("the pointer names agent definitions, which this command restores itself:\n%s", out)
	}
}

// ── `mdm install` step order and options ───────────────────────────────────

// swapInstallSteps replaces the four restore steps with recorders. None of
// them leaves an on-disk trace a test can tell apart from the others', so the
// order and the options that reach each one are only observable here.
func swapInstallSteps(t *testing.T, order *[]string, opts *map[string]restoreOptions) {
	t.Helper()
	origS, origA := installSkillsStep, installAgentsStep
	origK, origP := installKnowledgeStep, installPluginsStep
	t.Cleanup(func() {
		installSkillsStep, installAgentsStep = origS, origA
		installKnowledgeStep, installPluginsStep = origK, origP
	})
	installSkillsStep = func(global bool, o restoreOptions, cwd string) {
		*order = append(*order, "skills")
		(*opts)["skills"] = o
	}
	installAgentsStep = func(global bool, o restoreOptions, cwd string) {
		*order = append(*order, "agents")
		(*opts)["agents"] = o
	}
	installKnowledgeStep = func(allowHiddenChars bool) { *order = append(*order, "knowledge") }
	installPluginsStep = func(allowHiddenChars, skipMCP bool) { *order = append(*order, "plugins") }
}

// Guards two mutations: dropping a step, and reordering them. Plugins last is
// load-bearing, not cosmetic - see the comment in runInstallAll.
func TestRunInstallAllRestoresEverySectionInOrder(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedAllSections(t, cwd)

	var order []string
	recorded := map[string]restoreOptions{}
	swapInstallSteps(t, &order, &recorded)

	captureStdout(t, func() { runInstallAll(installAllOptions{restoreOptions: restoreOptions{yes: true}}) })

	want := []string{"skills", "agents", "knowledge", "plugins"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("restore order = %v, want %v", order, want)
	}
}

// The order test still passes when a step is called with the wrong opts. This
// pins that the exact value flows through to both steps that take one.
func TestRunInstallAllPassesOptsToEachStep(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedAllSections(t, cwd)

	var order []string
	recorded := map[string]restoreOptions{}
	swapInstallSteps(t, &order, &recorded)

	want := restoreOptions{yes: true, allowHiddenChars: true, copy: true}
	captureStdout(t, func() { runInstallAll(installAllOptions{restoreOptions: want}) })

	for _, step := range []string{"skills", "agents"} {
		if recorded[step] != want {
			t.Errorf("%s step opts = %+v, want %+v", step, recorded[step], want)
		}
	}
}

// An empty section must not run its step: each per-section restore prints its
// own "nothing found, add one with ..." block, and four of those for a
// skills-only project is three paragraphs telling the user nothing.
func TestRunInstallAllSkipsEmptySections(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	seedAgentsOnlyLock(t, cwd, agentSourceDir(t))

	var order []string
	recorded := map[string]restoreOptions{}
	swapInstallSteps(t, &order, &recorded)

	captureStdout(t, func() { runInstallAll(installAllOptions{restoreOptions: restoreOptions{yes: true}}) })

	if !reflect.DeepEqual(order, []string{"agents"}) {
		t.Fatalf("restore order = %v, want only the populated section", order)
	}
}

// ── `mdm install` scope resolution ─────────────────────────────────────────

// The rule: a lock in the working directory settles it with no prompt, the
// flags override, and --yes never reaches global scope on its own.
func TestResolveInstallScope(t *testing.T) {
	tests := []struct {
		name     string
		opts     installAllOptions
		withLock bool
		want     installScope
	}{
		{"a project lock settles it", installAllOptions{}, true, scopeProject},
		{"--project overrides a missing lock", installAllOptions{project: true}, false, scopeProject},
		{"--global overrides a present lock", installAllOptions{global: true}, true, scopeGlobal},
		{"no lock and nothing global", installAllOptions{}, false, scopeNone},
		{"--yes will not choose global", installAllOptions{restoreOptions: restoreOptions{yes: true}}, false, scopeNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t)
			cwd := t.TempDir()
			if tc.withLock {
				seedAgentsOnlyLock(t, cwd, agentSourceDir(t))
			}
			var got installScope
			captureStdout(t, func() { got = resolveInstallScope(tc.opts, cwd) })
			if got != tc.want {
				t.Errorf("resolveInstallScope = %v, want %v", got, tc.want)
			}
		})
	}
}

// Mutation this test catches: letting --yes fall through to global scope when
// the working directory has no lock. A CI step that meant to restore a
// checkout would install into the runner's home directory and report success.
func TestResolveInstallScopeYesRefusesImplicitGlobal(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()
	seedGlobalSkill(t)

	var got installScope
	out := captureStdout(t, func() {
		got = resolveInstallScope(installAllOptions{restoreOptions: restoreOptions{yes: true}}, cwd)
	})

	if got != scopeCancelled {
		t.Fatalf("resolveInstallScope = %v, want scopeCancelled", got)
	}
	if !strings.Contains(out, "--global") {
		t.Errorf("the refusal does not name the flag that would allow it:\n%s", out)
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
// told there is no lock, and pointed at `mdm skills add` - which is not the
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
// It runs through `mdm install`, which is what restores agent definitions
// alongside skills now that `mdm skills install` has narrowed to its own
// section.
func TestInstallAllCopyRecordsTheModeOnAnAgentsOnlyLock(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	setConfigured(t, cwd, "claude-code")
	seedAgentsOnlyLock(t, cwd, agentSourceDir(t))
	assertRecordedMode(t, cwd, "")

	captureStdout(t, func() {
		runInstallAll(installAllOptions{restoreOptions: restoreOptions{yes: true, copy: true}})
	})

	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	target := agentHarnessTarget("claude-code", cwd)
	if isSymlink(t, target) {
		t.Errorf("%s is still a symlink after --copy", target)
	}
}

// ── Seeding a lock that records every section ──────────────────────────────

// seedAllSections records one entry in each of the four lock sections. The
// steps under test are swapped for recorders, so only the counts matter here -
// nothing re-fetches these sources.
func seedAllSections(t *testing.T, cwd string) {
	t.Helper()
	if err := lock.AddSkillToLocalLock("a-skill", lock.LocalSkillLockEntry{
		Source: cwd, SourceType: "local",
	}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddAgentToLocalLock("an-agent", lock.AgentLockEntry{
		Source: cwd, SourceType: "local", AgentPath: "agents/an-agent.md",
	}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddBundleToKnowledgeLock("a-bundle", lock.KnowledgeLockEntry{
		Source: cwd, SourceType: "local", InstallDir: "knowledge/a-bundle",
	}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddPluginToLock("a-plugin", lock.PluginLockEntry{
		Source: cwd, SourceType: "local", InstallDir: ".agents/plugins/a-plugin",
	}, cwd); err != nil {
		t.Fatal(err)
	}
}

// seedGlobalSkill records one skill in the global state file, the state that
// makes a working directory with no lock worth offering global scope for.
func seedGlobalSkill(t *testing.T) {
	t.Helper()
	if err := lock.AddSkillToGlobalState("a-global-skill", lock.SkillLockEntry{
		Source: "example/repo", SourceType: "github",
	}); err != nil {
		t.Fatal(err)
	}
}

// Mutation this test catches: narrowing the "is this really an empty lock"
// check back to agent definitions alone. A lock recording only knowledge
// bundles or only plugins is still a lock, and telling its owner there is none
// - then pointing them at `mdm skills add` - answers a question they did not
// ask with a command that would not help.
func TestSkillRestoreOnAKnowledgeOrPluginOnlyLockDoesNotDenyTheLock(t *testing.T) {
	for _, section := range []string{"knowledge", "plugins"} {
		t.Run(section, func(t *testing.T) {
			isolateHome(t)
			cwd := t.TempDir()
			var err error
			if section == "knowledge" {
				err = lock.AddBundleToKnowledgeLock("kb", lock.KnowledgeLockEntry{
					Source: cwd, SourceType: "local", InstallDir: "knowledge/kb",
				}, cwd)
			} else {
				err = lock.AddPluginToLock("pg", lock.PluginLockEntry{
					Source: cwd, SourceType: "local", InstallDir: ".agents/plugins/pg",
				}, cwd)
			}
			if err != nil {
				t.Fatal(err)
			}

			out := captureStdout(t, func() {
				restoreSkillsFromCurrentLock(restoreOptions{yes: true}, cwd)
			})

			if strings.Contains(out, "No "+lockName+" found") {
				t.Errorf("the skill step denies a populated %s exists:\n%s", lockName, out)
			}
			if strings.Contains(out, "mdm skills add") {
				t.Errorf("a project with no skills is told to add a skill:\n%s", out)
			}
		})
	}
}
