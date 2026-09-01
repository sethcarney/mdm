package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sethcarney/mdm/internal/agent"
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
	// InstallModeBackfill is the install mode inferred from what is on
	// disk, empty when there is nothing to record. It covers both shapes:
	// an existing mdm.lock with no mode recorded yet (a project that
	// migrated to v2 before this feature existed, so there are no legacy
	// files left to trigger a migration), and the mode a fresh migration
	// will stamp on the lock it is about to write. Planning it for both
	// means --dry-run always names the write it is about to perform.
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
		if target.InstallMode == "" {
			plan.InstallModeBackfill = inferInstallMode(skillNames(target.Skills), target.ConfiguredAgents, false, cwd)
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

	// A fresh migration writes the merged lock, so the mode it will record
	// has to be inferred here rather than inside execution: --dry-run must
	// never hide a write it is about to perform. Only reachable with legacy
	// files present, because the merged lock is otherwise empty and
	// nothing gets written.
	if !plan.TargetExists && len(plan.Legacy) > 0 {
		plan.merged.InstallMode = inferInstallMode(skillNames(plan.merged.Skills), plan.merged.ConfiguredAgents, false, cwd)
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
	if len(d.configuredAgents) > 0 {
		m.merged.ConfiguredAgents = d.configuredAgents
	}
}

// scanAgents returns the agents whose install directories are worth
// scanning for a scope. The recorded list wins when there is one.
//
// It falls back to every agent the scope supports when the list is empty,
// which is a common shape rather than an edge case: configuredAgents only
// records agents the user picked interactively, so `mdm skills add <src>
// -a claude-code -y` installs skills and leaves the list empty. Inferring
// from an empty list would find nothing for exactly the --copy user this
// scan exists to serve. The fallback costs one Lstat per agent per skill
// and only ever reports a mode when a real directory is sitting at an
// agent's install path under a locked skill's name.
func scanAgents(agents []string, global bool) []string {
	if len(agents) > 0 {
		return agents
	}
	var all []string
	for name, cfg := range agent.AllAgents {
		if global && cfg.GlobalSkillsDir == "" {
			continue
		}
		all = append(all, name)
	}
	sort.Strings(all)
	return all
}

// skillNames lists the keys of a skills section, so both scopes can feed
// the same scanner despite holding different entry types.
func skillNames[E any](skills map[string]E) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// inferInstallMode reports the install mode a scope was using, by looking
// at what is actually on disk at each configured agent's install path, or
// at every agent the scope supports when no agents are recorded (see
// scanAgents). A real directory holding a SKILL.md means the user installed
// with --copy; a symlink means the default symlink mode. Absent, unreadable,
// or a directory that is not a skill is no evidence.
//
// The scan deliberately never looks at .agents/skills/<name>. That is the
// canonical directory, which a symlink install creates as a real directory
// and then points the per-agent paths at, so it reads as "copy" for every
// project regardless of mode. Only the per-agent install paths distinguish
// the two, which is also what re-materialization walks.
//
// The contract is InstallModeCopy or empty, never the literal "symlink":
// no evidence must leave the key absent so a later migration can still
// find it.
//
// This runs at migration time only. Scanning on every lock read would be
// both slow and surprising, and the recorded mode is authoritative once
// it exists.
func inferInstallMode(names, agents []string, global bool, cwd string) string {
	agents = scanAgents(agents, global)
	for _, name := range names {
		for _, agentName := range agents {
			// An agent that reads the shared .agents/skills directory has
			// no install path of its own: it reads the canonical directory
			// itself, which is a real directory in both modes. It is never
			// evidence either way.
			if agent.UsesSharedSkillsDir(agentName) {
				continue
			}
			// agent.SkillsInstallDir is the same resolution the installer
			// and the re-materializer use, so what this scan reads can never
			// drift from where installs actually land.
			installDir := agent.SkillsInstallDir(agentName, global, cwd)
			if installDir == "" {
				continue
			}
			// Lock keys are already stored in sanitized form, so the key
			// is the directory name.
			candidate := filepath.Join(installDir, name)
			info, err := os.Lstat(candidate)
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink == 0 && info.IsDir() {
				// Every mdm-installed skill directory holds a SKILL.md, and
				// `mdm doctor` reports one that does not. Requiring it keeps
				// an unrelated directory that merely shares a locked skill's
				// name from reading as a copy install, which the all-agents
				// fallback above otherwise makes easy to hit.
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
// described: it writes the lock the plan assembled to mdm.lock (when
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
		// plan.merged.InstallMode was already inferred while planning, so
		// what gets written here is exactly what --dry-run reported.
		if err := WriteProjectLock(plan.merged, cwd); err != nil {
			return err
		}
	}
	// A project that already migrated to v2 has no legacy files left and
	// TargetExists is true, so the branch above never runs, but its lock
	// may still have no InstallMode recorded. Backfill that independent of
	// whether any legacy file was absorbed above.
	if plan.TargetExists && plan.InstallModeBackfill != "" {
		target, err := readProjectLockE(cwd)
		if err != nil {
			return err
		}
		if target.InstallMode == "" {
			target.InstallMode = plan.InstallModeBackfill
			if err := WriteProjectLock(target, cwd); err != nil {
				return err
			}
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
	// InstallModeBackfill is the install mode inferred from what is on
	// disk for the global scope, empty when there is nothing to record.
	// A user who ran `mdm skills add -g --copy` before the switch existed
	// has no mode recorded; this is their recovery path, and it mirrors
	// the project-scope backfill.
	InstallModeBackfill string
	needed              bool
	merged              GlobalState
}

// Needed reports whether there is anything to migrate: a legacy global
// lock to absorb, or an install mode backfill pending.
func (m GlobalMigration) Needed() bool { return m.needed || m.InstallModeBackfill != "" }

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
			plan.InstallModeBackfill = inferInstallMode(skillNames(target.Skills), target.ConfiguredAgents, true, "")
		}
		return plan, nil
	}

	if !hasLegacy {
		return plan, nil
	}
	plan.merged = parsed
	plan.merged.InstallMode = inferInstallMode(skillNames(parsed.Skills), parsed.ConfiguredAgents, true, "")
	plan.InstallModeBackfill = plan.merged.InstallMode
	return plan, nil
}

// ExecuteGlobalMigration writes the state PlanGlobalMigration assembled to
// mdm-state.json (when it does not exist yet), backfills the install mode
// on an existing mdm-state.json that has none, and deletes the v1 global
// skills-lock.json. The global file is per-machine state, so no tombstone
// is left behind. A plan with orphaned entries discards them, so the
// caller must confirm that explicitly.
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
	// An mdm-state.json that predates the install-mode switch has no mode
	// recorded even though there is no legacy file left to absorb. Backfill
	// it independently, the same way the project scope does.
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
