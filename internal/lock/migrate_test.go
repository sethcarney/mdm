package lock

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agent"
)

func writeLegacyProjectLocks(t *testing.T, cwd string) {
	t.Helper()
	files := map[string]string{
		"skills-lock.json":    `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"}},"configuredAgents":["claude-code"]}`,
		"knowledge-lock.json": `{"version":1,"bundles":{"k1":{"source":"x","sourceType":"local","installDir":"knowledge/k1","specVersion":"0.1","installedAt":"t","updatedAt":"t"}}}`,
		"plugins-lock.json":   `{"version":1,"plugins":{"p1":{"source":"a/b","sourceType":"github","installDir":".agents/plugins/p1","dataDir":".agents/plugins-data/p1","specVersion":"1.0.0","installedAt":"t","updatedAt":"t"}}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(cwd, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectMigration(t *testing.T) {
	cwd := t.TempDir()
	writeLegacyProjectLocks(t, cwd)

	plan, err := PlanProjectMigration(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Needed() || plan.TargetExists || len(plan.Orphaned) != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	for fname, want := range map[string]int{"skills-lock.json": 1, "knowledge-lock.json": 1, "plugins-lock.json": 1} {
		if plan.Legacy[fname] != want {
			t.Errorf("plan.Legacy[%s] = %d, want %d", fname, plan.Legacy[fname], want)
		}
	}

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	assertMigratedProject(t, cwd)

	// The tombstone does not count as pending migration, and does not leak
	// into reads.
	plan, err = PlanProjectMigration(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Needed() {
		t.Errorf("tombstone should not re-trigger migration: %+v", plan)
	}
	if n := len(ReadProjectLock(cwd).Skills); n != 1 {
		t.Errorf("expected 1 skill after migration, got %d", n)
	}
}

func assertMigratedProject(t *testing.T, cwd string) {
	t.Helper()
	lk := ReadProjectLock(cwd)
	if _, ok := lk.Skills["s1"]; !ok {
		t.Error("skill entry not migrated")
	}
	if _, ok := lk.Knowledge["k1"]; !ok {
		t.Error("knowledge entry not migrated")
	}
	if _, ok := lk.Plugins["p1"]; !ok {
		t.Error("plugin entry not migrated")
	}
	if len(lk.ConfiguredAgents) != 1 {
		t.Errorf("configuredAgents not migrated: %v", lk.ConfiguredAgents)
	}

	// knowledge/plugins locks are deleted; skills-lock.json is a tombstone.
	for _, gone := range []string{"knowledge-lock.json", "plugins-lock.json"} {
		if _, err := os.Stat(filepath.Join(cwd, gone)); !os.IsNotExist(err) {
			t.Errorf("%s should be deleted", gone)
		}
	}
	data, err := os.ReadFile(filepath.Join(cwd, "skills-lock.json"))
	if err != nil {
		t.Fatalf("expected a tombstone: %v", err)
	}
	if !strings.Contains(string(data), `"_moved": "`+ProjectLockName+`"`) {
		t.Errorf("tombstone missing _moved marker: %s", data)
	}
}

func TestProjectMigrationNoTombstone(t *testing.T) {
	cwd := t.TempDir()
	writeLegacyProjectLocks(t, cwd)
	if err := ExecuteProjectMigration(cwd, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "skills-lock.json")); !os.IsNotExist(err) {
		t.Error("skills-lock.json should be deleted with tombstone=false")
	}
}

func TestPlanRejectsCorruptLegacyFile(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanProjectMigration(cwd); err == nil {
		t.Error("expected an error for a corrupt legacy file — migration must never delete what it could not read")
	}
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(`{"skills":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanProjectMigration(cwd); err == nil {
		t.Error("expected an error for a legacy file without a version")
	}
}

func TestPlanReportsOrphanedEntries(t *testing.T) {
	cwd := t.TempDir()
	// mdm.lock exists and knows s1; the stale legacy file also has s2.
	if err := AddSkillToLocalLock("s1", LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"},"s2":{"source":"o/r2","sourceType":"github"}}}`
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanProjectMigration(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.TargetExists || len(plan.Orphaned) != 1 || plan.Orphaned[0] != "skills-lock.json: s2" {
		t.Fatalf("unexpected plan: %+v", plan)
	}

	// Executing keeps mdm.lock as-is (s2 stays discarded) and retires
	// the legacy file.
	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	lk := ReadProjectLock(cwd)
	if _, ok := lk.Skills["s2"]; ok {
		t.Error("orphaned entry must not be resurrected")
	}
	if _, ok := lk.Skills["s1"]; !ok {
		t.Error("existing entry lost")
	}
}

func writeLegacyGlobalLock(t *testing.T, content string) string {
	t.Helper()
	stateDir := isolateGlobal(t)
	legacy := filepath.Join(stateDir, "skills", "skills-lock.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return legacy
}

func TestMigrateGlobalState(t *testing.T) {
	legacy := writeLegacyGlobalLock(t, `{"version":3,"skills":{"g1":{"source":"o/r","sourceType":"github","sourceUrl":"u","installedAt":"t","updatedAt":"t"}},"experimental":["knowledge"]}`)

	if err := ExecuteGlobalMigration(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy global lock should be deleted")
	}
	s := ReadGlobalState()
	if _, ok := s.Skills["g1"]; !ok {
		t.Error("global skill entry not migrated")
	}
	// Idempotent.
	if err := ExecuteGlobalMigration(); err != nil {
		t.Fatal(err)
	}
}

func TestGlobalMigrationRejectsCorruptLegacyFile(t *testing.T) {
	legacy := writeLegacyGlobalLock(t, `{"version":3,"skills":{"g1":`)

	if _, err := PlanGlobalMigration(); err == nil {
		t.Error("expected an error for a corrupt legacy global lock — migration must never delete what it could not read")
	}
	if err := ExecuteGlobalMigration(); err == nil {
		t.Error("execute must refuse a corrupt legacy global lock")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Error("legacy global lock must survive a refused migration")
	}
	if _, err := os.Stat(GetGlobalStatePath()); !os.IsNotExist(err) {
		t.Error("no mdm-state.json should be written for a refused migration")
	}
}

func TestGlobalMigrationReportsOrphanedEntries(t *testing.T) {
	writeLegacyGlobalLock(t, `{"version":3,"skills":{"g1":{"source":"o/r","sourceType":"github","sourceUrl":"u","installedAt":"t","updatedAt":"t"},"g2":{"source":"o/r2","sourceType":"github","sourceUrl":"u2","installedAt":"t","updatedAt":"t"}}}`)
	// mdm-state.json already exists and knows only g1.
	if err := WriteGlobalState(GlobalState{Skills: map[string]SkillLockEntry{"g1": {Source: "o/r", SourceType: "github"}}}); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanGlobalMigration()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.TargetExists || len(plan.Orphaned) != 1 || plan.Orphaned[0] != "g2" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestProjectMigrationRejectsUndecodableEntries(t *testing.T) {
	cwd := t.TempDir()
	// Valid JSON, but the entry does not decode into the skills struct. The
	// plan must refuse rather than promise an entry execution would drop.
	bad := `{"version":1,"skills":{"s1":{"source":123,"sourceType":"github"}}}`
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanProjectMigration(cwd); err == nil {
		t.Error("expected an error for an entry that does not decode")
	}
	if err := ExecuteProjectMigration(cwd, true); err == nil {
		t.Error("execute must refuse what the plan refuses")
	}
	if _, err := os.Stat(filepath.Join(cwd, ProjectLockName)); !os.IsNotExist(err) {
		t.Error("no mdm.lock should be written for a refused migration")
	}
	data, err := os.ReadFile(filepath.Join(cwd, "skills-lock.json"))
	if err != nil || string(data) != bad {
		t.Error("legacy file must survive a refused migration unchanged")
	}
}

func TestProjectMigrationPreservesEntryFields(t *testing.T) {
	cwd := t.TempDir()
	writeLegacyProjectLocks(t, cwd)
	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	lk := ReadProjectLock(cwd)
	if s := lk.Skills["s1"]; s.Source != "o/r" || s.SourceType != "github" {
		t.Errorf("skill fields not preserved: %+v", s)
	}
	if k := lk.Knowledge["k1"]; k.InstallDir != "knowledge/k1" || k.SpecVersion != "0.1" || k.InstalledAt != "t" {
		t.Errorf("knowledge fields not preserved: %+v", k)
	}
	if p := lk.Plugins["p1"]; p.DataDir != ".agents/plugins-data/p1" || p.SpecVersion != "1.0.0" || p.UpdatedAt != "t" {
		t.Errorf("plugin fields not preserved: %+v", p)
	}
}

// legacySkillsLockWithAgents writes a v1 skills-lock.json naming one skill
// and the agents the project had configured. Install-mode inference reads
// the per-agent install paths, so the agent list is part of the fixture.
func legacySkillsLockWithAgents(t *testing.T, cwd, agentName string) {
	t.Helper()
	legacy := `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"}},"configuredAgents":["` + agentName + `"]}`
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSkillDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestMigrationInfersCopyModeFromRealDirectories builds the on-disk shape a
// --copy install actually leaves: a real directory at the agent's own
// install path, and no canonical .agents/skills directory at all, because
// the copy branch never creates one.
func TestMigrationInfersCopyModeFromRealDirectories(t *testing.T) {
	cwd := t.TempDir()
	legacySkillsLockWithAgents(t, cwd, "claude-code")
	writeSkillDir(t, filepath.Join(cwd, ".claude", "skills", "s1"))

	plan, err := PlanProjectMigration(cwd)
	if err != nil {
		t.Fatal(err)
	}
	// The plan must name the mode it is about to write, so --dry-run can
	// report it rather than execution inventing one later.
	if plan.InstallModeBackfill != InstallModeCopy {
		t.Errorf("planned InstallModeBackfill = %q, want %q", plan.InstallModeBackfill, InstallModeCopy)
	}

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != InstallModeCopy {
		t.Errorf("InstallMode = %q, want %q", got, InstallModeCopy)
	}
}

func TestMigrationLeavesModeEmptyWithoutEvidence(t *testing.T) {
	cwd := t.TempDir()
	legacySkillsLockWithAgents(t, cwd, "claude-code")
	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Errorf("InstallMode = %q, want empty", got)
	}
}

// TestMigrationLeavesModeEmptyForSymlinkedSkills covers the default install
// shape, which is the most common input the scanner sees: a symlink install
// creates the canonical .agents/skills/<name> as a REAL directory and then
// points each agent's install path at it. Reading the canonical directory as
// evidence would flip every existing symlink project to copy, so the scan
// must look only at the per-agent paths.
func TestMigrationLeavesModeEmptyForSymlinkedSkills(t *testing.T) {
	cwd := t.TempDir()
	legacySkillsLockWithAgents(t, cwd, "claude-code")

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	writeSkillDir(t, canonical)
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, filepath.Join(linked, "s1")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Errorf("InstallMode = %q, want empty for a symlinked skill", got)
	}
}

// TestMigrationIgnoresCanonicalDirectoryAlone pins the inversion directly:
// a real canonical directory with no per-agent install path beside it (a
// symlink-mode project on a host where the link could not be created, or
// one whose agent directory was cleaned) is not evidence of copy mode.
func TestMigrationIgnoresCanonicalDirectoryAlone(t *testing.T) {
	cwd := t.TempDir()
	legacySkillsLockWithAgents(t, cwd, "claude-code")
	writeSkillDir(t, filepath.Join(cwd, ".agents", "skills", "s1"))

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Errorf("InstallMode = %q, want empty: the canonical directory is not an install location", got)
	}
}

// TestMigrationIgnoresSharedSkillsDirAgents covers agents that read the
// shared .agents/skills directory directly. Their install path IS the
// canonical directory, a real directory in both modes, so they can never
// distinguish the two and must not be read as evidence.
func TestMigrationIgnoresSharedSkillsDirAgents(t *testing.T) {
	if !agent.UsesSharedSkillsDir("amp") {
		t.Skip("fixture agent no longer uses the shared skills directory")
	}
	cwd := t.TempDir()
	legacySkillsLockWithAgents(t, cwd, "amp")
	writeSkillDir(t, filepath.Join(cwd, ".agents", "skills", "s1"))

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Errorf("InstallMode = %q, want empty for a shared-skills-dir agent", got)
	}
}

// TestMigrationBackfillsInstallModeOnExistingLock covers a project that
// already migrated to v2 before this feature existed: no legacy files are
// left to trigger a migration, but the existing mdm.lock still has no
// InstallMode recorded, and the skill on disk is a real directory.
func TestMigrationBackfillsInstallModeOnExistingLock(t *testing.T) {
	cwd := t.TempDir()
	if err := SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := AddSkillToLocalLock("s1", LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Fatalf("precondition failed: InstallMode = %q, want empty before backfill", got)
	}
	writeSkillDir(t, filepath.Join(cwd, ".claude", "skills", "s1"))

	plan, err := PlanProjectMigration(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Needed() {
		t.Fatal("plan should report a pending install-mode backfill even with no legacy files")
	}
	if plan.InstallModeBackfill != InstallModeCopy {
		t.Fatalf("InstallModeBackfill = %q, want %q", plan.InstallModeBackfill, InstallModeCopy)
	}

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != InstallModeCopy {
		t.Errorf("InstallMode = %q, want %q", got, InstallModeCopy)
	}

	// A second run must be a no-op: nothing left to migrate or backfill.
	plan, err = PlanProjectMigration(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Needed() {
		t.Errorf("plan should report nothing pending once the mode is recorded: %+v", plan)
	}
}

// injectedTestAgents holds the agents registerTestAgent has put into the
// registry and not yet cleaned up. isolateGlobal reads it so that reloading
// the registry on top of an injected agent fails loudly instead of silently
// dropping it. AllAgents is shared mutable state and so is this, so no test
// that touches either may run in parallel.
var injectedTestAgents = map[string]*agent.AgentConfig{}

// injectedTestAgentNames lists them sorted, so the failure message is stable.
func injectedTestAgentNames() []string {
	names := make([]string, 0, len(injectedTestAgents))
	for name := range injectedTestAgents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// registerTestAgent injects a throwaway agent whose install directories sit
// under temp dirs, so global-scope inference can be exercised without
// touching the real per-user agent directories. AllAgents is shared mutable
// state, so no test that calls this may run in parallel.
//
// Call isolateGlobal BEFORE this, never after: agent.Reload rebuilds AllAgents
// from scratch and would discard the injected agent, leaving the test to
// assert against the real registry and quite possibly still pass, for the
// wrong reason. Both this and isolateGlobal fail the test rather than let that
// happen silently.
func registerTestAgent(t *testing.T, name, projectDir, globalDir string) {
	t.Helper()
	cfg := &agent.AgentConfig{
		Name:            name,
		DisplayName:     name,
		SkillsDir:       projectDir,
		GlobalSkillsDir: globalDir,
	}
	agent.AllAgents[name] = cfg
	injectedTestAgents[name] = cfg
	t.Cleanup(func() {
		// Catches every way the injection can be lost, not only a mis-ordered
		// isolateGlobal: anything that rebuilds or overwrites the registry
		// mid-test leaves a different pointer, or none, behind.
		if agent.AllAgents[name] != cfg {
			t.Errorf("test agent %q was dropped from the registry during the test: something rebuilt agent.AllAgents (agent.Reload, most likely via isolateGlobal) after registerTestAgent ran, so the test asserted against the real registry", name)
		}
		delete(injectedTestAgents, name)
		delete(agent.AllAgents, name)
	})
}

// TestGlobalMigrationBackfillsInstallMode covers the global equivalent of
// the project backfill: a user who ran `mdm skills add -g --copy` before
// the switch existed has real directories on disk and no mode recorded, and
// `mdm migrate` is the documented recovery path.
func TestGlobalMigrationBackfillsInstallMode(t *testing.T) {
	isolateGlobal(t)
	globalSkills := t.TempDir()
	registerTestAgent(t, "mdm-test-agent", ".mdm-test/skills", globalSkills)

	state := EmptyGlobalState()
	state.Skills = map[string]SkillLockEntry{"s1": {Source: "o/r", SourceType: "github"}}
	state.ConfiguredAgents = []string{"mdm-test-agent"}
	if err := WriteGlobalState(state); err != nil {
		t.Fatal(err)
	}
	writeSkillDir(t, filepath.Join(globalSkills, "s1"))

	plan, err := PlanGlobalMigration()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Needed() {
		t.Fatal("plan should report a pending global install-mode backfill")
	}
	if plan.InstallModeBackfill != InstallModeCopy {
		t.Fatalf("InstallModeBackfill = %q, want %q", plan.InstallModeBackfill, InstallModeCopy)
	}

	if err := ExecuteGlobalMigration(); err != nil {
		t.Fatal(err)
	}
	if got := ReadGlobalState().InstallMode; got != InstallModeCopy {
		t.Errorf("global InstallMode = %q, want %q", got, InstallModeCopy)
	}

	// A second run must be a no-op: nothing left to migrate or backfill.
	plan, err = PlanGlobalMigration()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Needed() {
		t.Errorf("plan should report nothing pending once the global mode is recorded: %+v", plan)
	}
}

// With no agents recorded, global inference sweeps every agent the scope
// supports, and those paths live under the user's home directory. This
// exercises that sweep against a redirected home and asserts the redirect
// actually reached the agent registry: without it the sweep would read, and
// a later conversion would rewrite, real directories in the developer's own
// home.
func TestGlobalMigrationInfersCopyWithoutConfiguredAgents(t *testing.T) {
	isolateGlobal(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg := agent.AllAgents["claude-code"]
	if cfg == nil || cfg.GlobalSkillsDir == "" {
		t.Skip("fixture agent no longer supports global installs")
	}
	if !strings.HasPrefix(cfg.GlobalSkillsDir, home) {
		t.Fatalf("agent registry not isolated: global skills dir %q is outside the test home %q", cfg.GlobalSkillsDir, home)
	}
	writeSkillDir(t, filepath.Join(cfg.GlobalSkillsDir, "s1"))

	state := EmptyGlobalState()
	state.Skills = map[string]SkillLockEntry{"s1": {Source: "o/r", SourceType: "github"}}
	if err := WriteGlobalState(state); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanGlobalMigration()
	if err != nil {
		t.Fatal(err)
	}
	if plan.InstallModeBackfill != InstallModeCopy {
		t.Errorf("InstallModeBackfill = %q, want %q with no configuredAgents recorded", plan.InstallModeBackfill, InstallModeCopy)
	}
}

// A symlinked global install is not copy evidence, and leaves the key
// absent rather than writing the literal "symlink".
func TestGlobalMigrationLeavesModeEmptyForSymlinks(t *testing.T) {
	isolateGlobal(t)
	globalSkills := t.TempDir()
	registerTestAgent(t, "mdm-test-agent", ".mdm-test/skills", globalSkills)

	canonical := filepath.Join(t.TempDir(), "s1")
	writeSkillDir(t, canonical)
	if err := os.Symlink(canonical, filepath.Join(globalSkills, "s1")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	state := EmptyGlobalState()
	state.Skills = map[string]SkillLockEntry{"s1": {Source: "o/r", SourceType: "github"}}
	state.ConfiguredAgents = []string{"mdm-test-agent"}
	if err := WriteGlobalState(state); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanGlobalMigration()
	if err != nil {
		t.Fatal(err)
	}
	if plan.InstallModeBackfill != "" {
		t.Errorf("InstallModeBackfill = %q, want empty for a symlinked global install", plan.InstallModeBackfill)
	}
	if plan.Needed() {
		t.Errorf("nothing should be pending: %+v", plan)
	}
}

// TestMigrationInfersCopyWithoutConfiguredAgents covers the shape a
// non-interactive install leaves behind: configuredAgents only records
// agents picked from the interactive picker, so `mdm skills add <src> -a
// claude-code -y` writes skills and no agent list at all. Scanning only the
// recorded list would find nothing for exactly the --copy user this scan
// exists to serve.
func TestMigrationInfersCopyWithoutConfiguredAgents(t *testing.T) {
	cwd := t.TempDir()
	legacy := `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"}}}`
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	writeSkillDir(t, filepath.Join(cwd, ".claude", "skills", "s1"))

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != InstallModeCopy {
		t.Errorf("InstallMode = %q, want %q with no configuredAgents recorded", got, InstallModeCopy)
	}
}

// A real directory that is not a skill is not evidence of a copy install.
// The all-agents fallback scans every agent the scope supports, so any
// unrelated directory sitting at <some agent skills dir>/<locked skill name>
// would otherwise flip the whole scope to copy. Every mdm-installed skill
// directory holds a SKILL.md, so requiring one removes the false positive
// without weakening the true one.
func TestMigrationIgnoresDirectoriesWithoutSkillMd(t *testing.T) {
	cwd := t.TempDir()
	legacy := `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"}}}`
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	// A directory at an agent's install path, under the locked skill's name,
	// that mdm did not install: no SKILL.md inside.
	notASkill := filepath.Join(cwd, ".roo", "skills", "s1")
	if err := os.MkdirAll(notASkill, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notASkill, "notes.md"), []byte("# mine\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Errorf("InstallMode = %q, want empty: a directory with no SKILL.md is not an mdm install", got)
	}
}

// The same fallback must not turn a symlink project into copy: the wider
// agent sweep still only reports a mode for a real directory.
func TestMigrationWithoutConfiguredAgentsStillHonorsSymlinks(t *testing.T) {
	cwd := t.TempDir()
	legacy := `{"version":1,"skills":{"s1":{"source":"o/r","sourceType":"github"}}}`
	if err := os.WriteFile(filepath.Join(cwd, "skills-lock.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	writeSkillDir(t, canonical)
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, filepath.Join(linked, "s1")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	if err := ExecuteProjectMigration(cwd, true); err != nil {
		t.Fatal(err)
	}
	if got := ReadProjectLock(cwd).InstallMode; got != "" {
		t.Errorf("InstallMode = %q, want empty", got)
	}
}
