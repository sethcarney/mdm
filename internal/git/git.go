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

// GetLocalCommitSHA returns the HEAD commit SHA of an already-cloned repository directory.
func GetLocalCommitSHA(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed in %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// FetchRemoteCommitSHA fetches the commit SHA for the given ref on a remote git URL using
// "git ls-remote", which works for any git host (GitHub, GitLab, Bitbucket, self-hosted)
// without performing a full clone.  If ref is empty it resolves HEAD.
func FetchRemoteCommitSHA(gitURL, ref string) (string, error) {
	args := []string{"ls-remote", gitURL}
	if ref != "" {
		// Check branch and tag refs; include the bare ref in case it is a full refspec.
		args = append(args, "refs/heads/"+ref, "refs/tags/"+ref, ref)
	} else {
		args = append(args, "HEAD")
	}

	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_LFS_SKIP_SMUDGE=1",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote failed for %s: %w", gitURL, err)
	}

	// Output format: "<SHA>\t<refname>\n" …  Return the SHA of the first matching line.
	// Accept both SHA-1 (40 hex chars) and SHA-256 (64 hex chars) hashes.
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 1 && (len(parts[0]) == 40 || len(parts[0]) == 64) {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("no matching ref found in git ls-remote output for %s ref=%s", gitURL, ref)
}
