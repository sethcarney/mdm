package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

const (
	pairMd       = "---\nname: reviewer\ndescription: d\nmodel: sonnet\n---\nreview\n"
	pairTOML     = "name = \"reviewer\"\ndescription = \"d\"\ndeveloper_instructions = \"review natively\"\nmodel = \"gpt-5\"\nsandbox_mode = \"read-only\"\n"
	pairMdPath   = "agents/reviewer.md"
	pairTOMLPath = "agents/reviewer.toml"
)

var pairBase = lock.AgentLockEntry{Source: "o/r", SourceType: "github"}

// writeSource writes the given files under a fresh source root and returns
// the root and what discovery finds in it.
func writeSource(t *testing.T, files map[string]string) (string, []*agentfile.AgentFile) {
	t.Helper()
	root := t.TempDir()
	rediscover(t, root, files)
	found, err := agentfile.DiscoverAgentFiles(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return root, found
}

// rediscover replaces a source root's content.
func rediscover(t *testing.T, root string, files map[string]string) []*agentfile.AgentFile {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(root, "agents")); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		mustWrite(t, filepath.Join(root, filepath.FromSlash(rel)), []byte(content))
	}
	found, err := agentfile.DiscoverAgentFiles(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func reviewerEntry(t *testing.T, cwd string) lock.AgentLockEntry {
	t.Helper()
	entry, ok := lock.ReadProjectLock(cwd).Agents["reviewer"]
	if !ok {
		t.Fatal("no lock entry for reviewer")
	}
	return entry
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func agentFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// installPair installs the paired reviewer into harnesses and returns the
// source root.
func installPair(t *testing.T, cwd string, harnesses []string, mode InstallMode) string {
	t.Helper()
	root, found := writeSource(t, map[string]string{pairMdPath: pairMd, pairTOMLPath: pairTOML})
	captureStdout(t, func() {
		installAgentsForHarnesses(found, harnesses, false, mode, pairBase, root, cwd)
	})
	return root
}

// The issue's headline: a multi-harness install of a paired definition puts
// exactly one file in each harness, the native TOML in Codex with its optional
// attributes intact, and the markdown everywhere else.
// Mutation this test catches: installing the primary into every harness, which
// hands Codex a three-field conversion of the markdown and drops the TOML.
func TestPairedDefinitionInstallsOneVariantPerHarness(t *testing.T) {
	for _, mode := range []InstallMode{InstallModeSymlink, InstallModeCopy} {
		t.Run(string(mode), func(t *testing.T) {
			cwd := t.TempDir()
			installPair(t, cwd, []string{"claude-code", "codex", "github-copilot"}, mode)

			if got := readFile(t, agentHarnessPath("reviewer", "codex", false, cwd)); got != pairTOML {
				t.Errorf("Codex did not receive the native TOML verbatim:\n%s", got)
			}
			if got := readFile(t, agentHarnessPath("reviewer", "claude-code", false, cwd)); got != pairMd {
				t.Errorf("Claude Code did not receive the markdown verbatim:\n%s", got)
			}
			if got := readFile(t, agentHarnessPath("reviewer", "github-copilot", false, cwd)); got != pairMd {
				t.Errorf("Copilot did not receive the markdown verbatim:\n%s", got)
			}
			for _, h := range []string{"claude-code", "codex", "github-copilot"} {
				dir := harness.AgentsInstallDirFor(h, false, cwd)
				if got := agentFilesIn(t, dir); len(got) != 1 {
					t.Errorf("%s holds %v, want exactly one file for the one agent", h, got)
				}
			}

			entry := reviewerEntry(t, cwd)
			if entry.AgentPath != pairMdPath || entry.Format != string(agentfile.FormatMarkdown) {
				t.Errorf("primary = %q (%q), want the markdown file", entry.AgentPath, entry.Format)
			}
			if entry.Variants[string(agentfile.FormatTOML)] != pairTOMLPath || len(entry.Variants) != 1 {
				t.Errorf("Variants = %v, want the TOML file recorded", entry.Variants)
			}
			if len(lock.ReadProjectLock(cwd).Agents) != 1 {
				t.Errorf("the pair produced %d lock entries, want one", len(lock.ReadProjectLock(cwd).Agents))
			}
		})
	}
}

// Both canonical files exist whichever harnesses were chosen, so the entry's
// files do not depend on the selection - and adding Codex later, from the
// same source, hands it the native variant.
func TestPairedDefinitionKeepsBothCanonicalFiles(t *testing.T) {
	cwd := t.TempDir()
	root := installPair(t, cwd, []string{"claude-code"}, InstallModeSymlink)

	entry := reviewerEntry(t, cwd)
	for _, p := range agentCanonicalPaths("reviewer", entry, false, cwd) {
		if !fileExists(p) {
			t.Errorf("canonical file %s is missing", p)
		}
	}
	if st := agentStatusFor("reviewer", entry, false, cwd); st.CanonicalMissing {
		t.Error("a freshly installed pair reports a missing canonical file")
	}

	found := rediscover(t, root, map[string]string{pairMdPath: pairMd, pairTOMLPath: pairTOML})
	captureStdout(t, func() {
		installAgentsForHarnesses(found, []string{"codex"}, false, InstallModeSymlink, pairBase, root, cwd)
	})
	if got := readFile(t, agentHarnessPath("reviewer", "codex", false, cwd)); got != pairTOML {
		t.Errorf("Codex, added later, did not receive the native TOML:\n%s", got)
	}
	if got := reviewerEntry(t, cwd).Harnesses; strings.Join(got, ",") != "claude-code,codex" {
		t.Errorf("Harnesses = %v, want both", got)
	}
}

// Single-format fallback, both ways, is what it was before variants existed:
// markdown reaches Codex as the three core fields, TOML reaches a markdown
// harness converted, and neither records a variant.
func TestSingleFormatDefinitionsStillConvert(t *testing.T) {
	t.Run("markdown only", func(t *testing.T) {
		cwd := t.TempDir()
		root, found := writeSource(t, map[string]string{pairMdPath: pairMd})
		captureStdout(t, func() {
			installAgentsForHarnesses(found, []string{"claude-code", "codex"}, false, InstallModeSymlink, pairBase, root, cwd)
		})
		got := readFile(t, agentHarnessPath("reviewer", "codex", false, cwd))
		if strings.Contains(got, "model") {
			t.Errorf("markdown frontmatter leaked into the Codex conversion:\n%s", got)
		}
		if entry := reviewerEntry(t, cwd); entry.Variants != nil {
			t.Errorf("Variants = %v, want none", entry.Variants)
		}
	})
	t.Run("toml only", func(t *testing.T) {
		cwd := t.TempDir()
		root, found := writeSource(t, map[string]string{pairTOMLPath: pairTOML})
		captureStdout(t, func() {
			installAgentsForHarnesses(found, []string{"claude-code", "codex"}, false, InstallModeSymlink, pairBase, root, cwd)
		})
		got, err := agentfile.ParseAgentFile(agentHarnessPath("reviewer", "claude-code", false, cwd))
		if err != nil || got == nil || got.Format != agentfile.FormatMarkdown {
			t.Fatalf("Claude Code did not receive a markdown conversion: %+v, %v", got, err)
		}
		entry := reviewerEntry(t, cwd)
		if entry.Variants != nil || entry.Format != string(agentfile.FormatTOML) {
			t.Errorf("entry = %+v, want a plain TOML entry", entry)
		}
	})
}

// Mutation this test catches: judging every harness copy against the primary
// canonical file. Codex's copy of a pair comes from the TOML one, so against
// the markdown it reads as a file the user wrote - remove then refuses it and
// update skips it.
func TestPairedDefinitionIsOwnedInEveryHarness(t *testing.T) {
	for _, mode := range []InstallMode{InstallModeSymlink, InstallModeCopy} {
		t.Run(string(mode), func(t *testing.T) {
			cwd := t.TempDir()
			installPair(t, cwd, []string{"claude-code", "codex", "github-copilot"}, mode)
			entry := reviewerEntry(t, cwd)

			targets, foreign := refreshableHarnesses("reviewer", entry.Harnesses, entry, false, cwd)
			if len(foreign) != 0 || len(targets) != 3 {
				t.Fatalf("refreshable = %v, foreign = %v; mdm wrote all three", targets, foreign)
			}

			// An entry with no harness list infers the same three from disk.
			entry.Harnesses = nil
			if got := agentOwnedHarnesses("reviewer", entry, false, cwd); len(got) != 3 {
				t.Errorf("inferred harnesses = %v, want all three", got)
			}
		})
	}
}

// Remove is scoped by harness for a pair as for anything else, and a full
// removal leaves neither canonical file behind.
func TestRemovePairedDefinition(t *testing.T) {
	cwd := t.TempDir()
	installPair(t, cwd, []string{"claude-code", "codex"}, InstallModeSymlink)

	res, err := removeAgentFromDisk("reviewer", []string{"codex"}, reviewerEntry(t, cwd), false, cwd)
	if err != nil || res.fullyRemoved || len(res.foreign) != 0 {
		t.Fatalf("scoped remove: res=%+v err=%v", res, err)
	}
	if fileExists(agentHarnessPath("reviewer", "codex", false, cwd)) {
		t.Error("the Codex copy survived a removal scoped to Codex")
	}
	entry := reviewerEntry(t, cwd)
	if strings.Join(entry.Harnesses, ",") != "claude-code" || len(entry.Variants) != 1 {
		t.Errorf("entry after scoped remove = %+v; the variant record must survive", entry)
	}

	res, err = removeAgentFromDisk("reviewer", nil, entry, false, cwd)
	if err != nil || !res.fullyRemoved {
		t.Fatalf("full remove: res=%+v err=%v", res, err)
	}
	if left := agentFilesIn(t, harness.CanonicalAgentsDir(false, cwd)); len(left) != 0 {
		t.Errorf("canonical directory still holds %v", left)
	}
	if _, ok := lock.ReadProjectLock(cwd).Agents["reviewer"]; ok {
		t.Error("the lock entry survived a full removal")
	}
}

// A definition installed when its source had only markdown gains the native
// variant when the same source adds one: update re-points Codex at it, and
// the lock records the pair.
func TestUpdateAdoptsAVariantTheSourceAdded(t *testing.T) {
	cwd := t.TempDir()
	root, found := writeSource(t, map[string]string{pairMdPath: pairMd})
	captureStdout(t, func() {
		installAgentsForHarnesses(found, []string{"claude-code", "codex"}, false, InstallModeSymlink, pairBase, root, cwd)
	})

	found = rediscover(t, root, map[string]string{pairMdPath: pairMd, pairTOMLPath: pairTOML})
	var updated bool
	captureStdout(t, func() {
		updated = updateOneAgent(found[0], "reviewer", reviewerEntry(t, cwd), pairBase, root, false, cwd, InstallModeSymlink)
	})
	if !updated {
		t.Fatal("update did not refresh the entry")
	}
	if got := readFile(t, agentHarnessPath("reviewer", "codex", false, cwd)); got != pairTOML {
		t.Errorf("Codex still holds the conversion, not the native TOML:\n%s", got)
	}
	entry := reviewerEntry(t, cwd)
	if entry.Variants[string(agentfile.FormatTOML)] != pairTOMLPath {
		t.Errorf("Variants = %v, want the TOML file recorded", entry.Variants)
	}
	if _, foreign := refreshableHarnesses("reviewer", entry.Harnesses, entry, false, cwd); len(foreign) != 0 {
		t.Errorf("after the update %v read as files mdm did not write", foreign)
	}
}

// The reverse: the source drops its TOML. Codex goes back to a conversion,
// the lock stops recording a variant, and the TOML canonical file - which
// nothing reads any more - does not linger where no lock entry names it.
func TestUpdateDropsAVariantTheSourceRemoved(t *testing.T) {
	cwd := t.TempDir()
	root := installPair(t, cwd, []string{"claude-code", "codex"}, InstallModeSymlink)

	found := rediscover(t, root, map[string]string{pairMdPath: pairMd})
	var updated bool
	captureStdout(t, func() {
		updated = updateOneAgent(found[0], "reviewer", reviewerEntry(t, cwd), pairBase, root, false, cwd, InstallModeSymlink)
	})
	if !updated {
		t.Fatal("update did not refresh the entry")
	}
	got := readFile(t, agentHarnessPath("reviewer", "codex", false, cwd))
	if strings.Contains(got, "sandbox_mode") || !strings.Contains(got, "review") {
		t.Errorf("Codex was not rewritten from the markdown:\n%s", got)
	}
	if entry := reviewerEntry(t, cwd); entry.Variants != nil {
		t.Errorf("Variants = %v, want none", entry.Variants)
	}
	if left := agentFilesIn(t, harness.CanonicalAgentsDir(false, cwd)); len(left) != 1 || left[0] != "reviewer.md" {
		t.Errorf("canonical directory holds %v, want only reviewer.md", left)
	}
}

// Re-adding from the same source to one harness, after the source gained a
// variant, must also rewrite the harness that variant now serves. Left alone,
// Codex would hold a conversion of the markdown while the lock points it at
// the TOML file - a copy mdm wrote and could no longer recognize.
func TestAddRewritesHarnessesWhoseVariantChanged(t *testing.T) {
	cwd := t.TempDir()
	root, found := writeSource(t, map[string]string{pairMdPath: pairMd})
	captureStdout(t, func() {
		installAgentsForHarnesses(found, []string{"claude-code", "codex"}, false, InstallModeSymlink, pairBase, root, cwd)
	})

	found = rediscover(t, root, map[string]string{pairMdPath: pairMd, pairTOMLPath: pairTOML})
	var outcome agentInstallOutcome
	captureStdout(t, func() {
		outcome = installAgentsForHarnesses(found, []string{"claude-code"}, false, InstallModeSymlink, pairBase, root, cwd)
	})
	if outcome.refused != 0 || outcome.installed != 1 {
		t.Fatalf("outcome = %+v; a variant from the same source is not a conflict", outcome)
	}
	if got := readFile(t, agentHarnessPath("reviewer", "codex", false, cwd)); got != pairTOML {
		t.Errorf("Codex was left on the conversion:\n%s", got)
	}
}

// A same-named definition from another source is still a collision, variant
// or not.
func TestVariantFromAnotherSourceIsStillRefused(t *testing.T) {
	cwd := t.TempDir()
	root, found := writeSource(t, map[string]string{pairMdPath: pairMd})
	captureStdout(t, func() {
		installAgentsForHarnesses(found, []string{"claude-code"}, false, InstallModeSymlink, pairBase, root, cwd)
	})

	otherRoot, other := writeSource(t, map[string]string{pairMdPath: pairMd, pairTOMLPath: pairTOML})
	var outcome agentInstallOutcome
	captureStdout(t, func() {
		outcome = installAgentsForHarnesses(other, []string{"codex"}, false, InstallModeSymlink, lock.AgentLockEntry{Source: "someone/else", SourceType: "github"}, otherRoot, cwd)
	})
	if outcome.refused != 1 || outcome.installed != 0 {
		t.Fatalf("outcome = %+v, want the definition refused", outcome)
	}
	if fileExists(agentHarnessPath("reviewer", "codex", false, cwd)) {
		t.Error("a refused definition reached Codex")
	}
	if entry := reviewerEntry(t, cwd); entry.Source != "o/r" || entry.Variants != nil {
		t.Errorf("the refused add rewrote the lock entry: %+v", entry)
	}
}

// A pair no harness accepts leaves nothing behind: neither canonical file,
// and no lock entry.
func TestPairedDefinitionNoHarnessAcceptsLeavesNothing(t *testing.T) {
	cwd := t.TempDir()
	root, found := writeSource(t, map[string]string{pairMdPath: pairMd, pairTOMLPath: pairTOML})
	captureStdout(t, func() {
		installAgentsForHarnesses(found, []string{"no-such-harness"}, false, InstallModeSymlink, pairBase, root, cwd)
	})
	if _, err := os.Stat(harness.CanonicalAgentsDir(false, cwd)); err == nil {
		if left := agentFilesIn(t, harness.CanonicalAgentsDir(false, cwd)); len(left) != 0 {
			t.Errorf("canonical directory holds %v after an install nothing accepted", left)
		}
	}
	if len(lock.ReadProjectLock(cwd).Agents) != 0 {
		t.Error("a definition no harness accepted was recorded in the lock")
	}
}

// The hidden-character gate covers the variant too: the TOML is what Codex
// loads, so a scan of the markdown alone would pass a file nobody looked at.
func TestHiddenCharScanCoversTheVariant(t *testing.T) {
	_, found := writeSource(t, map[string]string{
		pairMdPath:   pairMd,
		pairTOMLPath: "name = \"reviewer\"\ndescription = \"d\"\ndeveloper_instructions = \"review\u200b\U000E0041 natively\"\n",
	})
	var ok bool
	captureStdout(t, func() { ok = checkAgentFilesMarkdownForHiddenChars(found, false) })
	if ok {
		t.Error("a hidden character in the TOML variant passed the scan")
	}
}

// A lock entry written before variants existed reads as one format, and a
// variant key that is not a format never becomes a path.
func TestLockedAgentFormatsToleratesOldAndOddEntries(t *testing.T) {
	old := lock.AgentLockEntry{AgentPath: "agents/critic.md"}
	if got := lockedAgentFormats(old); len(got) != 1 || got[0] != agentfile.FormatMarkdown {
		t.Errorf("formats of an entry with no format = %v, want markdown alone", got)
	}
	odd := lock.AgentLockEntry{Format: "markdown", Variants: map[string]string{"markdown": "x.md", "../../etc": "y", "toml": ""}}
	if got := lockedAgentFormats(odd); len(got) != 1 {
		t.Errorf("formats = %v; a repeated primary, an unknown key and an empty path are not variants", got)
	}
	if got := lockedFormatFor(old, "codex"); got != agentfile.FormatMarkdown {
		t.Errorf("Codex reads %q for an unpaired markdown entry, want the markdown primary", got)
	}
}
