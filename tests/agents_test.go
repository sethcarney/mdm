package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the built binary rather than the commands package,
// because every defect they cover was reachable only through the real
// command wiring: a scan that is never called, a name that diverges between
// two writers, a summary line and an exit code. A unit test on the helper
// each of those uses passes whether or not the command calls it.

// writeAgentSource lays out a source tree holding one agent definition and
// returns its path.
func writeAgentSource(t *testing.T, name, frontmatterName string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + frontmatterName + "\ndescription: a test agent definition\n---\n\nYou are " + frontmatterName + ".\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func hiddenAgentFixturePath(t *testing.T) string {
	t.Helper()
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("finding module root: %v", err)
	}
	return filepath.Join(root, "tests", "testdata", "hidden-agent")
}

// A definition's frontmatter name is third-party text and often not a legal
// file name. It is sanitized once, because it becomes both the file name and
// the lock key. When the two disagree, `mdm agents remove` drops the lock entry,
// finds no file, and leaves the definition live in the harness. Installing
// "Code Reviewer" and removing it is the sequence that catches that.
func TestAgentsAddSanitizesTheNameOnDiskAndInTheLock(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "code-reviewer", "Code Reviewer")

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents add exited %d:\n%s%s", code, stdout, stderr)
	}

	canonical := filepath.Join(projectDir, ".agents", "agents", "code-reviewer.md")
	harnessFile := filepath.Join(projectDir, ".claude", "agents", "code-reviewer.md")
	for _, p := range []string{canonical, harnessFile} {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("expected %s to exist, stat err=%v", p, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".agents", "agents", "Code Reviewer.md")); err == nil {
		t.Error("the raw frontmatter name was used as a file name")
	}

	lockData, err := os.ReadFile(filepath.Join(projectDir, lockName))
	if err != nil {
		t.Fatalf("reading the lock: %v", err)
	}
	if !strings.Contains(string(lockData), `"code-reviewer"`) {
		t.Fatalf("lock does not record the sanitized name:\n%s", lockData)
	}

	// The name in the lock must find the files on disk.
	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "remove", "code-reviewer", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}
	for _, p := range []string{canonical, harnessFile} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("remove reported success but %s survives (stat err=%v)", p, err)
		}
	}
}

// An agent definition is third-party markdown installed specifically to
// become a persona the model adopts, so it gets the same pre-install scan
// every other install path in mdm runs - and the same escape hatch.
func TestAgentsAddBlocksHiddenMarkdownCharacters(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", hiddenAgentFixturePath(t), "--harness", "claude-code", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected the hidden character scan to block the install, got code 0:\n%s", combined)
	}
	if !strings.Contains(combined, "Hidden character scan failed") || !strings.Contains(combined, "zero-width") {
		t.Fatalf("expected a hidden character finding in the output, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".agents", "agents", "hidden-agent.md")); !os.IsNotExist(err) {
		t.Errorf("expected nothing to be written, stat err=%v", err)
	}
}

func TestAgentsAddAllowsHiddenMarkdownCharactersWithFlag(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", hiddenAgentFixturePath(t), "--harness", "claude-code", "--project", "-y", "--allow-hidden-chars")
	combined := stdout + stderr
	if code != 0 {
		t.Fatalf("expected --allow-hidden-chars to let the install through, got code %d:\n%s", code, combined)
	}
	if !strings.Contains(combined, "Continuing because --allow-hidden-chars was provided") {
		t.Fatalf("expected the allow notice in the output, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".agents", "agents", "hidden-agent.md")); err != nil {
		t.Errorf("expected the definition to be installed, stat err=%v", err)
	}
}

// `mdm agents add .` is one keystroke from the documented `mdm agents add
// ./my-agents`, and .agents/agents is itself a conventional agents directory,
// so discovery finds mdm's own canonical copies. Copying one onto itself
// empties it: copyFile opens the destination O_TRUNC before reading the source.
func TestAgentsAddOnTheProjectItselfLeavesTheDefinitionIntact(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")

	env := isolatedEnv(projectDir, stateDir)
	if _, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup install exited %d: %s", code, stderr)
	}

	canonical := filepath.Join(projectDir, ".agents", "agents", "critic.md")
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("setup produced an empty canonical file")
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", ".", "--harness", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents add . exited %d:\n%s%s", code, stdout, stderr)
	}

	after, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatalf("reading the canonical file back: %v", err)
	}
	if len(after) == 0 {
		t.Fatal("the canonical file was truncated to zero bytes by re-adding the project itself")
	}
	if string(after) != string(before) {
		t.Errorf("the canonical file changed:\nbefore: %q\nafter:  %q", before, after)
	}
	// The harness's own file has to still resolve to real content.
	harnessContent, err := os.ReadFile(filepath.Join(projectDir, ".claude", "agents", "critic.md"))
	if err != nil {
		t.Fatalf("reading the harness file: %v", err)
	}
	if len(harnessContent) == 0 {
		t.Error("the harness file resolves to nothing")
	}
}

// A harness with no agent-definition directory recorded is a skip, not a
// failure - but a run that installed nothing anywhere is still a failed run,
// and "✓ Installed 1 agent definition" with exit 0 tells CI the opposite. The
// example is "universal", mdm's own convention rather than a vendor product,
// so no release can give it a directory and make it stale as Codex's did.
func TestAgentsAddSkipsAHarnessWithNoAgentDirAndFailsWhenNothingLands(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "universal", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected a non-zero exit when nothing was installed, got 0:\n%s", combined)
	}
	if strings.Contains(combined, "Installed 1 agent definition") {
		t.Errorf("a success line was printed for an install that wrote nothing:\n%s", combined)
	}
	if !strings.Contains(combined, "skipped") || !strings.Contains(combined, "no agent-definition directory is recorded") {
		t.Errorf("expected a skip with its reason, got:\n%s", combined)
	}
	if strings.Contains(combined, "failed for") {
		t.Errorf("a skip was reported as a failure:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, lockName)); !os.IsNotExist(err) {
		t.Errorf("a lock file was written for an install that wrote nothing, stat err=%v", err)
	}

	// The same definition aimed at a harness that DOES take one still
	// succeeds, and the summary names only the harness that received it.
	stdout, stderr, code = runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "universal", "--harness", "claude-code", "--project", "-y")
	combined = stdout + stderr
	if code != 0 {
		t.Fatalf("mixed harness install exited %d:\n%s", code, combined)
	}
	if !strings.Contains(combined, "Installed 1 agent definition") {
		t.Errorf("expected a success line, got:\n%s", combined)
	}
	if strings.Contains(combined, "Harnesses: Universal") || strings.Contains(combined, ", Universal") {
		t.Errorf("the summary names a harness that installed nothing:\n%s", combined)
	}
}

// The end of an install is the part a user reads. Codex and Copilot always
// receive real files by design, and the run has to say so without claiming a
// symlink failed or pointing at --copy, which cannot change a decision the
// harness itself forces. This drives the real binary because the wiring from
// the install result to the printed line is where the wrong message came from.
func TestAgentsAddExplainsMaterializationWithoutClaimingASymlinkFailure(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "codex", "--harness", "github-copilot", "--project", "-y")
	combined := stdout + stderr
	if code != 0 {
		t.Fatalf("mdm agents add exited %d:\n%s", code, combined)
	}
	for _, unwanted := range []string{"Could not create symlinks", "--copy", "symlinks failed", "those skills were copied"} {
		if strings.Contains(combined, unwanted) {
			t.Errorf("output says %q for an install where no symlink was attempted:\n%s", unwanted, combined)
		}
	}
	for _, want := range []string{
		"received a real file rather than a symlink",
		"GitHub Copilot: its agents directory is committed to the repository.",
		"Codex: it reads TOML.",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("output missing %q:\n%s", want, combined)
		}
	}

	// The note has to describe what actually landed.
	for _, p := range []string{
		filepath.Join(projectDir, ".codex", "agents", "critic.toml"),
		filepath.Join(projectDir, ".github", "agents", "critic.agent.md"),
	} {
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("nothing at %s: %v", p, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is a symlink; the note says it is a real file", p)
		}
	}
}

// A definition mdm cannot convert must cost nothing on disk. Encoding after
// the canonical write left raw TOML at .agents/agents/critic.md that no lock
// entry named, in a directory `mdm agents add .` scans, so list, remove and
// doctor could not see it and the next add would rediscover it.
func TestAgentsAddLeavesNothingBehindWhenTheDefinitionCannotBeConverted(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	root := t.TempDir()
	dir := filepath.Join(root, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// standup is a TOML local time. Codex reads it as written; nothing can
	// re-encode it, because the value's "no UTC offset" meaning is not portable.
	body := "name = \"critic\"\n" +
		"description = \"a test agent definition\"\n" +
		"standup = 09:30:00\n"
	if err := os.WriteFile(filepath.Join(dir, "critic.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", root, "--harness", "claude-code", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected a non-zero exit when nothing was installed, got 0:\n%s", combined)
	}
	if !strings.Contains(combined, "failed for") {
		t.Errorf("expected the definition to be reported as failed, got:\n%s", combined)
	}

	var left []string
	err := filepath.WalkDir(projectDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			left = append(left, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(left) > 0 {
		t.Errorf("a failed install left files behind, none of which any lock entry names: %v", left)
	}
}

// Mutation this test catches: dropping the --harness validation from
// runAgentRemove. An unrecognized harness name is not a no-op - it reports
// "removed from the given harness(es)" about a harness that does not exist,
// leaves the definition installed, and exits 0. `mdm agents add` rejects the
// same typo, so removal accepting it is the inconsistency that hides it.
func TestAgentsRemoveRejectsAnUnknownHarness(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSource(t, "critic", "critic")

	env := isolatedEnv(projectDir, stateDir)
	if _, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup: agents add exited %d: %s", code, stderr)
	}
	harnessFile := filepath.Join(projectDir, ".claude", "agents", "critic.md")

	// "copilot" is not a harness name; the real one is "github-copilot".
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "remove", "critic", "--harness", "copilot", "--project", "-y")
	combined := stdout + stderr

	if code == 0 {
		t.Errorf("exit code = 0 for an unrecognized harness; a script cannot tell this removal did nothing:\n%s", combined)
	}
	if !strings.Contains(combined, "copilot") {
		t.Errorf("output does not name the harness it rejected:\n%s", combined)
	}
	if strings.Contains(combined, "removed from the given harness") {
		t.Errorf("reported a removal from a harness that does not exist:\n%s", combined)
	}
	if _, err := os.Lstat(harnessFile); err != nil {
		t.Errorf("the definition was disturbed by a failed removal (stat err=%v)", err)
	}
}

// `mdm agents add .` adopts a hand-written .claude/agents/critic.md: it copies
// it to the canonical file and turns the original into a symlink. `mdm agents
// remove critic` then deleted both, taking the user's only copy with it. The
// skills side refuses to delete inside a local source; this is the same guard,
// plus turning the adopted link back into the real file it replaced.
func TestAgentsRemoveKeepsAHandWrittenDefinitionAdoptedFromTheProject(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	own := filepath.Join(projectDir, ".claude", "agents", "critic.md")
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "---\nname: critic\ndescription: mine\n---\n\nBe critical.\n"
	if err := os.WriteFile(own, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	env := isolatedEnv(projectDir, stateDir)

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", ".", "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("mdm agents add . exited %d:\n%s%s", code, stdout, stderr)
	}
	if fi, err := os.Lstat(own); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Skipf("setup: expected add . to adopt the file as a symlink (err=%v); nothing to un-adopt on this platform", err)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "remove", "critic", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}

	fi, err := os.Lstat(own)
	if err != nil {
		t.Fatalf("the user's own definition is gone after remove: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("the user's definition is still a symlink after remove; it should be a real file again")
	}
	got, err := os.ReadFile(own)
	if err != nil {
		t.Fatalf("reading the user's definition back: %v", err)
	}
	if string(got) != original {
		t.Errorf("the user's definition changed:\nwant: %q\ngot:  %q", original, got)
	}
	if !strings.Contains(stdout+stderr, "kept") {
		t.Errorf("expected the output to say what was kept, got:\n%s%s", stdout, stderr)
	}
	lockData, _ := os.ReadFile(filepath.Join(projectDir, lockName))
	if strings.Contains(string(lockData), `"critic"`) {
		t.Errorf("lock file still records critic:\n%s", lockData)
	}
}

// writeAgentSourceWith lays out a source tree holding one definition per name
// and returns its path.
func writeAgentSourceWith(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		body := "---\nname: " + n + "\ndescription: a test agent definition\n---\n\nYou are " + n + ".\n"
		if err := os.WriteFile(filepath.Join(dir, n+".md"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The help and the docs both say --agent takes space-separated values. The
// normalizer that makes that work listed --harness, --skill and -s only, so
// `--agent alpha beta` handed beta to cobra as a second positional argument
// and the command failed with "accepts 1 arg(s), received 2".
func TestAgentsAddAcceptsSpaceSeparatedAgentNames(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSourceWith(t, "alpha", "beta", "gamma")
	env := isolatedEnv(projectDir, stateDir)

	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--agent", "alpha", "beta", "--harness", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents add --agent alpha beta exited %d:\n%s%s", code, stdout, stderr)
	}
	for _, name := range []string{"alpha", "beta"} {
		if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", name+".md")); err != nil {
			t.Errorf("%s was not installed: %v", name, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "gamma.md")); !os.IsNotExist(err) {
		t.Errorf("gamma was installed although only alpha and beta were named")
	}
}

// validateNamedHarnesses fails only when no name is valid. With a mixed list
// it warns about the bad name and returns the good ones. Remove used to
// discard that result and carry on with the original slice, which happened to
// work because an unknown name resolves to no install path; the removal is
// now scoped to the validated list explicitly, and this pins the behavior
// either way: the warning, the scoped deletion, and the untouched harness.
func TestAgentsRemoveWithAMixedHarnessListRemovesFromTheValidOnes(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	src := writeAgentSourceWith(t, "critic")
	env := isolatedEnv(projectDir, stateDir)

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", src, "--harness", "claude-code", "cursor", "--project", "-y"); code != 0 {
		t.Fatalf("setup install exited %d:\n%s%s", code, stdout, stderr)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "remove", "critic", "--harness", "claude-code", "--harness", "nope", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents remove exited %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "Unknown harness: nope") {
		t.Errorf("expected a warning about the unknown harness, got:\n%s%s", stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "critic.md")); !os.IsNotExist(err) {
		t.Errorf("Claude Code's copy should be gone, Lstat err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".cursor", "agents", "critic.md")); err != nil {
		t.Errorf("Cursor's copy must survive a removal scoped to claude-code: %v", err)
	}
}

// A restore calls runAgentAdd once per source group. A group whose source
// holds no definitions any more used to os.Exit(1) from inside runAgentAdd,
// ending the restore before the groups after it ran. Groups restore in sorted
// source order, so the empty source is named to sort first.
func TestAgentsInstallSurvivesASourceWithNoDefinitions(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)
	realSrc := filepath.Join(projectDir, "b-src", "agents")
	if err := os.MkdirAll(realSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realSrc, "critic.md"), []byte("---\nname: critic\ndescription: d\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "a-empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", "./b-src", "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("setup install exited %d:\n%s%s", code, stdout, stderr)
	}
	lockPath := filepath.Join(projectDir, lockName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("lock is not JSON: %v\n%s", err, data)
	}
	agents, _ := doc["agents"].(map[string]any)
	if agents == nil {
		t.Fatalf("lock has no agents section:\n%s", data)
	}
	agents["ghost"] = map[string]any{"source": "./a-empty", "sourceType": "local", "agentPath": "", "format": "markdown"}
	doc["configuredHarnesses"] = []string{"claude-code"}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(projectDir, ".claude", "agents", "critic.md"),
		filepath.Join(projectDir, ".agents", "agents", "critic.md"),
	} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env, "agents", "install", "-y")
	if code != 0 {
		t.Fatalf("mdm agents install exited %d:\n%s%s", code, stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(projectDir, ".claude", "agents", "critic.md")); err != nil {
		t.Errorf("critic was not restored after the empty source; the restore stopped early:\n%s%s", stdout, stderr)
	}
}

// Skills record a local source as the cwd-relative form the user typed, so
// the project lock is portable. Agent entries stored the absolute path. And
// `mdm agents add .` rediscovers every installed definition through the
// harness directories and rewrote each entry's source to the project path,
// after which `agents update` could never find them again.
func TestAgentsAddRecordsARelativeLocalSourceAndDoesNotReclaimItsOwnInstalls(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	env := isolatedEnv(projectDir, stateDir)
	srcDir := filepath.Join(projectDir, "sk", "agents")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "critic.md"), []byte("---\nname: critic\ndescription: d\n---\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", "./sk", "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("mdm agents add exited %d:\n%s%s", code, stdout, stderr)
	}
	lockPath := filepath.Join(projectDir, lockName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"source": "./sk"`) {
		t.Errorf("lock should record the local source as ./sk, got:\n%s", data)
	}
	if strings.Contains(string(data), filepath.ToSlash(projectDir)) || strings.Contains(string(data), projectDir) {
		t.Errorf("lock records an absolute path:\n%s", data)
	}

	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"agents", "add", ".", "--harness", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm agents add . exited %d:\n%s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "already installed") {
		t.Errorf("re-adding the project should say critic is already installed, got:\n%s%s", stdout, stderr)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"source": "./sk"`) {
		t.Errorf("`agents add .` rewrote the recorded source:\n%s", after)
	}
}
