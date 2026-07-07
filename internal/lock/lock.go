package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sethcarney/mdm/internal/agent"
)

// ──────────────────────────────────────────────────────────
// Global skill lock (~/.agents/skills-lock.json)
// ──────────────────────────────────────────────────────────

const globalLockVersion = 3

type SkillLockEntry struct {
	Source      string `json:"source"`
	SourceType  string `json:"sourceType"`
	SourceURL   string `json:"sourceUrl"`
	Ref         string `json:"ref,omitempty"`
	SkillPath   string `json:"skillPath,omitempty"`
	InstalledAt string `json:"installedAt"`
	UpdatedAt   string `json:"updatedAt"`
	PluginName  string `json:"pluginName,omitempty"`
}

type DismissedPrompts struct {
	FindSkillsPrompt bool `json:"findSkillsPrompt,omitempty"`
}

type SkillLockFile struct {
	Version          int                       `json:"version"`
	Skills           map[string]SkillLockEntry `json:"skills"`
	Dismissed        DismissedPrompts          `json:"dismissed,omitempty"`
	ConfiguredAgents []string                  `json:"configuredAgents,omitempty"`
	Experimental     []string                  `json:"experimental,omitempty"`
}

func GetSkillLockPath() string {
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return filepath.Join(xdgState, "skills", "skills-lock.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, agent.AgentsDir, "skills-lock.json")
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
	return os.WriteFile(lockPath, data, 0600)
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

func GetGitHubToken() string {
	return os.Getenv("GITHUB_TOKEN")
}

// ──────────────────────────────────────────────────────────
// Local (project) skill lock (skills-lock.json)
// ──────────────────────────────────────────────────────────

const localLockVersion = 1

type LocalSkillLockEntry struct {
	Source     string `json:"source"`
	Ref        string `json:"ref,omitempty"`
	SourceType string `json:"sourceType"`
	SkillPath  string `json:"skillPath,omitempty"`
}

type LocalSkillLockFile struct {
	Version          int                            `json:"version"`
	Skills           map[string]LocalSkillLockEntry `json:"skills"`
	ConfiguredAgents []string                       `json:"configuredAgents,omitempty"`
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
	sorted := LocalSkillLockFile{Version: lock.Version, Skills: map[string]LocalSkillLockEntry{}, ConfiguredAgents: lock.ConfiguredAgents}
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
	return os.WriteFile(GetLocalLockPath(cwd), append(data, '\n'), 0600)
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

// GetConfiguredAgents returns the configured agent list for the given scope.
func GetConfiguredAgents(global bool, cwd string) []string {
	if global {
		return ReadSkillLock().ConfiguredAgents
	}
	return ReadLocalLock(cwd).ConfiguredAgents
}

// SetConfiguredAgents replaces the configured agent list for the given scope.
func SetConfiguredAgents(agents []string, global bool, cwd string) error {
	if global {
		lk := ReadSkillLock()
		lk.ConfiguredAgents = agents
		return WriteSkillLock(lk)
	}
	lk := ReadLocalLock(cwd)
	lk.ConfiguredAgents = agents
	return WriteLocalLock(lk, cwd)
}

// AddToConfiguredAgents appends agents that aren't already in the list.
func AddToConfiguredAgents(toAdd []string, global bool, cwd string) error {
	current := GetConfiguredAgents(global, cwd)
	existing := map[string]bool{}
	for _, a := range current {
		existing[a] = true
	}
	for _, a := range toAdd {
		if !existing[a] {
			current = append(current, a)
			existing[a] = true
		}
	}
	sort.Strings(current)
	return SetConfiguredAgents(current, global, cwd)
}

// RemoveFromConfiguredAgents removes the given agents from the configured list.
func RemoveFromConfiguredAgents(toRemove []string, global bool, cwd string) error {
	current := GetConfiguredAgents(global, cwd)
	removeSet := map[string]bool{}
	for _, a := range toRemove {
		removeSet[a] = true
	}
	result := current[:0]
	for _, a := range current {
		if !removeSet[a] {
			result = append(result, a)
		}
	}
	return SetConfiguredAgents(result, global, cwd)
}
