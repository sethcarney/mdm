package lock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// ──────────────────────────────────────────────────────────
// Forward compatibility
//
// mdm.lock and mdm-state.json abort the command when this binary
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

// writeFileAtomic writes via a temp file in the same directory plus rename,
// so a write that dies partway (disk full, crash) can never leave a
// half-written lock behind — a truncated mdm.lock would abort every
// subsequent command, including the migration that could have repaired it.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ──────────────────────────────────────────────────────────
// Unified project lock (mdm.lock)
//
// v2 stores every project-scoped section — skills, knowledge bundles,
// plugins, configured agents — in a single mdm.lock at the project
// root. The v1 binaries' hazard (locks read into fixed structs and
// rewritten wholesale, silently dropping keys they don't know) is closed
// here rather than by splitting files: unknown top-level keys survive a
// read/write round trip verbatim, so a future section added by a newer
// mdm cannot be destroyed by an older v2 binary touching its own section.
//
// Reads fall back to the v1 files (skills-lock.json, knowledge-lock.json,
// plugins-lock.json) when mdm.lock does not exist; writes always go
// to mdm.lock. The v1 files are left in place — `mdm migrate` owns
// retiring them.
// ──────────────────────────────────────────────────────────

// Install mode is a single top-level switch per scope, not a per-asset
// value. It governs every install, update, and restore in that scope.
// An empty value means symlink.
const (
	InstallModeSymlink = "symlink"
	InstallModeCopy    = "copy"
)

// ProjectLockName is the unified project lock file at the project root.
const ProjectLockName = "mdm.lock"

const projectLockVersion = 2

// ProjectLockFile is the in-memory form of mdm.lock. Unknown
// top-level keys are captured on read and re-emitted on write, and so are
// unknown keys inside each entry — a per-entry field added by a newer v2
// survives this binary rewriting the entry's known fields.
type ProjectLockFile struct {
	Version          int
	InstallMode      string
	ConfiguredAgents []string
	Skills           map[string]LocalSkillLockEntry
	Knowledge        map[string]KnowledgeLockEntry
	Plugins          map[string]PluginLockEntry
	extra            map[string]json.RawMessage
	rawSkills        map[string]json.RawMessage
	rawKnowledge     map[string]json.RawMessage
	rawPlugins       map[string]json.RawMessage
}

// knownJSONKeys lists a struct's json field names, so entry marshalling can
// tell a field this binary deliberately cleared (known, stays gone) from a
// field it has never heard of (unknown, must survive).
func knownJSONKeys(v any) map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}

var (
	knownSkillEntryKeys       = knownJSONKeys(LocalSkillLockEntry{})
	knownKnowledgeEntryKeys   = knownJSONKeys(KnowledgeLockEntry{})
	knownPluginEntryKeys      = knownJSONKeys(PluginLockEntry{})
	knownGlobalSkillEntryKeys = knownJSONKeys(SkillLockEntry{})
)

// mergeEntryExtras marshals a typed entry, then folds back any keys from
// the originally read JSON that the entry struct does not know about.
func mergeEntryExtras(typed any, raw json.RawMessage, known map[string]bool) (json.RawMessage, error) {
	typedJSON, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return typedJSON, nil
	}
	var rawMap map[string]json.RawMessage
	if json.Unmarshal(raw, &rawMap) != nil {
		return typedJSON, nil
	}
	extras := false
	for k := range rawMap {
		if !known[k] {
			extras = true
			break
		}
	}
	if !extras {
		return typedJSON, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(typedJSON, &out); err != nil {
		return nil, err
	}
	for k, v := range rawMap {
		if !known[k] {
			out[k] = v
		}
	}
	return json.Marshal(out)
}

// marshalSection renders one entry map, preserving unknown per-entry keys
// captured at read time.
func marshalSection[E any](entries map[string]E, raws map[string]json.RawMessage, known map[string]bool) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(entries))
	for name, entry := range entries {
		merged, err := mergeEntryExtras(entry, raws[name], known)
		if err != nil {
			return nil, err
		}
		out[name] = merged
	}
	return out, nil
}

// mergedSection is one entry section rendered for writing.
type mergedSection struct {
	key   string
	value map[string]json.RawMessage
}

// mergedSections renders each non-empty entry section with unknown
// per-entry keys folded back, in the fixed write order.
func (l ProjectLockFile) mergedSections() ([]mergedSection, error) {
	var out []mergedSection
	if len(l.Skills) > 0 {
		v, err := marshalSection(l.Skills, l.rawSkills, knownSkillEntryKeys)
		if err != nil {
			return nil, err
		}
		out = append(out, mergedSection{"skills", v})
	}
	if len(l.Knowledge) > 0 {
		v, err := marshalSection(l.Knowledge, l.rawKnowledge, knownKnowledgeEntryKeys)
		if err != nil {
			return nil, err
		}
		out = append(out, mergedSection{"knowledge", v})
	}
	if len(l.Plugins) > 0 {
		v, err := marshalSection(l.Plugins, l.rawPlugins, knownPluginEntryKeys)
		if err != nil {
			return nil, err
		}
		out = append(out, mergedSection{"plugins", v})
	}
	return out, nil
}

// captureRawEntries snapshots a section's entries as raw JSON for the
// unknown-key merge on write.
func captureRawEntries(section json.RawMessage) map[string]json.RawMessage {
	if section == nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(section, &m) != nil {
		return nil
	}
	return m
}

func (l ProjectLockFile) isEmpty() bool {
	return len(l.Skills) == 0 && len(l.Knowledge) == 0 && len(l.Plugins) == 0 &&
		len(l.ConfiguredAgents) == 0 && len(l.extra) == 0 && l.InstallMode == ""
}

// orderedObject builds a JSON object whose keys come out in the order they
// were written, which encoding/json's map marshalling cannot do. The first
// marshal error is kept and every later write is a no-op, so callers write
// their keys in a straight line and check once at the end.
type orderedObject struct {
	buf bytes.Buffer
	err error
}

func newOrderedObject() *orderedObject {
	o := &orderedObject{}
	o.buf.WriteByte('{')
	return o
}

// write appends key: value. It does nothing once an error has occurred.
func (o *orderedObject) write(key string, v any) {
	if o.err != nil {
		return
	}
	k, err := json.Marshal(key)
	if err != nil {
		o.err = err
		return
	}
	val, err := json.Marshal(v)
	if err != nil {
		o.err = err
		return
	}
	if o.buf.Len() > 1 {
		o.buf.WriteByte(',')
	}
	o.buf.Write(k)
	o.buf.WriteByte(':')
	o.buf.Write(val)
}

// writeExtra appends every unknown top-level key in sorted order, so a
// rewrite of the file is stable.
func (o *orderedObject) writeExtra(extra map[string]json.RawMessage) {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o.write(k, extra[k])
	}
}

// bytes closes the object and returns it, or the first error any write hit.
func (o *orderedObject) bytes() ([]byte, error) {
	if o.err != nil {
		return nil, o.err
	}
	o.buf.WriteByte('}')
	return o.buf.Bytes(), nil
}

// MarshalJSON emits known keys in a fixed order, then unknown keys sorted.
func (l ProjectLockFile) MarshalJSON() ([]byte, error) {
	sections, err := l.mergedSections()
	if err != nil {
		return nil, err
	}
	o := newOrderedObject()
	o.write("version", l.Version)
	if l.InstallMode != "" {
		o.write("installMode", l.InstallMode)
	}
	if len(l.ConfiguredAgents) > 0 {
		o.write("configuredAgents", l.ConfiguredAgents)
	}
	for _, s := range sections {
		o.write(s.key, s.value)
	}
	o.writeExtra(l.extra)
	return o.bytes()
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
	if err := decode("installMode", &l.InstallMode); err != nil {
		return err
	}
	if err := decode("configuredAgents", &l.ConfiguredAgents); err != nil {
		return err
	}
	l.rawSkills = captureRawEntries(raw["skills"])
	l.rawKnowledge = captureRawEntries(raw["knowledge"])
	l.rawPlugins = captureRawEntries(raw["plugins"])
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

// GetProjectLockPath returns the path of mdm.lock for the project.
func GetProjectLockPath(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, ProjectLockName)
}

// ReadProjectLock reads mdm.lock, falling back to the legacy v1 lock
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
		// mdm.lock (permissions, a directory) must abort, or `mdm
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
	// A version 1 lock predates the install-mode switch: upgrade it in
	// memory and let the next write persist the version. The mode stays
	// empty; `mdm migrate` infers it from disk. A range, not `== 1`, so the
	// next bump does not reintroduce the read-as-empty bug.
	if lk.Version >= 1 && lk.Version < projectLockVersion {
		lk.Version = projectLockVersion
	}
	if lk.Version < projectLockVersion {
		return EmptyProjectLock(), nil
	}
	normalizeProjectLock(&lk)
	return lk, nil
}

// WriteProjectLock writes mdm.lock. A lock with nothing left in it
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
	return writeFileAtomic(path, append(data, '\n'), 0600)
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
