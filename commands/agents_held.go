// Which harnesses hold an agent definition, and which of their files mdm may
// touch. The lock's harness list is the record; entries written before it
// existed are inferred from the disk, and in both cases only a file mdm can
// show it wrote is ever deleted or overwritten.
package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

// symlinkNamesCanonical reports whether the symlink at linkPath points at
// canonicalPath. Both are resolved when they can be; a dangling link - the
// canonical file has gone - is compared by the path it names instead, so a
// removal after `rm -rf .agents/agents` still recognizes its own links.
func symlinkNamesCanonical(linkPath, canonicalPath string) bool {
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err == nil {
		canonicalResolved, cerr := filepath.EvalSymlinks(canonicalPath)
		return cerr == nil && filepath.Clean(resolved) == filepath.Clean(canonicalResolved)
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	targetAbs, err1 := filepath.Abs(target)
	canonicalAbs, err2 := filepath.Abs(canonicalPath)
	return err1 == nil && err2 == nil && filepath.Clean(targetAbs) == filepath.Clean(canonicalAbs)
}

// mdmOwnsAgentFile reports whether the file harnessName reads at harnessPath
// is one mdm wrote from canonicalPath: a symlink to the canonical file, or a
// real file holding the canonical bytes or the canonical definition
// re-encoded for that harness (what encodeForHarness produces for Codex).
// Anything else - a hand-written file, a copy the user has edited since, a
// directory - is the user's, and no command deletes or overwrites it. A file
// that cannot be checked, because the canonical file it would be compared to
// has gone, is not mdm's to touch either.
func mdmOwnsAgentFile(harnessPath, harnessName, canonicalPath string) bool {
	fi, err := os.Lstat(harnessPath)
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return symlinkNamesCanonical(harnessPath, canonicalPath)
	}
	if !fi.Mode().IsRegular() {
		return false
	}
	got, err := os.ReadFile(harnessPath)
	if err != nil {
		return false
	}
	want, err := os.ReadFile(canonicalPath)
	if err != nil {
		return false
	}
	if bytes.Equal(got, want) {
		return true
	}
	a, err := agentfile.ParseAgentFile(canonicalPath)
	if err != nil || a == nil {
		return false
	}
	encoded, err := encodeForHarness(a, harnessName)
	if err != nil || encoded == nil {
		return false
	}
	return bytes.Equal(got, encoded)
}

// agentOwnedHarnesses returns, sorted, every harness with an agent-definition
// directory for this scope whose file for name is one mdm wrote. It is the
// inference for a lock entry that predates the harness list: such an entry
// cannot say where mdm installed the definition, and a same-named file in a
// harness mdm never wrote to is the user's rather than an install.
func agentOwnedHarnesses(name string, entry lock.AgentLockEntry, global bool, cwd string) []string {
	canonicalPath := agentCanonicalPath(name, lockedAgentFormat(entry), global, cwd)
	var found []string
	for harnessName := range harness.AllHarnesses {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target != "" && mdmOwnsAgentFile(target, harnessName, canonicalPath) {
			found = append(found, harnessName)
		}
	}
	sort.Strings(found)
	return found
}

// agentRecordedHarnesses returns the lock entry's harness list, sorted, or
// nil for an entry that predates the list.
func agentRecordedHarnesses(entry lock.AgentLockEntry) []string {
	if len(entry.Harnesses) == 0 {
		return nil
	}
	out := append([]string(nil), entry.Harnesses...)
	sort.Strings(out)
	return out
}

// agentInstalledIn returns, sorted, the harnesses holding the definition that
// still have its file on disk: the lock's list when the entry records one,
// with any harness whose file has gone missing dropped, and otherwise the
// harnesses whose file mdm can vouch for. A harness copy can go missing
// while the canonical file stays untouched, and list, remove and update all
// want the ones that are actually there.
func agentInstalledIn(name string, entry lock.AgentLockEntry, global bool, cwd string) []string {
	recorded := agentRecordedHarnesses(entry)
	if recorded == nil {
		return agentOwnedHarnesses(name, entry, global, cwd)
	}
	var present []string
	for _, harnessName := range recorded {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target == "" {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			present = append(present, harnessName)
		}
	}
	return present
}

// agentMissingFrom returns, sorted, the harnesses the lock says hold the
// definition but which have no file for it on disk. Nil for an entry that
// predates the harness list: there is no record to be missing from.
func agentMissingFrom(name string, entry lock.AgentLockEntry, global bool, cwd string) []string {
	var missing []string
	for _, harnessName := range agentRecordedHarnesses(entry) {
		target := agentHarnessPath(name, harnessName, global, cwd)
		if target == "" {
			continue
		}
		if _, err := os.Lstat(target); err != nil {
			missing = append(missing, harnessName)
		}
	}
	return missing
}

// unionHarnesses merges harness lists into one sorted list without
// duplicates.
func unionHarnesses(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, h := range list {
			if !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	sort.Strings(out)
	return out
}

// withoutHarnesses returns the names in list that are not in drop, in order.
func withoutHarnesses(list []string, drop map[string]bool) []string {
	var out []string
	for _, h := range list {
		if !drop[h] {
			out = append(out, h)
		}
	}
	return out
}
