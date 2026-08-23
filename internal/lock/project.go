package lock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ──────────────────────────────────────────────────────────
// Forward compatibility
//
// mdm-lock.json and mdm-state.json abort the command when this binary
// cannot understand them — a version from a newer mdm, or invalid JSON —
// rather than reading as empty. The v1 line learned this the hard way:
// its empty-on-unreadable fallback made `mdm skills install` in a
// newer-format project a silent no-op that exits 0 in CI (fixed in the
// final v1 patch releases). Aborting in the read path also stops
// read-modify-write commands from clobbering a file written by a newer
// version. The *legacy* project files fail the same way the final v1 patch
// releases did — corrupt or newer-versioned files abort rather than read
// as empty — with one exception: v2's own tombstone carries a deliberately
// newer version for v1 binaries to trip on, and reads as empty here.
// ──────────────────────────────────────────────────────────

func errNewerLock(path string, fileVersion, knownVersion int) error {
	return fmt.Errorf("%s was written by a newer version of mdm (lock version %d; this binary understands up to %d) — upgrade with 'mdm upgrade'",
		filepath.Base(path), fileVersion, knownVersion)
}

func errUnreadableLock(path string, err error) error {
	return fmt.Errorf("%s could not be parsed: %w — fix the file or restore it from version control",
		filepath.Base(path), err)
}

func fatalLock(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

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
// files when it does not exist. A missing lock reads as empty; one this
// binary cannot understand aborts the process (see Forward compatibility
// above).
func ReadProjectLock(cwd string) ProjectLockFile {
	lk, err := readProjectLockE(cwd)
	if err != nil {
		fatalLock(err)
	}
	return lk
}

func readProjectLockE(cwd string) (ProjectLockFile, error) {
	path := GetProjectLockPath(cwd)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return readLegacyLocksE(cwd)
		}
		// Only absence falls back to the legacy files — an unreadable
		// mdm-lock.json (permissions, a directory) must abort, or `mdm
		// skills install` becomes a silent exit-0 no-op in CI.
		return EmptyProjectLock(), errUnreadableLock(path, err)
	}
	var lk ProjectLockFile
	if err := json.Unmarshal(data, &lk); err != nil {
		return EmptyProjectLock(), errUnreadableLock(path, err)
	}
	if lk.Version > projectLockVersion {
		return EmptyProjectLock(), errNewerLock(path, lk.Version, projectLockVersion)
	}
	if lk.Version < projectLockVersion {
		return EmptyProjectLock(), nil
	}
	normalizeProjectLock(&lk)
	return lk, nil
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

// readLegacyLocksE assembles a project lock from the v1 per-feature files.
func readLegacyLocksE(cwd string) (ProjectLockFile, error) {
	lk := EmptyProjectLock()
	legacy, err := readLegacySkillsLockE(cwd)
	if err != nil {
		return lk, err
	}
	kb, err := readLegacyKnowledgeLockE(cwd)
	if err != nil {
		return lk, err
	}
	pl, err := readLegacyPluginsLockE(cwd)
	if err != nil {
		return lk, err
	}
	lk.Skills = legacy.Skills
	lk.ConfiguredAgents = legacy.ConfiguredAgents
	lk.Knowledge = kb.Bundles
	lk.Plugins = pl.Plugins
	return lk, nil
}
