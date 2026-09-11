package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/harness"
)

// v1 to v2 migration: everyday v2 reads tolerate some legacy oddities (a
// versionless file, a tombstone); migration must not, because it retires the
// source files. Every step re-parses the legacy files strictly and aborts on
// anything unexpected. Execution materializes the target from the exact parse
// the plan validated, never from a second, more tolerant read.

// LegacyProjectLockNames are the v1 project lock files, in display order.
var LegacyProjectLockNames = []string{"skills-lock.json", "knowledge-lock.json", "plugins-lock.json"}

// SkillsTombstone is written over skills-lock.json after a migration. The
// version is deliberately newer than any v1 lock, so a patched v1 binary
// refuses it and prints an upgrade pointer. Older v1 binaries read it as a
// valid empty lock, so the _comment exists for the human who finds it.
const SkillsTombstone = `{
  "version": 2,
  "skills": {},
  "_moved": "` + ProjectLockName + `",
  "_comment": "This project migrated to mdm v2 and its lock now lives in ` + ProjectLockName + `. Patched v1 releases refuse this file with an upgrade pointer; older v1 releases read it as empty and will install nothing from it - upgrade mdm instead. Delete this file once nothing runs a v1 mdm against this project."
}
`

// ProjectMigration describes what `mdm migrate` would do in a project.
type ProjectMigration struct {
	// Legacy maps each legacy file name that exists to the number of
	// entries it holds.
	Legacy map[string]int
	// TargetExists reports whether mdm.lock is already present.
	TargetExists bool
	// Orphaned lists legacy entries (as "file: name") that are absent from
	// the existing mdm.lock and would be discarded by retiring the
	// legacy files. Only populated when TargetExists.
	Orphaned []string
	// InstallModeBackfill is the install mode inferred from disk, empty when
	// there is nothing to record. Planned here so --dry-run names the write.
	InstallModeBackfill string
	// merged is the target lock assembled from the strictly parsed legacy
	// files. Execution writes exactly this when the target does not exist.
	merged ProjectLockFile
}

// Needed reports whether there is anything to migrate: legacy files to
// absorb, or an install mode backfill pending on an existing lock.
func (m ProjectMigration) Needed() bool { return len(m.Legacy) > 0 || m.InstallModeBackfill != "" }

// legacyFileData is one legacy file, fully decoded into the typed entry
// structs so nothing survives planning that execution could not carry over.
type legacyFileData struct {
	skills              map[string]LocalSkillLockEntry
	bundles             map[string]KnowledgeLockEntry
	plugins             map[string]PluginLockEntry
	configuredHarnesses []string
	isTombstone         bool
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
		Version             int                            `json:"version"`
		Skills              map[string]LocalSkillLockEntry `json:"skills"`
		Bundles             map[string]KnowledgeLockEntry  `json:"bundles"`
		Plugins             map[string]PluginLockEntry     `json:"plugins"`
		ConfiguredHarnesses []string                       `json:"configuredAgents"`
		MovedNote           string                         `json:"_moved"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return d, fmt.Errorf("%s is not valid JSON: %w", filepath.Base(path), err)
	}
	if raw.MovedNote != "" {
		// An earlier migration's tombstone - nothing left to carry over.
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
	d.configuredHarnesses = raw.ConfiguredHarnesses
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
		if target.InstallMode == "" {
			plan.InstallModeBackfill = inferInstallMode(skillNames(target.Skills), target.ConfiguredHarnesses, false, cwd)
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
			plan.Orphaned = append(plan.Orphaned, orphanedIn(target, fname, d)...)
		}
		plan.absorb(d)
		plan.Legacy[fname] = d.count()
	}
	sort.Strings(plan.Orphaned)

	// A fresh migration infers its mode while planning, so --dry-run never
	// hides a write. Only legacy files make the merged lock non-empty.
	if !plan.TargetExists && len(plan.Legacy) > 0 {
		plan.merged.InstallMode = inferInstallMode(skillNames(plan.merged.Skills), plan.merged.ConfiguredHarnesses, false, cwd)
		plan.InstallModeBackfill = plan.merged.InstallMode
	}
	return plan, nil
}

// orphanedIn lists d's entries that are absent from the existing target
// lock, as "file: name".
func orphanedIn(target ProjectLockFile, fname string, d legacyFileData) []string {
	var orphans []string
	for name := range d.skills {
		if _, ok := target.Skills[name]; !ok {
			orphans = append(orphans, fname+": "+name)
		}
	}
	for name := range d.bundles {
		if _, ok := target.Knowledge[name]; !ok {
			orphans = append(orphans, fname+": "+name)
		}
	}
	for name := range d.plugins {
		if _, ok := target.Plugins[name]; !ok {
			orphans = append(orphans, fname+": "+name)
		}
	}
	return orphans
}

// absorb folds one legacy file's entries into the plan's merged lock.
func (m *ProjectMigration) absorb(d legacyFileData) {
	for name, e := range d.skills {
		m.merged.Skills[name] = e
	}
	for name, e := range d.bundles {
		m.merged.Knowledge[name] = e
	}
	for name, e := range d.plugins {
		m.merged.Plugins[name] = e
	}
	// v1 spells this configuredAgents. v1 is frozen and in the wild, so the
	// legacy reader above keeps that tag; the rename lives in v2 only, and
	// this is the one place the two formats meet.
	if len(d.configuredHarnesses) > 0 {
		m.merged.ConfiguredHarnesses = d.configuredHarnesses
	}
}

// scanHarnesses returns the harnesses whose install directories are worth
// scanning: the recorded list, or every harness the scope supports when it is
// empty. An empty list is common, since configuredHarnesses records only
// interactive picks.
func scanHarnesses(harnesses []string, global bool) []string {
	if len(harnesses) > 0 {
		return harnesses
	}
	var all []string
	for name, cfg := range harness.AllHarnesses {
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		all = append(all, name)
	}
	sort.Strings(all)
	return all
}

// skillNames lists the keys of a skills section of either entry type.
func skillNames[E any](skills map[string]E) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// inferInstallMode reports the mode a scope was using from what is on disk at
// each harness's install path: a real directory holding a SKILL.md means copy,
// anything else is no evidence. It returns InstallModeCopy or "", never
// "symlink", so no evidence leaves the key absent. It never reads
// .agents/skills, which a symlink install creates as a real directory.
func inferInstallMode(names, harnesses []string, global bool, cwd string) string {
	harnesses = scanHarnesses(harnesses, global)
	for _, name := range names {
		for _, harnessName := range harnesses {
			// A shared-dir harness's install path is the canonical directory,
			// real in both modes: never evidence.
			if harness.UsesSharedSkillsDir(harnessName) {
				continue
			}
			// The same resolution the installer uses, so this cannot drift
			// from where installs land. Lock keys are already sanitized.
			installDir := harness.SkillsInstallDir(harnessName, global, cwd)
			if installDir == "" {
				continue
			}
			candidate := filepath.Join(installDir, name)
			info, err := os.Lstat(candidate)
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
				// Requiring a SKILL.md keeps an unrelated directory that
				// shares a locked skill's name from reading as a copy install.
				if _, err := os.Stat(filepath.Join(candidate, "SKILL.md")); err != nil {
					continue
				}
				return InstallModeCopy
			}
		}
	}
	return ""
}

// ExecuteProjectMigration performs the migration PlanProjectMigration
// described: it writes the assembled lock to mdm.lock, replaces
// skills-lock.json with a tombstone (or deletes it with tombstone=false), and
// deletes the other legacy files. A plan with orphaned entries discards them.
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
		// plan.merged already carries the mode --dry-run reported.
		if err := WriteProjectLock(plan.merged, cwd); err != nil {
			return err
		}
	}
	// An existing lock may still have no mode recorded, legacy files or not.
	if plan.TargetExists && plan.InstallModeBackfill != "" {
		if err := backfillProjectInstallMode(cwd, plan.InstallModeBackfill); err != nil {
			return err
		}
	}
	return retireLegacyProjectFiles(cwd, plan.Legacy, tombstone)
}

// backfillProjectInstallMode records mode on an existing mdm.lock that has
// none. A lock that gained a mode since the plan was made is left alone.
func backfillProjectInstallMode(cwd, mode string) error {
	target, err := readProjectLockE(cwd)
	if err != nil {
		return err
	}
	if target.InstallMode != "" {
		return nil
	}
	target.InstallMode = mode
	return WriteProjectLock(target, cwd)
}

// retireLegacyProjectFiles removes the legacy lock files the plan absorbed,
// leaving a tombstone in place of skills-lock.json when asked to.
func retireLegacyProjectFiles(cwd string, legacy map[string]int, tombstone bool) error {
	for _, fname := range LegacyProjectLockNames {
		if _, ok := legacy[fname]; !ok {
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
	// InstallModeBackfill is the install mode inferred from disk for the
	// global scope, empty when there is nothing to record. It mirrors the
	// project-scope backfill.
	InstallModeBackfill string
	needed              bool
	merged              GlobalState
}

// Needed reports whether there is anything to migrate: a legacy global
// lock to absorb, or an install mode backfill pending.
func (m GlobalMigration) Needed() bool { return m.needed || m.InstallModeBackfill != "" }

// strictReadLegacyGlobal parses the v1 global lock with no empty-on-error
// tolerance for broken JSON. A lock below the last v1 version migrates as
// empty - v1 itself deliberately discarded those on read.
func strictReadLegacyGlobal(path string) (GlobalState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return EmptyGlobalState(), err
	}
	var legacy struct {
		Version             int                       `json:"version"`
		Skills              map[string]SkillLockEntry `json:"skills"`
		Dismissed           DismissedPrompts          `json:"dismissed"`
		ConfiguredHarnesses []string                  `json:"configuredAgents"`
		Experimental        []string                  `json:"experimental"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return EmptyGlobalState(), fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if legacy.Skills == nil || legacy.Version < legacyGlobalLockVersion {
		return EmptyGlobalState(), nil
	}
	return GlobalState{
		Version:             globalStateVersion,
		Skills:              legacy.Skills,
		Dismissed:           legacy.Dismissed,
		ConfiguredHarnesses: legacy.ConfiguredHarnesses,
		Experimental:        legacy.Experimental,
	}, nil
}

// PlanGlobalMigration inspects the machine's global state and reports what a
// migration would do. It fails on a legacy file it cannot parse: the file is
// per-machine state with no copy in version control.
func PlanGlobalMigration() (GlobalMigration, error) {
	var plan GlobalMigration
	var parsed GlobalState
	legacy, hasLegacy := LegacyGlobalLockExists()
	if hasLegacy {
		plan.needed = true
		plan.LegacyPath = legacy
		var err error
		parsed, err = strictReadLegacyGlobal(legacy)
		if err != nil {
			return plan, err
		}
	}

	if _, err := os.Stat(GetGlobalStatePath()); err == nil {
		plan.TargetExists = true
		target, err := readGlobalStateE()
		if err != nil {
			return plan, err
		}
		if hasLegacy {
			for name := range parsed.Skills {
				if _, ok := target.Skills[name]; !ok {
					plan.Orphaned = append(plan.Orphaned, name)
				}
			}
			sort.Strings(plan.Orphaned)
		}
		if target.InstallMode == "" {
			plan.InstallModeBackfill = inferInstallMode(skillNames(target.Skills), target.ConfiguredHarnesses, true, "")
		}
		return plan, nil
	}

	if !hasLegacy {
		return plan, nil
	}
	plan.merged = parsed
	plan.merged.InstallMode = inferInstallMode(skillNames(parsed.Skills), parsed.ConfiguredHarnesses, true, "")
	plan.InstallModeBackfill = plan.merged.InstallMode
	return plan, nil
}

// ExecuteGlobalMigration writes the state PlanGlobalMigration assembled to
// mdm-state.json (when it does not exist yet), backfills the install mode on an
// existing mdm-state.json that has none, and deletes the v1 global
// skills-lock.json. A plan with orphaned entries discards them.
func ExecuteGlobalMigration() error {
	plan, err := PlanGlobalMigration()
	if err != nil {
		return err
	}
	if !plan.Needed() {
		return nil
	}
	if !plan.TargetExists && plan.LegacyPath != "" {
		if err := WriteGlobalState(plan.merged); err != nil {
			return err
		}
	}
	// An existing state file may still have no mode recorded.
	if plan.TargetExists && plan.InstallModeBackfill != "" {
		state, err := readGlobalStateE()
		if err != nil {
			return err
		}
		if state.InstallMode == "" {
			state.InstallMode = plan.InstallModeBackfill
			if err := WriteGlobalState(state); err != nil {
				return err
			}
		}
	}
	if plan.LegacyPath == "" {
		return nil
	}
	return os.Remove(plan.LegacyPath)
}
