package tests_test

import (
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
	defer os.RemoveAll(tmpDir)

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
	// Run in an isolated temp dir with no skills-lock.json and a fresh XDG_STATE_HOME
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

	if !strings.Contains(combined, "No skills-lock.json found") {
		t.Errorf("expected 'No skills-lock.json found' in output, got stdout=%q stderr=%q", stdout, stderr)
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
	if !strings.Contains(stdout, "Restore skills from skills-lock.json") {
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
	projectDir := t.TempDir()
	skillDir := t.TempDir()
	stateDir := t.TempDir()

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
	lockPath := filepath.Join(projectDir, "skills-lock.json")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading skills-lock.json: %v", err)
	}
	content := string(data)

	// The stored source must NOT be an absolute path — it should be relative.
	if strings.Contains(content, skillDir) {
		t.Errorf("skills-lock.json contains the absolute skill path %q; expected a relative path.\nlock file:\n%s", skillDir, content)
	}
	// It should start with "./" or "../" in the JSON.
	if !strings.Contains(content, `"./`) && !strings.Contains(content, `"../`) {
		t.Errorf("skills-lock.json does not contain a relative path (./ or ../).\nlock file:\n%s", content)
	}
}
