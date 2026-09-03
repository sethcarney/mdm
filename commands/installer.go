package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sethcarney/mdm/internal/harness"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/skill"
)

type InstallMode string

const (
	InstallModeSymlink InstallMode = "symlink"
	InstallModeCopy    InstallMode = "copy"
)

type InstallResult struct {
	Success       bool
	Path          string
	CanonicalPath string
	Mode          InstallMode
	SymlinkFailed bool
	Error         string
}

func sanitizeName(name string) string {
	lower := strings.ToLower(name)
	// Replace non-alphanumeric/dot/underscore with hyphen
	re := regexp.MustCompile(`[^a-z0-9._]+`)
	sanitized := re.ReplaceAllString(lower, "-")
	// Remove leading/trailing dots and hyphens
	re2 := regexp.MustCompile(`^[.\-]+|[.\-]+$`)
	sanitized = re2.ReplaceAllString(sanitized, "")
	if len(sanitized) > 255 {
		sanitized = sanitized[:255]
	}
	if sanitized == "" {
		return "unnamed-skill"
	}
	return sanitized
}

func skillNameMatches(name, filter string) bool {
	return strings.EqualFold(name, filter) || strings.EqualFold(sanitizeName(name), sanitizeName(filter))
}

func isPathSafe(basePath, targetPath string) bool {
	base, err1 := filepath.Abs(basePath)
	target, err2 := filepath.Abs(targetPath)
	if err1 != nil || err2 != nil {
		return false
	}
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	return target == base || strings.HasPrefix(target, base+string(filepath.Separator))
}

// isInsideOrEqual reports whether target is equal to root or is a descendant
// of root. Both paths must already be absolute and clean.
func isInsideOrEqual(target, root string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}

func getCanonicalSkillsDir(global bool, cwd string) string {
	return harness.CanonicalSkillsDir(global, cwd)
}

// getHarnessBaseDir resolves where a harness's skills live for a scope. The
// resolution itself lives in the harness package so that install-mode inference
// in internal/lock computes exactly the same paths; see harness.SkillsInstallDir.
func getHarnessBaseDir(harnessName string, global bool, cwd string) string {
	return harness.SkillsInstallDir(harnessName, global, cwd)
}

func cleanAndCreateDir(path string) error {
	_ = os.RemoveAll(path)
	return os.MkdirAll(path, 0755)
}

func resolveParentSymlinks(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return abs
	}
	return filepath.Join(real, base)
}

// symlinkFn creates the link in createSymlink. It is a package-level variable
// only so a test can force the symlink-to-copy fallback deterministically;
// production code always goes through os.Symlink. Like the other test seams
// in this package it is shared mutable state, so no test may swap it while
// running in parallel with another test that installs.
var symlinkFn = os.Symlink

func createSymlink(target, linkPath string) bool {
	resolvedTarget, _ := filepath.Abs(target)
	resolvedLink, _ := filepath.Abs(linkPath)

	// Check if they resolve to the same real path
	realTarget, _ := filepath.EvalSymlinks(resolvedTarget)
	realLink, _ := filepath.EvalSymlinks(resolvedLink)
	if realTarget != "" && realLink != "" && realTarget == realLink {
		return true
	}

	// Also check with parent symlinks resolved
	if resolveParentSymlinks(target) == resolveParentSymlinks(linkPath) {
		return true
	}

	// Remove existing link/dir at linkPath
	if info, err := os.Lstat(linkPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			existing, _ := os.Readlink(linkPath)
			if existing != "" {
				existingResolved, _ := filepath.Abs(filepath.Join(filepath.Dir(linkPath), existing))
				if existingResolved == resolvedTarget {
					return true
				}
			}
			_ = os.Remove(linkPath)
		} else {
			_ = os.RemoveAll(linkPath)
		}
	}

	// Create parent directory
	linkDir := filepath.Dir(linkPath)
	if err := os.MkdirAll(linkDir, 0755); err != nil {
		return false
	}

	// Compute relative path for the symlink
	realLinkDir := resolveParentSymlinks(linkDir)
	relPath, err := filepath.Rel(realLinkDir, target)
	if err != nil {
		relPath, err = filepath.Rel(linkDir, target)
		if err != nil {
			return false
		}
	}

	if err := symlinkFn(relPath, linkPath); err != nil {
		return false
	}
	return true
}

var excludeFiles = map[string]bool{"metadata.json": true}
var excludeDirs = map[string]bool{".git": true, "__pycache__": true, "__pypackages__": true}

func isExcluded(name string, isDir bool) bool {
	if excludeFiles[name] {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	if isDir && excludeDirs[name] {
		return true
	}
	return false
}

func copyDirectory(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if isExcluded(e.Name(), e.IsDir()) {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				// Skip broken symlinks
				info, lerr := os.Lstat(srcPath)
				if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
					continue
				}
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	// Dereference symlinks
	realSrc, err := filepath.EvalSymlinks(src)
	if err != nil {
		return err
	}
	in, err := os.Open(realSrc)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

type copyFunc func(dst string) error

func writeSkillFiles(targetDir string, files []struct{ Path, Contents string }) error {
	for _, f := range files {
		fullPath := filepath.Join(targetDir, f.Path)
		if !isPathSafe(targetDir, fullPath) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(fullPath, []byte(f.Contents), 0644); err != nil {
			return err
		}
	}
	return nil
}

func performSymlinkInstall(canonicalDir, harnessDir, harnessName string, global bool, mode InstallMode, cp copyFunc) InstallResult {
	if err := refuseIfPluginOwned(canonicalDir, global); err != nil {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
	}
	if err := cleanAndCreateDir(canonicalDir); err != nil {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
	}
	if err := cp(canonicalDir); err != nil {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
	}
	if global && harness.UsesSharedSkillsDir(harnessName) {
		return InstallResult{Success: true, Path: canonicalDir, CanonicalPath: canonicalDir, Mode: InstallModeSymlink}
	}
	if createSymlink(canonicalDir, harnessDir) {
		return InstallResult{Success: true, Path: harnessDir, CanonicalPath: canonicalDir, Mode: InstallModeSymlink}
	}
	if err := cleanAndCreateDir(harnessDir); err != nil {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
	}
	if err := cp(harnessDir); err != nil {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
	}
	return InstallResult{Success: true, Path: harnessDir, CanonicalPath: canonicalDir, Mode: InstallModeSymlink, SymlinkFailed: true}
}

// refuseIfPluginOwned blocks a standalone skill install from clobbering a
// skill that an installed plugin owns; the plugins lock section is the sole
// manager of those. Only project scope can be plugin-owned.
func refuseIfPluginOwned(canonicalDir string, global bool) error {
	if global {
		return nil
	}
	cwd, _ := os.Getwd()
	if owner, _ := skillDirOwner(canonicalDir, cwd); owner != "" {
		return fmt.Errorf("skill is managed by plugin %q - use 'mdm plugins remove %s' first", owner, owner)
	}
	return nil
}

func validateHarnessInstall(harnessName string, global bool, mode InstallMode) (*harness.HarnessConfig, *InstallResult) {
	a := harness.AllHarnesses[harnessName]
	if a == nil {
		r := InstallResult{Success: false, Path: "", Mode: mode, Error: "unknown harness: " + harnessName}
		return nil, &r
	}
	if global && a.GlobalSkillsDir == "" {
		r := InstallResult{Success: false, Path: "", Mode: mode, Error: a.DisplayName + " does not support global skill installation"}
		return nil, &r
	}
	return a, nil
}

func installSkillForHarness(s *skill.Skill, harnessName string, global bool, mode InstallMode) InstallResult {
	cwd, _ := os.Getwd()
	if _, errResult := validateHarnessInstall(harnessName, global, mode); errResult != nil {
		return *errResult
	}

	rawName := s.Name
	if rawName == "" {
		rawName = filepath.Base(s.Path)
	}
	skillName := sanitizeName(rawName)

	canonicalBase := getCanonicalSkillsDir(global, cwd)
	canonicalDir := filepath.Join(canonicalBase, skillName)
	harnessBase := getHarnessBaseDir(harnessName, global, cwd)
	harnessDir := filepath.Join(harnessBase, skillName)

	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(harnessBase, harnessDir) {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: "potential path traversal detected"}
	}

	if mode == InstallModeCopy {
		if err := cleanAndCreateDir(harnessDir); err != nil {
			return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
		}
		if err := copyDirectory(s.Path, harnessDir); err != nil {
			return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
		}
		return InstallResult{Success: true, Path: harnessDir, Mode: InstallModeCopy}
	}

	return performSymlinkInstall(canonicalDir, harnessDir, harnessName, global, mode,
		func(dst string) error { return copyDirectory(s.Path, dst) })
}

func installSkillFilesForHarness(skillName string, files []struct{ Path, Contents string }, harnessName string, global bool, mode InstallMode) InstallResult {
	cwd, _ := os.Getwd()
	if _, errResult := validateHarnessInstall(harnessName, global, mode); errResult != nil {
		return *errResult
	}

	sName := sanitizeName(skillName)
	canonicalBase := getCanonicalSkillsDir(global, cwd)
	canonicalDir := filepath.Join(canonicalBase, sName)
	harnessBase := getHarnessBaseDir(harnessName, global, cwd)
	harnessDir := filepath.Join(harnessBase, sName)

	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(harnessBase, harnessDir) {
		return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: "potential path traversal detected"}
	}

	cp := func(dst string) error { return writeSkillFiles(dst, files) }

	if mode == InstallModeCopy {
		if err := cleanAndCreateDir(harnessDir); err != nil {
			return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
		}
		if err := cp(harnessDir); err != nil {
			return InstallResult{Success: false, Path: harnessDir, Mode: mode, Error: err.Error()}
		}
		return InstallResult{Success: true, Path: harnessDir, Mode: InstallModeCopy}
	}

	return performSymlinkInstall(canonicalDir, harnessDir, harnessName, global, mode, cp)
}

func isSkillInstalled(skillName, harnessName string, global bool) bool {
	a := harness.AllHarnesses[harnessName]
	if a == nil {
		return false
	}
	if global && a.GlobalSkillsDir == "" {
		return false
	}
	cwd, _ := os.Getwd()
	sName := sanitizeName(skillName)
	var targetBase string
	if global {
		targetBase = a.GlobalSkillsDir
	} else {
		targetBase = filepath.Join(cwd, a.SkillsDir)
	}
	skillDir := filepath.Join(targetBase, sName)
	if !isPathSafe(targetBase, skillDir) {
		return false
	}
	_, err := os.Stat(skillDir)
	return err == nil
}

func getCanonicalPath(skillName string, global bool) string {
	cwd, _ := os.Getwd()
	sName := sanitizeName(skillName)
	canonicalBase := getCanonicalSkillsDir(global, cwd)
	return filepath.Join(canonicalBase, sName)
}

type InstalledSkill struct {
	Name          string
	Description   string
	License       string `json:"license,omitempty"`
	Compatibility string `json:"compatibility,omitempty"`
	Ref           string `json:"ref,omitempty"`
	Plugin        string `json:"plugin,omitempty"` // owning plugin, when installed via mdm plugins
	Path          string
	CanonicalPath string
	Scope         string // "project" or "global"
	// Harnesses keeps the JSON key "Agents": `mdm skills list --json` output
	// is a stable external contract this commit does not change.
	Harnesses []string `json:"Agents"`
}

type scopeEntry struct {
	isGlobal    bool
	path        string
	harnessType string
}

func filterHarnessesToCheck(detected []string, harnessFilter []string) []string {
	if len(harnessFilter) == 0 {
		return detected
	}
	var filtered []string
	for _, a := range detected {
		for _, f := range harnessFilter {
			if a == f {
				filtered = append(filtered, a)
				break
			}
		}
	}
	return filtered
}

func harnessDirForScope(harnessName string, isGlobal bool, cwd string) string {
	a := harness.AllHarnesses[harnessName]
	if a == nil {
		return ""
	}
	if isGlobal {
		return a.GlobalSkillsDir
	}
	return filepath.Join(cwd, a.SkillsDir)
}

func scopeEntryExists(scopes []scopeEntry, path string, isGlobal bool) bool {
	for _, s := range scopes {
		if s.path == path && s.isGlobal == isGlobal {
			return true
		}
	}
	return false
}

func appendDetectedHarnessScopes(scopes []scopeEntry, harnessesToCheck []string, isGlobal bool, cwd string) []scopeEntry {
	for _, harnessName := range harnessesToCheck {
		a := harness.AllHarnesses[harnessName]
		if a == nil || (isGlobal && a.GlobalSkillsDir == "") {
			continue
		}
		harnessDir := harnessDirForScope(harnessName, isGlobal, cwd)
		if !scopeEntryExists(scopes, harnessDir, isGlobal) {
			scopes = append(scopes, scopeEntry{isGlobal: isGlobal, path: harnessDir, harnessType: harnessName})
		}
	}
	return scopes
}

func appendUndetectedHarnessScopes(scopes []scopeEntry, harnessesToCheck []string, configured []string, isGlobal bool, cwd string) []scopeEntry {
	for harnessName, a := range harness.AllHarnesses {
		if contains(harnessesToCheck, harnessName) {
			continue
		}
		// Only consider harnesses that were explicitly configured (saved in the
		// lock file). Without this guard, any harness whose SkillsDir coincides
		// with a directory that happens to exist on disk (e.g. openclaw →
		// "./skills") would be mistakenly treated as an install target.
		if !contains(configured, harnessName) {
			continue
		}
		if isGlobal && a.GlobalSkillsDir == "" {
			continue
		}
		harnessDir := harnessDirForScope(harnessName, isGlobal, cwd)
		if scopeEntryExists(scopes, harnessDir, isGlobal) {
			continue
		}
		if _, statErr := os.Stat(harnessDir); statErr == nil {
			scopes = append(scopes, scopeEntry{isGlobal: isGlobal, path: harnessDir, harnessType: harnessName})
		}
	}
	return scopes
}

func buildScopeEntries(harnessesToCheck []string, scopeTypes []bool, cwd string) []scopeEntry {
	var scopes []scopeEntry
	for _, isGlobal := range scopeTypes {
		configured := lock.GetConfiguredHarnesses(isGlobal, cwd)
		scopes = append(scopes, scopeEntry{isGlobal: isGlobal, path: getCanonicalSkillsDir(isGlobal, cwd)})
		scopes = appendDetectedHarnessScopes(scopes, harnessesToCheck, isGlobal, cwd)
		scopes = appendUndetectedHarnessScopes(scopes, harnessesToCheck, configured, isGlobal, cwd)
	}
	return scopes
}

func parseSkillInDir(skillDir string) *skill.Skill {
	skillMdPath := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillMdPath); err != nil {
		return nil
	}
	s, err := skill.ParseSkillMd(skillMdPath, true)
	if err != nil || s == nil {
		return nil
	}
	return s
}

func mergeHarnessSkillIntoMap(skillsMap map[string]*InstalledSkill, mapKey, harnessType string, s *skill.Skill, skillDir, scopeKey string) {
	if existing, ok := skillsMap[mapKey]; ok {
		if !contains(existing.Harnesses, harnessType) {
			existing.Harnesses = append(existing.Harnesses, harnessType)
		}
	} else {
		skillsMap[mapKey] = &InstalledSkill{
			Name: s.Name, Description: s.Description,
			License: s.License, Compatibility: s.Compatibility,
			Path: skillDir, CanonicalPath: skillDir,
			Scope: scopeKey, Harnesses: []string{harnessType},
		}
	}
}

func harnessHasSkill(harnessBase, dirName, sName, skillName string) bool {
	for _, name := range []string{dirName, sName} {
		harnessSkillDir := filepath.Join(harnessBase, name)
		if !isPathSafe(harnessBase, harnessSkillDir) {
			continue
		}
		if _, err := os.Stat(harnessSkillDir); err == nil {
			return true
		}
	}
	harnessEntries, err := os.ReadDir(harnessBase)
	if err != nil {
		return false
	}
	for _, ae := range harnessEntries {
		if !ae.IsDir() {
			continue
		}
		candidateDir := filepath.Join(harnessBase, ae.Name())
		candidateSkill, err := skill.ParseSkillMd(filepath.Join(candidateDir, "SKILL.md"), true)
		if err == nil && candidateSkill != nil && candidateSkill.Name == skillName {
			return true
		}
	}
	return false
}

func findHarnessesForSkill(s *skill.Skill, dirName string, harnessesToCheck []string, isGlobal bool, cwd string) []string {
	sName := sanitizeName(s.Name)
	var result []string
	for _, harnessName := range harnessesToCheck {
		a := harness.AllHarnesses[harnessName]
		if a == nil || (isGlobal && a.GlobalSkillsDir == "") {
			continue
		}
		harnessBase := harnessDirForScope(harnessName, isGlobal, cwd)
		if harnessHasSkill(harnessBase, dirName, sName, s.Name) {
			result = append(result, harnessName)
		}
	}
	return result
}

func mergeCanonicalSkillIntoMap(skillsMap map[string]*InstalledSkill, mapKey string, harnesses []string, s *skill.Skill, skillDir, scopeKey string) {
	if existing, ok := skillsMap[mapKey]; ok {
		for _, ag := range harnesses {
			if !contains(existing.Harnesses, ag) {
				existing.Harnesses = append(existing.Harnesses, ag)
			}
		}
	} else {
		skillsMap[mapKey] = &InstalledSkill{
			Name: s.Name, Description: s.Description,
			License: s.License, Compatibility: s.Compatibility,
			Path: skillDir, CanonicalPath: skillDir,
			Scope: scopeKey, Harnesses: harnesses,
		}
	}
}

// isSkillDirEntry reports whether a scope-dir entry can hold a skill: a
// plain directory, or a symlinked directory - plugin skills link the
// canonical dir into the plugin's own directory rather than copying.
func isSkillDirEntry(base string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(base, e.Name()))
	return err == nil && info.IsDir()
}

func populateScopeSkills(scope scopeEntry, harnessesToCheck []string, cwd string, skillsMap map[string]*InstalledSkill) {
	entries, err := os.ReadDir(scope.path)
	if err != nil {
		return
	}
	scopeKey := "project"
	if scope.isGlobal {
		scopeKey = "global"
	}
	for _, e := range entries {
		if !isSkillDirEntry(scope.path, e) {
			continue
		}
		skillDir := filepath.Join(scope.path, e.Name())
		s := parseSkillInDir(skillDir)
		if s == nil {
			continue
		}
		mapKey := scopeKey + ":" + s.Name
		if scope.harnessType != "" {
			mergeHarnessSkillIntoMap(skillsMap, mapKey, scope.harnessType, s, skillDir, scopeKey)
			continue
		}
		harnesses := findHarnessesForSkill(s, e.Name(), harnessesToCheck, scope.isGlobal, cwd)
		mergeCanonicalSkillIntoMap(skillsMap, mapKey, harnesses, s, skillDir, scopeKey)
	}
}

func shortenPath(fullPath, cwd string) string {
	home, _ := os.UserHomeDir()
	if fullPath == home || strings.HasPrefix(fullPath, home+string(filepath.Separator)) {
		return "~" + fullPath[len(home):]
	}
	if fullPath == cwd || strings.HasPrefix(fullPath, cwd+string(filepath.Separator)) {
		return "." + fullPath[len(cwd):]
	}
	return fullPath
}

func listInstalledSkills(global *bool, harnessFilter []string) ([]*InstalledSkill, error) {
	cwd, _ := os.Getwd()
	harnessesToCheck := filterHarnessesToCheck(harness.DetectInstalledHarnesses(), harnessFilter)

	scopeTypes := []bool{false, true}
	if global != nil {
		scopeTypes = []bool{*global}
	}
	scopes := buildScopeEntries(harnessesToCheck, scopeTypes, cwd)

	skillsMap := map[string]*InstalledSkill{}
	for _, scope := range scopes {
		populateScopeSkills(scope, harnessesToCheck, cwd, skillsMap)
	}

	globalLock := lock.ReadGlobalState()
	localLock := lock.ReadLocalLock(cwd)
	for _, s := range skillsMap {
		if s.Ref != "" {
			continue
		}
		// Lock files key skills by their sanitized name, not the raw
		// frontmatter name.
		key := sanitizeName(s.Name)
		if entry, ok := globalLock.Skills[key]; ok && entry.Ref != "" {
			s.Ref = entry.Ref
		} else if entry, ok := localLock.Skills[key]; ok && entry.Ref != "" {
			s.Ref = entry.Ref
		}
	}

	for _, s := range skillsMap {
		if s.Scope == "project" {
			s.Plugin = pluginOwningSkill(s.Name, cwd)
		}
	}

	var result []*InstalledSkill
	for _, s := range skillsMap {
		result = append(result, s)
	}
	return result, nil
}

// installWellKnownSkillForHarness installs a well-known skill for a harness.
// (moved from registry/wellknown.go since it depends on installSkillFilesForHarness)
func installWellKnownSkillForHarness(sk *registry.WellKnownSkill, harnessName string, global bool, mode InstallMode) InstallResult {
	var files []struct{ Path, Contents string }
	for path, content := range sk.Files {
		files = append(files, struct{ Path, Contents string }{path, content})
	}
	return installSkillFilesForHarness(sk.InstallName, files, harnessName, global, mode)
}

// linkInstalledSkillToHarness symlinks (or copies) an already-installed skill's
// canonical directory into the given harness's own skills directory.
// Returns true when the harness's skill directory now exists (created or was already present).
func linkInstalledSkillToHarness(skillName, harnessName string, global bool, cwd string) bool {
	a := harness.AllHarnesses[harnessName]
	if a == nil || (global && a.GlobalSkillsDir == "") {
		return false
	}
	canonicalBase := getCanonicalSkillsDir(global, cwd)
	canonicalDir := filepath.Join(canonicalBase, skillName)
	if _, err := os.Stat(canonicalDir); err != nil {
		return false
	}
	harnessBase := getHarnessBaseDir(harnessName, global, cwd)
	harnessDir := filepath.Join(harnessBase, skillName)
	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(harnessBase, harnessDir) {
		return false
	}
	if _, err := os.Stat(harnessDir); err == nil {
		return true // already present
	}
	if createSymlink(canonicalDir, harnessDir) {
		return true
	}
	if err := os.MkdirAll(harnessBase, 0755); err != nil {
		return false
	}
	return copyDirectory(canonicalDir, harnessDir) == nil
}
