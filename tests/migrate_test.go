package tests_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeV1Project(t *testing.T, dir string) {
	t.Helper()
	skills := `{"version":1,"skills":{"my-skill":{"source":"./my-skill","sourceType":"local"}},"configuredAgents":["claude-code"]}`
	knowledge := `{"version":1,"bundles":{"kb":{"source":"./kb","sourceType":"local","installDir":"knowledge/kb","specVersion":"0.1","installedAt":"t","updatedAt":"t"}}}`
	for name, content := range map[string]string{"skills-lock.json": skills, "knowledge-lock.json": knowledge} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMigrateDryRunDoesNotClearGraduatedOptIns pins that `migrate --dry-run`
// leaves the global state file untouched even when it carries stale opt-ins for
// graduated features. Clearing them ran before the dry-run guard, so the dry
// run rewrote mdm-state.json - the one thing --dry-run promises never to do.
func TestMigrateDryRunDoesNotClearGraduatedOptIns(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(home, stateDir)

	statePath := filepath.Join(stateDir, "mdm", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"version":1,"experimental":["knowledge","plugins"],"skills":{}}`)
	if err := os.WriteFile(statePath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	// Nothing to migrate in the project, so this hits the clear-opt-ins branch.
	stdout, stderr, code := runMdmInDir(t, dir, env, "migrate", "--dry-run")
	if code != 0 {
		t.Fatalf("migrate --dry-run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Would clear") {
		t.Errorf("dry run should report what it would clear, got: %q", stdout)
	}
	if got, _ := os.ReadFile(statePath); string(got) != string(original) {
		t.Errorf("dry run modified the state file:\n%s", got)
	}

	// A real migrate does clear them.
	if _, stderr, code := runMdmInDir(t, dir, env, "migrate"); code != 0 {
		t.Fatalf("migrate exited %d: %s", code, stderr)
	}
	if got, _ := os.ReadFile(statePath); strings.Contains(string(got), "knowledge") {
		t.Errorf("real migrate should have cleared the stale opt-ins:\n%s", got)
	}
}

func TestMigrateDryRunChangesNothing(t *testing.T) {
	dir := t.TempDir()
	writeV1Project(t, dir)

	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "migrate", "--dry-run")
	if code != 0 {
		t.Fatalf("migrate --dry-run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "skills-lock.json") || !strings.Contains(stdout, "knowledge-lock.json") || !strings.Contains(stdout, "Dry run") {
		t.Errorf("expected a migration plan and dry-run notice, got: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); !os.IsNotExist(err) {
		t.Error("dry run must not write mdm.lock")
	}
	if _, err := os.Stat(filepath.Join(dir, "skills-lock.json")); err != nil {
		t.Error("dry run must not touch legacy files")
	}
}

func TestMigrateProjectEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeV1Project(t, dir)

	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "migrate", "-y")
	if code != 0 {
		t.Fatalf("migrate exited %d:\n%s%s", code, stdout, stderr)
	}

	lockData, err := os.ReadFile(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatalf("expected mdm.lock: %v", err)
	}
	for _, want := range []string{"my-skill", "kb", "claude-code"} {
		if !strings.Contains(string(lockData), want) {
			t.Errorf("expected %q in migrated lock, got:\n%s", want, lockData)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "knowledge-lock.json")); !os.IsNotExist(err) {
		t.Error("knowledge-lock.json should be deleted")
	}
	tomb, err := os.ReadFile(filepath.Join(dir, "skills-lock.json"))
	if err != nil {
		t.Fatalf("expected a skills-lock.json tombstone: %v", err)
	}
	if !strings.Contains(string(tomb), "_moved") {
		t.Errorf("tombstone missing marker: %s", tomb)
	}

	// Second run: nothing left to do.
	stdout, _, code = runMdmInDir(t, dir, freshEnv(t), "migrate", "-y")
	if code != 0 || !strings.Contains(stdout, "Nothing to migrate") {
		t.Errorf("expected idempotent no-op, code=%d out=%q", code, stdout)
	}
}

func TestMigrateRefusesToDiscardWithoutForce(t *testing.T) {
	dir := t.TempDir()
	// mdm.lock knows nothing; the legacy file has an entry.
	if err := os.WriteFile(filepath.Join(dir, lockName), []byte(`{"version":1,"skills":{"other":{"source":"o/r","sourceType":"github"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills-lock.json"), []byte(`{"version":1,"skills":{"stale":{"source":"o/r","sourceType":"github"}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "migrate", "-y")
	if code == 0 {
		t.Fatalf("expected refusal without --force, got:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "stale") || !strings.Contains(stdout+stderr, "--force") {
		t.Errorf("refusal should name the orphaned entry and the flag, got stdout=%q stderr=%q", stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills-lock.json")); err != nil {
		t.Error("refused migration must not touch legacy files")
	}

	if _, stderr, code := runMdmInDir(t, dir, freshEnv(t), "migrate", "-y", "--force"); code != 0 {
		t.Fatalf("migrate --force exited %d: %s", code, stderr)
	}
	if _, err := os.ReadFile(filepath.Join(dir, "skills-lock.json")); err != nil {
		t.Error("expected a tombstone after --force")
	}
}

func TestDoctorFlagsLegacyLockFiles(t *testing.T) {
	dir := t.TempDir()
	writeV1Project(t, dir)

	// The fixture's skill directories don't exist, so doctor reports
	// error-level issues and exits 1.
	stdout, _, code := runMdmInDir(t, dir, freshEnv(t), "doctor", "-p")
	if code != 1 {
		t.Fatalf("doctor with error-level issues should exit 1, exited %d", code)
	}
	if !strings.Contains(stdout, "mdm migrate") {
		t.Errorf("doctor should point at mdm migrate when v1 lock files exist, got:\n%s", stdout)
	}
}

// TestRemoveLastSkillFromV1ProjectStaysRemoved covers the upgrade path where
// v2 removes the last entry of a project that still has a v1 skills-lock.json:
// an empty mdm.lock has to shadow the v1 file, or the next read falls back to
// it and `mdm skills install` reinstalls what was just removed.
func TestRemoveLastSkillFromV1ProjectStaysRemoved(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sk", "plain")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: plain\ndescription: plain\n---\nBody.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// What a v1.93.0 `skills add ./sk/plain -p -a claude-code -y` leaves behind.
	canonical := filepath.Join(dir, ".agents", "skills", "plain")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("---\nname: plain\ndescription: plain\n---\nBody.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills-lock.json"), []byte(`{"version":1,"skills":{"plain":{"source":"sk/plain","sourceType":"local"}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, freshEnv(t), "skills", "remove", "plain", "-y")
	if code != 0 || !strings.Contains(stdout, "Removed plain") {
		t.Fatalf("remove exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); err != nil {
		t.Fatalf("an empty %s must shadow skills-lock.json after the removal: %v", lockName, err)
	}
	stdout, stderr, code = runMdmInDir(t, dir, freshEnv(t), "skills", "install", "-y")
	if code != 0 {
		t.Fatalf("install exited %d:\n%s%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "Restoring") {
		t.Fatalf("the removed skill was resurrected from skills-lock.json:\n%s", stdout)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Fatal("removed skill is back on disk")
	}
}
