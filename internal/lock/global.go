package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/sethcarney/mdm/internal/agent"
)

// ──────────────────────────────────────────────────────────
// Global state (~/.agents/mdm-state.json)
//
// Per-user, per-machine state: globally installed skills, dismissed
// prompts, the global configured-agent list, and experimental opt-ins.
// v1 called this file skills-lock.json, but unlike the project lock it is
// never committed or shared — v2 names it what it is. Unknown top-level
// keys survive a read/write round trip, same as the project lock.
//
// Reads fall back to the v1 skills-lock.json when mdm-state.json does not
// exist; writes always go to mdm-state.json. `mdm migrate` retires the
// v1 file.
// ──────────────────────────────────────────────────────────

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

// GlobalState is the in-memory form of mdm-state.json. Unknown top-level
// keys are captured on read and re-emitted on write, and so are unknown
// keys inside each skill entry (see ProjectLockFile).
type GlobalState struct {
	Version          int
	InstallMode      string
	Skills           map[string]SkillLockEntry
	Dismissed        DismissedPrompts
	ConfiguredAgents []string
	Experimental     []string
	extra            map[string]json.RawMessage
	rawSkills        map[string]json.RawMessage
}

// MarshalJSON emits known keys in a fixed order, then unknown keys sorted.
func (s GlobalState) MarshalJSON() ([]byte, error) {
	mergedSkills, err := marshalSection(s.Skills, s.rawSkills, knownGlobalSkillEntryKeys)
	if err != nil {
		return nil, err
	}
	o := newOrderedObject()
	o.write("version", s.Version)
	if s.InstallMode != "" {
		o.write("installMode", s.InstallMode)
	}
	if len(s.ConfiguredAgents) > 0 {
		o.write("configuredAgents", s.ConfiguredAgents)
	}
	o.write("skills", mergedSkills)
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
	if err := decode("configuredAgents", &s.ConfiguredAgents); err != nil {
		return err
	}
	s.rawSkills = captureRawEntries(raw["skills"])
	if err := decode("skills", &s.Skills); err != nil {
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
	return filepath.Join(home, agent.AgentsDir, "mdm-state.json")
}

// legacyGlobalLockPath returns where v1 kept the global skills-lock.json.
func legacyGlobalLockPath() string {
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return filepath.Join(xdgState, "skills", "skills-lock.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, agent.AgentsDir, "skills-lock.json")
}

// ReadGlobalState reads mdm-state.json, falling back to the legacy v1
// skills-lock.json when it does not exist. Missing state reads as empty;
// state this binary cannot understand aborts the process (see Forward
// compatibility in project.go).
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
		// Only absence falls back to the legacy file — an unreadable
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
	// memory and let the next write persist the version. The mode stays
	// empty; `mdm migrate` infers it from disk. A range, not `== 1`, so the
	// next bump does not reintroduce the read-as-empty bug.
	if s.Version >= 1 && s.Version < globalStateVersion {
		s.Version = globalStateVersion
	}
	if s.Version < globalStateVersion {
		return EmptyGlobalState(), nil
	}
	if s.Skills == nil {
		s.Skills = map[string]SkillLockEntry{}
	}
	return s, nil
}

// readLegacyGlobalLock keeps v1's deliberate read-as-empty tolerance for
// the global lock (version resets there were how old global locks were
// discarded on upgrade). Everyday reads may fall back through it, but
// `mdm migrate` strict-parses the file before retiring it — see
// PlanGlobalMigration.
func readLegacyGlobalLock() GlobalState {
	data, err := os.ReadFile(legacyGlobalLockPath())
	if err != nil {
		return EmptyGlobalState()
	}
	var legacy struct {
		Version          int                       `json:"version"`
		Skills           map[string]SkillLockEntry `json:"skills"`
		Dismissed        DismissedPrompts          `json:"dismissed"`
		ConfiguredAgents []string                  `json:"configuredAgents"`
		Experimental     []string                  `json:"experimental"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return EmptyGlobalState()
	}
	if legacy.Skills == nil || legacy.Version < legacyGlobalLockVersion {
		return EmptyGlobalState()
	}
	return GlobalState{
		Version:          globalStateVersion,
		Skills:           legacy.Skills,
		Dismissed:        legacy.Dismissed,
		ConfiguredAgents: legacy.ConfiguredAgents,
		Experimental:     legacy.Experimental,
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
	}
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
