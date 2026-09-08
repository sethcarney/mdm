package commands

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agentfile"
	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
)

// ── Fixtures ───────────────────────────────────────────────────────────────────

// isolateHome points the global state and every user-level harness directory
// at a fresh temp home, so a test touching global scope never reads or
// rewrites the developer's real files. The registry resolves global paths
// once at init, so harness.Reload is what makes the redirect stick; its cleanup
// is registered first so it runs after the env is restored. Not parallel-safe.
func isolateHome(t *testing.T) string {
	t.Helper()
	t.Cleanup(harness.Reload)
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads this on Windows
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	harness.Reload()
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

// linkSkillInto lays out a symlink-mode install for one harness: the canonical
// .agents/skills/<name> and <harnessDir>/skills/<name> pointing at it.
func linkSkillInto(t *testing.T, cwd, harnessDir, name string) (canonical, link string) {
	t.Helper()
	canonical = filepath.Join(cwd, ".agents", "skills", name)
	writeSkillDir(t, canonical)
	link = filepath.Join(cwd, harnessDir, "skills", name)
	symlinkOrSkip(t, canonical, link)
	return canonical, link
}

// linkSkill is linkSkillInto for Claude Code, the harness most tests use.
func linkSkill(t *testing.T, cwd, name string) (canonical, link string) {
	t.Helper()
	return linkSkillInto(t, cwd, ".claude", name)
}

// writeCopiedSkill lays out a copy-mode install for Claude Code: a real
// directory at the harness path and no canonical directory, which is what
// `--copy` writes for a harness with a directory of its own.
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

func setConfigured(t *testing.T, cwd string, harnesses ...string) {
	t.Helper()
	if err := lock.SetConfiguredHarnesses(harnesses, false, cwd); err != nil {
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

// failRenameNth fails only the nth rename of the test and performs every other,
// so a conversion that renames more than once can be broken at one step and its
// recovery watched. Mirrors failRename; equally not parallel-safe.
func failRenameNth(t *testing.T, n int) {
	t.Helper()
	orig := renameFn
	calls := 0
	renameFn = func(from, to string) error {
		calls++
		if calls == n {
			return errors.New("forced rename failure")
		}
		return orig(from, to)
	}
	t.Cleanup(func() { renameFn = orig })
}

// failSymlink swaps symlinkFn for one that always errors, standing in for a
// Windows host without the symlink privilege - the host class copy mode exists
// for. Shared mutable state, so not parallel-safe.
func failSymlink(t *testing.T) {
	t.Helper()
	orig := symlinkFn
	symlinkFn = func(_, _ string) error { return errors.New("symlinks unavailable") }
	t.Cleanup(func() { symlinkFn = orig })
}

// readlinkOrFail returns a symlink's target exactly as it is stored, so a test
// can assert a recovery put back the link it found rather than a link of its
// own making.
func readlinkOrFail(t *testing.T, link string) string {
	t.Helper()
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("reading the link at %s: %v", link, err)
	}
	return got
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
			global, _, ok := promptScopeAndHarnesses(tc.opts, cwd)
			if !ok {
				t.Fatal("promptScopeAndHarnesses returned not ok")
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
	// The canonical directory stays: shared-dir harnesses install into it in
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
// says: that list only records what the interactive picker last saved, so a
// harness installed with `-a <harness> -y` is missing from it. Converting only
// the listed harnesses would leave the scope half symlinked and half copied.
func TestApplyScopeInstallModeConvertsEveryInstall(t *testing.T) {
	cases := []struct {
		name        string
		harnessDirs map[string]string // harness name -> project directory
		configured  []string
	}{
		{"configured harness", map[string]string{"claude-code": ".claude"}, []string{"claude-code"}},
		{"no configured harnesses", map[string]string{"claude-code": ".claude"}, nil},
		{"harness missing from configuredAgents", map[string]string{"claude-code": ".claude", "roo": ".roo"}, []string{"claude-code"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			var canonical string
			links := map[string]string{}
			for name, dir := range tc.harnessDirs {
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

// A directory cannot be renamed over a symlink on either platform, so its link
// is set aside with a rename first. When that first rename fails the install
// path was never touched and there is nothing to recover.
func TestRematerializeRenameFailureLeavesTheSymlinkAsItWas(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	want := readlinkOrFail(t, link)
	failRename(t)

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), []conversionPath{selfNamedConversionPath(link)})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	assertLinkTo(t, link, canonical)
	if got := readlinkOrFail(t, link); got != want {
		t.Errorf("link target = %q, want it untouched (%q)", got, want)
	}
	assertNoTempDirs(t, filepath.Dir(link))
}

// The recovery from a failed move-in has to work on a host that cannot create
// a symlink at all. Windows without Developer Mode is that host, and copy mode
// is what it runs; a link can still be sitting at the install path, cloned or
// committed by a teammate on a platform that has them. Restoring by calling
// createSymlink there could only fail, and the install path would stay empty
// while the run reported the content stranded in a temp directory.
//
// Mutation this test catches: recovering with createSymlink(resolved, target)
// instead of renaming the set-aside link back into place.
func TestRematerializeRestoresTheSymlinkWithoutCreatingOne(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	want := readlinkOrFail(t, link)
	// 1 sets the link aside, 2 moves the copy in, 3 puts the link back.
	failRenameNth(t, 2)
	failSymlink(t)

	n, err := rematerializeScope(InstallModeCopy, getCanonicalSkillsDir(false, cwd), []conversionPath{selfNamedConversionPath(link)})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	assertLinkTo(t, link, canonical)
	if got := readlinkOrFail(t, link); got != want {
		t.Errorf("restored link target = %q, want the link that was there (%q)", got, want)
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

// A harness that reads the shared .agents/skills directory has no install
// path of its own: its path IS the canonical directory, real in both modes.
// Listing it made --copy announce a conversion and then convert nothing.
func TestScopeInstallPathsSkipsSharedSkillsDirHarnesses(t *testing.T) {
	if !harness.UsesSharedSkillsDir("amp") {
		t.Skip("fixture harness no longer uses the shared skills directory")
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
// canonical copy first because a copy install never wrote one for this harness.
func TestRematerializeConvertsRealDirectoriesToSymlinks(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	canonical := filepath.Join(cwd, ".agents", "skills")

	n, err := rematerializeScope(InstallModeSymlink, canonical, []conversionPath{selfNamedConversionPath(target)})
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
// A copy that has drifted from the canonical directory holds edits nothing
// else does. Linking it would resolve to the canonical content and the edits
// would be gone, which is what the converter used to do (and what this test
// used to assert). The copy stays a copy, uncounted, and the run says why.
func TestRematerializeToSymlinkKeepsAnEditedCopyAsACopy(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	if err := os.WriteFile(filepath.Join(target, "SKILL.md"), []byte("edited locally\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(cwd, ".agents", "skills")
	writeSkillDir(t, filepath.Join(canonical, "s1"))
	if err := os.WriteFile(filepath.Join(canonical, "s1", "SKILL.md"), []byte("canonical\n"), 0600); err != nil {
		t.Fatal(err)
	}

	n, err := rematerializeScope(InstallModeSymlink, canonical, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0: an edited copy is not linked", n)
	}
	assertRealSkill(t, target)
	got, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "edited locally\n" {
		t.Errorf("copy content = %q, want the local edits kept", got)
	}
}

// A copy identical to the canonical directory has nothing to lose, so it is
// linked, and the existing canonical content is what the link resolves to.
func TestRematerializeToSymlinkLinksAnUnchangedCopy(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedSkill(t, cwd, "s1")
	canonical := filepath.Join(cwd, ".agents", "skills")
	if err := copyDirectory(target, filepath.Join(canonical, "s1")); err != nil {
		t.Fatal(err)
	}

	n, err := rematerializeScope(InstallModeSymlink, canonical, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("converted %d, want 1", n)
	}
	assertLinkTo(t, target, filepath.Join(canonical, "s1"))
}

// A link whose canonical target is gone used to fail EvalSymlinks and abort
// the whole conversion, leaving the scope half converted with the old mode
// recorded. mdm's own dangling link is skipped with a pointer at doctor; the
// healthy install beside it still converts.
func TestRematerializeSkipsADanglingMdmLink(t *testing.T) {
	cwd := t.TempDir()
	canonicalDir := filepath.Join(cwd, ".agents", "agents")
	if err := os.MkdirAll(canonicalDir, 0755); err != nil {
		t.Fatal(err)
	}
	healthy := filepath.Join(canonicalDir, "critic.md")
	if err := os.WriteFile(healthy, []byte("---\nname: critic\ndescription: d\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(installDir, "ghost.md")
	if err := os.Symlink(filepath.Join(canonicalDir, "ghost.md"), dangling); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
	good := filepath.Join(installDir, "critic.md")
	if err := os.Symlink(healthy, good); err != nil {
		t.Fatal(err)
	}

	paths := []conversionPath{selfNamedConversionPath(dangling), selfNamedConversionPath(good)}
	n, err := rematerializeScope(InstallModeCopy, canonicalDir, paths)
	if err != nil {
		t.Fatalf("a dangling link aborted the conversion: %v", err)
	}
	if n != 1 {
		t.Errorf("converted %d, want 1: the healthy link", n)
	}
	if info, err := os.Lstat(good); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the healthy link was not converted (err=%v)", err)
	}
	if info, err := os.Lstat(dangling); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the dangling link should be left for doctor (err=%v)", err)
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

	n, err := rematerializeScope(InstallModeSymlink, canonical, []conversionPath{selfNamedConversionPath(target)})
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

	n, err := rematerializeScope(InstallModeSymlink, filepath.Join(cwd, ".agents", "skills"), []conversionPath{selfNamedConversionPath(target)})
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

// Cancelling at the harness selection must leave the scope untouched: a
// conversion run before the picker converted the project before a single
// skill was fetched, so backing out left copies behind and nothing installed.
func TestPromptScopeDoesNotConvertWhenHarnessSelectionFails(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	setConfigured(t, cwd, "claude-code")
	lockSkill(t, cwd, "s1")

	// Naming only an unknown harness fails selection without a terminal.
	opts := AddOptions{Project: true, Yes: true, Copy: true, Harnesses: []string{"definitely-not-a-harness"}}
	if _, _, ok := promptScopeAndHarnesses(opts, cwd); ok {
		t.Fatal("promptScopeAndHarnesses returned ok with no valid harness")
	}
	assertRecordedMode(t, cwd, "")
	assertLinkTo(t, link, canonical)
}

// promptScopeAndHarnesses must not commit the mode: callers still have gates
// after it (the post-audit confirmation, cherry-pick's clobber filter), and
// returning from one after a conversion leaves a converted scope with nothing
// installed. The caller's commit is what does the work.
func TestPromptScopeAndHarnessesDoesNotCommitTheMode(t *testing.T) {
	cwd := t.TempDir()
	canonical, link := linkSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")

	opts := AddOptions{Project: true, Yes: true, Copy: true, Harnesses: []string{"claude-code"}}
	global, harnesses, ok := promptScopeAndHarnesses(opts, cwd)
	if !ok || len(harnesses) != 1 || harnesses[0] != "claude-code" {
		t.Fatalf("harnesses = %v ok = %v, want [claude-code] true", harnesses, ok)
	}
	assertRecordedMode(t, cwd, "")
	assertLinkTo(t, link, canonical)

	if _, ok := commitScopeInstallMode(opts, global, cwd); !ok {
		t.Fatal("commitScopeInstallMode returned not ok")
	}
	assertRealSkill(t, link)
}

// dropClobberingHarnesses can empty the harness list, in which case nothing is
// installed and the scope must not be converted for it.
func TestInstallForksDoesNotConvertWhenEveryHarnessIsDropped(t *testing.T) {
	isolateHome(t)
	cwd := t.TempDir()
	// OpenClaw reads ./skills, the default forks directory, so it is always dropped.
	if a := harness.AllHarnesses["openclaw"]; a == nil || a.SkillsDir != defaultForksDir {
		t.Skip("fixture harness no longer reads the forks directory")
	}
	forkDir := filepath.Join(cwd, defaultForksDir, "f1")
	writeSkillDir(t, forkDir)
	canonical, link := linkSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")

	installForks([]string{forkDir}, CherryPickOptions{
		Dir: defaultForksDir, Project: true, Yes: true, Copy: true, Harnesses: []string{"openclaw"},
	}, cwd)

	assertRecordedMode(t, cwd, "")
	assertLinkTo(t, link, canonical)
}

// The two mode flags contradict each other, so every command taking them
// must refuse the pair.
//
// Each command's Run is replaced with a stub first. Cobra rejects the pair in
// ValidateFlagGroups, before Run, so the real handler must never be reached
// here - and reaching it is not harmless. "o/r" is a GitHub shorthand: with a
// MarkFlagsMutuallyExclusive registration lost, which is the one regression
// this test exists to catch, `add` and `agents add` would clone
// https://github.com/o/r.git for real and then call os.Exit(1) on the
// authentication failure from inside the command. os.Exit takes the whole
// test binary down with it, so the regression would show up as a crashed,
// network-dependent run with no `--- FAIL` line for this or any other test
// queued behind it. The stub keeps the failure a plain assertion, offline.
func TestCopyAndSymlinkFlagsAreMutuallyExclusive(t *testing.T) {
	cases := []struct {
		name string
		cmd  *cobra.Command
		args []string
	}{
		{"add", buildAddCmd("test"), []string{"o/r", "--copy", "--symlink"}},
		{"cherry-pick", buildCherryPickCmd("test"), []string{"o/r", "--copy", "--symlink"}},
		{"install", buildInstallFromLockCmd("test"), []string{"--copy", "--symlink"}},
		{"agents add", buildAgentAddCmd(), []string{"o/r", "--copy", "--symlink"}},
		{"agents install", buildAgentsInstallCmd(), []string{"--copy", "--symlink"}},
	}
	for _, tc := range cases {
		ran := false
		tc.cmd.Run = func(*cobra.Command, []string) { ran = true }
		tc.cmd.RunE = nil
		tc.cmd.SetArgs(tc.args)
		tc.cmd.SetOut(io.Discard)
		tc.cmd.SetErr(io.Discard)
		err := tc.cmd.Execute()
		if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
			t.Errorf("%s --copy --symlink: err = %v, want a mutual-exclusion error", tc.name, err)
		}
		if ran {
			t.Errorf("%s --copy --symlink: the command ran; the flag pair was accepted", tc.name)
		}
	}
}

// ── Single-file conversions (agent definitions) ─────────────────────────────
//
// Agent definitions are single files: canonical at .agents/agents/<name>.md,
// installed as <name>.md, or <name><ext> for a harness with its own suffix.
// These fixtures mirror the directory-shaped ones above.

// writeAgentFile writes a canonical-shaped agent-definition file at
// dir/name+".md" and returns its path.
func writeAgentFile(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: d\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// agentSourceDir writes one discoverable "critic" definition into a fresh
// temp directory and returns that directory, for use as a local source by an
// add or a restore. DiscoverAgentFiles scans conventional subdirectories, so
// the file goes in "agents/" rather than at the root.
func agentSourceDir(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeAgentFile(t, filepath.Join(src, "agents"), "critic")
	return src
}

// runAgentsAdd drives the real `mdm agents add` command rather than
// runAgentAdd, so the flags the command registers are part of what is tested.
func runAgentsAdd(t *testing.T, src string, extra ...string) {
	t.Helper()
	cmd := buildAgentAddCmd()
	cmd.SetArgs(append([]string{src, "--project", "--yes"}, extra...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	var err error
	captureStdout(t, func() { err = cmd.Execute() })
	if err != nil {
		t.Fatalf("agents add %v: %v", extra, err)
	}
}

// runAgentsInstall drives the real `mdm agents install` command, the restore
// counterpart of runAgentsAdd. It takes no source: the lock supplies that.
func runAgentsInstall(t *testing.T, extra ...string) {
	t.Helper()
	cmd := buildAgentsInstallCmd()
	cmd.SetArgs(append([]string{"--yes"}, extra...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	var err error
	captureStdout(t, func() { err = cmd.Execute() })
	if err != nil {
		t.Fatalf("agents install %v: %v", extra, err)
	}
}

// linkAgentFile lays out a symlink-mode agent install: the canonical
// .agents/agents/<name>.md and <cwd>/.claude/agents/<name>.md pointing at it.
func linkAgentFile(t *testing.T, cwd, name string) (canonical, canonicalDir, link string) {
	t.Helper()
	canonicalDir = filepath.Join(cwd, ".agents", "agents")
	canonical = writeAgentFile(t, canonicalDir, name)
	link = filepath.Join(cwd, ".claude", "agents", name+".md")
	symlinkOrSkip(t, canonical, link)
	return canonical, canonicalDir, link
}

// writeCopiedAgentFile lays out a copy-mode agent install: a real file at
// the harness path and no canonical file, mirroring writeCopiedSkill.
func writeCopiedAgentFile(t *testing.T, cwd, name string) string {
	t.Helper()
	return writeAgentFile(t, filepath.Join(cwd, ".claude", "agents"), name)
}

// failCopyFile swaps copyFileFn for one that always errors, since a copy
// failure cannot be forced reliably at the OS level. Mirrors failCopy.
func failCopyFile(t *testing.T) {
	t.Helper()
	orig := copyFileFn
	copyFileFn = func(_, _ string) error { return errors.New("forced copy failure") }
	t.Cleanup(func() { copyFileFn = orig })
}

// Agent definitions are single files, and they obey the same scope-wide
// mode as skills. A converter that only understands directories would
// leave them behind on whichever shape they were installed with.
func TestRematerializeConvertsASingleFile(t *testing.T) {
	cwd := t.TempDir()
	canonicalDir := filepath.Join(cwd, ".agents", "agents")
	if err := os.MkdirAll(canonicalDir, 0755); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(canonicalDir, "critic.md")
	if err := os.WriteFile(canonical, []byte("---\nname: critic\ndescription: d\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(cwd, ".claude", "agents")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installDir, "critic.md")
	if err := os.Symlink(canonical, target); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}

	n, err := rematerializeScope(InstallModeCopy, canonicalDir, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("converted %d, want 1", n)
	}
	info, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("still a symlink after conversion to copy")
	}
	if _, err := os.Stat(canonical); err != nil {
		t.Errorf("canonical file destroyed: %v", err)
	}
}

// A file already installed as a real copy is left alone: nothing to convert.
func TestRematerializeFileIsANoOpWhenAlreadyReal(t *testing.T) {
	cwd := t.TempDir()
	real := writeCopiedAgentFile(t, cwd, "critic")

	n, err := rematerializeScope(InstallModeCopy, filepath.Join(cwd, ".agents", "agents"), []conversionPath{selfNamedConversionPath(real)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	if _, err := os.Stat(real); err != nil {
		t.Fatal(err)
	}
}

// A failed file copy leaves the symlink, and the install path, exactly as
// they were: no empty gap between old and new.
func TestRematerializeFileFailureLeavesSymlinkUnchanged(t *testing.T) {
	cwd := t.TempDir()
	canonical, canonicalDir, link := linkAgentFile(t, cwd, "critic")
	failCopyFile(t)

	n, err := rematerializeScope(InstallModeCopy, canonicalDir, []conversionPath{selfNamedConversionPath(link)})
	if err == nil {
		t.Fatal("expected an error from the forced copy failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("install path is empty after a failed conversion: %v", err)
	}
	assertLinkTo(t, link, canonical)
	assertNoTempDirs(t, filepath.Dir(link))
}

// A file is renamed straight over its link, so the install path is never
// emptied first and a failed rename needs no recovery: the link is still there,
// exactly as it was. failSymlink is what makes that load-bearing - a conversion
// that removes the link first can only put one back by creating it, and this
// host cannot.
//
// Mutation this test catches: an os.Remove(target) before the rename in the
// file arm of replaceLinkWithCopy, which leaves the install path empty.
func TestRematerializeFileRenameFailureLeavesTheLinkUntouched(t *testing.T) {
	cwd := t.TempDir()
	canonical, canonicalDir, link := linkAgentFile(t, cwd, "critic")
	want := readlinkOrFail(t, link)
	failRename(t)
	failSymlink(t)

	n, err := rematerializeScope(InstallModeCopy, canonicalDir, []conversionPath{selfNamedConversionPath(link)})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("install path is empty after a failed conversion: %v", err)
	}
	assertLinkTo(t, link, canonical)
	if got := readlinkOrFail(t, link); got != want {
		t.Errorf("link target = %q, want it untouched (%q)", got, want)
	}
	assertNoTempDirs(t, filepath.Dir(link))
}

// A symlink mdm did not create, pointing outside the canonical directory,
// is left untouched: it exists so edits propagate, and a frozen copy would
// break that.
func TestRematerializeLeavesAForeignFileSymlinkAlone(t *testing.T) {
	cwd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "critic.md")
	if err := os.WriteFile(outside, []byte("mine\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(cwd, ".claude", "agents", "critic.md")
	symlinkOrSkip(t, outside, link)

	n, err := rematerializeScope(InstallModeCopy, filepath.Join(cwd, ".agents", "agents"), []conversionPath{selfNamedConversionPath(link)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0: a foreign link is not mdm's to replace", n)
	}
	assertLinkTo(t, link, outside)
}

// ── File: copy to symlink ────────────────────────────────────────────────────

// --symlink turns a real file install back into an mdm link, creating the
// canonical copy first because a copy install never wrote one.
func TestRematerializeConvertsASingleFileToSymlink(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedAgentFile(t, cwd, "critic")
	canonicalDir := filepath.Join(cwd, ".agents", "agents")

	n, err := rematerializeScope(InstallModeSymlink, canonicalDir, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("converted %d, want 1", n)
	}
	assertLinkTo(t, target, filepath.Join(canonicalDir, "critic.md"))
	if _, err := os.Stat(filepath.Join(canonicalDir, "critic.md")); err != nil {
		t.Errorf("canonical file missing after conversion: %v", err)
	}
}

// A copy that differs from the existing canonical file holds edits the
// canonical file does not, so linking it would drop them. It stays a copy,
// uncounted, with a warning. This test used to assert the opposite.
func TestRematerializeToSymlinkKeepsAnEditedFileAsACopy(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedAgentFile(t, cwd, "critic")
	if err := os.WriteFile(target, []byte("---\nname: critic\ndescription: edited locally\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canonicalDir := filepath.Join(cwd, ".agents", "agents")
	canonicalPath := writeAgentFile(t, canonicalDir, "critic")
	if err := os.WriteFile(canonicalPath, []byte("canonical\n"), 0600); err != nil {
		t.Fatal(err)
	}

	n, err := rematerializeScope(InstallModeSymlink, canonicalDir, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0: an edited copy is not linked", n)
	}
	if isSymlink(t, target) {
		t.Fatal("the edited copy was replaced by a link")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "---\nname: critic\ndescription: edited locally\n---\n" {
		t.Errorf("copy content = %q, want the local edits kept", got)
	}
}

// A copy identical to the existing canonical file is linked, and the
// canonical file is what the link resolves to.
func TestRematerializeToSymlinkLinksAnUnchangedFile(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedAgentFile(t, cwd, "critic")
	canonicalDir := filepath.Join(cwd, ".agents", "agents")
	canonicalPath := writeAgentFile(t, canonicalDir, "critic")

	n, err := rematerializeScope(InstallModeSymlink, canonicalDir, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("converted %d, want 1", n)
	}
	assertLinkTo(t, target, canonicalPath)
}

// If the canonical copy cannot be created, the real file stays in place.
func TestRematerializeToSymlinkCopyFailureLeavesTheFile(t *testing.T) {
	cwd := t.TempDir()
	target := writeCopiedAgentFile(t, cwd, "critic")
	failCopyFile(t)

	n, err := rematerializeScope(InstallModeSymlink, filepath.Join(cwd, ".agents", "agents"), []conversionPath{selfNamedConversionPath(target)})
	if err == nil {
		t.Fatal("expected an error from the forced copy failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	if isSymlink(t, target) {
		t.Error("target became a symlink despite the forced copy failure")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("install path is empty after a failed conversion: %v", err)
	}
}

// If the real file cannot be set aside, it stays exactly where it was.
func TestRematerializeToSymlinkRenameFailureLeavesTheFile(t *testing.T) {
	cwd := t.TempDir()
	symlinkOrSkip(t, cwd, filepath.Join(cwd, "probe"))
	target := writeCopiedAgentFile(t, cwd, "critic")
	failRename(t)

	n, err := rematerializeScope(InstallModeSymlink, filepath.Join(cwd, ".agents", "agents"), []conversionPath{selfNamedConversionPath(target)})
	if err == nil {
		t.Fatal("expected an error from the forced rename failure")
	}
	if n != 0 {
		t.Errorf("converted %d, want 0", n)
	}
	if isSymlink(t, target) {
		t.Error("target became a symlink despite the forced rename failure")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("install path is empty after a failed conversion: %v", err)
	}
}

// A real file at a tracked install path that does not parse as an agent
// definition belongs to someone else, the mirror of a real directory with no
// SKILL.md, and stays untouched. The concrete case: a user hand-writes
// .claude/agents/critic.md and the lock records a definition also named
// "critic" at that path.
func TestRematerializeToSymlinkLeavesANonAgentFileAlone(t *testing.T) {
	cwd := t.TempDir()
	target := filepath.Join(cwd, ".claude", "agents", "critic.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("mine, not an agent definition\n"), 0600); err != nil {
		t.Fatal(err)
	}
	canonicalDir := filepath.Join(cwd, ".agents", "agents")

	n, err := rematerializeScope(InstallModeSymlink, canonicalDir, []conversionPath{selfNamedConversionPath(target)})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("converted %d, want 0: a file that does not parse as an agent definition is not mdm's to replace", n)
	}
	if isSymlink(t, target) {
		t.Error("target became a symlink despite not parsing as an agent definition")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mine, not an agent definition\n" {
		t.Error("target content changed despite not parsing as an agent definition")
	}
	if _, err := os.Stat(filepath.Join(canonicalDir, "critic.md")); !os.IsNotExist(err) {
		t.Errorf("canonical file created for a non-agent file: %v", err)
	}
}

// ── Scope conversion reaches agent definitions ─────────────────────────────

// lockAgent records one agent definition in the project lock, so the scope
// conversion can find it the way it finds a skill.
func lockAgent(t *testing.T, cwd, name string) {
	t.Helper()
	if err := lock.AddAgentToLocalLock(name, lock.AgentLockEntry{Source: "o/r", SourceType: "github"}, cwd); err != nil {
		t.Fatal(err)
	}
}

// The install mode is a property of the scope, so `mdm skills add --copy` must
// convert agent definitions too. This goes through applyScopeInstallMode rather
// than rematerializeScope: the converters handle single files, and the gap was
// in the wiring that never handed them an agent definition's path.
func TestApplyScopeInstallModeConvertsAgentDefinitions(t *testing.T) {
	cwd := t.TempDir()
	skillCanonical, skillLink := linkSkill(t, cwd, "s1")
	agentCanonical, _, agentLink := linkAgentFile(t, cwd, "critic")
	lockSkill(t, cwd, "s1")
	lockAgent(t, cwd, "critic")
	assertRecordedMode(t, cwd, "")

	if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true", mode, ok)
	}
	assertRecordedMode(t, cwd, lock.InstallModeCopy)

	assertRealSkill(t, skillLink)
	assertSameMode(t, skillLink, skillCanonical)
	if isSymlink(t, agentLink) {
		t.Fatalf("%s is still a symlink after the scope switched to copy mode", agentLink)
	}
	got, err := os.ReadFile(agentLink)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(agentCanonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("converted agent definition = %q, want %q", got, want)
	}

	// And back: the switch is lossless in both directions for both shapes.
	if mode, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd); !ok || mode != InstallModeSymlink {
		t.Fatalf("mode = %q ok = %v, want symlink true", mode, ok)
	}
	if !isSymlink(t, agentLink) {
		t.Error("agent definition did not convert back to a symlink")
	}
	if !isSymlink(t, skillLink) {
		t.Error("skill did not convert back to a symlink")
	}
}

// canonicalFileNames lists what the scope's canonical agents directory holds,
// which is how a duplicate minted from an install path's basename shows up.
func canonicalFileNames(t *testing.T, canonicalDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// GitHub Copilot reads agent definitions only as "<name>.agent.md", so its
// install path's basename is not the canonical file's name, and a canonical
// name derived from that basename mints a duplicate. Copilot always
// materializes now, so the switch leaves even a symlink install written by an
// older mdm alone, and the canonical directory keeps its one file.
func TestApplyScopeInstallModeKeepsOneCanonicalFileForASuffixedHarness(t *testing.T) {
	cwd := t.TempDir()
	canonicalDir := harness.CanonicalAgentsDir(false, cwd)
	canonical := writeAgentFile(t, canonicalDir, "critic")
	link := agentHarnessPath("critic", "github-copilot", false, cwd)
	if base := filepath.Base(link); base != "critic.agent.md" {
		t.Fatalf("github-copilot install path basename = %q, want %q; this test is only meaningful for a harness that overrides the extension", base, "critic.agent.md")
	}
	symlinkOrSkip(t, canonical, link)
	lockAgent(t, cwd, "critic")

	if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
		t.Fatalf("mode = %q ok = %v, want copy true", mode, ok)
	}
	assertLinkTo(t, link, canonical)
	if mode, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd); !ok || mode != InstallModeSymlink {
		t.Fatalf("mode = %q ok = %v, want symlink true", mode, ok)
	}
	assertLinkTo(t, link, canonical)

	if names := canonicalFileNames(t, canonicalDir); len(names) != 1 || names[0] != "critic.md" {
		t.Errorf("canonical directory holds %v, want exactly [critic.md]", names)
	}
}

// copyToLink is reached only through scopeAgentInstallPaths, which marks every
// harness reading its own extension as materialized, so no production path can
// now hand it a canonicalName differing from the target's basename. The guard
// stays reachable by a plausible harness - a custom extension in a directory
// nobody commits - so it is exercised here directly, with no wiring in front.
func TestCopyToLinkUsesTheGivenCanonicalName(t *testing.T) {
	if !symlinkProbe(t) {
		t.Skip("symlinks unavailable on this host; a copy here proves nothing")
	}
	cwd := t.TempDir()
	canonicalDir := harness.CanonicalAgentsDir(false, cwd)
	if err := os.MkdirAll(canonicalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := writeAgentFile(t, filepath.Join(cwd, ".suffixed", "agents"), "critic.agent")

	did, err := copyToLink(resolvedDir(canonicalDir), target, "critic.md")
	if err != nil || !did {
		t.Fatalf("copyToLink did = %v err = %v, want true nil", did, err)
	}
	if names := canonicalFileNames(t, canonicalDir); len(names) != 1 || names[0] != "critic.md" {
		t.Errorf("canonical directory holds %v, want exactly [critic.md]", names)
	}
	assertLinkTo(t, target, filepath.Join(canonicalDir, "critic.md"))
}

// The conversion count the scope prints, and the "converted N of them"
// number a failure reports, are only meaningful against a stable order.
// AllHarnesses is a map, so the agent sweep sorts it.
func TestScopeAgentInstallPathsIsSortedAndDeduplicated(t *testing.T) {
	cwd := t.TempDir()
	linkAgentFile(t, cwd, "critic")
	lockAgent(t, cwd, "critic")

	first := scopeAgentInstallPaths(false, cwd)
	if len(first) == 0 {
		t.Fatal("no agent install paths found for a locked, installed definition")
	}
	for i := 0; i < 20; i++ {
		if got := scopeAgentInstallPaths(false, cwd); !reflect.DeepEqual(got, first) {
			t.Fatalf("order is not stable across runs:\n%v\n%v", first, got)
		}
	}
	seen := map[string]bool{}
	for _, p := range first {
		if seen[p.target] {
			t.Errorf("duplicate path %s", p.target)
		}
		seen[p.target] = true
	}
}

// ── A materialized install is not converted ────────────────────────────────

// lockAgentInFormat is lockAgent for a definition whose canonical file is not
// markdown. The recorded format decides the canonical file's name, and with it
// which harnesses have to materialize.
func lockAgentInFormat(t *testing.T, cwd, name string, format agentfile.Format) {
	t.Helper()
	entry := lock.AgentLockEntry{Source: "o/r", SourceType: "github", Format: string(format)}
	if err := lock.AddAgentToLocalLock(name, entry, cwd); err != nil {
		t.Fatal(err)
	}
}

// installAgentForTest installs one parsed definition into harnessName through
// the real installer and returns the path it wrote. The fixtures above build
// installs by hand; these tests need whatever the installer actually lays down.
func installAgentForTest(t *testing.T, a *agentfile.AgentFile, harnessName, cwd string) string {
	t.Helper()
	res := installAgentFile(a, harnessName, false, cwd, InstallModeSymlink)
	if !res.Success {
		t.Fatalf("installing %s for %s: %s", a.Name, harnessName, res.Error)
	}
	return res.Path
}

// assertMaterializedDefinition checks path is a real file, not a symlink, whose
// bytes parse as a definition in the format harnessName reads. Reading the file
// is the point: a symlink to a canonical file in the other format is still a
// readable path, and only parsing it in the harness's own format shows that the
// bytes behind it are not the ones the harness can use.
func assertMaterializedDefinition(t *testing.T, path, harnessName string) {
	t.Helper()
	want := harness.AgentFormat(harnessName)
	if isSymlink(t, path) {
		t.Fatalf("%s is a symlink; %s needs real %s bytes there", path, harnessName, want)
	}
	got, err := agentfile.ParseAgentFile(path)
	if err != nil {
		t.Fatalf("%s does not parse as %s: %v", path, want, err)
	}
	if got == nil {
		t.Fatalf("%s parses as nothing, so it carries no name or description", path)
	}
	if got.Format != want {
		t.Errorf("%s parsed as %q, want %q", path, got.Format, want)
	}
}

// A materialized install is a real file by rule, not by mode: Copilot's
// .github/agents is committed, and a TOML canonical converts for every markdown
// harness. Both of those become symlinks without the skip; the Codex row is a
// regression case, noted on it. The round trip runs through
// applyScopeInstallMode, where the install paths and their names are decided.
func TestApplyScopeInstallModeLeavesAMaterializedAgentInstallAlone(t *testing.T) {
	if !symlinkProbe(t) {
		t.Skip("symlinks unavailable on this host; nothing here can round trip")
	}
	cases := []struct {
		name         string
		sourceExt    string
		materialized string
		// linked is a harness taking the canonical file as it stands, or ""
		// when no harness in this case does. It must convert both ways.
		linked string
	}{
		// isMdmOwnedCopyInstall parses a .toml install as a definition, so the
		// materialized skip is the only thing keeping Codex's re-encoded copy
		// of a markdown canonical from being linked back at markdown bytes.
		{"codex reads toml", ".md", "codex", "claude-code"},
		{"copilot commits its agents directory", ".md", "github-copilot", "claude-code"},
		{"a toml canonical converts for markdown harnesses", ".toml", "claude-code", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			skillCanonical, skillLink := linkSkill(t, cwd, "s1")
			lockSkill(t, cwd, "s1")

			a := writeParsedAgent(t, tc.sourceExt)
			name := agentDiskName(a.Name)
			materialized := installAgentForTest(t, a, tc.materialized, cwd)
			var linked string
			if tc.linked != "" {
				linked = installAgentForTest(t, a, tc.linked, cwd)
				if !isSymlink(t, linked) {
					t.Fatalf("%s did not install as a symlink; this case proves nothing", linked)
				}
			}
			lockAgentInFormat(t, cwd, name, a.Format)

			if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
				t.Fatalf("switch to copy: mode = %q ok = %v", mode, ok)
			}
			assertMaterializedDefinition(t, materialized, tc.materialized)
			assertRealSkill(t, skillLink)

			if mode, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd); !ok || mode != InstallModeSymlink {
				t.Fatalf("switch back to symlink: mode = %q ok = %v", mode, ok)
			}
			assertMaterializedDefinition(t, materialized, tc.materialized)
			assertLinkTo(t, skillLink, skillCanonical)
			if linked != "" {
				assertLinkTo(t, linked, agentCanonicalPath(name, a.Format, false, cwd))
			}
		})
	}
}

// The bug is fixable by skipping every agent install, and that would strand
// ordinary ones on whichever shape they were installed with. Claude Code reads
// markdown out of a generated directory, so a markdown definition installed
// there converts in both directions like any skill.
func TestApplyScopeInstallModeStillConvertsAnOrdinaryAgentInstall(t *testing.T) {
	if !symlinkProbe(t) {
		t.Skip("symlinks unavailable on this host; a copy here proves nothing")
	}
	cwd := t.TempDir()
	a := writeParsedAgent(t, ".md")
	name := agentDiskName(a.Name)
	installed := installAgentForTest(t, a, "claude-code", cwd)
	lockAgentInFormat(t, cwd, name, a.Format)

	if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
		t.Fatalf("switch to copy: mode = %q ok = %v", mode, ok)
	}
	if isSymlink(t, installed) {
		t.Fatalf("%s is still a symlink after the scope switched to copy mode", installed)
	}
	if mode, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd); !ok || mode != InstallModeSymlink {
		t.Fatalf("switch back to symlink: mode = %q ok = %v", mode, ok)
	}
	assertLinkTo(t, installed, agentCanonicalPath(name, a.Format, false, cwd))
}

// A TOML canonical is the one definition Codex takes as a plain symlink, so
// nothing marks its install materialized and the copy→symlink direction has to
// convert it like any other. Mutation this test catches: isMdmOwnedCopyInstall
// reading the target with agentfile.ParseAgentMd rather than ParseAgentFile. A
// .toml file carries no `---` frontmatter, so ParseAgentMd returns (nil, nil),
// the copy is judged foreign, and the switch back to symlink mode converts
// nothing while recording `installMode: symlink` for the scope.
func TestApplyScopeInstallModeConvertsATomlCopyBackToASymlink(t *testing.T) {
	if !symlinkProbe(t) {
		t.Skip("symlinks unavailable on this host; a copy here proves nothing")
	}
	cwd := t.TempDir()
	a := writeParsedAgent(t, ".toml")
	name := agentDiskName(a.Name)
	installed := installAgentForTest(t, a, "codex", cwd)
	lockAgentInFormat(t, cwd, name, a.Format)
	if !isSymlink(t, installed) {
		t.Fatalf("%s did not install as a symlink; this test proves nothing", installed)
	}

	if mode, ok := applyScopeInstallMode(InstallModeCopy, false, cwd); !ok || mode != InstallModeCopy {
		t.Fatalf("switch to copy: mode = %q ok = %v", mode, ok)
	}
	if isSymlink(t, installed) {
		t.Fatalf("%s is still a symlink after the scope switched to copy mode", installed)
	}

	if mode, ok := applyScopeInstallMode(InstallModeSymlink, false, cwd); !ok || mode != InstallModeSymlink {
		t.Fatalf("switch back to symlink: mode = %q ok = %v", mode, ok)
	}
	assertLinkTo(t, installed, agentCanonicalPath(name, a.Format, false, cwd))
}

// ── The mode flags on `mdm agents add` ─────────────────────────────────────

// Mutation this test catches: dropping --copy and --symlink from the
// `agents add` command. A project holding only agent definitions has no other
// route to its scope's install mode, so without them the mode stays unset.
func TestAgentsAddCopyRecordsTheScopeMode(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	setConfigured(t, cwd, "claude-code")
	assertRecordedMode(t, cwd, "")

	runAgentsAdd(t, agentSourceDir(t), "--copy")

	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	target := agentHarnessTarget("claude-code", cwd)
	if isSymlink(t, target) {
		t.Errorf("%s is a symlink after an install in copy mode", target)
	}
}

// The install mode is a property of the scope, not of an asset type, so
// setting it through `agents add` converts the scope's skills as well, in both
// directions. Mutation this test catches: giving agent definitions a mode of
// their own, which would leave the skill on its original shape.
func TestAgentsAddModeFlagsConvertTheWholeScope(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	skillCanonical, skillLink := linkSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")
	agentLink := agentHarnessTarget("claude-code", cwd)
	symlinkOrSkip(t, writeAgentFile(t, harness.CanonicalAgentsDir(false, cwd), "critic"), agentLink)
	lockAgent(t, cwd, "critic")
	setConfigured(t, cwd, "claude-code")
	src := agentSourceDir(t)

	runAgentsAdd(t, src, "--copy")
	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	assertRealSkill(t, skillLink)
	if isSymlink(t, agentLink) {
		t.Errorf("%s is still a symlink after the scope switched to copy mode", agentLink)
	}

	runAgentsAdd(t, src, "--symlink")
	assertRecordedMode(t, cwd, lock.InstallModeSymlink)
	assertLinkTo(t, skillLink, skillCanonical)
	assertLinkTo(t, agentLink, filepath.Join(harness.CanonicalAgentsDir(false, cwd), "critic.md"))
}

// ── The mode flags on `mdm agents install` ─────────────────────────────────

// A project holding only agent definitions restores them with `agents
// install`, so that command has to be able to change the scope's mode too.
// Mutation this test catches: dropping the flags from `agents install`, which
// fails with `unknown flag: --copy`. The recorded mode is the load-bearing
// assertion; the file kind alone cannot tell a conversion from a fresh copy.
func TestAgentsInstallCopyRecordsTheScopeMode(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	setConfigured(t, cwd, "claude-code")
	seedAgentsOnlyLock(t, cwd, agentSourceDir(t))
	assertRecordedMode(t, cwd, "")

	runAgentsInstall(t, "--copy")

	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	target := agentHarnessTarget("claude-code", cwd)
	if isSymlink(t, target) {
		t.Errorf("%s is still a symlink after --copy", target)
	}
}

// The restore path must not become a way to convert agent definitions alone.
// Mutation this test catches: scoping the conversion to agent paths, which
// leaves the scope's skill on whichever shape it was installed with while the
// lock claims the whole scope is in copy mode.
func TestAgentsInstallCopyConvertsTheScopesSkills(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	_, skillLink := linkSkill(t, cwd, "s1")
	lockSkill(t, cwd, "s1")
	setConfigured(t, cwd, "claude-code")
	seedLockedAgent(t, cwd, agentSourceDir(t))

	runAgentsInstall(t, "--copy")

	assertRecordedMode(t, cwd, lock.InstallModeCopy)
	assertRealSkill(t, skillLink)
	if isSymlink(t, agentHarnessTarget("claude-code", cwd)) {
		t.Error("the agent definition is still a symlink after --copy")
	}
}
