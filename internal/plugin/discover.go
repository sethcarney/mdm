package plugin

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var skipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"__pycache__":  true,
}

func hasManifest(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ManifestFile))
	return err == nil && info.Mode().IsRegular()
}

// FindPluginRoots locates plugin roots under root. A plugin.json at root
// itself wins outright; otherwise directories up to two levels deep are
// scanned, which covers both single-plugin repositories and monorepos
// laid out as plugins/<name>/.
func FindPluginRoots(root string) []string {
	if hasManifest(root) {
		return []string{root}
	}
	var roots []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return nil //nolint:nilerr // unreadable entries are skipped, not fatal
		}
		name := d.Name()
		if skipDirs[name] || strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil || strings.Count(rel, string(filepath.Separator)) >= 2 {
			return filepath.SkipDir
		}
		if hasManifest(p) {
			roots = append(roots, p)
			return filepath.SkipDir
		}
		return nil
	})
	sort.Strings(roots)
	return roots
}
