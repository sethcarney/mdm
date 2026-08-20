package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ──────────────────────────────────────────────────────────
// v1 → v2 migration
//
// Everyday v2 reads tolerate broken legacy files by treating them as
// empty; migration must not, because it retires the source files. Every
// step here re-parses the legacy files strictly and aborts on anything
// unexpected, so a migration never destroys data it could not read.
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
}

// Needed reports whether there is anything to migrate.
func (m ProjectMigration) Needed() bool { return len(m.Legacy) > 0 }

// strictReadLegacy parses one legacy file with no empty-on-error tolerance.
// It returns the entry names per section key, and whether the file is a
// tombstone from an earlier migration.
func strictReadLegacy(path string) (names map[string][]string, isTombstone bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	var raw struct {
		Version   int                        `json:"version"`
		Skills    map[string]json.RawMessage `json:"skills"`
		Bundles   map[string]json.RawMessage `json:"bundles"`
		Plugins   map[string]json.RawMessage `json:"plugins"`
		MovedNote string                     `json:"_moved"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false, fmt.Errorf("%s is not valid JSON: %w", filepath.Base(path), err)
	}
	if raw.MovedNote != "" {
		// An earlier migration's tombstone — nothing left to carry over.
		return map[string][]string{}, true, nil
	}
	if raw.Version < 1 {
		return nil, false, fmt.Errorf("%s has no recognizable version field", filepath.Base(path))
	}
	names = map[string][]string{}
	for key, section := range map[string]map[string]json.RawMessage{
		"skills": raw.Skills, "bundles": raw.Bundles, "plugins": raw.Plugins,
	} {
		for name := range section {
			names[key] = append(names[key], name)
		}
	}
	return names, false, nil
}

// PlanProjectMigration inspects the project and reports what a migration
// would do. It fails on legacy files it cannot parse.
func PlanProjectMigration(cwd string) (ProjectMigration, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	plan := ProjectMigration{Legacy: map[string]int{}}
	_, err := os.Stat(GetProjectLockPath(cwd))
	plan.TargetExists = err == nil

	var target ProjectLockFile
	if plan.TargetExists {
		target = ReadProjectLock(cwd)
	}
	inTarget := func(section, name string) bool {
		switch section {
		case "skills":
			_, ok := target.Skills[name]
			return ok
		case "bundles":
			_, ok := target.Knowledge[name]
			return ok
		case "plugins":
			_, ok := target.Plugins[name]
			return ok
		}
		return false
	}

	for _, fname := range LegacyProjectLockNames {
		path := filepath.Join(cwd, fname)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		sections, isTombstone, err := strictReadLegacy(path)
		if err != nil {
			return plan, err
		}
		if isTombstone {
			continue
		}
		count := 0
		for section, names := range sections {
			count += len(names)
			if plan.TargetExists {
				for _, name := range names {
					if !inTarget(section, name) {
						plan.Orphaned = append(plan.Orphaned, fname+": "+name)
					}
				}
			}
		}
		plan.Legacy[fname] = count
	}
	return plan, nil
}

// ExecuteProjectMigration performs the migration PlanProjectMigration
// described: it materializes mdm-lock.json (when it does not exist yet,
// from the legacy fallback read), replaces skills-lock.json with a
// tombstone (or deletes it with tombstone=false), and deletes the other
// legacy files. Call PlanProjectMigration first; a plan with orphaned
// entries discards them, so the caller must confirm that explicitly.
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
		// ReadProjectLock assembles the legacy files; writing persists them.
		if err := WriteProjectLock(ReadProjectLock(cwd), cwd); err != nil {
			return err
		}
	}
	for _, fname := range LegacyProjectLockNames {
		if _, ok := plan.Legacy[fname]; !ok {
			continue
		}
		path := filepath.Join(cwd, fname)
		if fname == "skills-lock.json" && tombstone {
			if err := os.WriteFile(path, []byte(SkillsTombstone), 0600); err != nil {
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

// MigrateGlobalState moves the v1 global skills-lock.json into
// mdm-state.json and deletes the old file. The global file is per-machine
// state, so no tombstone is left behind.
func MigrateGlobalState() error {
	legacy, ok := LegacyGlobalLockExists()
	if !ok {
		return nil
	}
	if _, err := os.Stat(GetGlobalStatePath()); err != nil {
		// ReadGlobalState falls back to the legacy file; writing persists it.
		if err := WriteGlobalState(ReadGlobalState()); err != nil {
			return err
		}
	}
	return os.Remove(legacy)
}
