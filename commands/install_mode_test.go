package commands

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
)

// ── Fixtures ───────────────────────────────────────────────────────────────────

// isolateHome points the global state and every user-level agent directory
// at a fresh temp home, so a test touching global scope never reads or
// rewrites the developer's real files. The registry resolves global paths
// once at init, so agent.Reload is what makes the redirect stick; its cleanup
// is registered first so it runs after the env is restored. Not parallel-safe.
func isolateHome(t *testing.T) string {
	t.Helper()
	t.Cleanup(agent.Reload)
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	agent.Reload()
	return home
}

// writeSkillDir creates dir holding a SKILL.md.
func writeSkillDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+filepath.Base(dir)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

// symlinkOrSkip creates a symlink, skipping the test where the host cannot.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
}

// linkSkillInto lays out a symlink-mode install for one agent: the canonical
// .agents/skills/<name> and <agentDir>/skills/<name> pointing at it.
func linkSkillInto(t *testing.T, cwd, agentDir, name string) (canonical, link string) {
	t.Helper()
	canonical = filepath.Join(cwd, ".agents", "skills", name)
	writeSkillDir(t, canonical)
	link = filepath.Join(cwd, agentDir, "skills", name)
	symlinkOrSkip(t, canonical, link)
	return canonical, link
}

// linkSkill is linkSkillInto for Claude Code, the agent most tests use.
func linkSkill(t *testing.T, cwd, name string) (canonical, link string) {
	t.Helper()
	return linkSkillInto(t, cwd, ".claude", name)
}

// writeCopiedSkill lays out a copy-mode install for Claude Code: a real
// directory at the agent path and no canonical directory, which is what
// `--copy` writes for an agent with a directory of its own.
func writeCopiedSkill(t *testing.T, cwd, name string) string {
	t.Helper()
	dir := filepath.Join(cwd, ".claude", "skills", name)
	writeSkillDir(t, dir)
	return dir
}

func lockSkill(t *testing.T, cwd, name string) {
	t.Helper()
	if err := lock.AddSkillToLocalLock(name, lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
}

func setConfigured(t *testing.T, cwd string, agents ...string) {
	t.Helper()
	if err := lock.SetConfiguredAgents(agents, false, cwd); err != nil {
		t.Fatal(err)
	}
}

func setMode(t *testing.T, cwd, mode string) {
	t.Helper()
	if err := lock.SetInstallMode(mode, false, cwd); err != nil {
		t.Fatal(err)
	}
}

// failCopy and failRename swap the package-level hooks for ones that always
// error, since neither failure can be forced reliably at the OS level.
func failCopy(t *testing.T) {
	t.Helper()
	orig := copyDirFn
	copyDirFn = func(_, _ string) error { return errors.New("forced copy failure") }
	t.Cleanup(func() { copyDirFn = orig })
}

func failRename(t *testing.T) {
	t.Helper()
	orig := renameFn
	renameFn = func(_, _ string) error { return errors.New("forced rename failure") }
	t.Cleanup(func() { renameFn = orig })
}

// ── Assertions ─────────────────────────────────────────────────────────────────

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("nothing at %s: %v", path, err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

// assertRealSkill checks that path is a real directory holding a SKILL.md.
func assertRealSkill(t *testing.T, path string) {
	t.Helper()
	if isSymlink(t, path) {
		t.Errorf("%s is a symlink, want a real directory", path)
	}
	if _, err := os.Stat(filepath.Join(path, "SKILL.md")); err != nil {
		t.Errorf("%s has no SKILL.md: %v", path, err)
	}
}

// assertLinkTo checks that link is a symlink resolving to target.
func assertLinkTo(t *testing.T, link, target string) {
	t.Helper()
	if !isSymlink(t, link) {
		t.Fatalf("%s is not a symlink", link)
	}
	got, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(target); got != want {
		t.Errorf("%s resolves to %q, want %q", link, got, want)
	}
}

// assertNoTempDirs fails when a conversion left its scratch directory behind.
func assertNoTempDirs(t *testing.T, dir string) {
	t.Helper()
	if leftovers, _ := filepath.Glob(filepath.Join(dir, "*.mdm-tmp-*")); len(leftovers) != 0 {
		t.Errorf("temp directories left behind: %v", leftovers)
	}
}

// assertSameMode checks two directories share permission bits. A converted
// install is built under os.MkdirTemp (0700) and must end up with the
// canonical directory's mode. Skipped on Windows, where mode bits mean little.
func assertSameMode(t *testing.T, got, want string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if gotInfo.Mode().Perm() != wantInfo.Mode().Perm() {
		t.Errorf("%s mode = %o, want %o", got, gotInfo.Mode().Perm(), wantInfo.Mode().Perm())
	}
}

func assertRecordedMode(t *testing.T, cwd, want string) {
	t.Helper()
	if got := lock.GetInstallMode(false, cwd); got != want {
		t.Errorf("recorded mode = %q, want %q", got, want)
	}
}

// ── Mode resolution ────────────────────────────────────────────────────────────

// commitScopeInstallMode resolves the mode from the flags and the recorded
// mode: an explicit flag wins, otherwise the scope's mode is inherited, and
// symlink is the default. Yes: true mirrors how install and update call it.
func TestCommitScopeInstallModeResolvesMode(t *testing.T) {
	cases := []struct {
		name         string
		recorded     string
		opts         AddOptions
		want         InstallMode
		wantRecorded string
	}{
		{"default is symlink", "", AddOptions{}, InstallModeSymlink, ""},
		{"inherits recorded copy", lock.InstallModeCopy, AddOptions{}, InstallModeCopy, lock.InstallModeCopy},
		{"--copy wins over recorded symlink", lock.InstallModeSymlink, AddOptions{Copy: true}, InstallModeCopy, lock.InstallModeCopy},
		{"--symlink wins over recorded copy", lock.InstallModeCopy, AddOptions{Symlink: true}, InstallModeSymlink, lock.InstallModeSymlink},
		{"--symlink on a fresh scope writes nothing", "", AddOptions{Symlink: true}, InstallModeSymlink, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			if tc.recorded != "" {
				setMode(t, cwd, tc.recorded)
			}
			tc.opts.Project, tc.opts.Yes = true, true
			global, _, ok := promptScopeAndAgents(tc.opts, cwd)
			if !ok {
				t.Fatal("promptScopeAndAgents returned not ok")
			}
			mode, ok := commitScopeInstallMode(tc.opts, global, cwd)
			if !ok || mode != tc.want {
				t.Fatalf("mode = %q ok = %v, want %q true", mode, ok, tc.want)
			}
			assertRecordedMode(t, cwd, tc.wantRecorded)
			if tc.wantRecorded == "" {
				if _, err := os.Stat(lock.GetProjectLockPath(cwd)); !os.IsNotExist(err) {
					t.Errorf("a plain symlink install must not write a lock file: %v", err)
				}
			}
		})
	}
}

// With nothing installed there is nothing to convert, so applying a mode
// just records it. Recording copy is idempotent, and recording the default
// symlink writes nothing at all.
func TestApplyScopeInstallModeRecordsMode(t *testing.T) {
	cases := []struct {
		name         string
		apply        []InstallMode
		wantRecorded string
	}{
		{"first copy", []InstallMode{InstallModeCopy}, lock.InstallModeCopy},
		{"copy twice", []InstallMode{InstallModeCopy, InstallModeCopy}, lock.InstallModeCopy},
		{"plain symlink", []InstallMode{InstallModeSymlink}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			for _, m := range tc.apply {
				if mode, ok := applyScopeInstallMode(m, false, cwd); !ok || mode != m {
					t.Fatalf("mode = %q ok = %v, want %q true", mode, ok, m)
				}
			}
			assertRecordedMode(t, cwd, tc.wantRecorded)
		})
	}
}

// ── Symlink to copy ────────────────────────────────────────────────────────────

func TestRematerializeConvertsSymlinksToRealDirectories(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	setConfigured(t, cwd, "claude-code")
	lockSkill(t, cwd, "s1")

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), scopeInstallPaths(false, cwd))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("converted %d, want 1", n)
	}
	assertRealSkill(t, link)
	// The canonical directory stays: shared-dir agents install into it in
	// copy mode too, and doctor and remove resolve it for every locked skill.
	assertRealSkill(t, canonical)
}

func TestRematerializeIsANoOpWhenAlreadyReal(t *testing.T) {
	cwd := t.TempDir()
	real := writeCopiedSkill(t, cwd, "s1")
	setConfigured(t, cwd, "claude-code")
	lockSkill(t, cwd, "s1")

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), scopeInstallPaths(false, cwd))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	assertRealSkill(t, real)
}

// --copy converts every install in the scope, whatever configuredAgents
// says: that list only records what the interactive picker last saved, so an
// agent installed with `-a <agent> -y` is missing from it. Converting only
// the listed agents would leave the scope half symlinked and half copied.
func TestApplyScopeInstallModeConvertsEveryInstall(t *testing.T) {
	cases := []struct {
		name       string
		agentDirs  map[string]string // agent name -> project directory
		configured []string
	}{
		{"configured agent", map[string]string{"claude-code": ".claude"}, []string{"claude-code"}},
		{"no configured agents", map[string]string{"claude-code": ".claude"}, nil},
		{"agent missing from configuredAgents", map[string]string{"claude-code": ".claude", "roo": ".roo"}, []string{"claude-code"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			var canonical string
			links := map[string]string{}
			for name, dir := range tc.agentDirs {
				canonical, links[name] = linkSkillInto(t, cwd, dir, "s1")
			}
			lockSkill(t, cwd, "s1")
			if tc.configured != nil {
				setConfigured(t, cwd, tc.configured...)
			}
			assertRecordedMode(t, cwd, "")

			if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
				t.Fatalf("mode = %q ok = %v, want copy true", mode, ok)
			}
			assertRecordedMode(t, cwd, lock.InstallModeCopy)
			for _, link := range links {
				assertRealSkill(t, link)
				assertSameMode(t, link, canonical)
			}
		})
	}
}

// A failed copy leaves the recorded mode, the symlink, and the directory
// exactly as they were.
func TestRematerializeFailureLeavesModeAndSymlinkUnchanged(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	setConfigured(t, cwd, "claude-code")
	lockSkill(t, cwd, "s1")
	setMode(t, cwd, lock.InstallModeSymlink)
	failCopy(t)

	if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); ok {
		t.Fatalf("ok = true (mode %q), want false on a copy failure", mode)
	}
	assertRecordedMode(t, cwd, lock.InstallModeSymlink)
	assertLinkTo(t, link, canonical)
	assertNoTempDirs(t, filepath.Dir(link))
}

// When the final rename fails the symlink is put back, and put back RELATIVE
// like every other mdm link, or anything comparing link targets would read
// the recovered one as foreign.
func TestRematerializeRestoresARelativeSymlinkAfterARenameFailure(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	failRename(t)

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), []string{link})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	assertLinkTo(t, link, canonical)
	if restored, _ := os.Readlink(link); filepath.IsAbs(restored) {
		t.Errorf("restored link target = %q, want a relative path", restored)
	}
	assertNoTempDirs(t, filepath.Dir(link))
}

// A symlink mdm did not create points outside the canonical directory and
// exists so edits propagate; replacing it with a frozen copy would break that.
func TestRematerializeLeavesForeignSymlinksAlone(t *testing.T) {
	cwd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "pdf-skill")
	writeSkillDir(t, outside)
	link := filepath.Join(cwd, ".claude", "skills", "pdf")
	symlinkOrSkip(t, outside, link)
	setConfigured(t, cwd, "claude-code")
	lockSkill(t, cwd, "pdf")

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), scopeInstallPaths(false, cwd))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0: a foreign link is not mdm's to replace", n)
	}
	assertLinkTo(t, link, outside)
}

// An agent that reads the shared .agents/skills directory has no install
// path of its own: its path IS the canonical directory, real in both modes.
// Listing it made --copy announce a conversion and then convert nothing.
func TestScopeInstallPathsSkipsSharedSkillsDirAgents(t *testing.T) {
	if !agent.UsesSharedSkillsDir("amp") {
		t.Skip("fixture agent no longer uses the shared skills directory")
	}
	cwd := t.TempDir()
	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	writeSkillDir(t, canonical)
	lockSkill(t, cwd, "s1")

	if paths := scopeInstallPaths(false, cwd); len(paths) != 0 {
		t.Errorf("scopeInstallPaths = %v, want none", paths)
	}
	if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true", mode, ok)
	}
	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	assertRealSkill(t, canonical)
}

// scopeSkillNames must read the lock of the scope it was asked about.
func TestScopeSkillNamesFollowsScope(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()
	lockSkill(t, cwd, "project-only")
	if err := lock.AddSkillToGlobalState("global-only", lock.SkillLockEntry{Source: "o/r", SourceType: "github"}); err != nil {
		t.Fatal(err)
	}
	if got := scopeSkillNames(false, cwd); len(got) != 1 || got[0] != "project-only" {
		t.Errorf("project scope names = %v, want [project-only]", got)
	}
	if got := scopeSkillNames(true, cwd); len(got) != 1 || got[0] != "global-only" {
		t.Errorf("global scope names = %v, want [global-only]", got)
	}
}

// ── Copy to symlink ────────────────────────────────────────────────────────────

// --symlink turns each real install back into an mdm link, creating the
// canonical copy first because a copy install never wrote one for this agent.
func TestRematerializeConvertsRealDirectoriesToSymlinks(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	canonical := filepath.Join(cwd, ".agents", "skills")

	n, err := rematerializeScope(InstallModeSymlink, canonical, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("converted %d, want 1", n)
	}
	assertLinkTo(t, target, filepath.Join(canonical, "s1"))
	assertRealSkill(t, filepath.Join(canonical, "s1"))
	assertNoTempDirs(t, filepath.Dir(target))
}

// An existing canonical directory is kept as is, not overwritten from the copy.
func TestRematerializeToSymlinkKeepsExistingCanonical(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	canonical := filepath.Join(cwd, ".agents", "skills")
	writeSkillDir(t, filepath.Join(canonical, "s1"))
	if err := os.WriteFile(filepath.Join(canonical, "s1", "SKILL.md"), []byte("canonical\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := rematerializeScope(InstallModeSymlink, canonical, []string{target}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "canonical\n" {
		t.Errorf("link content = %q, want the existing canonical content", got)
	}
}

// A real directory without a SKILL.md is not an mdm install and is left alone.
func TestRematerializeToSymlinkLeavesForeignDirectoriesAlone(t *testing.T) {
	cwd := t.TempDir()
	target := filepath.Join(cwd, ".claude", "skills", "s1")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "notes.txt"), []byte("mine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(cwd, ".agents", "skills")

	n, err := rematerializeScope(InstallModeSymlink, canonical, []string{target})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	if isSymlink(t, target) {
		t.Error("foreign directory replaced with a symlink")
	}
	if _, err := os.Stat(filepath.Join(canonical, "s1")); !os.IsNotExist(err) {
		t.Errorf("canonical directory created for a foreign directory: %v", err)
	}
}

// If the copy cannot be set aside, the real directory stays in place.
func TestRematerializeToSymlinkRenameFailureLeavesTheCopy(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	failRename(t)

	n, err := rematerializeScope(InstallModeSymlink, filepath.Join(cwd, ".agents", "skills"), []string{target})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	assertRealSkill(t, target)
}

// --symlink on a scope recorded as copy converts it, records symlink, and a
// later plain add inherits symlink rather than copy.
func TestApplyScopeInstallModeSymlinkOverridesRecordedCopy(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")
	setMode(t, cwd, lock.InstallModeCopy)

	if mode, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd); !ok || mode != InstallModeSymlink {
		t.Fatalf("mode = %q ok = %v, want symlink true", mode, ok)
	}
	assertRecordedMode(t, cwd, lock.InstallModeSymlink)
	assertLinkTo(t, target, filepath.Join(cwd, ".agents", "skills", "s1"))

	if _, ok := commitScopeInstallMode(AddOptions{Project: true, Yes: true}, false, cwd); !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	assertRecordedMode(t, cwd, lock.InstallModeSymlink)
}

// ── Where the mode is committed ────────────────────────────────────────────────

// Cancelling at the agent selection must leave the scope untouched: a
// conversion run before the picker converted the project before a single
// skill was fetched, so backing out left copies behind and nothing installed.
func TestPromptScopeDoesNotConvertWhenAgentSelectionFails(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	setConfigured(t, cwd, "claude-code")
	lockSkill(t, cwd, "s1")

	// Naming only an unknown agent fails selection without a terminal.
	opts := AddOptions{Project: true, Yes: true, Copy: true, Agents: []string{"definitely-not-an-agent"}}
	if _, _, ok := promptScopeAndAgents(opts, cwd); ok {
		t.Fatal("promptScopeAndAgents returned ok with no valid agent")
	}
	assertRecordedMode(t, cwd, "")
	assertLinkTo(t, link, canonical)
}

// promptScopeAndAgents must not commit the mode: callers still have gates
// after it (the post-audit confirmation, cherry-pick's clobber filter), and
// returning from one after a conversion leaves a converted scope with nothing
// installed. The caller's commit is what does the work.
func TestPromptScopeAndAgentsDoesNotCommitTheMode(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")

	opts := AddOptions{Project: true, Yes: true, Copy: true, Agents: []string{"claude-code"}}
	global, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok || len(agents) != 1 || agents[0] != "claude-code" {
		t.Fatalf("agents = %v ok = %v, want [claude-code] true", agents, ok)
	}
	assertRecordedMode(t, cwd, "")
	assertLinkTo(t, link, canonical)

	if _, ok := commitScopeInstallMode(opts, global, cwd); !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	assertRealSkill(t, link)
}

// dropClobberingAgents can empty the agent list, in which case nothing is
// installed and the scope must not be converted for it.
func TestInstallForksDoesNotConvertWhenEveryAgentIsDropped(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()
	// OpenClaw reads ./skills, the default forks directory, so it is always dropped.
	if a := agent.AllAgents["openclaw"]; a == nil || a.SkillsDir != defaultForksDir {
		t.Skip("fixture agent no longer reads the forks directory")
	}
	forkDir := filepath.Join(cwd, defaultForksDir, "f1")
	writeSkillDir(t, forkDir)
	canonical, link := linkSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")

	installForks([]string{forkDir}, CherryPickOptions{
		Dir: defaultForksDir, Project: true, Yes: true, Copy: true, Agents: []string{"openclaw"},
	}, cwd)

	assertRecordedMode(t, cwd, "")
	assertLinkTo(t, link, canonical)
}

// The two mode flags contradict each other, so every command taking them
// must refuse the pair.
func TestCopyAndSymlinkFlagsAreMutuallyExclusive(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{"add", buildAddCmd("test"), []string{"o/r", "--copy", "--symlink"}},
		{"cherry-pick", buildCherryPickCmd("test"), []string{"o/r", "--copy", "--symlink"}},
		{"install", buildInstallFromLockCmd("test"), []string{"--copy", "--symlink"}},
	}
	for _, tc := range cases {
		tc.cmd.SetArgs(tc.args)
		tc.cmd.SetOut(io.Discard)
		tc.cmd.SetErr(io.Discard)
		err := tc.cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
			t.Errorf("%s --copy --symlink: err = %v, want a mutual-exclusion error", tc.name, err)
		}
	}
}
