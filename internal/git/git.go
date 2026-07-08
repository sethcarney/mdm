package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const cloneTimeout = 120 * time.Second

type GitCloneError struct {
	Message   string
	URL       string
	IsTimeout bool
	IsAuth    bool
}

func (e *GitCloneError) Error() string {
	return e.Message
}

// gitBaseEnv returns the environment used for every git subprocess mdm spawns.
//
// GIT_ALLOW_PROTOCOL restricts git to the https and ssh transports. This is the
// authoritative defense against git's local-command "remote helper" transports
// (ext::, fd::), which turn a repository URL into arbitrary command execution —
// e.g. `git clone 'ext::sh -c "…"'`. Because a skills-lock.json source string is
// replayed verbatim by `mdm skills install`/`update`, an unrestricted git could
// be coerced into running code from a checked-in lock file. The allowlist is
// inherited by submodule and recursive operations, so it holds even for git URLs
// that were never seen by mdm's own parser. See docs/security/git-transport-restrictions.md.
func gitBaseEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_LFS_SKIP_SMUDGE=1",
		"GIT_ALLOW_PROTOCOL=https:ssh",
	)
}

// isAllowedTransport reports whether gitURL uses a transport mdm permits (https
// or ssh). It rejects git's local-command transports (ext::, fd::), any explicit
// scheme other than https/ssh (e.g. git://, http://, file://), and URLs that
// begin with '-' (which git may misinterpret as an option flag). scp-like syntax
// (git@host:path) and other schemeless forms are accepted here and further
// constrained by GIT_ALLOW_PROTOCOL at exec time.
func isAllowedTransport(gitURL string) bool {
	trimmed := strings.TrimSpace(gitURL)
	if trimmed == "" || strings.HasPrefix(trimmed, "-") {
		return false
	}
	if idx := strings.Index(trimmed, "://"); idx > 0 {
		scheme := strings.ToLower(trimmed[:idx])
		return scheme == "https" || scheme == "ssh"
	}
	// No explicit scheme: reject the local-command helper transports outright.
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "ext::") || strings.HasPrefix(lower, "fd::") {
		return false
	}
	return true
}

// errBlockedTransport builds the typed error returned when a git URL is refused
// before it ever reaches the git binary.
func errBlockedTransport(gitURL string) error {
	return &GitCloneError{
		Message: fmt.Sprintf("Refusing to use %q: only https and ssh git transports are allowed.\n  mdm blocks git's ext::/fd:: and other local-command transports because they execute arbitrary commands.\n  See https://github.com/sethcarney/mdm/blob/main/docs/security/git-transport-restrictions.md", gitURL),
		URL:     gitURL,
	}
}

// CloneOptions tunes the behaviour of CloneRepoWithOptions.
type CloneOptions struct {
	// Verbose streams git's live progress (clone counters, transfer speed)
	// to Progress instead of silently buffering it. This is what powers the
	// `mdm skills add --verbose` flag so users can see large clones advancing
	// rather than staring at a frozen spinner.
	Verbose bool
	// Progress is where live git output is written when Verbose is true.
	// Defaults to os.Stderr when nil.
	Progress io.Writer
}

// CloneRepo performs a shallow clone with no live progress output, buffering
// git's output for error reporting only. Kept for callers that don't need
// streaming.
func CloneRepo(gitURL, ref string) (string, error) {
	return CloneRepoWithOptions(gitURL, ref, CloneOptions{})
}

// CloneRepoWithOptions shallow-clones gitURL (optionally at ref) into a fresh
// temp dir. When opts.Verbose is set, git's progress meter is streamed live to
// opts.Progress (default os.Stderr) while still being captured for diagnostics.
func CloneRepoWithOptions(gitURL, ref string, opts CloneOptions) (string, error) {
	if !isAllowedTransport(gitURL) {
		return "", errBlockedTransport(gitURL)
	}

	tmpDir, err := os.MkdirTemp("", "skills-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	args := []string{"clone", "--depth", "1"}
	if opts.Verbose {
		// --progress forces git to emit its counters even though stderr is not
		// a TTY (it's wired through an io.MultiWriter below).
		args = append(args, "--progress")
	}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	// "--" terminates option parsing so a URL beginning with "-" can't be
	// misread by git as a flag.
	args = append(args, "--", gitURL, tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitBaseEnv()

	out, runErr := runClone(cmd, opts)
	if runErr != nil {
		_ = os.RemoveAll(tmpDir)
		msg := out
		if runErr.Error() != "" {
			msg = runErr.Error() + "\n" + msg
		}
		// A context deadline produces a generic "signal: killed" error whose
		// text doesn't mention a timeout, so classify it explicitly here.
		if ctx.Err() == context.DeadlineExceeded {
			msg += "\ntimed out after " + cloneTimeout.String()
		}
		return "", classifyCloneError(msg, gitURL)
	}

	return tmpDir, nil
}

// runClone executes the clone, returning git's combined output as a string.
// In verbose mode the output is teed live to the configured writer.
func runClone(cmd *exec.Cmd, opts CloneOptions) (string, error) {
	if !opts.Verbose {
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	w := opts.Progress
	if w == nil {
		w = os.Stderr
	}
	var buf bytes.Buffer
	mw := io.MultiWriter(&buf, w)
	cmd.Stdout = mw
	cmd.Stderr = mw
	err := cmd.Run()
	return buf.String(), err
}

// classifyCloneError maps git's output to a typed GitCloneError so callers can
// distinguish timeouts and auth failures from generic clone errors.
func classifyCloneError(msg, gitURL string) error {
	isTimeout := strings.Contains(msg, "timed out") || strings.Contains(msg, "block timeout")
	isAuth := strings.Contains(msg, "Authentication failed") ||
		strings.Contains(msg, "could not read Username") ||
		strings.Contains(msg, "Permission denied") ||
		strings.Contains(msg, "Repository not found")

	if isTimeout {
		return &GitCloneError{
			Message: "Clone timed out. The repository may be very large, or your connection is slow.\n  - Re-run with --verbose to watch git's progress\n  - For private repos, ensure your SSH keys or credentials are configured (ssh-add -l)",
			URL:     gitURL, IsTimeout: true,
		}
	}
	if isAuth {
		return &GitCloneError{
			Message: fmt.Sprintf("Authentication failed for %s.\n  - For private repos, ensure you have access\n  - For SSH: Check your keys with 'ssh -T git@github.com'\n  - For HTTPS: Check your git credentials with 'git config --global credential.helper'", gitURL),
			URL:     gitURL, IsAuth: true,
		}
	}
	return &GitCloneError{
		Message: fmt.Sprintf("Failed to clone %s: %s", gitURL, strings.TrimSpace(msg)),
		URL:     gitURL,
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed in %s: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// DefaultBranch returns the default branch name of a remote git repository by running
// "git ls-remote --symref <url> HEAD" and parsing the symbolic ref line.
// Falls back to "main" if the default branch cannot be determined.
func DefaultBranch(gitURL string) string {
	if !isAllowedTransport(gitURL) {
		return "main"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--symref", "--", gitURL, "HEAD")
	cmd.Env = gitBaseEnv()
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	return parseSymrefBranch(string(out))
}

// parseSymrefBranch extracts the default branch name from the output of
// "git ls-remote --symref <url> HEAD". The relevant line looks like
// "ref: refs/heads/<branch>\tHEAD". Returns "main" when no branch can be
// parsed, including from malformed or empty output.
func parseSymrefBranch(out string) string {
	const prefix = "ref: refs/heads/"
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		// The branch name is the text between the prefix and the first
		// whitespace (the line is "ref: refs/heads/<branch>\tHEAD"). Cut at
		// the first tab/space rather than using strings.Fields, so a
		// malformed line with an empty branch name (e.g. "ref: refs/heads/"
		// or "ref: refs/heads/\tHEAD") yields "" and is skipped instead of
		// panicking or mis-parsing the trailing "HEAD" as the branch.
		branch := strings.TrimPrefix(line, prefix)
		if i := strings.IndexAny(branch, " \t"); i >= 0 {
			branch = branch[:i]
		}
		if branch != "" {
			return branch
		}
	}
	return "main"
}

// FetchRemoteCommitSHA fetches the commit SHA for the given ref on a remote git URL using
// "git ls-remote", which works for any git host (GitHub, GitLab, Bitbucket, self-hosted)
// without performing a full clone.  If ref is empty it resolves HEAD.
func FetchRemoteCommitSHA(gitURL, ref string) (string, error) {
	if !isAllowedTransport(gitURL) {
		return "", errBlockedTransport(gitURL)
	}
	// "--" terminates option parsing before the repository/ref positionals.
	args := []string{"ls-remote", "--", gitURL}
	if ref != "" {
		// Check branch and tag refs; include the bare ref in case it is a full refspec.
		args = append(args, "refs/heads/"+ref, "refs/tags/"+ref, ref)
	} else {
		args = append(args, "HEAD")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	cmd := exec.CommandContext(ctx2, "git", args...)
	cmd.Env = gitBaseEnv()
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

// FetchRemoteTags returns all tag names from a remote git repository without
// performing a full clone. Annotated tag dereference lines ("^{}") are skipped
// so each tag name appears exactly once.
func FetchRemoteTags(gitURL string) ([]string, error) {
	if !isAllowedTransport(gitURL) {
		return nil, errBlockedTransport(gitURL)
	}
	cmd := exec.Command("git", "ls-remote", "--tags", "--", gitURL)
	cmd.Env = gitBaseEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote --tags failed for %s: %w", gitURL, err)
	}
	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref := parts[1]
		if !strings.HasPrefix(ref, "refs/tags/") {
			continue
		}
		tag := strings.TrimPrefix(ref, "refs/tags/")
		if strings.HasSuffix(tag, "^{}") {
			continue
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

// ── Semver utilities ───────────────────────────────────────────────────────────

type semverParts struct {
	major, minor, patch int
	pre                 string // pre-release suffix, empty for release tags
}

func parseSemver(s string) (semverParts, bool) {
	s = strings.TrimPrefix(s, "v")
	pre := ""
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		pre = s[idx+1:]
		s = s[:idx]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semverParts{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return semverParts{}, false
		}
		nums[i] = n
	}
	return semverParts{nums[0], nums[1], nums[2], pre}, true
}

func cmpSemver(a, b semverParts) int {
	for _, pair := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	// release (empty pre) sorts higher than pre-release
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	default:
		if a.pre < b.pre {
			return -1
		}
		return 1
	}
}

// IsSemverTag reports whether s is a valid semver tag (e.g. "v1.2.3").
func IsSemverTag(s string) bool {
	_, ok := parseSemver(s)
	return ok
}

// LatestSemverTag returns the highest stable (non-pre-release) semver tag from
// the provided list, or "" if none qualify.
func LatestSemverTag(tags []string) string {
	var best *semverParts
	bestStr := ""
	for _, tag := range tags {
		sv, ok := parseSemver(tag)
		if !ok || sv.pre != "" {
			continue
		}
		if best == nil || cmpSemver(sv, *best) > 0 {
			best = &sv
			bestStr = tag
		}
	}
	return bestStr
}

// CompareSemverTags compares two semver tag strings and returns -1, 0, or 1
// (a < b, a == b, a > b). Returns 0 if either string is not a valid semver tag.
func CompareSemverTags(a, b string) int {
	av, aok := parseSemver(a)
	bv, bok := parseSemver(b)
	if !aok || !bok {
		return 0
	}
	return cmpSemver(av, bv)
}
