package blob

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
}

type SkillSnapshotFile struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

type SkillDownloadResponse struct {
	Files []SkillSnapshotFile `json:"files"`
	Hash  string              `json:"hash"`
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

const downloadBaseURL = "https://skills.sh"
const fetchTimeout = 10 * time.Second

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
		if err != nil || status != 200 {
			return nil, fmt.Errorf("status %d", status)
		}
		var result struct {
			SHA  string      `json:"sha"`
			Tree []TreeEntry `json:"tree"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		return &RepoTree{SHA: result.SHA, Branch: branch, Tree: result.Tree}, nil
	}

	if ref != nil && *ref != "" {
		return tryBranch(*ref)
	}

	// Try main then master
	if tree, err := tryBranch("main"); err == nil {
		return tree, nil
	}
	if tree, err := tryBranch("master"); err == nil {
		return tree, nil
	}
	return nil, fmt.Errorf("could not fetch repo tree for %s", ownerRepo)
}

// FetchSkillVersion fetches the version field from a SKILL.md file in a GitHub
// repository. skillPath is the path to the SKILL.md file within the repo (e.g.
// "skills/my-skill/SKILL.md" or "SKILL.md"). When ref is empty, "main" and
// "master" are tried in order.
func FetchSkillVersion(ownerRepo, ref, skillPath, token string) (string, error) {
	mdPath := skillPath
	if mdPath == "" {
		mdPath = "SKILL.md"
	}

	tryRef := func(branch string) (string, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		body, ok := fetchSkillMDContent(ctx, ownerRepo, branch, mdPath, token)
		if !ok {
			return "", false
		}
		data, _ := skill.ParseFrontmatter(string(body))
		ver, _ := data["version"].(string)
		return ver, true
	}

	if ref != "" {
		ver, ok := tryRef(ref)
		if !ok {
			return "", fmt.Errorf("could not fetch SKILL.md from %s at ref %s", ownerRepo, ref)
		}
		return ver, nil
	}

	for _, branch := range []string{"main", "master"} {
		if ver, ok := tryRef(branch); ok {
			return ver, nil
		}
	}
	return "", fmt.Errorf("could not fetch SKILL.md from %s", ownerRepo)
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

var blobAllowedOwners = map[string]bool{
	"vercel":      true,
	"vercel-labs": true,
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

func fetchSkillMDContent(ctx context.Context, ownerRepo, branch, skillMdPath, token string) ([]byte, bool) {
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", ownerRepo, branch, skillMdPath)
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

func fetchBlobSkillData(ctx context.Context, owner, repo, slug, branch, subpath, skillMdPath, name, desc, version string, body []byte) *BlobSkill {
	dlURL := fmt.Sprintf("%s/api/download?owner=%s&repo=%s&slug=%s&branch=%s", downloadBaseURL, owner, repo, slug, branch)
	if subpath != "" {
		dlURL += "&subpath=" + url.QueryEscape(subpath)
	}
	dlBody, dlStatus, dlErr := HttpGet(ctx, dlURL, nil)
	if dlErr != nil || dlStatus != 200 {
		files := []SkillSnapshotFile{{Path: "SKILL.md", Contents: string(body)}}
		return &BlobSkill{Skill: skill.Skill{Name: name, Description: desc, Version: version}, Files: files, RepoPath: skillMdPath}
	}
	var dlResp SkillDownloadResponse
	if err := json.Unmarshal(dlBody, &dlResp); err != nil {
		return nil
	}
	return &BlobSkill{Skill: skill.Skill{Name: name, Description: desc, Version: version}, Files: dlResp.Files, SnapshotHash: dlResp.Hash, RepoPath: skillMdPath}
}

func TryBlobInstall(ownerRepo string, opts struct {
	Subpath         string
	SkillFilter     string
	Ref             string
	Token           string
	IncludeInternal bool
}) (*BlobInstallResult, error) {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 {
		return nil, nil
	}
	if !blobAllowedOwners[strings.ToLower(parts[0])] {
		return nil, nil
	}

	var refPtr *string
	if opts.Ref != "" {
		refPtr = &opts.Ref
	}

	tree, err := FetchRepoTree(ownerRepo, refPtr, opts.Token)
	if err != nil {
		return nil, nil
	}

	skillPaths := findSkillMdPaths(tree, opts.Subpath)
	if len(skillPaths) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var blobSkills []*BlobSkill
	for _, skillMdPath := range skillPaths {
		branch := tree.Branch
		body, ok := fetchSkillMDContent(ctx, ownerRepo, branch, skillMdPath, opts.Token)
		if !ok {
			continue
		}
		data, _ := skill.ParseFrontmatter(string(body))
		name, _ := data["name"].(string)
		desc, _ := data["description"].(string)
		version, _ := data["version"].(string)
		if name == "" || desc == "" {
			continue
		}
		if !checkBlobSkillFilter(data, name, opts.SkillFilter, opts.IncludeInternal) {
			continue
		}
		blobSkill := fetchBlobSkillData(ctx, parts[0], parts[1], ToSkillSlug(name), branch, opts.Subpath, skillMdPath, name, desc, version, body)
		if blobSkill == nil {
			continue
		}
		blobSkills = append(blobSkills, blobSkill)
	}

	if len(blobSkills) == 0 {
		return nil, nil
	}
	return &BlobInstallResult{Skills: blobSkills, Tree: tree}, nil
}
