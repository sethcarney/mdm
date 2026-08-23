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
	if _, err := os.Stat(filepath.Join(dir, "mdm-lock.json")); !os.IsNotExist(err) {
		t.Error("dry run must not write mdm-lock.json")
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

	lockData, err := os.ReadFile(filepath.Join(dir, "mdm-lock.json"))
	if err != nil {
		t.Fatalf("expected mdm-lock.json: %v", err)
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
	// mdm-lock.json knows nothing; the legacy file has an entry.
	if err := os.WriteFile(filepath.Join(dir, "mdm-lock.json"), []byte(`{"version":1,"skills":{"other":{"source":"o/r","sourceType":"github"}}}`), 0600); err != nil {
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
