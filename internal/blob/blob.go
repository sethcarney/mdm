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

func GetSkillFolderHashFromTree(tree *RepoTree, skillPath string) string {
	// skillPath is like "skills/react-best-practices/SKILL.md"
	// We want the SHA of the folder (tree entry for the parent dir)
	folder := skillPath
	if strings.HasSuffix(folder, "/SKILL.md") {
		folder = folder[:len(folder)-9]
	} else if strings.HasSuffix(folder, "SKILL.md") {
		folder = folder[:len(folder)-8]
	}
	folder = strings.TrimSuffix(folder, "/")

	for _, e := range tree.Tree {
		if e.Type == "tree" && e.Path == folder {
			return e.SHA
		}
	}
	// Fallback: try the SKILL.md blob SHA
	for _, e := range tree.Tree {
		if e.Type == "blob" && e.Path == skillPath {
			return e.SHA
		}
	}
	return ""
}

func FetchSkillFolderHash(ownerRepo, skillPath, token string, ref *string) (string, error) {
	tree, err := FetchRepoTree(ownerRepo, ref, token)
	if err != nil {
		return "", err
	}
	hash := GetSkillFolderHashFromTree(tree, skillPath)
	return hash, nil
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
	owner := strings.ToLower(parts[0])
	if !blobAllowedOwners[owner] {
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

	// Find SKILL.md files in tree
	var skillPaths []string
	for _, e := range tree.Tree {
		if e.Type != "blob" {
			continue
		}
		if !strings.HasSuffix(e.Path, "/SKILL.md") && e.Path != "SKILL.md" {
			continue
		}
		if opts.Subpath != "" && !strings.HasPrefix(e.Path, opts.Subpath+"/") {
			continue
		}
		skillPaths = append(skillPaths, e.Path)
	}

	if len(skillPaths) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var blobSkills []*BlobSkill

	for _, skillMdPath := range skillPaths {
		// Fetch SKILL.md content to get name/description
		branch := tree.Branch
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", ownerRepo, branch, skillMdPath)
		headers := map[string]string{}
		if opts.Token != "" {
			headers["Authorization"] = "Bearer " + opts.Token
		}
		body, status, err := HttpGet(ctx, rawURL, headers)
		if err != nil || status != 200 {
			continue
		}
		data, _ := skill.ParseFrontmatter(string(body))
		nameVal, _ := data["name"]
		descVal, _ := data["description"]
		name, _ := nameVal.(string)
		desc, _ := descVal.(string)
		if name == "" || desc == "" {
			continue
		}

		// Apply skill filter
		if opts.SkillFilter != "" {
			if !strings.EqualFold(name, opts.SkillFilter) && !strings.EqualFold(ToSkillSlug(name), opts.SkillFilter) {
				continue
			}
		}

		// Check internal
		isInternal := false
		if metaVal, ok := data["metadata"]; ok {
			if metaMap, ok := metaVal.(map[string]interface{}); ok {
				if b, ok := metaMap["internal"].(bool); ok && b {
					isInternal = true
				}
			}
		}
		if isInternal && !opts.IncludeInternal {
			continue
		}

		// Download full skill via skills.sh API
		slug := ToSkillSlug(name)
		dlURL := fmt.Sprintf("%s/api/download?owner=%s&repo=%s&slug=%s&branch=%s", downloadBaseURL, parts[0], parts[1], slug, branch)
		if opts.Subpath != "" {
			dlURL += "&subpath=" + url.QueryEscape(opts.Subpath)
		}
		dlBody, dlStatus, dlErr := HttpGet(ctx, dlURL, nil)
		if dlErr != nil || dlStatus != 200 {
			// Fall back to constructing files from tree
			var files []SkillSnapshotFile
			files = append(files, SkillSnapshotFile{Path: "SKILL.md", Contents: string(body)})
			blobSkills = append(blobSkills, &BlobSkill{
				Skill:        skill.Skill{Name: name, Description: desc},
				Files:        files,
				SnapshotHash: "",
				RepoPath:     skillMdPath,
			})
			continue
		}

		var dlResp SkillDownloadResponse
		if err := json.Unmarshal(dlBody, &dlResp); err != nil {
			continue
		}

		blobSkills = append(blobSkills, &BlobSkill{
			Skill:        skill.Skill{Name: name, Description: desc},
			Files:        dlResp.Files,
			SnapshotHash: dlResp.Hash,
			RepoPath:     skillMdPath,
		})
	}

	if len(blobSkills) == 0 {
		return nil, nil
	}

	return &BlobInstallResult{Skills: blobSkills, Tree: tree}, nil
}
