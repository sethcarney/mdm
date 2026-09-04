package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ──────────────────────────────────────────────────────────
// Local (project) skill lock - the skills section of mdm.lock
// ──────────────────────────────────────────────────────────

const localLockVersion = 1

type LocalSkillLockEntry struct {
	Source     string `json:"source"`
	Ref        string `json:"ref,omitempty"`
	SourceType string `json:"sourceType"`
	SkillPath  string `json:"skillPath,omitempty"`
}

// AgentLockEntry records one installed agent-definition file. It mirrors
// LocalSkillLockEntry: the same source fields, plus AgentPath — where the
// file sat inside the source tree — so a later update can find it again.
// Unlike SkillPath, AgentPath has no omitempty: a definition is a single
// file, not a directory, so there is no well-known name (like SKILL.md) an
// update could fall back to guessing; losing this path silently would make
// the entry impossible to refresh from its source.
type AgentLockEntry struct {
	Source     string `json:"source"`
	SourceType string `json:"sourceType"`
	Ref        string `json:"ref,omitempty"`
	AgentPath  string `json:"agentPath"`
}

// LocalSkillLockFile is a view of the skills section of the project lock.
// Reading and writing it goes through mdm.lock (with legacy
// skills-lock.json fallback on read); the other sections are preserved.
//
// json.Unmarshal into this struct is also how readLegacySkillsLockE reads a
// real v1 skills-lock.json directly, so ConfiguredHarnesses keeps the v1 tag
// (configuredAgents) rather than the v2 one: v1 is frozen and in the wild,
// and this struct's tag is the only thing standing between a real v1 file
// and this field. Field writes never go through this struct's own
// MarshalJSON — WriteLocalLock copies onto ProjectLockFile, which owns the
// v2 configuredHarnesses key — so the v1 tag here costs nothing on write.
type LocalSkillLockFile struct {
	Version             int                            `json:"version"`
	Skills              map[string]LocalSkillLockEntry `json:"skills"`
	ConfiguredHarnesses []string                       `json:"configuredAgents,omitempty"`
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
// final v1 patch releases did - corrupt or newer-versioned files are an
// error, not an empty lock - except for v2's own tombstone, which reads as
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
		Version:             localLockVersion,
		Skills:              pl.Skills,
		ConfiguredHarnesses: pl.ConfiguredHarnesses,
	}
}

func WriteLocalLock(lock LocalSkillLockFile, cwd string) error {
	pl := ReadProjectLock(cwd)
	pl.Skills = lock.Skills
	pl.ConfiguredHarnesses = lock.ConfiguredHarnesses
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

// AddAgentToLocalLock records one installed agent definition in mdm.lock.
// It mirrors AddSkillToLocalLock but goes through ProjectLockFile directly
// rather than the LocalSkillLockFile view: that view carries only the
// skills and configuredHarnesses sections across a read/write round trip,
// not agents.
func AddAgentToLocalLock(name string, entry AgentLockEntry, cwd string) error {
	pl := ReadProjectLock(cwd)
	if pl.Agents == nil {
		pl.Agents = map[string]AgentLockEntry{}
	}
	pl.Agents[name] = entry
	return WriteProjectLock(pl, cwd)
}

// RemoveAgentFromLocalLock removes one agent definition's entry from
// mdm.lock, mirroring RemoveSkillFromLocalLock.
func RemoveAgentFromLocalLock(name string, cwd string) error {
	pl := ReadProjectLock(cwd)
	if _, ok := pl.Agents[name]; !ok {
		return nil
	}
	delete(pl.Agents, name)
	return WriteProjectLock(pl, cwd)
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

// GetConfiguredHarnesses returns the configured harness list for the given scope.
func GetConfiguredHarnesses(global bool, cwd string) []string {
	if global {
		return ReadGlobalState().ConfiguredHarnesses
	}
	return ReadLocalLock(cwd).ConfiguredHarnesses
}

// SetConfiguredHarnesses replaces the configured harness list for the given scope.
func SetConfiguredHarnesses(harnesses []string, global bool, cwd string) error {
	if global {
		lk := ReadGlobalState()
		lk.ConfiguredHarnesses = harnesses
		return WriteGlobalState(lk)
	}
	lk := ReadLocalLock(cwd)
	lk.ConfiguredHarnesses = harnesses
	return WriteLocalLock(lk, cwd)
}

// GetInstallMode returns the scope's recorded install mode. An empty
// string means symlink, which is the default for a scope that has never
// had the switch set.
func GetInstallMode(global bool, cwd string) string {
	if global {
		return ReadGlobalState().InstallMode
	}
	return ReadProjectLock(cwd).InstallMode
}

// SetInstallMode records the scope's install mode. It goes through the
// project lock directly rather than the LocalSkillLockFile view, which
// carries only the skills and configuredHarnesses sections across a write.
func SetInstallMode(mode string, global bool, cwd string) error {
	if global {
		s := ReadGlobalState()
		s.InstallMode = mode
		return WriteGlobalState(s)
	}
	lk := ReadProjectLock(cwd)
	lk.InstallMode = mode
	return WriteProjectLock(lk, cwd)
}

// AddToConfiguredAgents appends harnesses that aren't already in the list.
func AddToConfiguredAgents(toAdd []string, global bool, cwd string) error {
	current := GetConfiguredHarnesses(global, cwd)
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
	return SetConfiguredHarnesses(current, global, cwd)
}

// RemoveFromConfiguredAgents removes the given harnesses from the configured list.
func RemoveFromConfiguredAgents(toRemove []string, global bool, cwd string) error {
	current := GetConfiguredHarnesses(global, cwd)
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
	return SetConfiguredHarnesses(result, global, cwd)
}
