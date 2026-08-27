package tests_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/version"
)

var mdmBin string

func TestMain(m *testing.M) {
	// Build the mdm binary into a temp directory
	tmpDir, err := os.MkdirTemp("", "mdm-test-")
	if err != nil {
		panic("failed to create temp dir: " + err.Error())
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	mdmBin = filepath.Join(tmpDir, "mdm")
	if runtime.GOOS == "windows" {
		mdmBin += ".exe"
	}

	srcDir := filepath.Join(filepath.Dir(tmpDir), "..")
	// Use the module root (where go.mod lives)
	modRoot, err := findModRoot()
	if err != nil {
		panic("could not find module root: " + err.Error())
	}

	cmd := exec.Command("go", "build", "-o", mdmBin, ".")
	cmd.Dir = modRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic("failed to build mdm: " + string(out))
	}
	_ = srcDir

	os.Exit(m.Run())
}

// findModRoot walks up from the tests/ directory to find the go.mod file.
func findModRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}

func runMdm(t *testing.T, args ...string) (stdout string, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(mdmBin, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return stdout, stderr, exitCode
}

func TestVersion(t *testing.T) {
	stdout, _, code := runMdm(t, "--version")
	if code != 0 {
		t.Fatalf("mdm --version exited %d", code)
	}
	if !strings.Contains(stdout, version.Version) {
		t.Errorf("expected version output to contain %q, got: %q", version.Version, stdout)
	}
}

func TestHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "--help")
	if code != 0 {
		t.Fatalf("mdm --help exited %d", code)
	}
	for _, expected := range []string{"skills", "upgrade", "completion"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("expected --help output to contain %q, got: %q", expected, stdout)
		}
	}
}

func TestSkillsHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "skills", "--help")
	if code != 0 {
		t.Fatalf("mdm skills --help exited %d", code)
	}
	for _, expected := range []string{"add", "remove", "list", "find", "update", "init"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("expected skills --help output to contain %q, got: %q", expected, stdout)
		}
	}
}

func TestAddHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "skills", "add", "--help")
	if code != 0 {
		t.Fatalf("mdm skills add --help exited %d", code)
	}
	for _, expected := range []string{"--agent", "--skill"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("expected skills add --help output to contain %q, got: %q", expected, stdout)
		}
	}
}

func TestRemoveHelp(t *testing.T) {
	_, _, code := runMdm(t, "skills", "remove", "--help")
	if code != 0 {
		t.Fatalf("mdm skills remove --help exited %d", code)
	}
}

func TestListHelp(t *testing.T) {
	_, _, code := runMdm(t, "skills", "list", "--help")
	if code != 0 {
		t.Fatalf("mdm skills list --help exited %d", code)
	}
}

func TestAuditHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "skills", "audit", "--help")
	if code != 0 {
		t.Fatalf("mdm skills audit --help exited %d", code)
	}
	for _, expected := range []string{"--global", "--project", "--json"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("expected skills audit --help output to contain %q, got: %q", expected, stdout)
		}
	}
}

func TestDoctorHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "doctor", "--help")
	if code != 0 {
		t.Fatalf("mdm doctor --help exited %d", code)
	}
	for _, expected := range []string{"--global", "--project", "hash mismatch; global installs"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("expected doctor --help output to contain %q, got: %q", expected, stdout)
		}
	}
}

func runMdmInDir(t *testing.T, dir string, env []string, args ...string) (stdout string, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(mdmBin, args...)
	cmd.Dir = dir
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if env != nil {
		cmd.Env = env
	}
	err := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return stdout, stderr, exitCode
}

func TestInstallNoLockFile(t *testing.T) {
	// Run in an isolated temp dir with no lock file and a fresh XDG_STATE_HOME
	// so there is no global lock file either.
	tmpDir := t.TempDir()
	stateDir := t.TempDir()

	// Build a minimal environment (inherit PATH so the binary can run).
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + tmpDir,
		"XDG_STATE_HOME=" + stateDir,
	}

	stdout, stderr, _ := runMdmInDir(t, tmpDir, env, "skills", "install", "-y")
	combined := stdout + stderr

	if !strings.Contains(combined, "No "+lockName+" found") {
		t.Errorf("expected 'No mdm.lock found' in output, got stdout=%q stderr=%q", stdout, stderr)
	}
	if strings.Contains(combined, "Please provide a package source") {
		t.Errorf("unexpected 'Please provide a package source' error in output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestInstallHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "skills", "install", "--help")
	if code != 0 {
		t.Fatalf("mdm skills install --help exited %d", code)
	}
	if !strings.Contains(stdout, "Restore skills from "+lockName) {
		t.Errorf("expected install help to contain description, got: %q", stdout)
	}
}

func TestNormalizeMultiFlags(t *testing.T) {
	// This should NOT produce "unknown flag" or "flag needs an argument" in stderr.
	// Uses a non-existent local path so it fails fast without any network call.
	_, stderr, _ := runMdm(t, "skills", "add", "/nonexistent-mdm-test-path", "-a", "claude", "cursor", "--list")
	if strings.Contains(stderr, "unknown flag") {
		t.Errorf("unexpected 'unknown flag' in stderr: %q", stderr)
	}
	if strings.Contains(stderr, "flag needs an argument") {
		t.Errorf("unexpected 'flag needs an argument' in stderr: %q", stderr)
	}
}

func TestCompletion(t *testing.T) {
	stdout, _, code := runMdm(t, "completion", "bash")
	if code != 0 {
		t.Fatalf("mdm completion bash exited %d", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "#") {
		t.Errorf("expected bash completion output to start with '#', got: %q", stdout[:min(50, len(stdout))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestLocalSkillLockUsesRelativePath(t *testing.T) {
	// Create an isolated project dir and a sibling skill dir.
	parentDir := t.TempDir()
	if realParent, err := filepath.EvalSymlinks(parentDir); err == nil {
		parentDir = realParent
	}
	projectDir := filepath.Join(parentDir, "project")
	skillDir := filepath.Join(parentDir, "skill")
	stateDir := t.TempDir()
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}

	// Write a minimal SKILL.md (with YAML frontmatter) in skillDir so mdm recognises it.
	skillMd := filepath.Join(skillDir, "SKILL.md")
	skillContent := "---\nname: test-local-skill\ndescription: a test skill for unit tests\n---\n\n# Test Local Skill\n"
	if err := os.WriteFile(skillMd, []byte(skillContent), 0644); err != nil {
		t.Fatalf("creating SKILL.md: %v", err)
	}

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + projectDir,
		"XDG_STATE_HOME=" + stateDir,
	}

	// Install the local skill into the project scope, non-interactively.
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"skills", "add", skillDir, "--agent", "claude-code", "--project", "-y")
	if code != 0 {
		t.Fatalf("mdm skills add failed (code %d):\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// Read the produced lock file.
	lockPath := filepath.Join(projectDir, lockName)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading mdm.lock: %v", err)
	}
	content := string(data)

	// The stored source must NOT be an absolute path — it should be relative.
	if strings.Contains(content, skillDir) {
		t.Errorf("mdm.lock contains the absolute skill path %q; expected a relative path.\nlock file:\n%s", skillDir, content)
	}
	// It should start with "./" or "../" in the JSON.
	if !strings.Contains(content, `"./`) && !strings.Contains(content, `"../`) {
		t.Errorf("mdm.lock does not contain a relative path (./ or ../).\nlock file:\n%s", content)
	}
}

func TestSkillsAddBlocksHiddenMarkdownCharacters(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	skillDir := hiddenSkillFixturePath(t)

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"skills", "add", skillDir, "--agent", "claude-code", "--project", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected hidden character scan to block install, got code 0:\n%s", combined)
	}
	if !strings.Contains(combined, "Hidden character scan failed") || !strings.Contains(combined, "zero-width") {
		t.Fatalf("expected hidden character finding in output, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".agents", "skills", "hidden-test-skill")); !os.IsNotExist(err) {
		t.Fatalf("expected skill directory not to be created, stat err=%v", err)
	}
}

func TestSkillsAddAllowsHiddenMarkdownCharactersWithFlag(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	skillDir := hiddenSkillFixturePath(t)

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env,
		"skills", "add", skillDir, "--agent", "claude-code", "--project", "-y", "--allow-hidden-chars")
	combined := stdout + stderr
	if code != 0 {
		t.Fatalf("expected install with --allow-hidden-chars to succeed, got code %d:\n%s", code, combined)
	}
	if !strings.Contains(combined, "Continuing because --allow-hidden-chars was provided") {
		t.Fatalf("expected allow warning in output, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".agents", "skills", "hidden-test-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected skill to be installed, stat err=%v", err)
	}
}

func TestSkillsInstallBlocksHiddenMarkdownCharactersFromLock(t *testing.T) {
	projectDir := t.TempDir()
	stateDir := t.TempDir()
	skillDir := hiddenSkillFixturePath(t)
	lockContent := `{
  "version": 1,
  "skills": {
    "hidden-test-skill": {
      "source": "` + filepath.ToSlash(skillDir) + `",
      "sourceType": "local"
    }
  },
  "configuredAgents": ["claude-code"]
}
`
	if err := os.WriteFile(filepath.Join(projectDir, "skills-lock.json"), []byte(lockContent), 0600); err != nil {
		t.Fatalf("creating skills-lock.json: %v", err)
	}

	env := isolatedEnv(projectDir, stateDir)
	stdout, stderr, code := runMdmInDir(t, projectDir, env, "skills", "install", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected hidden character scan to block install, got code 0:\n%s", combined)
	}
	if !strings.Contains(combined, "Hidden character scan failed") || !strings.Contains(combined, "zero-width") {
		t.Fatalf("expected hidden character finding in output, got:\n%s", combined)
	}
}

func isolatedEnv(home, stateDir string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_STATE_HOME=" + stateDir,
	}
}

func hiddenSkillFixturePath(t *testing.T) string {
	t.Helper()
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("finding module root: %v", err)
	}
	return filepath.Join(root, "tests", "testdata", "hidden-skill")
}

// ─── cherry-pick ───────────────────────────────────────────────────────────────

func TestCherryPickHelp(t *testing.T) {
	stdout, _, code := runMdm(t, "skills", "cherry-pick", "--help")
	if code != 0 {
		t.Fatalf("mdm skills cherry-pick --help exited %d", code)
	}
	for _, expected := range []string{"--as", "--dir", "--status", "--force", "ATTRIBUTION.md"} {
		if !strings.Contains(stdout, expected) {
			t.Errorf("expected cherry-pick help to contain %q, got: %q", expected, stdout)
		}
	}
}

// writeUpstreamFixture builds a source repository with one licensed skill and
// returns its root.
func writeUpstreamFixture(t *testing.T, root string) string {
	t.Helper()
	files := map[string]string{
		"LICENSE":                            "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\n",
		"skills/code-review/SKILL.md":        "---\nname: code-review\ndescription: Review a diff for bugs.\n---\n\n# Code review\n",
		"skills/code-review/references/x.md": "reference material\n",
	}
	for rel, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("creating %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatalf("writing %s: %v", rel, err)
		}
	}
	return root
}

// cherryPickFixture is an isolated project directory plus an upstream source to
// fork from, with an environment that touches neither the real HOME nor the
// user's global lock file.
type cherryPickFixture struct {
	project  string
	upstream string
	env      []string
}

func setupCherryPick(t *testing.T) cherryPickFixture {
	t.Helper()
	parentDir := t.TempDir()
	if realParent, err := filepath.EvalSymlinks(parentDir); err == nil {
		parentDir = realParent
	}
	projectDir := filepath.Join(parentDir, "project")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatalf("creating project dir: %v", err)
	}
	return cherryPickFixture{
		project:  projectDir,
		upstream: writeUpstreamFixture(t, filepath.Join(parentDir, "upstream")),
		env: []string{
			"PATH=" + os.Getenv("PATH"),
			"HOME=" + parentDir,
			"XDG_STATE_HOME=" + filepath.Join(parentDir, "state"),
		},
	}
}

func (f cherryPickFixture) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	return runMdmInDir(t, f.project, f.env, args...)
}

// fork runs the standard cherry-pick used by these tests and returns the fork dir.
func (f cherryPickFixture) fork(t *testing.T, extra ...string) string {
	t.Helper()
	args := append([]string{"skills", "cherry-pick", f.upstream, "-s", "code-review", "--as", "our-review", "-y"}, extra...)
	stdout, stderr, code := f.run(t, args...)
	if code != 0 {
		t.Fatalf("cherry-pick exited %d: stdout=%q stderr=%q", code, stdout, stderr)
	}
	return filepath.Join(f.project, "skills", "our-review")
}

type forkOrigin struct {
	Skill    string `json:"skill"`
	Upstream struct {
		Name        string `json:"name"`
		SourceType  string `json:"sourceType"`
		SkillPath   string `json:"skillPath"`
		License     string `json:"license"`
		LicenseFile string `json:"licenseFile"`
	} `json:"upstream"`
	ContentHash string `json:"contentHash"`
}

func readForkOrigin(t *testing.T, forkDir string) forkOrigin {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(forkDir, ".mdm-origin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var origin forkOrigin
	if err := json.Unmarshal(data, &origin); err != nil {
		t.Fatalf("origin file is not valid JSON: %v", err)
	}
	return origin
}

func TestCherryPickVendorsTheSkill(t *testing.T) {
	f := setupCherryPick(t)
	forkDir := f.fork(t)

	for _, rel := range []string{"SKILL.md", "references/x.md", "ATTRIBUTION.md", "LICENSE.upstream", ".mdm-origin.json"} {
		if _, err := os.Stat(filepath.Join(forkDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s in the fork: %v", rel, err)
		}
	}

	skillMd, err := os.ReadFile(filepath.Join(forkDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skillMd), "name: our-review") {
		t.Errorf("expected the fork to carry its new name, got: %q", skillMd)
	}
}

func TestCherryPickRecordsProvenance(t *testing.T) {
	f := setupCherryPick(t)
	origin := readForkOrigin(t, f.fork(t))

	if origin.Skill != "our-review" || origin.Upstream.Name != "code-review" {
		t.Errorf("unexpected origin record: %+v", origin)
	}
	if origin.Upstream.SourceType != "local" || origin.Upstream.SkillPath != "skills/code-review/SKILL.md" {
		t.Errorf("unexpected upstream location: %+v", origin)
	}
	if origin.Upstream.License != "MIT" || origin.Upstream.LicenseFile != "LICENSE.upstream" {
		t.Errorf("expected the repository license to be recorded and copied: %+v", origin)
	}
	if !strings.HasPrefix(origin.ContentHash, "sha256:") {
		t.Errorf("expected a content hash, got %q", origin.ContentHash)
	}
}

// The fork is the project's own file now: nothing may replace it silently.
func TestCherryPickRefusesToOverwriteWithoutForce(t *testing.T) {
	f := setupCherryPick(t)
	forkDir := f.fork(t)
	edited := "---\nname: our-review\ndescription: ours now.\n---\nour own paragraph\n"
	if err := os.WriteFile(filepath.Join(forkDir, "SKILL.md"), []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := f.run(t, "skills", "cherry-pick", f.upstream, "-s", "code-review", "--as", "our-review", "-y")
	if code == 0 {
		t.Errorf("expected a second cherry-pick to fail without --force: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "--force") {
		t.Errorf("expected the refusal to mention --force, got stdout=%q stderr=%q", stdout, stderr)
	}
	got, err := os.ReadFile(filepath.Join(forkDir, "SKILL.md"))
	if err != nil || string(got) != edited {
		t.Errorf("the refused overwrite must leave local edits intact, got %q (%v)", got, err)
	}

	if _, _, code = f.run(t, "skills", "cherry-pick", f.upstream, "-s", "code-review", "--as", "our-review", "-y", "--force"); code != 0 {
		t.Errorf("expected --force to replace the fork, exited %d", code)
	}
}

func TestCherryPickStatusReportsEdits(t *testing.T) {
	f := setupCherryPick(t)
	forkDir := f.fork(t)

	stdout, stderr, code := f.run(t, "skills", "cherry-pick", "--status")
	if code != 0 {
		t.Fatalf("cherry-pick --status exited %d: %q %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "our-review") || !strings.Contains(stdout, "unmodified") {
		t.Errorf("expected status to list the untouched fork, got: %q", stdout)
	}

	skillMd := filepath.Join(forkDir, "SKILL.md")
	data, err := os.ReadFile(skillMd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillMd, append(data, []byte("\nour own paragraph\n")...), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, _ = f.run(t, "skills", "cherry-pick", "--status")
	if !strings.Contains(stdout, "edited since fork") {
		t.Errorf("expected status to notice the edit, got: %q", stdout)
	}
}

func TestCherryPickDryRunWritesNothing(t *testing.T) {
	f := setupCherryPick(t)

	stdout, stderr, code := f.run(t, "skills", "cherry-pick", f.upstream, "-s", "*", "-y", "--dry-run")
	if code != 0 {
		t.Fatalf("dry run exited %d: stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "Dry run") {
		t.Errorf("expected a dry-run notice, got: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(f.project, "skills")); !os.IsNotExist(err) {
		t.Error("a dry run must not create the forks directory")
	}
}
