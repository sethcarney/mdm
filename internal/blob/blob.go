package blob

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/version"
)

type TreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int    `json:"size"`
}

type RepoTree struct {
	SHA    string `json:"sha"`
	Branch string
	Tree   []TreeEntry `json:"tree"`
	// Truncated is set when GitHub could not return the full recursive tree
	// (very large repos). When true, skill discovery via the API is unreliable
	// and callers should fall back to a git clone.
	Truncated bool
}

type SkillSnapshotFile struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

type BlobSkill struct {
	skill.Skill
	Files        []SkillSnapshotFile
	SnapshotHash string
	RepoPath     string
}

type BlobInstallResult struct {
	Skills []*BlobSkill
	Tree   *RepoTree
}

// ErrTreeTruncated is returned when the repo is too large for the GitHub tree
// API to return in full, so the API fast-path can't reliably find every skill.
var ErrTreeTruncated = errors.New("repository tree too large to read via GitHub API (truncated)")

// APIError describes a non-success response from the GitHub API.
type APIError struct {
	Status      int
	RateLimited bool
	Msg         string
}

func (e *APIError) Error() string {
	if e.RateLimited {
		return "GitHub API rate limit exceeded — set GITHUB_TOKEN to raise the limit"
	}
	if e.Status != 0 {
		return fmt.Sprintf("GitHub API request failed (status %d)", e.Status)
	}
	return e.Msg
}

// IsRateLimited reports whether err is (or wraps) a GitHub API rate-limit error.
func IsRateLimited(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.RateLimited
}

func HttpGet(ctx context.Context, url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", version.AppName+"-cli/"+version.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

func FetchRepoTree(ownerRepo string, ref *string, token string) (*RepoTree, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	tryBranch := func(branch string) (*RepoTree, error) {
		url := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", ownerRepo, branch)
		body, status, err := HttpGet(ctx, url, headers)
		if err != nil {
			return nil, &APIError{Msg: err.Error()}
		}
		if status != 200 {
			rl := (status == 403 || status == 429) && bytes.Contains(bytes.ToLower(body), []byte("rate limit"))
			return nil, &APIError{Status: status, RateLimited: rl, Msg: string(body)}
		}
		var result struct {
			SHA       string      `json:"sha"`
			Tree      []TreeEntry `json:"tree"`
			Truncated bool        `json:"truncated"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		return &RepoTree{SHA: result.SHA, Branch: branch, Tree: result.Tree, Truncated: result.Truncated}, nil
	}

	if ref != nil && *ref != "" {
		return tryBranch(*ref)
	}

	// Try main, then master. A rate-limit error won't change between the two,
	// so short-circuit instead of wasting a second request (and the rate limit).
	tree, err := tryBranch("main")
	if err == nil {
		return tree, nil
	}
	if IsRateLimited(err) {
		return nil, err
	}
	return tryBranch("master")
}

func ToSkillSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// Remove non alphanumeric/hyphen
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		}
	}
	s = b.String()
	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
}

func findSkillMdPaths(tree *RepoTree, subpath string) []string {
	var skillPaths []string
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		if !strings.HasSuffix(e.Path, "/SKILL.md") && e.Path != "SKILL.md" {
			continue
		}
		if subpath != "" && !strings.HasPrefix(e.Path, subpath+"/") {
			continue
		}
		skillPaths = append(skillPaths, e.Path)
	}
	return skillPaths
}

// fetchRawFile downloads a single file's contents from raw.githubusercontent.com.
// Raw fetches are served by a CDN and do not count against the GitHub API rate
// limit, so collecting many files this way is cheap.
func fetchRawFile(ctx context.Context, ownerRepo, branch, path, token string) ([]byte, bool) {
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", ownerRepo, branch, path)
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	body, status, err := HttpGet(ctx, rawURL, headers)
	if err != nil || status != 200 {
		return nil, false
	}
	return body, true
}

func fetchSkillMDContent(ctx context.Context, ownerRepo, branch, skillMdPath, token string) ([]byte, bool) {
	return fetchRawFile(ctx, ownerRepo, branch, skillMdPath, token)
}

// skillDirPrefix returns the directory prefix (with trailing slash) that
// contains skillMdPath, or "" when SKILL.md sits at the repo root.
func skillDirPrefix(skillMdPath string) string {
	if idx := strings.LastIndex(skillMdPath, "/"); idx >= 0 {
		return skillMdPath[:idx+1]
	}
	return ""
}

// collectSkillFiles downloads every file under the skill's directory directly
// from GitHub, returning them with paths relative to the skill root (so the
// installer can write them verbatim). For a root-level SKILL.md only SKILL.md
// itself is taken, to avoid pulling the entire repository.
func collectSkillFiles(ctx context.Context, ownerRepo, branch, skillMdPath, token string, tree *RepoTree) []SkillSnapshotFile {
	dir := skillDirPrefix(skillMdPath)
	var files []SkillSnapshotFile
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		rel, ok := skillRelPath(dir, e.Path)
		if !ok {
			continue
		}
		body, fetched := fetchRawFile(ctx, ownerRepo, branch, e.Path, token)
		if !fetched {
			continue
		}
		files = append(files, SkillSnapshotFile{Path: rel, Contents: string(body)})
	}
	return files
}

// skillRelPath decides whether a tree entry belongs to the skill rooted at dir
// (a directory prefix ending in "/", or "" for a repo-root skill) and, if so,
// returns its path relative to the skill root. Root-level skills only claim
// SKILL.md itself so the whole repo isn't pulled in.
func skillRelPath(dir, entryPath string) (string, bool) {
	if dir == "" {
		if entryPath == "SKILL.md" {
			return "SKILL.md", true
		}
		return "", false
	}
	if !strings.HasPrefix(entryPath, dir) {
		return "", false
	}
	return strings.TrimPrefix(entryPath, dir), true
}

func checkBlobSkillFilter(data map[string]interface{}, name, filter string, includeInternal bool) bool {
	if filter != "" && !strings.EqualFold(name, filter) && !strings.EqualFold(ToSkillSlug(name), filter) {
		return false
	}
	isInternal := false
	if metaVal, ok := data["metadata"]; ok {
		if metaMap, ok := metaVal.(map[string]interface{}); ok {
			if b, ok := metaMap["internal"].(bool); ok && b {
				isInternal = true
			}
		}
	}
	return !isInternal || includeInternal
}

// RemoteSkillMeta holds the name and description of a skill discovered from a
// remote SKILL.md without downloading full skill content.
type RemoteSkillMeta struct {
	Name        string
	Description string
}

// FetchRemoteSkillList returns skill metadata from any public GitHub repo
// without downloading full skill content. Unlike TryBlobInstall it has no
// owner allowlist restriction because it performs no write operations.
func FetchRemoteSkillList(ownerRepo, ref, subpath, token string) ([]*RemoteSkillMeta, error) {
	var refPtr *string
	if ref != "" {
		refPtr = &ref
	}
	tree, err := FetchRepoTree(ownerRepo, refPtr, token)
	if err != nil {
		return nil, err
	}
	skillPaths := findSkillMdPaths(tree, subpath)
	if len(skillPaths) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var results []*RemoteSkillMeta
	for _, p := range skillPaths {
		body, ok := fetchSkillMDContent(ctx, ownerRepo, tree.Branch, p, token)
		if !ok {
			continue
		}
		data, _ := skill.ParseFrontmatter(string(body))
		name, _ := data["name"].(string)
		desc, _ := data["description"].(string)
		if name == "" {
			continue
		}
		results = append(results, &RemoteSkillMeta{Name: name, Description: desc})
	}
	return results, nil
}

// InstallOptions configures a GitHub API fast-path install.
type InstallOptions struct {
	Subpath         string
	SkillFilter     string
	Ref             string
	Token           string
	IncludeInternal bool
	// Logf, when non-nil, receives verbose diagnostic messages describing each
	// step of the API fetch (tree, skill discovery, per-file downloads).
	Logf func(format string, a ...interface{})
}

func (o InstallOptions) log(format string, a ...interface{}) {
	if o.Logf != nil {
		o.Logf(format, a...)
	}
}

// TryBlobInstall resolves skills from any public GitHub repo using the GitHub
// API plus raw.githubusercontent.com — no git clone and no GitHub CLI required.
//
// It returns:
//   - (result, nil)            when skills were found,
//   - (nil, nil)               when the repo simply has no installable skills,
//   - (nil, ErrTreeTruncated)  when the repo is too large to read via the API,
//   - (nil, *APIError)         on a GitHub API failure (e.g. rate limit).
//
// In the last two cases the caller should fall back to a full git clone.
func TryBlobInstall(ownerRepo string, opts InstallOptions) (*BlobInstallResult, error) {
	if parts := strings.SplitN(ownerRepo, "/", 2); len(parts) != 2 {
		return nil, nil
	}

	var refPtr *string
	if opts.Ref != "" {
		refPtr = &opts.Ref
	}

	opts.log("GitHub API: fetching repo tree for %s", ownerRepo)
	tree, err := FetchRepoTree(ownerRepo, refPtr, opts.Token)
	if err != nil {
		opts.log("tree fetch failed: %v", err)
		return nil, err
	}
	if tree.Truncated {
		opts.log("tree truncated at %d entries — repo too large for the API path", len(tree.Tree))
		return nil, ErrTreeTruncated
	}

	skillPaths := findSkillMdPaths(tree, opts.Subpath)
	opts.log("scanned tree: %d SKILL.md path(s) on branch %s", len(skillPaths), tree.Branch)
	if len(skillPaths) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var blobSkills []*BlobSkill
	for _, skillMdPath := range skillPaths {
		if bs := buildSkillFromPath(ctx, ownerRepo, tree, skillMdPath, opts); bs != nil {
			blobSkills = append(blobSkills, bs)
		}
	}

	if len(blobSkills) == 0 {
		return nil, nil
	}
	return &BlobInstallResult{Skills: blobSkills, Tree: tree}, nil
}

// buildSkillFromPath parses one SKILL.md and, if it qualifies, downloads the
// full set of files under its directory directly from GitHub.
func buildSkillFromPath(ctx context.Context, ownerRepo string, tree *RepoTree, skillMdPath string, opts InstallOptions) *BlobSkill {
	branch := tree.Branch
	body, ok := fetchSkillMDContent(ctx, ownerRepo, branch, skillMdPath, opts.Token)
	if !ok {
		opts.log("could not fetch %s", skillMdPath)
		return nil
	}
	data, _ := skill.ParseFrontmatter(string(body))
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	version, _ := data["version"].(string)
	if name == "" || desc == "" {
		return nil
	}
	if !checkBlobSkillFilter(data, name, opts.SkillFilter, opts.IncludeInternal) {
		return nil
	}
	files := collectSkillFiles(ctx, ownerRepo, branch, skillMdPath, opts.Token, tree)
	if len(files) == 0 {
		files = []SkillSnapshotFile{{Path: "SKILL.md", Contents: string(body)}}
	}
	opts.log("resolved skill %q (%d file(s)) from %s", name, len(files), skillMdPath)
	return &BlobSkill{Skill: skill.Skill{Name: name, Description: desc, Version: version}, Files: files, RepoPath: skillMdPath}
}
