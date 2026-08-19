package lock

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// ──────────────────────────────────────────────────────────
// Unified project lock (mdm-lock.json)
//
// v2 stores every project-scoped section — skills, knowledge bundles,
// plugins, configured agents — in a single mdm-lock.json at the project
// root. The v1 binaries' hazard (locks read into fixed structs and
// rewritten wholesale, silently dropping keys they don't know) is closed
// here rather than by splitting files: unknown top-level keys survive a
// read/write round trip verbatim, so a future section added by a newer
// mdm cannot be destroyed by an older v2 binary touching its own section.
//
// Reads fall back to the v1 files (skills-lock.json, knowledge-lock.json,
// plugins-lock.json) when mdm-lock.json does not exist; writes always go
// to mdm-lock.json. The v1 files are left in place — `mdm migrate` owns
// retiring them.
// ──────────────────────────────────────────────────────────

// ProjectLockName is the unified project lock file at the project root.
const ProjectLockName = "mdm-lock.json"

const projectLockVersion = 1

// ProjectLockFile is the in-memory form of mdm-lock.json. Unknown
// top-level keys are captured on read and re-emitted on write.
type ProjectLockFile struct {
	Version          int
	ConfiguredAgents []string
	Skills           map[string]LocalSkillLockEntry
	Knowledge        map[string]KnowledgeLockEntry
	Plugins          map[string]PluginLockEntry
	extra            map[string]json.RawMessage
}

func (l ProjectLockFile) isEmpty() bool {
	return len(l.Skills) == 0 && len(l.Knowledge) == 0 && len(l.Plugins) == 0 &&
		len(l.ConfiguredAgents) == 0 && len(l.extra) == 0
}

// MarshalJSON emits known keys in a fixed order, then unknown keys sorted.
func (l ProjectLockFile) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	writeKey := func(key string, v any) error {
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		k, err := json.Marshal(key)
		if err != nil {
			return err
		}
		val, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(val)
		return nil
	}
	if err := writeKey("version", l.Version); err != nil {
		return nil, err
	}
	if len(l.ConfiguredAgents) > 0 {
		if err := writeKey("configuredAgents", l.ConfiguredAgents); err != nil {
			return nil, err
		}
	}
	if len(l.Skills) > 0 {
		if err := writeKey("skills", l.Skills); err != nil {
			return nil, err
		}
	}
	if len(l.Knowledge) > 0 {
		if err := writeKey("knowledge", l.Knowledge); err != nil {
			return nil, err
		}
	}
	if len(l.Plugins) > 0 {
		if err := writeKey("plugins", l.Plugins); err != nil {
			return nil, err
		}
	}
	extraKeys := make([]string, 0, len(l.extra))
	for k := range l.extra {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		if err := writeKey(k, l.extra[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON decodes the known sections and preserves every other
// top-level key verbatim in extra.
func (l *ProjectLockFile) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*l = ProjectLockFile{}
	decode := func(key string, dst any) error {
		v, ok := raw[key]
		if !ok {
			return nil
		}
		delete(raw, key)
		return json.Unmarshal(v, dst)
	}
	if err := decode("version", &l.Version); err != nil {
		return err
	}
	if err := decode("configuredAgents", &l.ConfiguredAgents); err != nil {
		return err
	}
	if err := decode("skills", &l.Skills); err != nil {
		return err
	}
	if err := decode("knowledge", &l.Knowledge); err != nil {
		return err
	}
	if err := decode("plugins", &l.Plugins); err != nil {
		return err
	}
	if len(raw) > 0 {
		l.extra = raw
	}
	return nil
}

// GetProjectLockPath returns the path of mdm-lock.json for the project.
func GetProjectLockPath(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, ProjectLockName)
}

// ReadProjectLock reads mdm-lock.json, falling back to the legacy v1 lock
// files when it does not exist. A missing or unreadable lock reads as empty.
func ReadProjectLock(cwd string) ProjectLockFile {
	data, err := os.ReadFile(GetProjectLockPath(cwd))
	if err != nil {
		return readLegacyLocks(cwd)
	}
	var lk ProjectLockFile
	if err := json.Unmarshal(data, &lk); err != nil {
		return EmptyProjectLock()
	}
	if lk.Version < projectLockVersion {
		return EmptyProjectLock()
	}
	normalizeProjectLock(&lk)
	return lk
}

// WriteProjectLock writes mdm-lock.json. A lock with nothing left in it
// removes the file instead, matching the v1 knowledge/plugins behavior.
func WriteProjectLock(lk ProjectLockFile, cwd string) error {
	path := GetProjectLockPath(cwd)
	if lk.isEmpty() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	lk.Version = projectLockVersion
	data, err := json.MarshalIndent(lk, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func EmptyProjectLock() ProjectLockFile {
	lk := ProjectLockFile{Version: projectLockVersion}
	normalizeProjectLock(&lk)
	return lk
}

func normalizeProjectLock(lk *ProjectLockFile) {
	if lk.Skills == nil {
		lk.Skills = map[string]LocalSkillLockEntry{}
	}
	if lk.Knowledge == nil {
		lk.Knowledge = map[string]KnowledgeLockEntry{}
	}
	if lk.Plugins == nil {
		lk.Plugins = map[string]PluginLockEntry{}
	}
}

// readLegacyLocks assembles a project lock from the v1 per-feature files.
func readLegacyLocks(cwd string) ProjectLockFile {
	lk := EmptyProjectLock()
	legacy := readLegacySkillsLock(cwd)
	lk.Skills = legacy.Skills
	lk.ConfiguredAgents = legacy.ConfiguredAgents
	lk.Knowledge = readLegacyKnowledgeLock(cwd).Bundles
	lk.Plugins = readLegacyPluginsLock(cwd).Plugins
	return lk
}
