package okf

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func hasIndexMd(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "index.md"))
	return err == nil && !info.IsDir()
}

func hasMarkdown(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if p != dir && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(name), ".md") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// FindBundleRoots locates OKF bundle roots under root. A bundle root is a
// directory containing an index.md whose ancestors contain none (nested
// index.md files belong to the same bundle). When no index.md exists
// anywhere, root itself is the bundle if it holds any markdown at all —
// index files are recommended by the spec but not required.
func FindBundleRoots(root string) []string {
	var roots []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if p != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
			return filepath.SkipDir
		}
		if hasIndexMd(p) {
			roots = append(roots, p)
			return filepath.SkipDir
		}
		return nil
	})
	if len(roots) == 0 && hasMarkdown(root) {
		return []string{root}
	}
	return roots
}
