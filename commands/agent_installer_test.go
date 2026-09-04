package commands

import (
	"os"
	"path/filepath"
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

// A harness with no agent concept has nowhere to put a definition. It is
// skipped, not failed: installing to five harnesses where one has no agent
// support is a normal thing to do.
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
// sanitized string is the lock key — a divergence there is what let `mdm
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
	if want := agentCanonicalPath(name, false, cwd); res.CanonicalPath != want {
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

	// The source IS the canonical file — exactly what discovery hands back
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
// `mdm agents add .` — .claude/agents is scanned before .agents/agents.
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

// A harness with no agent concept is a skip, and callers have to be able to
// tell that from a failure without string-matching the reason.
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
