package registry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/version"
)

type WellKnownSkillEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

type WellKnownIndex struct {
	Skills []WellKnownSkillEntry `json:"skills"`
}

type WellKnownSkill struct {
	Name        string
	Description string
	Content     string // SKILL.md content
	InstallName string
	SourceURL   string
	Files       map[string]string // path -> content
	IndexEntry  WellKnownSkillEntry
}

var wellKnownPaths = []string{".well-known/agent-skills", ".well-known/skills"}

func HttpGetText(ctx context.Context, rawURL string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", version.AppName+"-cli/"+version.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return string(body), resp.StatusCode, err
}

func FetchWellKnownIndex(baseURL string) (*WellKnownIndex, string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, "", "", err
	}
	basePath := strings.TrimSuffix(u.Path, "/")

	type urlEntry struct {
		indexURL      string
		resolvedBase  string
		wellKnownPath string
	}

	var urlsToTry []urlEntry
	for _, wkPath := range wellKnownPaths {
		// Path-relative
		urlsToTry = append(urlsToTry, urlEntry{
			indexURL:      u.Scheme + "://" + u.Host + basePath + "/" + wkPath + "/index.json",
			resolvedBase:  u.Scheme + "://" + u.Host + basePath,
			wellKnownPath: wkPath,
		})
		// Root if there's a sub-path
		if basePath != "" {
			urlsToTry = append(urlsToTry, urlEntry{
				indexURL:      u.Scheme + "://" + u.Host + "/" + wkPath + "/index.json",
				resolvedBase:  u.Scheme + "://" + u.Host,
				wellKnownPath: wkPath,
			})
		}
	}

	for _, entry := range urlsToTry {
		body, status, err := HttpGetText(ctx, entry.indexURL)
		if err != nil || status != 200 {
			continue
		}
		var idx WellKnownIndex
		if err := json.Unmarshal([]byte(body), &idx); err != nil {
			continue
		}
		if len(idx.Skills) == 0 {
			continue
		}
		valid := true
		for _, s := range idx.Skills {
			if !IsValidWellKnownEntry(s) {
				valid = false
				break
			}
		}
		if valid {
			return &idx, entry.resolvedBase, entry.wellKnownPath, nil
		}
	}
	return nil, "", "", nil
}

func IsValidWellKnownEntry(e WellKnownSkillEntry) bool {
	if e.Name == "" || e.Description == "" || len(e.Files) == 0 {
		return false
	}
	hasSkillMdFile := false
	for _, f := range e.Files {
		if strings.EqualFold(f, "SKILL.md") {
			hasSkillMdFile = true
		}
		if strings.HasPrefix(f, "/") || strings.HasPrefix(f, "\\") || strings.Contains(f, "..") {
			return false
		}
	}
	return hasSkillMdFile
}

func FetchWellKnownSkillByEntry(resolvedBase string, entry WellKnownSkillEntry, wellKnownPath string) (*WellKnownSkill, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	skillBaseURL := strings.TrimSuffix(resolvedBase, "/") + "/" + wellKnownPath + "/" + entry.Name
	skillMdURL := skillBaseURL + "/SKILL.md"

	body, status, err := HttpGetText(ctx, skillMdURL)
	if err != nil || status != 200 {
		return nil, nil
	}

	data, _ := skill.ParseFrontmatter(body)
	nameVal, _ := data["name"].(string)
	descVal, _ := data["description"].(string)
	if nameVal == "" || descVal == "" {
		return nil, nil
	}

	files := map[string]string{"SKILL.md": body}

	// Fetch other files
	for _, filePath := range entry.Files {
		if strings.EqualFold(filePath, "SKILL.md") {
			continue
		}
		fileURL := skillBaseURL + "/" + filePath
		fileBody, fileStatus, fileErr := HttpGetText(ctx, fileURL)
		if fileErr == nil && fileStatus == 200 {
			files[filePath] = fileBody
		}
	}

	return &WellKnownSkill{
		Name:        nameVal,
		Description: descVal,
		Content:     body,
		InstallName: entry.Name,
		SourceURL:   skillMdURL,
		Files:       files,
		IndexEntry:  entry,
	}, nil
}

func FetchAllWellKnownSkills(baseURL string) ([]*WellKnownSkill, error) {
	idx, resolvedBase, wkPath, err := FetchWellKnownIndex(baseURL)
	if err != nil || idx == nil {
		return nil, err
	}

	var skills []*WellKnownSkill
	for _, entry := range idx.Skills {
		sk, err := FetchWellKnownSkillByEntry(resolvedBase, entry, wkPath)
		if err == nil && sk != nil {
			skills = append(skills, sk)
		}
	}
	return skills, nil
}

func GetWellKnownSourceIdentifier(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	hostname := u.Hostname()
	hostname = strings.TrimPrefix(hostname, "www.")
	return hostname
}
