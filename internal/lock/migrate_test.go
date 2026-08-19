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
	if !strings.Contains(string(data), `"_moved": "mdm-lock.json"`) {
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
	// mdm-lock.json exists and knows s1; the stale legacy file also has s2.
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

	// Executing keeps mdm-lock.json as-is (s2 stays discarded) and retires
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

func TestMigrateGlobalState(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateDir)
	legacy := filepath.Join(stateDir, "skills", "skills-lock.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	content := `{"version":3,"skills":{"g1":{"source":"o/r","sourceType":"github","sourceUrl":"u","installedAt":"t","updatedAt":"t"}},"experimental":["knowledge"]}`
	if err := os.WriteFile(legacy, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := MigrateGlobalState(); err != nil {
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
	if err := MigrateGlobalState(); err != nil {
		t.Fatal(err)
	}
}
