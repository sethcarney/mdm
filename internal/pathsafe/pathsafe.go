// Package pathsafe holds the containment checks mdm applies to directory
// paths that come out of a source being installed — a plugin manifest's
// skillDirs or agentsDirs, or a caller-supplied subpath — before it opens
// anything they name.
//
// It exists as its own package because both internal/skill and
// internal/agentfile need exactly this pair of checks, and a security guard
// is the last thing that should exist as two copies: two copies drift, and
// only one of them gets the next fix.
//
// The two checks are meant to be used together and in this order. IsSafeRelDir
// is a lexical pass over the declared string; ResolvedContains resolves the
// candidate on disk and compares it with the real search root. Neither is
// sufficient alone: a string that looks local can be a symlink out of the
// tree, and a resolved path tells you nothing about a declaration like "."
// that is in-tree but not a subdirectory at all.
package pathsafe

import (
	"path/filepath"
	"strings"
)

// IsSafeRelDir reports whether d is a source-relative directory that cannot
// escape the search root, judged purely from the path STRING. Every directory
// a plugin manifest declares goes through it, because the manifest lives
// inside the source being installed and that source is third-party.
//
// This is a cheap first pass only. It cannot see that a directory entry which
// lexically looks fine (e.g. "extra-skills") is actually a symlink pointing
// somewhere else on disk; callers additionally resolve every candidate against
// the real search root, see ResolvedContains. filepath.IsAbs alone is not
// enough here either: on Windows it reports false for a rooted-but-driveless
// path like "/Users/victim", so a naive absolute check would let that through.
// filepath.IsLocal covers that, plus "..", empty, and (on Windows) reserved
// device names in one lexical pass; "." is rejected on top because a source
// declaring "." would make the search root scan itself as a "declared"
// subdirectory, which is harmless but not what skillDirs or agentsDirs means.
func IsSafeRelDir(d string) bool {
	if d == "" || d == "." {
		return false
	}
	return filepath.IsLocal(d)
}

// ResolvedContains reports whether candidate, once symlinks are resolved,
// still lies inside resolvedRoot (itself already resolved by the caller).
//
// The paths come from the source being installed, which is untrusted: a
// directory or file inside that source can be a symlink whose target lies
// anywhere on the victim's disk (e.g. an "extra-skills" entry that is really
// a link to the user's Documents folder), and mdm would then scan it and
// install whatever it found there. IsSafeRelDir only inspects the declared
// path string and cannot see that; only resolving the entry on disk and
// comparing it with the real root closes that gap. Any resolution error,
// including a broken or looping symlink, is treated as unsafe rather than
// followed.
//
// This deliberately leaves a check-then-use window open: the candidate is
// resolved here and then reopened by its unresolved name (os.ReadFile /
// os.ReadDir), so an attacker who swaps a real entry for a symlink between
// the two defeats it. Closing that needs handle-based open-then-verify APIs
// Go does not expose portably, this is a one-shot scan at install time, and
// an attacker able to write into the source tree mid-install can simply drop
// a hostile skill or agent file into it instead, which is the threat the
// callers already assume.
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
