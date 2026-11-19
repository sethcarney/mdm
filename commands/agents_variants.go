// Harness-specific variants: one agent a source ships in two formats, a
// general markdown definition and a native Codex TOML one. The pair is one
// definition everywhere - one name, one lock entry, one file per harness - and
// these helpers answer the two questions that then depend on the harness:
// which variant it is handed, and which canonical file its copy came from.
package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

// agentFormats lists every format mdm knows, in the order a lock entry's
// formats are reported: the order is fixed so nothing depends on map order.
var agentFormats = []agentfile.Format{agentfile.FormatMarkdown, agentfile.FormatTOML}

// lockedAgentFormats returns the formats a lock entry has a canonical file
// in: the primary first, then any variant. The lock is a file people edit and
// merge, so a variant key that is not a format, or that repeats the primary,
// is ignored rather than turned into a path.
func lockedAgentFormats(entry lock.AgentLockEntry) []agentfile.Format {
	primary := lockedAgentFormat(entry)
	out := []agentfile.Format{primary}
	for _, f := range agentFormats {
		if f != primary && entry.Variants[string(f)] != "" {
			out = append(out, f)
		}
	}
	return out
}

// lockedFormatFor returns the format of the canonical file harnessName's copy
// comes from: the harness's own format when the entry records a variant in
// it, and otherwise the primary, which is then converted.
func lockedFormatFor(entry lock.AgentLockEntry, harnessName string) agentfile.Format {
	want := harness.AgentFormat(harnessName)
	for _, f := range lockedAgentFormats(entry) {
		if f == want {
			return f
		}
	}
	return lockedAgentFormat(entry)
}

// agentServingCanonical returns the canonical file harnessName's copy of a
// recorded definition comes from. Ownership is always judged against this
// file: a Codex copy of a paired definition is a link to, or a copy of, the
// TOML variant, and comparing it to the markdown one would call every such
// install a file the user wrote.
func agentServingCanonical(name string, entry lock.AgentLockEntry, harnessName string, global bool, cwd string) string {
	return agentCanonicalPath(name, lockedFormatFor(entry, harnessName), global, cwd)
}

// agentCanonicalPaths returns every canonical file a lock entry stands for,
// the primary first.
func agentCanonicalPaths(name string, entry lock.AgentLockEntry, global bool, cwd string) []string {
	var out []string
	for _, f := range lockedAgentFormats(entry) {
		out = append(out, agentCanonicalPath(name, f, global, cwd))
	}
	return out
}

// lockedAgentSourcePaths returns the source-relative path of every file the
// entry was installed from, the primary first. Empty for an entry written
// before the path was recorded.
func lockedAgentSourcePaths(entry lock.AgentLockEntry) []string {
	var out []string
	if entry.AgentPath != "" {
		out = append(out, entry.AgentPath)
	}
	primary := lockedAgentFormat(entry)
	for _, f := range agentFormats {
		if p := entry.Variants[string(f)]; f != primary && p != "" {
			out = append(out, p)
		}
	}
	return out
}

// agentVariantFor returns the variant of a that harnessName is handed.
func agentVariantFor(a *agentfile.AgentFile, harnessName string) *agentfile.AgentFile {
	return a.VariantFor(harness.AgentFormat(harnessName))
}

// agentVariantPaths returns the lock's record of a's variant: its format
// mapped to its path in the source. Nil for a definition with no variant, so
// the key is omitted and the entry is what it always was.
func agentVariantPaths(a *agentfile.AgentFile, rootDir string) map[string]string {
	if a.Alt == nil {
		return nil
	}
	return map[string]string{string(agentCanonicalFormat(a.Alt)): agentFileRepoPath(a.Alt.Path, rootDir)}
}

// agentSourceFormats returns the formats a discovered definition ships in.
func agentSourceFormats(a *agentfile.AgentFile) map[agentfile.Format]bool {
	out := map[agentfile.Format]bool{}
	for _, v := range a.Variants() {
		out[agentCanonicalFormat(v)] = true
	}
	return out
}

// writeAgentCanonicals gives every variant of a its canonical file. Installing
// into a harness writes only the variant that harness reads, so a pair
// installed to markdown harnesses alone would otherwise have no TOML canonical
// while its lock entry records one, and list, doctor and a later install into
// Codex would each have to guess whether that is damage. Both files always
// exist for a paired entry; that is the invariant they check.
func writeAgentCanonicals(a *agentfile.AgentFile, name string, global bool, cwd string) error {
	for _, v := range a.Variants() {
		if err := copyAgentFileUnlessSame(v.Path, agentCanonicalPath(name, agentCanonicalFormat(v), global, cwd)); err != nil {
			return err
		}
	}
	return nil
}

// reservedHarnesses returns the harnesses in held whose canonical file
// changes when the entry prior is replaced by the definition a: a harness that
// was reading a conversion and now has a native variant, or the reverse when
// the source dropped one. Such a harness has to be rewritten in the same run.
// Left alone, its copy would be one made from a canonical file the lock no
// longer points it at, which mdmOwnsAgentFile cannot recognize - so update
// would skip it and remove would refuse it, for a file mdm wrote.
func reservedHarnesses(prior lock.AgentLockEntry, a *agentfile.AgentFile, held []string) []string {
	var out []string
	for _, h := range held {
		if lockedFormatFor(prior, h) != agentCanonicalFormat(agentVariantFor(a, h)) {
			out = append(out, h)
		}
	}
	return out
}

// pruneDroppedCanonicals deletes the canonical file of every format the prior
// entry recorded and the definition no longer ships. stale names the
// harnesses this run did not rewrite: a format one of them still reads from is
// kept, since its copy can be a link to that very file.
func pruneDroppedCanonicals(name string, prior lock.AgentLockEntry, a *agentfile.AgentFile, stale []string, global bool, cwd string) {
	now := agentSourceFormats(a)
	for _, f := range lockedAgentFormats(prior) {
		if now[f] {
			continue
		}
		inUse := false
		for _, h := range stale {
			if lockedFormatFor(prior, h) == f {
				inUse = true
				break
			}
		}
		if inUse {
			continue
		}
		path := agentCanonicalPath(name, f, global, cwd)
		if !isPathSafe(harness.CanonicalAgentsDir(global, cwd), path) {
			continue
		}
		if err := removeFileFn(path); err != nil && !os.IsNotExist(err) {
			ui.LogWarn(fmt.Sprintf("%s: could not remove the %s canonical file the source no longer ships (%s): %v", name, agentFormatLabel(f), path, err))
		}
	}
}

// agentFormatLabels renders formats for a user-facing line, sorted.
func agentFormatLabels(formats []agentfile.Format) string {
	labels := make([]string, 0, len(formats))
	for _, f := range formats {
		labels = append(labels, agentFormatLabel(f))
	}
	sort.Strings(labels)
	return strings.Join(labels, ", ")
}
