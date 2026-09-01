package commands

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
)

// isolateHome points the global state AND every user-level directory the
// agent registry resolves at a fresh temp home, so a test that touches
// global scope can never read or write the developer's real files.
//
// Redirecting the state file alone is not enough: global install paths are
// built from the user's home directory, so listing a global scope's install
// paths would otherwise Lstat, and a conversion would rewrite, real
// directories under the developer's home. That has already happened once
// during development, which is why this is not left to each test.
//
// agent.Reload is what makes the redirect stick: the registry resolves every
// global path once, at package init. Its cleanup is registered before the
// t.Setenv calls so it runs after them, rebuilding the registry from the
// restored environment. Tests using this must not run in parallel.
func isolateHome(t *testing.T) string {
	t.Helper()
	t.Cleanup(agent.Reload)

	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere; set
	// both so the isolation does not depend on which platform runs the test.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	agent.Reload()

	return home
}

func TestPromptScopeInheritsRecordedCopyMode(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.SetInstallMode(lock.InstallModeCopy, false, cwd); err != nil {
		t.Fatal(err)
	}
	// Yes: true skips every prompt, mirroring how `mdm install` and
	// `mdm skills update` reach this function.
	opts := AddOptions{Project: true, Yes: true}
	global, _, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		t.Fatal("promptScopeAndAgents returned not ok")
	}
	mode, ok := commitScopeInstallMode(opts, global, cwd)
	if !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	if mode != InstallModeCopy {
		t.Errorf("mode = %q, want %q: a restore must not re-symlink copy installs", mode, InstallModeCopy)
	}
}

func TestPromptScopeDefaultsToSymlink(t *testing.T) {
	cwd := t.TempDir()
	opts := AddOptions{Project: true, Yes: true}
	global, _, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		t.Fatal("promptScopeAndAgents returned not ok")
	}
	mode, ok := commitScopeInstallMode(opts, global, cwd)
	if !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	if mode != InstallModeSymlink {
		t.Errorf("mode = %q, want %q", mode, InstallModeSymlink)
	}
}

func TestPromptScopeExplicitCopyWinsOverSymlinkScope(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.SetInstallMode(lock.InstallModeSymlink, false, cwd); err != nil {
		t.Fatal(err)
	}
	opts := AddOptions{Project: true, Yes: true, Copy: true}
	global, _, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		t.Fatal("promptScopeAndAgents returned not ok")
	}
	mode, ok := commitScopeInstallMode(opts, global, cwd)
	if !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	if mode != InstallModeCopy {
		t.Errorf("mode = %q, want %q", mode, InstallModeCopy)
	}
}

func TestApplyScopeInstallModeRecordsFirstCopy(t *testing.T) {
	cwd := t.TempDir()
	mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true)
	if !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true", mode, ok)
	}
	if got := lock.GetInstallMode(false, cwd); got != lock.InstallModeCopy {
		t.Errorf("scope mode = %q, want %q", got, lock.InstallModeCopy)
	}
}

func TestApplyScopeInstallModeIsIdempotent(t *testing.T) {
	cwd := t.TempDir()
	if _, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true); !ok {
		t.Fatal("first call not ok")
	}
	if _, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true); !ok {
		t.Fatal("second call not ok")
	}
	if got := lock.GetInstallMode(false, cwd); got != lock.InstallModeCopy {
		t.Errorf("scope mode = %q, want %q", got, lock.InstallModeCopy)
	}
}

func TestApplyScopeInstallModeDoesNotRecordPlainSymlink(t *testing.T) {
	cwd := t.TempDir()
	if _, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd, true); !ok {
		t.Fatal("not ok")
	}
	// A default symlink install must not write a lock file just to say so.
	if got := lock.GetInstallMode(false, cwd); got != "" {
		t.Errorf("scope mode = %q, want empty", got)
	}
}

func TestRematerializeConvertsSymlinksToRealDirectories(t *testing.T) {
	cwd := t.TempDir()

	// A canonical skill plus a symlink standing in for an installed one.
	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(canonical, filepath.Join(linked, "s1")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), scopeInstallPaths(false, cwd))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("converted %d skills, want 1", n)
	}
	info, err := os.Lstat(filepath.Join(linked, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("still a symlink after re-materialization")
	}
	if _, err := os.Stat(filepath.Join(linked, "s1", "SKILL.md")); err != nil {
		t.Errorf("content not copied: %v", err)
	}
	// The canonical directory stays: agents that read the shared skills
	// directory install into it in copy mode too, and it is the path doctor
	// and remove resolve for every locked skill.
	if _, err := os.Stat(filepath.Join(canonical, "SKILL.md")); err != nil {
		t.Errorf("canonical directory removed by the conversion: %v", err)
	}
}

func TestRematerializeIsANoOpWhenAlreadyReal(t *testing.T) {
	cwd := t.TempDir()
	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	// A real (non-symlink) install already sitting at the agent's install
	// path. Without this, the loop body never runs and a return of 0 is
	// trivial rather than a genuine no-op.
	real := filepath.Join(cwd, ".claude", "skills", "s1")
	if err := os.MkdirAll(real, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), scopeInstallPaths(false, cwd))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d skills, want 0", n)
	}
	info, err := os.Lstat(real)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("a real directory was replaced with a symlink")
	}
	if _, err := os.Stat(filepath.Join(real, "SKILL.md")); err != nil {
		t.Errorf("real directory content missing after no-op: %v", err)
	}
}

func TestRematerializeFailureLeavesModeAndSymlinkUnchanged(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.SetInstallMode(lock.InstallModeSymlink, false, cwd); err != nil {
		t.Fatal(err)
	}

	// Force the copy step to fail. Real OS-level fault injection (denying
	// read/write permissions) is not reliably forceable on Windows, so this
	// swaps the package-level copy hook for a fake that always errors.
	orig := copyDirFn
	copyDirFn = func(src, dst string) error { return errors.New("forced copy failure") }
	defer func() { copyDirFn = orig }()

	mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true)
	if ok {
		t.Fatalf("applyScopeInstallMode returned ok=true (mode=%q), want false on a copy failure", mode)
	}

	if got := lock.GetInstallMode(false, cwd); got != lock.InstallModeSymlink {
		t.Errorf("recorded install mode = %q, want unchanged %q after a failed conversion", got, lock.InstallModeSymlink)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced despite the copy failing")
	}
	// No temp directory should be left behind.
	entries, err := os.ReadDir(linked)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("linked skills dir has %d entries, want 1 (leftover temp dir?)", len(entries))
	}
}

// TestApplyScopeInstallModeConvertsUnrecordedSymlinkScope is the common
// path: a project that predates the install-mode switch has symlinked
// installs and NO recorded mode, and the user passes --copy. Gating the
// conversion on "a mode string is already recorded" skipped exactly this
// case, leaving the lock saying copy over symlinks on disk.
func TestApplyScopeInstallModeConvertsUnrecordedSymlinkScope(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	if got := lock.GetInstallMode(false, cwd); got != "" {
		t.Fatalf("precondition failed: recorded mode = %q, want empty", got)
	}

	mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true)
	if !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true", mode, ok)
	}
	if got := lock.GetInstallMode(false, cwd); got != lock.InstallModeCopy {
		t.Errorf("recorded mode = %q, want %q", got, lock.InstallModeCopy)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("still a symlink: the lock now says copy while the disk says symlink")
	}
	if _, err := os.Stat(filepath.Join(linkPath, "SKILL.md")); err != nil {
		t.Errorf("content not copied: %v", err)
	}
}

// A scope with nothing installed has nothing to convert, so it must record
// the mode without prompting. yes=false here: reaching the prompt in a
// non-interactive test would hang or cancel, which is the assertion.
func TestApplyScopeInstallModeDoesNotPromptWithNothingInstalled(t *testing.T) {
	cwd := t.TempDir()
	mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, false)
	if !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true without a prompt", mode, ok)
	}
	if got := lock.GetInstallMode(false, cwd); got != lock.InstallModeCopy {
		t.Errorf("recorded mode = %q, want %q", got, lock.InstallModeCopy)
	}
}

// Cancelling at the agent selection must leave the scope exactly as it was.
// Applying the mode records it and re-materializes every install in the
// scope, so doing that before the agent picker converted the whole project
// during the prompt phase, before a single skill had been fetched: the user
// backing out, or a clone failing, left copies behind and nothing installed.
func TestPromptScopeDoesNotConvertWhenAgentSelectionFails(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	// Naming only an unknown agent is how agent selection fails without a
	// terminal; the interactive picker returning false is the same branch.
	_, _, ok := promptScopeAndAgents(AddOptions{
		Project: true,
		Yes:     true,
		Copy:    true,
		Agents:  []string{"definitely-not-an-agent"},
	}, cwd)
	if ok {
		t.Fatal("promptScopeAndAgents returned ok with no valid agent")
	}

	if got := lock.GetInstallMode(false, cwd); got != "" {
		t.Errorf("recorded mode = %q, want empty: nothing was installed", got)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the scope was converted even though the install never started")
	}
}

// When the final rename fails, the symlink is put back. It has to be put
// back the way mdm writes every other link, which is RELATIVE: an absolute
// link is shaped unlike its neighbours, and anything comparing link targets
// would read the recovered one as foreign.
func TestRematerializeRestoresARelativeSymlinkAfterARenameFailure(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	// Force the swap to fail. A rename inside one directory does not fail on
	// demand, so this swaps the package-level hook the same way the copy
	// failure test does.
	orig := renameFn
	renameFn = func(oldpath, newpath string) error { return errors.New("forced rename failure") }
	defer func() { renameFn = orig }()

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), []string{linkPath})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted = %d, want 0", n)
	}

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("nothing left at the install path: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was not restored")
	}
	restored, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.IsAbs(restored) {
		t.Errorf("restored link target = %q, want a relative path like every other mdm link", restored)
	}
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(canonical); resolved != want {
		t.Errorf("restored link resolves to %q, want %q", resolved, want)
	}
	// The temp directory holding the copy must not be left behind.
	entries, err := os.ReadDir(linked)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("linked skills dir has %d entries, want 1 (leftover temp dir?)", len(entries))
	}
}

// A symlink mdm did not create must survive a switch to copy mode. Every
// mdm link points into the scope's canonical skills directory; a hand-made
// link to somewhere else exists precisely so edits propagate, and replacing
// it with a frozen copy would break that silently.
func TestRematerializeLeavesForeignSymlinksAlone(t *testing.T) {
	cwd := t.TempDir()

	// The user's own skill source, outside the project entirely.
	outside := filepath.Join(t.TempDir(), "pdf-skill")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "SKILL.md"), []byte("# pdf\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "pdf")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("pdf", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), scopeInstallPaths(false, cwd))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d installs, want 0: a foreign link is not mdm's to replace", n)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the user's own symlink was replaced by a copy")
	}
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(outside); resolved != want {
		t.Errorf("link now resolves to %q, want %q", resolved, want)
	}
}

// An agent that reads the shared .agents/skills directory installs nothing
// of its own: its install path IS the canonical directory, which is a real
// directory in both modes and can never be converted. Listing it as an
// install path made `--copy` warn that every installed skill would be
// re-materialized, take the user's confirmation, and then convert nothing.
func TestScopeInstallPathsSkipsSharedSkillsDirAgents(t *testing.T) {
	if !agent.UsesSharedSkillsDir("amp") {
		t.Skip("fixture agent no longer uses the shared skills directory")
	}
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	// amp is in the registry, so the scope-wide sweep reaches it; only the
	// shared-skills-dir skip keeps its canonical path out of the result.
	if paths := scopeInstallPaths(false, cwd); len(paths) != 0 {
		t.Errorf("scopeInstallPaths = %v, want none: the canonical directory is not convertible", paths)
	}

	// yes=false: reaching the prompt in a non-interactive test cancels, which
	// is the assertion. Nothing is convertible, so nothing may be asked.
	mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, false)
	if !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true without a prompt", mode, ok)
	}
	if got := lock.GetInstallMode(false, cwd); got != lock.InstallModeCopy {
		t.Errorf("recorded mode = %q, want %q", got, lock.InstallModeCopy)
	}
	info, err := os.Lstat(canonical)
	if err != nil || !info.IsDir() {
		t.Fatalf("canonical skill directory disturbed: %v", err)
	}
}

// scopeSkillNames must follow the scope. Reading the project lock in global
// scope misses every globally installed skill, and converts nothing at all
// when run from a directory that has no project lock.
func TestScopeSkillNamesFollowsScope(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()

	if err := lock.AddSkillToLocalLock("project-only", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
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

// The scope's install paths must still be found when configuredAgents is
// empty, which is what a non-interactive `mdm skills add -a <agent> -y`
// leaves behind. Without the scope-wide sweep, --copy in such a project
// converts nothing while recording copy.
func TestApplyScopeInstallModeConvertsWithoutConfiguredAgents(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	if agents := lock.GetConfiguredAgents(false, cwd); len(agents) != 0 {
		t.Fatalf("precondition failed: configuredAgents = %v, want empty", agents)
	}

	if _, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true); !ok {
		t.Fatal("applyScopeInstallMode returned not ok")
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("symlink not converted when configuredAgents is empty")
	}
}

// A non-empty configuredAgents must not narrow the sweep. The list records
// only what the interactive agent picker last saved, and the picker saves it
// before the mode is applied, so a scope switch that consulted it would see a
// selection that omits every agent installed non-interactively.
//
// The shape: `mdm skills add repo -a roo -y` symlinks into .roo/skills and
// leaves configuredAgents empty, then an interactive `mdm skills add other
// --copy` saves ["claude-code"] from the picker. Converting only the saved
// agents would leave .roo/skills symlinked into a canonical directory that
// copy-mode installs no longer refresh, pinning that agent to pre-switch
// content forever.
func TestApplyScopeInstallModeConvertsAgentsMissingFromConfiguredAgents(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	// Two agents installed, only one of them ever named in the picker.
	linkPaths := map[string]string{}
	for agentName, dir := range map[string]string{"roo": ".roo", "claude-code": ".claude"} {
		installDir := filepath.Join(cwd, dir, "skills")
		if err := os.MkdirAll(installDir, 0755); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(installDir, "s1")
		if err := os.Symlink(canonical, linkPath); err != nil {
			t.Skipf("symlinks unavailable on this host: %v", err)
		}
		linkPaths[agentName] = linkPath
	}

	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
	// What promptAgents saves before applyScopeInstallMode runs.
	if err := lock.SetConfiguredAgents([]string{"claude-code"}, false, cwd); err != nil {
		t.Fatal(err)
	}

	if _, ok := applyScopeInstallMode(InstallModeCopy, false, cwd, true); !ok {
		t.Fatal("applyScopeInstallMode returned not ok")
	}

	for agentName, linkPath := range linkPaths {
		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("%s install path missing: %v", agentName, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is still a symlink: the scope is half symlinked and half copied", agentName)
		}
		if _, err := os.Stat(filepath.Join(linkPath, "SKILL.md")); err != nil {
			t.Errorf("%s content not copied: %v", agentName, err)
		}
	}
}

// promptScopeAndAgents must not commit the install mode. Committing it
// converts every install in the scope and records the new mode, and several
// callers still have gates between the agent selection and the first install:
// the post-audit confirmation in `mdm skills add`, and the clobber filter in
// `mdm skills cherry-pick`. Returning from any of them after a conversion
// would leave a converted scope with nothing installed.
func TestPromptScopeAndAgentsDoesNotCommitTheMode(t *testing.T) {
	cwd := t.TempDir()

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	opts := AddOptions{Project: true, Yes: true, Copy: true, Agents: []string{"claude-code"}}
	global, agents, ok := promptScopeAndAgents(opts, cwd)
	if !ok {
		t.Fatal("promptScopeAndAgents returned not ok")
	}
	if len(agents) != 1 || agents[0] != "claude-code" {
		t.Fatalf("agents = %v, want [claude-code]", agents)
	}

	if got := lock.GetInstallMode(false, cwd); got != "" {
		t.Errorf("recorded mode = %q, want empty: the mode is committed by the caller, not here", got)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the scope was converted before the caller's remaining gates ran")
	}

	// And the caller's commit still does the work.
	if _, ok := commitScopeInstallMode(opts, global, cwd); !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	info, err = os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("commitScopeInstallMode did not convert the scope")
	}
}

// The cherry-pick half of the same window. dropClobberingAgents removes every
// agent that already reads the forks directory, and an empty result means
// nothing is installed at all; converting the scope first would record copy
// over a project that never got a copied install.
func TestInstallForksDoesNotConvertWhenEveryAgentIsDropped(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()

	// OpenClaw reads ./skills, which is also the default forks directory, so
	// it is the agent dropClobberingAgents always removes.
	if agent.AllAgents["openclaw"] == nil || agent.AllAgents["openclaw"].SkillsDir != defaultForksDir {
		t.Skip("fixture agent no longer reads the forks directory")
	}

	forkDir := filepath.Join(cwd, defaultForksDir, "f1")
	if err := os.MkdirAll(forkDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forkDir, "SKILL.md"), []byte("---\nname: f1\ndescription: d\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}

	canonical := filepath.Join(cwd, ".agents", "skills", "s1")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonical, "SKILL.md"), []byte("# s1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(cwd, ".claude", "skills")
	if err := os.MkdirAll(linked, 0755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linked, "s1")
	if err := os.Symlink(canonical, linkPath); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	if err := lock.AddSkillToLocalLock("s1", lock.LocalSkillLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}

	installForks([]string{forkDir}, CherryPickOptions{
		Dir:     defaultForksDir,
		Project: true,
		Yes:     true,
		Copy:    true,
		Agents:  []string{"openclaw"},
	}, cwd)

	if got := lock.GetInstallMode(false, cwd); got != "" {
		t.Errorf("recorded mode = %q, want empty: no agent was left to install to", got)
	}
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the scope was converted for an install that never happened")
	}
}
