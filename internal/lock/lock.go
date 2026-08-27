package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ──────────────────────────────────────────────────────────
// Local (project) skill lock — the skills section of mdm.lock
// ──────────────────────────────────────────────────────────

const localLockVersion = 1

type LocalSkillLockEntry struct {
	Source     string `json:"source"`
	Ref        string `json:"ref,omitempty"`
	SourceType string `json:"sourceType"`
	SkillPath  string `json:"skillPath,omitempty"`
}

// LocalSkillLockFile is a view of the skills section of the project lock.
// Reading and writing it goes through mdm.lock (with legacy
// skills-lock.json fallback on read); the other sections are preserved.
type LocalSkillLockFile struct {
	Version          int                            `json:"version"`
	Skills           map[string]LocalSkillLockEntry `json:"skills"`
	ConfiguredAgents []string                       `json:"configuredAgents,omitempty"`
}

// legacyTombstone reports whether a legacy file is v2's own migration
// tombstone (carrying a _moved pointer) rather than v1 data.
func legacyTombstone(data []byte) bool {
	var t struct {
		Moved string `json:"_moved"`
	}
	return json.Unmarshal(data, &t) == nil && t.Moved != ""
}

// readLegacySkillsLockE reads the v1 skills-lock.json directly. It is only
// consulted when mdm.lock does not exist. It fails the same way the
// final v1 patch releases did — corrupt or newer-versioned files are an
// error, not an empty lock — except for v2's own tombstone, which reads as
// empty by design.
func readLegacySkillsLockE(cwd string) (LocalSkillLockFile, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	path := filepath.Join(cwd, "skills-lock.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyLocalLock(), nil
		}
		return EmptyLocalLock(), errUnreadableLock(path, err)
	}
	if legacyTombstone(data) {
		return EmptyLocalLock(), nil
	}
	var lock LocalSkillLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return EmptyLocalLock(), errUnreadableLock(path, err)
	}
	if lock.Version > localLockVersion {
		return EmptyLocalLock(), errNewerLock(path, lock.Version, localLockVersion)
	}
	if lock.Skills == nil || lock.Version < localLockVersion {
		return EmptyLocalLock(), nil
	}
	return lock, nil
}

func ReadLocalLock(cwd string) LocalSkillLockFile {
	pl := ReadProjectLock(cwd)
	return LocalSkillLockFile{
		Version:          localLockVersion,
		Skills:           pl.Skills,
		ConfiguredAgents: pl.ConfiguredAgents,
	}
}

func WriteLocalLock(lock LocalSkillLockFile, cwd string) error {
	pl := ReadProjectLock(cwd)
	pl.Skills = lock.Skills
	pl.ConfiguredAgents = lock.ConfiguredAgents
	return WriteProjectLock(pl, cwd)
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
	if _, err := os.Stat(filepath.Join(cwd, ProjectLockName)); err == nil {
		return true
	}
	// A migration tombstone is a pointer, not project skills.
	if data, err := os.ReadFile(filepath.Join(cwd, "skills-lock.json")); err == nil && !legacyTombstone(data) {
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
		return ReadGlobalState().ConfiguredAgents
	}
	return ReadLocalLock(cwd).ConfiguredAgents
}

// SetConfiguredAgents replaces the configured agent list for the given scope.
func SetConfiguredAgents(agents []string, global bool, cwd string) error {
	if global {
		lk := ReadGlobalState()
		lk.ConfiguredAgents = agents
		return WriteGlobalState(lk)
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
