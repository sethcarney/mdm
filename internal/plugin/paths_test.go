package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCwd(t *testing.T) {
	valid := []string{"", "./work", "./a/b", "${PLUGIN_ROOT}", "${PLUGIN_ROOT}/sub", "${PLUGIN_DATA}", "${PLUGIN_DATA}/cache"}
	for _, cwd := range valid {
		if err := ValidateCwd(cwd); err != nil {
			t.Errorf("ValidateCwd(%q) = %v, want nil", cwd, err)
		}
	}
	invalid := []string{"/abs", "work", "../out", "./../out", "${PLUGIN_ROOT}/../out", "${PLUGIN_DATA}/../out", "${OTHER}/x", "${PLUGIN_ROOT}extra"}
	for _, cwd := range invalid {
		if err := ValidateCwd(cwd); err == nil {
			t.Errorf("ValidateCwd(%q) = nil, want error", cwd)
		}
	}
}

func TestExpandVars(t *testing.T) {
	got := ExpandVars("${PLUGIN_ROOT}/bin --data ${PLUGIN_DATA} ${UNKNOWN}", "/root", "/data")
	want := "/root/bin --data /data ${UNKNOWN}"
	if got != want {
		t.Errorf("ExpandVars = %q, want %q", got, want)
	}
	// Single, non-recursive replacement: a placeholder inside a
	// replacement value must not be expanded again.
	got = ExpandVars("${PLUGIN_ROOT}", "/evil/${PLUGIN_DATA}", "/data")
	if got != "/evil/${PLUGIN_DATA}" {
		t.Errorf("expansion must not rescan replaced text, got %q", got)
	}
}

func TestResolveWithin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWithin(root, "./sub"); err != nil {
		t.Errorf("ResolveWithin ./sub: %v", err)
	}
	// A path that does not exist yet is fine as long as it is lexically contained.
	if _, err := ResolveWithin(root, "./sub/future"); err != nil {
		t.Errorf("ResolveWithin ./sub/future: %v", err)
	}
	for _, rel := range []string{"sub", "/abs", "./../escape", "./a/../../escape"} {
		if _, err := ResolveWithin(root, rel); err == nil {
			t.Errorf("ResolveWithin(%q) = nil, want error", rel)
		}
	}
}

func TestResolveWithinSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "sneaky")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ResolveWithin(root, "./sneaky"); err == nil {
		t.Error("symlink escaping the plugin root must be rejected")
	}
	// A symlink resolving inside the root is allowed by the spec.
	if err := os.MkdirAll(filepath.Join(root, "real"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWithin(root, "./alias"); err != nil {
		t.Errorf("internal symlink should be allowed: %v", err)
	}
}
