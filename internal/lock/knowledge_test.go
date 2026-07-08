package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKnowledgeLockRoundTrip(t *testing.T) {
	cwd := t.TempDir()

	if lk := ReadKnowledgeLock(cwd); len(lk.Bundles) != 0 {
		t.Fatalf("expected empty lock, got %v", lk.Bundles)
	}

	entry := KnowledgeLockEntry{
		Source:      "acme/sales-knowledge",
		SourceType:  "github",
		Ref:         "v1.0.0",
		InstallDir:  "knowledge/sales",
		SpecVersion: "0.1",
		ContentHash: "sha256:abc",
	}
	if err := AddBundleToKnowledgeLock("sales", entry, cwd); err != nil {
		t.Fatal(err)
	}

	lk := ReadKnowledgeLock(cwd)
	got, ok := lk.Bundles["sales"]
	if !ok {
		t.Fatal("expected sales entry after add")
	}
	if got.Ref != "v1.0.0" || got.InstallDir != "knowledge/sales" || got.SpecVersion != "0.1" {
		t.Errorf("unexpected entry: %+v", got)
	}
	if got.InstalledAt == "" || got.UpdatedAt == "" {
		t.Error("expected timestamps to be set")
	}

	// Re-adding preserves InstalledAt.
	entry.Ref = "v1.1.0"
	if err := AddBundleToKnowledgeLock("sales", entry, cwd); err != nil {
		t.Fatal(err)
	}
	updated := ReadKnowledgeLock(cwd).Bundles["sales"]
	if updated.InstalledAt != got.InstalledAt {
		t.Errorf("InstalledAt changed on update: %q vs %q", updated.InstalledAt, got.InstalledAt)
	}
	if updated.Ref != "v1.1.0" {
		t.Errorf("Ref = %q, want v1.1.0", updated.Ref)
	}
}

func TestKnowledgeLockRemoveDeletesFileWhenEmpty(t *testing.T) {
	cwd := t.TempDir()
	if err := AddBundleToKnowledgeLock("a", KnowledgeLockEntry{Source: "x", SourceType: "local", InstallDir: "knowledge/a", SpecVersion: "0.1"}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := RemoveBundleFromKnowledgeLock("a", cwd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "knowledge-lock.json")); !os.IsNotExist(err) {
		t.Error("expected knowledge-lock.json to be deleted when the last bundle is removed")
	}
	// Removing a missing entry is a no-op.
	if err := RemoveBundleFromKnowledgeLock("missing", cwd); err != nil {
		t.Fatal(err)
	}
}

func TestKnowledgeLockDoesNotTouchSkillLocks(t *testing.T) {
	cwd := t.TempDir()
	if err := AddSkillToLocalLock("my-skill", LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(GetLocalLockPath(cwd))
	if err != nil {
		t.Fatal(err)
	}

	if err := AddBundleToKnowledgeLock("sales", KnowledgeLockEntry{Source: "x", SourceType: "local", InstallDir: "knowledge/sales", SpecVersion: "0.1"}, cwd); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(GetLocalLockPath(cwd))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("knowledge lock operations must not modify skills-lock.json")
	}
}
