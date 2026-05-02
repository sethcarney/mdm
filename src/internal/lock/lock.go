package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sethcarney/mdm/internal/agent"
)

// ──────────────────────────────────────────────────────────
// Global skill lock (~/.agents/.skill-lock.json)
// ──────────────────────────────────────────────────────────

const globalLockVersion = 3

type SkillLockEntry struct {
	Source          string `json:"source"`
	SourceType      string `json:"sourceType"`
	SourceURL       string `json:"sourceUrl"`
	Ref             string `json:"ref,omitempty"`
	SkillPath       string `json:"skillPath,omitempty"`
	SkillFolderHash string `json:"skillFolderHash"`
	InstalledAt     string `json:"installedAt"`
	UpdatedAt       string `json:"updatedAt"`
	PluginName      string `json:"pluginName,omitempty"`
}

type DismissedPrompts struct {
	FindSkillsPrompt bool `json:"findSkillsPrompt,omitempty"`
}

type SkillLockFile struct {
	Version            int                       `json:"version"`
	Skills             map[string]SkillLockEntry `json:"skills"`
	Dismissed          DismissedPrompts          `json:"dismissed,omitempty"`
	LastSelectedAgents []string                  `json:"lastSelectedAgents,omitempty"`
}

func GetSkillLockPath() string {
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return filepath.Join(xdgState, "skills", ".skill-lock.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, agent.AgentsDir, ".skill-lock.json")
}

func ReadSkillLock() SkillLockFile {
	lockPath := GetSkillLockPath()
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return EmptySkillLock()
	}
	var lock SkillLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return EmptySkillLock()
	}
	if lock.Skills == nil || lock.Version < globalLockVersion {
		return EmptySkillLock()
	}
	return lock
}

func WriteSkillLock(lock SkillLockFile) error {
	lockPath := GetSkillLockPath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(lockPath, data, 0644)
}

func EmptySkillLock() SkillLockFile {
	return SkillLockFile{
		Version: globalLockVersion,
		Skills:  map[string]SkillLockEntry{},
	}
}

func AddSkillToLock(skillName string, entry SkillLockEntry) error {
	lock := ReadSkillLock()
	now := time.Now().UTC().Format(time.RFC3339)
	if existing, ok := lock.Skills[skillName]; ok {
		entry.InstalledAt = existing.InstalledAt
	} else {
		entry.InstalledAt = now
	}
	entry.UpdatedAt = now
	lock.Skills[skillName] = entry
	return WriteSkillLock(lock)
}

func RemoveSkillFromLock(skillName string) error {
	lock := ReadSkillLock()
	if _, ok := lock.Skills[skillName]; !ok {
		return nil
	}
	delete(lock.Skills, skillName)
	return WriteSkillLock(lock)
}

func IsPromptDismissed(key string) bool {
	lock := ReadSkillLock()
	if key == "findSkillsPrompt" {
		return lock.Dismissed.FindSkillsPrompt
	}
	return false
}

func DismissPrompt(key string) error {
	lock := ReadSkillLock()
	if key == "findSkillsPrompt" {
		lock.Dismissed.FindSkillsPrompt = true
	}
	return WriteSkillLock(lock)
}

func GetLastSelectedAgents() []string {
	lock := ReadSkillLock()
	return lock.LastSelectedAgents
}

func SaveSelectedAgents(agents []string) error {
	lock := ReadSkillLock()
	lock.LastSelectedAgents = agents
	return WriteSkillLock(lock)
}

func GetGitHubToken() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	if tok := os.Getenv("GH_TOKEN"); tok != "" {
		return tok
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok
		}
	}
	return ""
}

func ComputeContentHash(content string) string {
	h := sha256.New()
	h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// ──────────────────────────────────────────────────────────
// Local (project) skill lock (skills-lock.json)
// ──────────────────────────────────────────────────────────

const localLockVersion = 1

type LocalSkillLockEntry struct {
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SourceType   string `json:"sourceType"`
	ComputedHash string `json:"computedHash,omitempty"`
}

type LocalSkillLockFile struct {
	Version int                            `json:"version"`
	Skills  map[string]LocalSkillLockEntry `json:"skills"`
}

func GetLocalLockPath(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, "skills-lock.json")
}

func ReadLocalLock(cwd string) LocalSkillLockFile {
	data, err := os.ReadFile(GetLocalLockPath(cwd))
	if err != nil {
		return EmptyLocalLock()
	}
	var lock LocalSkillLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return EmptyLocalLock()
	}
	if lock.Skills == nil || lock.Version < localLockVersion {
		return EmptyLocalLock()
	}
	return lock
}

func WriteLocalLock(lock LocalSkillLockFile, cwd string) error {
	// Sort keys for deterministic output
	sorted := LocalSkillLockFile{Version: lock.Version, Skills: map[string]LocalSkillLockEntry{}}
	keys := make([]string, 0, len(lock.Skills))
	for k := range lock.Skills {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sorted.Skills[k] = lock.Skills[k]
	}
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetLocalLockPath(cwd), append(data, '\n'), 0644)
}

func EmptyLocalLock() LocalSkillLockFile {
	return LocalSkillLockFile{Version: localLockVersion, Skills: map[string]LocalSkillLockEntry{}}
}

func AddSkillToLocalLock(skillName string, entry LocalSkillLockEntry, cwd string) error {
	lock := ReadLocalLock(cwd)
	lock.Skills[skillName] = entry
	return WriteLocalLock(lock, cwd)
}

func RemoveSkillFromLocalLock(skillName string, cwd string) error {
	lock := ReadLocalLock(cwd)
	if _, ok := lock.Skills[skillName]; !ok {
		return nil
	}
	delete(lock.Skills, skillName)
	return WriteLocalLock(lock, cwd)
}

func ComputeSkillFolderHash(skillDir string) (string, error) {
	type fileEntry struct {
		path    string
		content []byte
	}
	var files []fileEntry

	err := filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		if info.IsDir() {
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, _ := filepath.Rel(skillDir, path)
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		files = append(files, fileEntry{path: rel, content: data})
		return nil
	})
	if err != nil {
		return "", err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})

	h := sha256.New()
	for _, f := range files {
		io.WriteString(h, f.path)
		h.Write(f.content)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func HasProjectSkills(cwd string) bool {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if _, err := os.Stat(filepath.Join(cwd, "skills-lock.json")); err == nil {
		return true
	}
	skillsDir := filepath.Join(cwd, ".agents", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); err == nil {
				return true
			}
		}
	}
	return false
}

// Silence unused imports
var _ = fmt.Sprintf
