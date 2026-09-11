package pathsafe

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestIsSafeRelDir(t *testing.T) {
	cases := map[string]bool{
		"agents":       true,
		"a/b":          true,
		"a/../b":       true,
		"":             false,
		".":            false,
		"./":           false,
		".//":          false,
		"x/..":         false,
		"agents/../":   false,
		"..":           false,
		"../x":         false,
		"a/../../x":    false,
		"/etc/passwd":  false,
		"/Users/other": false,
	}
	if runtime.GOOS == "windows" {
		cases[`C:\x`] = false
		cases[`\x`] = false
	}
	for d, want := range cases {
		if got := IsSafeRelDir(d); got != want {
			t.Errorf("IsSafeRelDir(%q) = %v, want %v", d, got, want)
		}
	}
}

// ResolvedContains judges the resolved path, so a candidate equal to the root
// is inside it, a sibling is not, and a symlink inside the root that points
// outside is not either. A candidate that does not exist cannot be judged
// and counts as unsafe.
func TestResolvedContains(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "agents")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	if !ResolvedContains(resolvedRoot, root) {
		t.Error("the root itself should count as inside")
	}
	if !ResolvedContains(resolvedRoot, inside) {
		t.Error("a real subdirectory should count as inside")
	}
	if ResolvedContains(resolvedRoot, outside) {
		t.Error("a sibling directory should not count as inside")
	}
	if ResolvedContains(resolvedRoot, filepath.Join(root, "missing")) {
		t.Error("a path that does not exist cannot be judged and must be unsafe")
	}

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	if ResolvedContains(resolvedRoot, link) {
		t.Error("a symlink under the root that resolves outside it must be unsafe")
	}
	if ResolvedContains(resolvedRoot, filepath.Join(link, "x")) {
		t.Error("a path through an escaping symlink must be unsafe")
	}
}
