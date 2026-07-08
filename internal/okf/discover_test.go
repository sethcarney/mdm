package okf

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFindBundleRootsNested(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bundles", "ga4", "index.md"), "# GA4\n")
	writeFile(t, filepath.Join(root, "bundles", "ga4", "tables", "index.md"), "# nested index belongs to ga4\n")
	writeFile(t, filepath.Join(root, "bundles", "bitcoin", "index.md"), "# Bitcoin\n")
	writeFile(t, filepath.Join(root, "README.md"), "not a bundle doc\n")

	roots := FindBundleRoots(root)
	if len(roots) != 2 {
		t.Fatalf("expected 2 bundle roots, got %v", roots)
	}
	for _, r := range roots {
		base := filepath.Base(r)
		if base != "ga4" && base != "bitcoin" {
			t.Errorf("unexpected bundle root %q", r)
		}
	}
}

func TestFindBundleRootsIndexAtTop(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.md"), "# top\n")
	writeFile(t, filepath.Join(root, "sub", "index.md"), "# sub index belongs to the top bundle\n")

	roots := FindBundleRoots(root)
	if len(roots) != 1 || roots[0] != root {
		t.Fatalf("expected [%s], got %v", root, roots)
	}
}

func TestFindBundleRootsNoIndexFallsBackToRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "concepts", "a.md"), "---\ntype: Concept\n---\n")

	roots := FindBundleRoots(root)
	if len(roots) != 1 || roots[0] != root {
		t.Fatalf("expected [%s], got %v", root, roots)
	}
}

func TestFindBundleRootsEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "data.json"), "{}")

	if roots := FindBundleRoots(root); len(roots) != 0 {
		t.Fatalf("expected no bundle roots, got %v", roots)
	}
}

func TestHashBundleDirDetectsChanges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.md"), "# v1\n")

	h1, err := HashBundleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashBundleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("hash not deterministic: %s vs %s", h1, h2)
	}

	writeFile(t, filepath.Join(root, "index.md"), "# v2\n")
	h3, err := HashBundleDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Error("expected hash to change when content changes")
	}
}
