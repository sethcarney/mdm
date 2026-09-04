// Package pathsafe holds the containment checks mdm applies to directory paths
// that an installed source declares, before it opens anything they name.
// Callers run IsSafeRelDir on the declared string, then ResolvedContains on the
// candidate resolved on disk. Neither check is sufficient alone.
package pathsafe

import (
	"path/filepath"
	"strings"
)

// IsSafeRelDir reports whether d is a source-relative directory that cannot
// escape the search root, judged from the path string alone. It guards the
// directories a plugin manifest declares, and nothing else. filepath.IsAbs is
// too weak here: on Windows it reports false for a rooted-but-driveless path
// like "/Users/victim". "." names the search root, not a subdirectory.
func IsSafeRelDir(d string) bool {
	if d == "" || d == "." {
		return false
	}
	return filepath.IsLocal(d)
}

// ResolvedContains reports whether candidate, once symlinks are resolved, still
// lies inside resolvedRoot, which the caller resolves first. A resolution error
// counts as unsafe. The check-then-use window is a known and accepted gap:
// callers reopen candidate by its unresolved name, so a swap between the two
// calls defeats this.
func ResolvedContains(resolvedRoot, candidate string) bool {
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
