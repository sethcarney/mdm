package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ──────────────────────────────────────────────────────────
// v1 → v2 migration
//
// Everyday v2 reads tolerate some legacy oddities (a versionless file, a
// tombstone); migration must not, because it retires the source files.
// Every step here re-parses the legacy files strictly — down to the shape
// of each entry — and aborts on anything unexpected, so a migration never
// destroys data it could not read. Execution materializes the target from
// the exact parse the plan validated, never from a second, more tolerant
// read.
// ──────────────────────────────────────────────────────────

// LegacyProjectLockNames are the v1 project lock files, in display order.
var LegacyProjectLockNames = []string{"skills-lock.json", "knowledge-lock.json", "plugins-lock.json"}

// SkillsTombstone is written over skills-lock.json after a migration. The
// version is deliberately newer than any v1 lock: the final v1 patch
// releases refuse locks with a version they don't understand and print an
// upgrade pointer, so a patched v1 binary fails loudly here. Older v1
// binaries read the file as a valid, empty lock — they cannot be made to
// error — so the _comment exists for the human who finds it.
const SkillsTombstone = `{
  "version": 2,
  "skills": {},
  "_moved": "mdm-lock.json",
  "_comment": "This project migrated to mdm v2 and its lock now lives in mdm-lock.json. Patched v1 releases refuse this file with an upgrade pointer; older v1 releases read it as empty and will install nothing from it - upgrade mdm instead. Delete this file once nothing runs a v1 mdm against this project."
}
`

// ProjectMigration describes what `mdm migrate` would do in a project.
type ProjectMigration struct {
	// Legacy maps each legacy file name that exists to the number of
	// entries it holds.
	Legacy map[string]int
	// TargetExists reports whether mdm-lock.json is already present.
	TargetExists bool
	// Orphaned lists legacy entries (as "file: name") that are absent from
	// the existing mdm-lock.json and would be discarded by retiring the
	// legacy files. Only populated when TargetExists.
	Orphaned []string
	// merged is the target lock assembled from the strictly parsed legacy
	// files. Execution writes exactly this when the target does not exist.
	merged ProjectLockFile
}

// Needed reports whether there is anything to migrate.
func (m ProjectMigration) Needed() bool { return len(m.Legacy) > 0 }

// legacyFileData is one legacy file, fully decoded into the typed entry
// structs so nothing survives planning that execution could not carry over.
type legacyFileData struct {
	skills           map[string]LocalSkillLockEntry
	bundles          map[string]KnowledgeLockEntry
	plugins          map[string]PluginLockEntry
	configuredAgents []string
	isTombstone      bool
}

func (d legacyFileData) count() int {
	return len(d.skills) + len(d.bundles) + len(d.plugins)
}

// strictReadLegacy parses one legacy file with no empty-on-error tolerance:
// invalid JSON, an unrecognized version, or an entry that does not decode
// into its struct all abort the migration.
func strictReadLegacy(path string) (legacyFileData, error) {
	var d legacyFileData
	data, err := os.ReadFile(path)
	if err != nil {
		return d, err
	}
	var raw struct {
		Version          int                            `json:"version"`
		Skills           map[string]LocalSkillLockEntry `json:"skills"`
		Bundles          map[string]KnowledgeLockEntry  `json:"bundles"`
		Plugins          map[string]PluginLockEntry     `json:"plugins"`
		ConfiguredAgents []string                       `json:"configuredAgents"`
		MovedNote        string                         `json:"_moved"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return d, fmt.Errorf("%s is not valid JSON: %w", filepath.Base(path), err)
	}
	if raw.MovedNote != "" {
		// An earlier migration's tombstone — nothing left to carry over.
		d.isTombstone = true
		return d, nil
	}
	if raw.Version < 1 {
		return d, fmt.Errorf("%s has no recognizable version field", filepath.Base(path))
	}
	if raw.Version > 1 {
		return d, fmt.Errorf("%s has lock version %d, which no v1 release wrote", filepath.Base(path), raw.Version)
	}
	d.skills = raw.Skills
	d.bundles = raw.Bundles
	d.plugins = raw.Plugins
	d.configuredAgents = raw.ConfiguredAgents
	return d, nil
}

// PlanProjectMigration inspects the project and reports what a migration
// would do. It fails on legacy files it cannot parse.
func PlanProjectMigration(cwd string) (ProjectMigration, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	plan := ProjectMigration{Legacy: map[string]int{}, merged: EmptyProjectLock()}
	_, err := os.Stat(GetProjectLockPath(cwd))
	plan.TargetExists = err == nil

	var target ProjectLockFile
	if plan.TargetExists {
		target, err = readProjectLockE(cwd)
		if err != nil {
			return plan, err
		}
	}

	for _, fname := range LegacyProjectLockNames {
		path := filepath.Join(cwd, fname)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		d, err := strictReadLegacy(path)
		if err != nil {
			return plan, err
		}
		if d.isTombstone {
			continue
		}
		if plan.TargetExists {
			for name := range d.skills {
				if _, ok := target.Skills[name]; !ok {
					plan.Orphaned = append(plan.Orphaned, fname+": "+name)
				}
			}
			for name := range d.bundles {
				if _, ok := target.Knowledge[name]; !ok {
					plan.Orphaned = append(plan.Orphaned, fname+": "+name)
				}
			}
			for name := range d.plugins {
				if _, ok := target.Plugins[name]; !ok {
					plan.Orphaned = append(plan.Orphaned, fname+": "+name)
				}
			}
		}
		for name, e := range d.skills {
			plan.merged.Skills[name] = e
		}
		for name, e := range d.bundles {
			plan.merged.Knowledge[name] = e
		}
		for name, e := range d.plugins {
			plan.merged.Plugins[name] = e
		}
		if len(d.configuredAgents) > 0 {
			plan.merged.ConfiguredAgents = d.configuredAgents
		}
		plan.Legacy[fname] = d.count()
	}
	sort.Strings(plan.Orphaned)
	return plan, nil
}

// ExecuteProjectMigration performs the migration PlanProjectMigration
// described: it writes the lock the plan assembled to mdm-lock.json (when
// it does not exist yet), replaces skills-lock.json with a tombstone (or
// deletes it with tombstone=false), and deletes the other legacy files.
// Call PlanProjectMigration first; a plan with orphaned entries discards
// them, so the caller must confirm that explicitly.
func ExecuteProjectMigration(cwd string, tombstone bool) error {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	plan, err := PlanProjectMigration(cwd)
	if err != nil {
		return err
	}
	if !plan.Needed() {
		return nil
	}
	if !plan.TargetExists {
		if err := WriteProjectLock(plan.merged, cwd); err != nil {
			return err
		}
	}
	for _, fname := range LegacyProjectLockNames {
		if _, ok := plan.Legacy[fname]; !ok {
			continue
		}
		path := filepath.Join(cwd, fname)
		if fname == "skills-lock.json" && tombstone {
			if err := writeFileAtomic(path, []byte(SkillsTombstone), 0600); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// LegacyGlobalLockExists reports whether the v1 global skills-lock.json is
// still on disk, and where.
func LegacyGlobalLockExists() (string, bool) {
	path := legacyGlobalLockPath()
	_, err := os.Stat(path)
	return path, err == nil
}

// GlobalMigration describes what migrating the global state would do.
type GlobalMigration struct {
	// LegacyPath is the v1 global skills-lock.json, when it exists.
	LegacyPath string
	// TargetExists reports whether mdm-state.json is already present.
	TargetExists bool
	// Orphaned lists legacy skill names absent from the existing
	// mdm-state.json that retiring the legacy file would discard. Only
	// populated when TargetExists.
	Orphaned []string
	needed   bool
	merged   GlobalState
}

// Needed reports whether there is anything to migrate.
func (m GlobalMigration) Needed() bool { return m.needed }

// strictReadLegacyGlobal parses the v1 global lock with no empty-on-error
// tolerance for broken JSON. A lock below the last v1 version migrates as
// empty — v1 itself deliberately discarded those on read.
func strictReadLegacyGlobal(path string) (GlobalState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EmptyGlobalState(), err
	}
	var legacy struct {
		Version          int                       `json:"version"`
		Skills           map[string]SkillLockEntry `json:"skills"`
		Dismissed        DismissedPrompts          `json:"dismissed"`
		ConfiguredAgents []string                  `json:"configuredAgents"`
		Experimental     []string                  `json:"experimental"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return EmptyGlobalState(), fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if legacy.Skills == nil || legacy.Version < legacyGlobalLockVersion {
		return EmptyGlobalState(), nil
	}
	return GlobalState{
		Version:          globalStateVersion,
		Skills:           legacy.Skills,
		Dismissed:        legacy.Dismissed,
		ConfiguredAgents: legacy.ConfiguredAgents,
		Experimental:     legacy.Experimental,
	}, nil
}

// PlanGlobalMigration inspects the machine's global state and reports what
// a migration would do. It fails on a legacy file it cannot parse — the
// file is per-machine state with no copy in version control, so it must
// never be deleted on the strength of a read that fell back to empty.
func PlanGlobalMigration() (GlobalMigration, error) {
	var plan GlobalMigration
	legacy, ok := LegacyGlobalLockExists()
	if !ok {
		return plan, nil
	}
	plan.needed = true
	plan.LegacyPath = legacy
	parsed, err := strictReadLegacyGlobal(legacy)
	if err != nil {
		return plan, err
	}
	if _, err := os.Stat(GetGlobalStatePath()); err == nil {
		plan.TargetExists = true
		target, err := readGlobalStateE()
		if err != nil {
			return plan, err
		}
		for name := range parsed.Skills {
			if _, ok := target.Skills[name]; !ok {
				plan.Orphaned = append(plan.Orphaned, name)
			}
		}
		sort.Strings(plan.Orphaned)
		return plan, nil
	}
	plan.merged = parsed
	return plan, nil
}

// ExecuteGlobalMigration writes the state PlanGlobalMigration assembled to
// mdm-state.json (when it does not exist yet) and deletes the v1 global
// skills-lock.json. The global file is per-machine state, so no tombstone
// is left behind. A plan with orphaned entries discards them, so the
// caller must confirm that explicitly.
func ExecuteGlobalMigration() error {
	plan, err := PlanGlobalMigration()
	if err != nil {
		return err
	}
	if !plan.needed {
		return nil
	}
	if !plan.TargetExists {
		if err := WriteGlobalState(plan.merged); err != nil {
			return err
		}
	}
	return os.Remove(plan.LegacyPath)
}
