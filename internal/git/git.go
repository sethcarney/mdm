package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitCloneError struct {
	Message   string
	URL       string
	IsTimeout bool
	IsAuth    bool
}

func (e *GitCloneError) Error() string {
	return e.Message
}

func CloneRepo(gitURL, ref string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "skills-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, gitURL, tmpDir)

	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_LFS_SKIP_SMUDGE=1",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		msg := string(out)
		if err.Error() != "" {
			msg = err.Error() + "\n" + msg
		}
		isTimeout := strings.Contains(msg, "timed out") || strings.Contains(msg, "block timeout")
		isAuth := strings.Contains(msg, "Authentication failed") ||
			strings.Contains(msg, "could not read Username") ||
			strings.Contains(msg, "Permission denied") ||
			strings.Contains(msg, "Repository not found")

		if isTimeout {
			return "", &GitCloneError{
				Message: fmt.Sprintf("Clone timed out. Ensure you have access and your SSH keys or credentials are configured:\n  - For SSH: ssh-add -l\n  - For HTTPS: git config --global credential.helper"),
				URL:     gitURL, IsTimeout: true,
			}
		}
		if isAuth {
			return "", &GitCloneError{
				Message: fmt.Sprintf("Authentication failed for %s.\n  - For private repos, ensure you have access\n  - For SSH: Check your keys with 'ssh -T git@github.com'\n  - For HTTPS: Check your git credentials with 'git config --global credential.helper'", gitURL),
				URL:     gitURL, IsAuth: true,
			}
		}
		return "", &GitCloneError{
			Message: fmt.Sprintf("Failed to clone %s: %s", gitURL, strings.TrimSpace(msg)),
			URL:     gitURL,
		}
	}

	return tmpDir, nil
}

func CleanupTempDir(dir string) error {
	if dir == "" {
		return nil
	}
	resolveReal := func(p string) string {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return real
		}
		abs, _ := filepath.Abs(p)
		return abs
	}
	absDir := resolveReal(dir)
	absTmp := resolveReal(os.TempDir())
	if absDir != absTmp && !strings.HasPrefix(absDir, absTmp+string(filepath.Separator)) {
		return fmt.Errorf("attempted to clean up directory outside of temp directory")
	}
	return os.RemoveAll(dir)
}
