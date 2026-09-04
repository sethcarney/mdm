package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/sethcarney/mdm/internal/harness"
)

// Global state (~/.agents/mdm-state.json): per-user, per-machine state. It
// holds globally installed skills, dismissed prompts, the global
// configured-harness list, and experimental opt-ins. It is never committed.
// Unknown top-level keys survive a round trip. Reads fall back to the v1
// skills-lock.json when it is absent; writes always go to mdm-state.json.

const globalStateVersion = 2

// legacyGlobalLockVersion is the version the v1 global skills-lock.json
// had to carry to be readable.
const legacyGlobalLockVersion = 3

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

// GlobalState is the in-memory form of mdm-state.json. Unknown top-level keys
// are captured on read and re-emitted on write, and so are unknown keys inside
// each skill entry (see ProjectLockFile).
type GlobalState struct {
	Version             int
	InstallMode         string
	Skills              map[string]SkillLockEntry
	Agents              map[string]AgentLockEntry
	Dismissed           DismissedPrompts
	ConfiguredHarnesses []string
	Experimental        []string
	extra               map[string]json.RawMessage
	rawSkills           map[string]json.RawMessage
	rawAgents           map[string]json.RawMessage
}

// MarshalJSON emits known keys in a fixed order, then unknown keys sorted.
func (s GlobalState) MarshalJSON() ([]byte, error) {
	mergedSkills, err := marshalSection(s.Skills, s.rawSkills, knownGlobalSkillEntryKeys)
	if err != nil {
		return nil, err
	}
	mergedAgents, err := marshalSection(s.Agents, s.rawAgents, knownAgentEntryKeys)
	if err != nil {
		return nil, err
	}
	o := newOrderedObject()
	o.write("version", s.Version)
	if s.InstallMode != "" {
		o.write("installMode", s.InstallMode)
	}
	if len(s.ConfiguredHarnesses) > 0 {
		o.write("configuredHarnesses", s.ConfiguredHarnesses)
	}
	o.write("skills", mergedSkills)
	if len(mergedAgents) > 0 {
		o.write("agents", mergedAgents)
	}
	if s.Dismissed != (DismissedPrompts{}) {
		o.write("dismissed", s.Dismissed)
	}
	if len(s.Experimental) > 0 {
		o.write("experimental", s.Experimental)
	}
	o.writeExtra(s.extra)
	return o.bytes()
}

// UnmarshalJSON decodes the known sections and preserves every other
// top-level key verbatim in extra.
func (s *GlobalState) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = GlobalState{}
	decode := func(key string, dst any) error {
		v, ok := raw[key]
		if !ok {
			return nil
		}
		delete(raw, key)
		return json.Unmarshal(v, dst)
	}
	if err := decode("version", &s.Version); err != nil {
		return err
	}
	if err := decode("installMode", &s.InstallMode); err != nil {
		return err
	}
	if _, ok := raw["configuredHarnesses"]; ok {
		if err := decode("configuredHarnesses", &s.ConfiguredHarnesses); err != nil {
			return err
		}
	} else if _, ok := raw["configuredAgents"]; ok {
		// mdm-state.json's v2 format shipped this key spelled
		// configuredAgents. Real files exist with that spelling, so decode
		// falls back to it and deletes it from raw, which recovers the
		// harness list and leaves one spelling behind.
		if err := decode("configuredAgents", &s.ConfiguredHarnesses); err != nil {
			return err
		}
	}
	s.rawSkills = captureRawEntries(raw["skills"])
	s.rawAgents = captureRawEntries(raw["agents"])
	if err := decode("skills", &s.Skills); err != nil {
		return err
	}
	if err := decode("agents", &s.Agents); err != nil {
		return err
	}
	if err := decode("dismissed", &s.Dismissed); err != nil {
		return err
	}
	if err := decode("experimental", &s.Experimental); err != nil {
		return err
	}
	if len(raw) > 0 {
		s.extra = raw
	}
	return nil
}

// GetGlobalStatePath returns the path of the per-user state file.
func GetGlobalStatePath() string {
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return filepath.Join(xdgState, "mdm", "state.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, harness.SharedRootDir, "mdm-state.json")
}

// legacyGlobalLockPath returns where v1 kept the global skills-lock.json.
func legacyGlobalLockPath() string {
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return filepath.Join(xdgState, "skills", "skills-lock.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, harness.SharedRootDir, "skills-lock.json")
}

// ReadGlobalState reads mdm-state.json, falling back to the legacy v1
// skills-lock.json when it does not exist. Missing state reads as empty; state
// this binary cannot understand aborts the process.
func ReadGlobalState() GlobalState {
	s, err := readGlobalStateE()
	if err != nil {
		fatalLock(err)
	}
	return s
}

func readGlobalStateE() (GlobalState, error) {
	path := GetGlobalStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return readLegacyGlobalLock(), nil
		}
		// Only absence falls back to the legacy file - an unreadable
		// mdm-state.json must abort, not read as empty.
		return EmptyGlobalState(), errUnreadableLock(path, err)
	}
	var s GlobalState
	if err := json.Unmarshal(data, &s); err != nil {
		return EmptyGlobalState(), errUnreadableLock(path, err)
	}
	if s.Version > globalStateVersion {
		return EmptyGlobalState(), errNewerLock(path, s.Version, globalStateVersion)
	}
	// A version 1 state file predates the install-mode switch: upgrade it in
	// memory and let the next write persist the version. `mdm migrate` infers
	// the mode from disk. A range, not `== 1`, so the next bump keeps this path.
	if s.Version >= 1 && s.Version < globalStateVersion {
		s.Version = globalStateVersion
	}
	if s.Version < globalStateVersion {
		return EmptyGlobalState(), nil
	}
	if s.Skills == nil {
		s.Skills = map[string]SkillLockEntry{}
	}
	if s.Agents == nil {
		s.Agents = map[string]AgentLockEntry{}
	}
	return s, nil
}

// readLegacyGlobalLock keeps v1's read-as-empty tolerance for the global lock.
// Everyday reads fall back through it, but `mdm migrate` strict-parses the file
// before retiring it; see PlanGlobalMigration.
func readLegacyGlobalLock() GlobalState {
	data, err := os.ReadFile(legacyGlobalLockPath())
	if err != nil {
		return EmptyGlobalState()
	}
	var legacy struct {
		Version             int                       `json:"version"`
		Skills              map[string]SkillLockEntry `json:"skills"`
		Dismissed           DismissedPrompts          `json:"dismissed"`
		ConfiguredHarnesses []string                  `json:"configuredAgents"`
		Experimental        []string                  `json:"experimental"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return EmptyGlobalState()
	}
	if legacy.Skills == nil || legacy.Version < legacyGlobalLockVersion {
		return EmptyGlobalState()
	}
	return GlobalState{
		Version:             globalStateVersion,
		Skills:              legacy.Skills,
		Agents:              map[string]AgentLockEntry{},
		Dismissed:           legacy.Dismissed,
		ConfiguredHarnesses: legacy.ConfiguredHarnesses,
		Experimental:        legacy.Experimental,
	}
}

func WriteGlobalState(s GlobalState) error {
	path := GetGlobalStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	s.Version = globalStateVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0600)
}

func EmptyGlobalState() GlobalState {
	return GlobalState{
		Version: globalStateVersion,
		Skills:  map[string]SkillLockEntry{},
		Agents:  map[string]AgentLockEntry{},
	}
}

// AddAgentToGlobalState records one installed agent definition in
// mdm-state.json, mirroring AddSkillToGlobalState. An agent entry carries no
// timestamps: AgentLockEntry has none, matching the project lock's entry shape.
func AddAgentToGlobalState(name string, entry AgentLockEntry) error {
	state := ReadGlobalState()
	state.Agents[name] = entry
	return WriteGlobalState(state)
}

// RemoveAgentFromGlobalState removes one agent definition's entry from
// mdm-state.json, mirroring RemoveSkillFromGlobalState.
func RemoveAgentFromGlobalState(name string) error {
	state := ReadGlobalState()
	if _, ok := state.Agents[name]; !ok {
		return nil
	}
	delete(state.Agents, name)
	return WriteGlobalState(state)
}

func AddSkillToGlobalState(skillName string, entry SkillLockEntry) error {
	state := ReadGlobalState()
	now := time.Now().UTC().Format(time.RFC3339)
	if existing, ok := state.Skills[skillName]; ok {
		entry.InstalledAt = existing.InstalledAt
	} else {
		entry.InstalledAt = now
	}
	entry.UpdatedAt = now
	state.Skills[skillName] = entry
	return WriteGlobalState(state)
}

func RemoveSkillFromGlobalState(skillName string) error {
	state := ReadGlobalState()
	if _, ok := state.Skills[skillName]; !ok {
		return nil
	}
	delete(state.Skills, skillName)
	return WriteGlobalState(state)
}

func IsPromptDismissed(key string) bool {
	state := ReadGlobalState()
	if key == "findSkillsPrompt" {
		return state.Dismissed.FindSkillsPrompt
	}
	return false
}

func DismissPrompt(key string) error {
	state := ReadGlobalState()
	if key == "findSkillsPrompt" {
		state.Dismissed.FindSkillsPrompt = true
	}
	return WriteGlobalState(state)
}

func GetGitHubToken() string {
	return os.Getenv("GITHUB_TOKEN")
}
