package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
)

// Copilot loads only .agent.md. Writing plain .md there installs a file the
// harness never reads, which fails silently at use time rather than here.
func TestAgentFileExtHonorsTheHarnessSuffix(t *testing.T) {
	if got := harness.AgentFileExt("github-copilot"); got != ".agent.md" {
		t.Errorf("github-copilot ext = %q, want .agent.md", got)
	}
	if got := harness.AgentFileExt("claude-code"); got != ".md" {
		t.Errorf("claude-code ext = %q, want .md", got)
	}
	if got := harness.AgentFileExt("no-such-harness"); got != ".md" {
		t.Errorf("unknown harness ext = %q, want the .md default", got)
	}
}

// A harness with no agent-definition directory recorded has nowhere to put a
// definition. It is skipped, not failed: installing to five harnesses where
// one has no directory recorded is a normal thing to do.
func TestInstallAgentFileSkipsAHarnessWithNoAgentDir(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(cwd, "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}

	var without string
	for name, h := range harness.AllHarnesses {
		if h.AgentsInstallDir == "" {
			without = name
			break
		}
	}
	if without == "" {
		t.Skip("every harness supports agent definitions")
	}
	res := installAgentFile(a, without, false, cwd, InstallModeSymlink)
	if res.Success {
		t.Errorf("installing to %q reported success; want a skip", without)
	}

	// The skip has to happen BEFORE the canonical copy is written, not just
	// report failure afterward. If the skip check were moved to run after
	// that write, Success would still be false here and this test would
	// still look like it passed, while a stray canonical file was left on
	// disk with no harness pointing at it.
	canonicalPath := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
	if _, err := os.Stat(canonicalPath); !os.IsNotExist(err) {
		t.Errorf("canonical file exists at %q despite the skip (stat err=%v); the skip must run before any write", canonicalPath, err)
	}
}

// AgentFileExt matters only if installAgentFile names the file it writes after
// it. A test calling AgentFileExt in isolation passes even when installAgentFile
// writes "<name>.md" unconditionally, which Copilot never reads. This looks at
// the harness directory on disk instead of the InstallResult.
func TestInstallAgentFileWritesTheHarnessRequiredFilename(t *testing.T) {
	tests := []struct {
		harnessName  string
		wantFileName string
		wrongName    string
	}{
		{"github-copilot", "critic.agent.md", "critic.md"},
		{"claude-code", "critic.md", "critic.agent.md"},
	}
	for _, tc := range tests {
		t.Run(tc.harnessName, func(t *testing.T) {
			cwd := t.TempDir()
			src := filepath.Join(t.TempDir(), "critic.md")
			if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}

			res := installAgentFile(a, tc.harnessName, false, cwd, InstallModeCopy)
			if !res.Success {
				t.Fatalf("install to %q failed: %s", tc.harnessName, res.Error)
			}

			harnessDir := harness.AgentsInstallDirFor(tc.harnessName, false, cwd)
			wantPath := filepath.Join(harnessDir, tc.wantFileName)
			if _, err := os.Stat(wantPath); err != nil {
				t.Errorf("%s: expected file at %q, stat error: %v", tc.harnessName, wantPath, err)
			}
			wrongPath := filepath.Join(harnessDir, tc.wrongName)
			if _, err := os.Stat(wrongPath); err == nil {
				t.Errorf("%s: found %q; this harness only reads %q, so this file is silently never seen", tc.harnessName, wrongPath, tc.wantFileName)
			}

			canonicalPath := filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md")
			if _, err := os.Stat(canonicalPath); err != nil {
				t.Errorf("%s: expected canonical copy at %q, stat error: %v", tc.harnessName, canonicalPath, err)
			}
		})
	}
}

// The frontmatter name is third-party text: "Code Reviewer" is a perfectly
// ordinary thing to write there, and a perfectly bad file name. It has to be
// sanitized at the point it becomes a path component, because the same
// sanitized string is the lock key - a divergence there is what let `mdm
// agents remove` drop the lock entry while the file stayed on disk.
func TestInstallAgentFileSanitizesTheNameForEveryPath(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(cwd, "src.md")
	if err := os.WriteFile(src, []byte("---\nname: Code Reviewer\ndescription: d\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "Code Reviewer", Description: "d", Path: src}

	res := installAgentFile(a, "claude-code", false, cwd, InstallModeCopy)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}

	name := agentDiskName(a.Name)
	if name != "code-reviewer" {
		t.Fatalf("agentDiskName(%q) = %q, want code-reviewer", a.Name, name)
	}
	// Both paths must be the ones the lock key resolves to, which is what
	// list, remove, update, and doctor all look for.
	if want := agentCanonicalPath(name, agentfile.FormatMarkdown, false, cwd); res.CanonicalPath != want {
		t.Errorf("canonical path = %q, want %q", res.CanonicalPath, want)
	}
	if want := agentHarnessPath(name, "claude-code", false, cwd); res.Path != want {
		t.Errorf("harness path = %q, want %q", res.Path, want)
	}
	for _, p := range []string{res.CanonicalPath, res.Path} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("nothing at %s: %v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(harness.CanonicalAgentsDir(false, cwd), "Code Reviewer.md")); err == nil {
		t.Error("the raw frontmatter name was written to disk")
	}
}

// Reinstalling a definition mdm already owns must be a no-op, not a
// self-inflicted truncation. `mdm agents add .` reaches this because
// .agents/agents is a conventional agents directory, but the guard is on the
// copy itself so it also covers routes nobody has enumerated.
func TestInstallAgentFileDoesNotCopyTheCanonicalFileOntoItself(t *testing.T) {
	cwd := t.TempDir()
	canonicalDir := harness.CanonicalAgentsDir(false, cwd)
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: critic\ndescription: d\n---\nreal content\n"
	canonical := filepath.Join(canonicalDir, "critic.md")
	if err := os.WriteFile(canonical, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// The source IS the canonical file - exactly what discovery hands back
	// when the source tree is the project itself.
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: canonical}
	res := installAgentFile(a, "claude-code", false, cwd, InstallModeCopy)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}

	got, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("canonical file = %q, want it untouched (%q)", got, body)
	}
	harnessGot, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(harnessGot) != body {
		t.Errorf("harness file = %q, want %q", harnessGot, body)
	}
}

// The same guard has to hold when the source is a harness's symlink back
// into the canonical directory, which is what discovery finds first for
// `mdm agents add .` - .claude/agents is scanned before .agents/agents.
func TestInstallAgentFileDoesNotTruncateThroughAHarnessSymlink(t *testing.T) {
	cwd := t.TempDir()
	canonicalDir := harness.CanonicalAgentsDir(false, cwd)
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: critic\ndescription: d\n---\nreal content\n"
	canonical := filepath.Join(canonicalDir, "critic.md")
	if err := os.WriteFile(canonical, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	harnessDir := harness.AgentsInstallDirFor("claude-code", false, cwd)
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(harnessDir, "critic.md")
	if err := os.Symlink(canonical, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: link}
	if res := installAgentFile(a, "claude-code", false, cwd, InstallModeSymlink); !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}
	got, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("canonical file = %q, want it untouched (%q)", got, body)
	}
}

// A harness with no agent-definition directory recorded is a skip, and
// callers have to be able to tell that from a failure without
// string-matching the reason.
func TestInstallAgentFileMarksANoAgentHarnessAsSkipped(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(cwd, "critic.md")
	if err := os.WriteFile(src, []byte("---\nname: critic\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &agentfile.AgentFile{Name: "critic", Description: "d", Path: src}

	var without string
	for name, h := range harness.AllHarnesses {
		if h.AgentsInstallDir == "" {
			without = name
			break
		}
	}
	if without == "" {
		t.Skip("every harness supports agent definitions")
	}
	res := installAgentFile(a, without, false, cwd, InstallModeSymlink)
	if res.Success || !res.Skipped {
		t.Errorf("result = {Success:%v Skipped:%v}, want a skip", res.Success, res.Skipped)
	}
	if res.Error == "" {
		t.Error("a skip must still carry its reason for the caller to print")
	}

	// An unknown harness is a genuine failure, not a skip: the caller asked
	// for something that does not exist.
	if res := installAgentFile(a, "no-such-harness", false, cwd, InstallModeSymlink); res.Skipped {
		t.Error("an unknown harness was reported as a skip")
	}
}

// symlinkProbe reports whether this host can create a symlink at all. A test
// that asserts an install is a symlink otherwise reads a copy fallback as a
// bug in the link/materialize decision.
func symlinkProbe(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return os.Symlink(target, filepath.Join(dir, "link")) == nil
}

// writeMarkdownAgent lays down one markdown definition and returns its path.
func writeMarkdownAgent(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name+".md")
	body := "---\nname: " + name + "\ndescription: a definition\n---\n\nYou are " + name + ".\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Codex reads TOML. A symlink hands it the canonical markdown bytes under a
// .toml name, which Codex ignores without saying so, and the install still
// reports success. So the install has to convert: a real file, in TOML, that
// parses back to the same definition.
func TestInstallAgentFileMaterializesForAHarnessThatReadsAnotherFormat(t *testing.T) {
	cwd := t.TempDir()
	src := writeMarkdownAgent(t, t.TempDir(), "critic")
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}

	res := installAgentFile(a, "codex", false, cwd, InstallModeSymlink)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}
	if filepath.Ext(res.Path) != ".toml" {
		t.Errorf("installed path = %q, want a .toml name", res.Path)
	}

	info, err := os.Lstat(res.Path)
	if err != nil {
		t.Fatalf("nothing at %s: %v", res.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s is a symlink; Codex would read markdown bytes under a .toml name", res.Path)
	}

	got, err := agentfile.ParseAgentFile(res.Path)
	if err != nil {
		t.Fatalf("the installed file does not parse as TOML: %v", err)
	}
	if got == nil {
		t.Fatal("the installed file parsed as nothing, so it carries no name or description")
	}
	if got.Name != a.Name || got.Format != agentfile.FormatTOML {
		t.Errorf("installed definition = {Name:%q Format:%q}, want {%q %q}", got.Name, got.Format, a.Name, agentfile.FormatTOML)
	}

	// The scope keeps its recorded mode; only the thing on disk differs.
	if res.Mode != InstallModeSymlink {
		t.Errorf("result mode = %q, want the scope's recorded %q", res.Mode, InstallModeSymlink)
	}
	if !res.Materialized {
		t.Error("a real file written by design must be marked Materialized, or the summary cannot describe it")
	}
	if res.SymlinkFailed {
		t.Error("no symlink was attempted, so reporting a failed one sends the user to --copy for nothing")
	}
}

// Copilot reads markdown, so format alone would say "link". Its directory is
// .github/agents, which people commit: a symlink arrives on a teammate's
// checkout as a text file holding a path. The always-materialize flag has to
// override the format match.
func TestInstallAgentFileMaterializesForAnAlwaysMaterializeHarness(t *testing.T) {
	if !symlinkProbe(t) {
		t.Skip("symlinks unavailable on this host; a copy here proves nothing")
	}
	cwd := t.TempDir()
	src := writeMarkdownAgent(t, t.TempDir(), "critic")
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}
	if harness.AgentFormat("github-copilot") != a.Format {
		t.Fatalf("this test only means something while the formats match: %q vs %q", harness.AgentFormat("github-copilot"), a.Format)
	}

	res := installAgentFile(a, "github-copilot", false, cwd, InstallModeSymlink)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}
	info, err := os.Lstat(res.Path)
	if err != nil {
		t.Fatalf("nothing at %s: %v", res.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("%s is a symlink; .github is committed, so this reaches a teammate as a text file holding a path", res.Path)
	}
}

// A symlink can already be sitting at a materialize-by-rule path: GitHub
// documents committing .github/agents, so a teammate on a platform with
// symlinks can commit one, and an intermediate build of mdm wrote one there
// itself. os.WriteFile opens O_TRUNC and follows a symlink, so the materialized
// write would land Copilot's re-encoded bytes in the canonical file every other
// harness links at, and leave the install a link where the harness needs a real
// file - while the run reports success and explains that Copilot always gets a
// real file.
//
// Mutation this test catches: writeMaterializedAgent writing the encoded bytes
// with os.WriteFile(harnessPath, data, 0o644) instead of replacing the path.
func TestInstallAgentFileReplacesASymlinkAtAMaterializedPath(t *testing.T) {
	cwd := t.TempDir()
	canonicalDir := harness.CanonicalAgentsDir(false, cwd)
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(canonicalDir, "critic.md")
	if err := os.WriteFile(canonical, []byte("---\nname: critic\ndescription: d\n---\n\nORIGINAL\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	harnessPath := agentHarnessPath("critic", "github-copilot", false, cwd)
	if err := os.MkdirAll(filepath.Dir(harnessPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, harnessPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	src := writeMarkdownAgent(t, t.TempDir(), "critic")
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}

	res := installAgentFile(a, "github-copilot", false, cwd, InstallModeSymlink)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}
	if isSymlink(t, harnessPath) {
		t.Errorf("%s is still a symlink; the materialized write followed it instead of replacing it", harnessPath)
	}
	got, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("canonical file = %q, want the source bytes %q: the harness write went through the symlink", got, want)
	}
}

// The bug is fixable by materializing everything, and that would be worse:
// Claude Code reads markdown out of a generated directory, where a symlink is
// what keeps the harness copy in step with the canonical file.
func TestInstallAgentFileStillSymlinksForAMatchingHarness(t *testing.T) {
	if !symlinkProbe(t) {
		t.Skip("symlinks unavailable on this host; a copy here proves nothing")
	}
	cwd := t.TempDir()
	src := writeMarkdownAgent(t, t.TempDir(), "critic")
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}

	res := installAgentFile(a, "claude-code", false, cwd, InstallModeSymlink)
	if !res.Success {
		t.Fatalf("install failed: %s", res.Error)
	}
	info, err := os.Lstat(res.Path)
	if err != nil {
		t.Fatalf("nothing at %s: %v", res.Path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is a real file; a matching format in a generated directory must stay a symlink", res.Path)
	}
	if res.SymlinkFailed {
		t.Error("a real symlink was reported as a fallback copy")
	}
}

// writeUnencodableAgent lays down a TOML definition carrying a local time,
// which Encode refuses because BurntSushi's encoder shifts one by the host's
// UTC offset. It parses fine; only re-encoding it fails.
func writeUnencodableAgent(t *testing.T) *agentfile.AgentFile {
	t.Helper()
	src := filepath.Join(t.TempDir(), "critic.toml")
	body := `name = "critic"
description = "a definition"
developer_instructions = "be critical"
standup = 09:30:00
`
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}
	return a
}

// The installer has to surface Encode's refusal against the definition, and
// leave nothing behind. Encoding after the canonical write stranded raw TOML
// at .agents/agents/critic.md that no lock entry named and that list, remove
// and doctor could not see, in a directory discovery scans.
func TestInstallAgentFileReportsAnUnencodableDefinition(t *testing.T) {
	cwd := t.TempDir()
	a := writeUnencodableAgent(t)

	res := installAgentFile(a, "claude-code", false, cwd, InstallModeSymlink)
	if res.Success {
		t.Fatalf("install reported success for a definition that cannot be encoded; %s holds whatever was written", res.Path)
	}
	// This used to assert the error named the definition, because the summary
	// printed only harness names. The summary now prints this error under a
	// line that names the definition, so what the error owes the user is the
	// offending key and the reason.
	if !strings.Contains(res.Error, "standup") {
		t.Errorf("error = %q; it must name the offending key", res.Error)
	}
	if !strings.Contains(res.Error, "cannot be re-encoded") {
		t.Errorf("error = %q; it must say why the key cannot be converted", res.Error)
	}
	if _, err := os.Lstat(res.Path); !os.IsNotExist(err) {
		t.Errorf("something was written to %s despite the failure (stat err=%v)", res.Path, err)
	}

	canonical := agentCanonicalPath(agentDiskName(a.Name), agentfile.FormatTOML, false, cwd)
	if _, err := os.Lstat(canonical); !os.IsNotExist(err) {
		t.Errorf("the canonical file survives a failed install at %s (stat err=%v); nothing names it, so nothing can remove it", canonical, err)
	}
	if entries, err := os.ReadDir(harness.CanonicalAgentsDir(false, cwd)); err == nil && len(entries) > 0 {
		t.Errorf("the canonical directory is not empty after a failed install: %v", entries)
	}
}

// The canonical file is written once per definition and shared by every
// harness targeted. One harness refusing the definition must not cost another
// the file it legitimately installed, which is why the fix is to encode
// before writing rather than to clean up afterward.
func TestInstallAgentFileKeepsTheCanonicalWhenAnotherHarnessSucceeded(t *testing.T) {
	cwd := t.TempDir()
	a := writeUnencodableAgent(t)

	// codex reads TOML, so this source needs no conversion and installs.
	ok := installAgentFile(a, "codex", false, cwd, InstallModeSymlink)
	if !ok.Success {
		t.Fatalf("install to codex failed: %s", ok.Error)
	}
	// claude-code reads markdown, so the same source has to be re-encoded,
	// and Encode refuses it.
	bad := installAgentFile(a, "claude-code", false, cwd, InstallModeSymlink)
	if bad.Success {
		t.Fatal("install to claude-code reported success for a definition that cannot be encoded")
	}

	canonical := agentCanonicalPath(agentDiskName(a.Name), agentfile.FormatTOML, false, cwd)
	if _, err := os.Stat(canonical); err != nil {
		t.Fatalf("the canonical file is gone at %s (%v); codex's install points at it", canonical, err)
	}
	if _, err := os.Stat(ok.Path); err != nil {
		t.Errorf("codex's install no longer resolves at %s: %v", ok.Path, err)
	}
}

// writeEmptyBodyAgent returns a markdown definition carrying frontmatter and
// no instructions, the shape a Codex install cannot be made from.
func writeEmptyBodyAgent(t *testing.T) *agentfile.AgentFile {
	t.Helper()
	src := filepath.Join(t.TempDir(), "hollow.md")
	if err := os.WriteFile(src, []byte("---\nname: hollow\ndescription: d\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &agentfile.AgentFile{Name: "hollow", Description: "d", Path: src}
}

// Mutation this test catches: dropping the empty-instructions guard in
// encodeForHarness. Codex requires developer_instructions, so an empty body
// installs a file the harness will not load, reported as a success.
func TestInstallAgentFileRefusesAnEmptyBodyForATOMLHarness(t *testing.T) {
	cwd := t.TempDir()
	a := writeEmptyBodyAgent(t)

	res := installAgentFile(a, "codex", false, cwd, InstallModeSymlink)
	if res.Success {
		t.Fatalf("install reported success for a definition Codex cannot load; %s holds whatever was written", res.Path)
	}
	if !strings.Contains(res.Error, "developer_instructions") {
		t.Errorf("error = %q; it must name the field Codex requires", res.Error)
	}
	if _, err := os.Lstat(res.Path); !os.IsNotExist(err) {
		t.Errorf("something was written to %s despite the refusal (stat err=%v)", res.Path, err)
	}
}

// The refusal is Codex's alone: the same definition is legitimate markdown.
func TestInstallAgentFileInstallsAnEmptyBodyToAMarkdownHarness(t *testing.T) {
	cwd := t.TempDir()
	a := writeEmptyBodyAgent(t)

	res := installAgentFile(a, "claude-code", false, cwd, InstallModeCopy)
	if !res.Success {
		t.Fatalf("install failed for a markdown harness that accepts an empty body: %s", res.Error)
	}
}

// truncateThenFailCopyFile swaps copyFileFn for one that opens the destination
// exactly as copyFile does - O_CREATE|O_WRONLY|O_TRUNC - and then fails without
// writing anything. That is what a crash, a full volume or an I/O error inside
// copyFile's io.Copy leaves behind, and the only way to observe the truncation
// window without a fault injector. Shared mutable state, so not parallel-safe.
func truncateThenFailCopyFile(t *testing.T) {
	t.Helper()
	orig := copyFileFn
	copyFileFn = func(_, dst string) error {
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_ = f.Close()
		return errors.New("forced copy failure after truncating the destination")
	}
	t.Cleanup(func() { copyFileFn = orig })
}

// In symlink mode the canonical file is not a spare copy: it is what every
// harness symlink resolves to. copyFile opens its destination O_TRUNC and then
// streams, so writing the canonical in place empties it before the first byte
// of the new version arrives, and a crash in that window leaves every harness
// reading a zero-length definition with nothing to restore it from.
//
// Mutation this test catches: copyAgentFileUnlessSame calling
// copyFileFn(src, dst) directly instead of building the new content beside dst
// and renaming it over.
func TestInstallAgentFileKeepsTheCanonicalWhenTheCopyFails(t *testing.T) {
	cwd := t.TempDir()
	canonicalPath := agentCanonicalPath("critic", agentfile.FormatMarkdown, false, cwd)
	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := "---\nname: critic\ndescription: d\n---\n\nthe version every harness is reading\n"
	if err := os.WriteFile(canonicalPath, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	src := writeMarkdownAgent(t, t.TempDir(), "critic")
	a, err := agentfile.ParseAgentFile(src)
	if err != nil || a == nil {
		t.Fatalf("parsing the source: a=%v err=%v", a, err)
	}
	truncateThenFailCopyFile(t)

	res := installAgentFile(a, "claude-code", false, cwd, InstallModeSymlink)
	if res.Success {
		t.Fatal("install reported success while the copy of the canonical file failed")
	}
	got, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("the canonical file is gone: %v", err)
	}
	if string(got) != previous {
		t.Errorf("canonical file = %q, want the previous version (%q): the failed copy truncated it in place", got, previous)
	}
	assertNoTempDirs(t, filepath.Dir(canonicalPath))
}
