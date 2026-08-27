package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
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
