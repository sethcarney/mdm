package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sethcarney/mdm/internal/agent"
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

func getCanonicalSkillsDir(global bool, cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	var baseDir string
	if global {
		baseDir, _ = os.UserHomeDir()
	} else {
		baseDir = cwd
	}
	return filepath.Join(baseDir, agent.AgentsDir, agent.SkillsSubdir)
}

func getAgentBaseDir(agentName string, global bool, cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	a := agent.AllAgents[agentName]
	if a == nil {
		return ""
	}
	if agent.UsesSharedSkillsDir(agentName) {
		return getCanonicalSkillsDir(global, cwd)
	}
	if global {
		if a.GlobalSkillsDir == "" {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, a.SkillsDir)
		}
		return a.GlobalSkillsDir
	}
	return filepath.Join(cwd, a.SkillsDir)
}

func cleanAndCreateDir(path string) error {
	os.RemoveAll(path)
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
			os.Remove(linkPath)
		} else {
			os.RemoveAll(linkPath)
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

	if err := os.Symlink(relPath, linkPath); err != nil {
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
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
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

func performSymlinkInstall(canonicalDir, agentDir, agentName string, global bool, mode InstallMode, cp copyFunc) InstallResult {
	if err := cleanAndCreateDir(canonicalDir); err != nil {
		return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
	}
	if err := cp(canonicalDir); err != nil {
		return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
	}
	if global && agent.UsesSharedSkillsDir(agentName) {
		return InstallResult{Success: true, Path: canonicalDir, CanonicalPath: canonicalDir, Mode: InstallModeSymlink}
	}
	if createSymlink(canonicalDir, agentDir) {
		return InstallResult{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: InstallModeSymlink}
	}
	if err := cleanAndCreateDir(agentDir); err != nil {
		return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
	}
	if err := cp(agentDir); err != nil {
		return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
	}
	return InstallResult{Success: true, Path: agentDir, CanonicalPath: canonicalDir, Mode: InstallModeSymlink, SymlinkFailed: true}
}

func validateAgentInstall(agentName string, global bool, mode InstallMode) (*agent.AgentConfig, *InstallResult) {
	a := agent.AllAgents[agentName]
	if a == nil {
		r := InstallResult{Success: false, Path: "", Mode: mode, Error: "unknown agent: " + agentName}
		return nil, &r
	}
	if global && a.GlobalSkillsDir == "" {
		r := InstallResult{Success: false, Path: "", Mode: mode, Error: a.DisplayName + " does not support global skill installation"}
		return nil, &r
	}
	return a, nil
}

func installSkillForAgent(s *skill.Skill, agentName string, global bool, mode InstallMode) InstallResult {
	cwd, _ := os.Getwd()
	if _, errResult := validateAgentInstall(agentName, global, mode); errResult != nil {
		return *errResult
	}

	rawName := s.Name
	if rawName == "" {
		rawName = filepath.Base(s.Path)
	}
	skillName := sanitizeName(rawName)

	canonicalBase := getCanonicalSkillsDir(global, cwd)
	canonicalDir := filepath.Join(canonicalBase, skillName)
	agentBase := getAgentBaseDir(agentName, global, cwd)
	agentDir := filepath.Join(agentBase, skillName)

	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(agentBase, agentDir) {
		return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: "potential path traversal detected"}
	}

	if mode == InstallModeCopy {
		if err := cleanAndCreateDir(agentDir); err != nil {
			return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
		}
		if err := copyDirectory(s.Path, agentDir); err != nil {
			return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
		}
		return InstallResult{Success: true, Path: agentDir, Mode: InstallModeCopy}
	}

	return performSymlinkInstall(canonicalDir, agentDir, agentName, global, mode,
		func(dst string) error { return copyDirectory(s.Path, dst) })
}

func installSkillFilesForAgent(skillName string, files []struct{ Path, Contents string }, agentName string, global bool, mode InstallMode) InstallResult {
	cwd, _ := os.Getwd()
	if _, errResult := validateAgentInstall(agentName, global, mode); errResult != nil {
		return *errResult
	}

	sName := sanitizeName(skillName)
	canonicalBase := getCanonicalSkillsDir(global, cwd)
	canonicalDir := filepath.Join(canonicalBase, sName)
	agentBase := getAgentBaseDir(agentName, global, cwd)
	agentDir := filepath.Join(agentBase, sName)

	if !isPathSafe(canonicalBase, canonicalDir) || !isPathSafe(agentBase, agentDir) {
		return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: "potential path traversal detected"}
	}

	cp := func(dst string) error { return writeSkillFiles(dst, files) }

	if mode == InstallModeCopy {
		if err := cleanAndCreateDir(agentDir); err != nil {
			return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
		}
		if err := cp(agentDir); err != nil {
			return InstallResult{Success: false, Path: agentDir, Mode: mode, Error: err.Error()}
		}
		return InstallResult{Success: true, Path: agentDir, Mode: InstallModeCopy}
	}

	return performSymlinkInstall(canonicalDir, agentDir, agentName, global, mode, cp)
}

func isSkillInstalled(skillName, agentName string, global bool) bool {
	a := agent.AllAgents[agentName]
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

func getInstallPath(skillName, agentName string, global bool) string {
	cwd, _ := os.Getwd()
	sName := sanitizeName(skillName)
	agentBase := getAgentBaseDir(agentName, global, cwd)
	return filepath.Join(agentBase, sName)
}

type InstalledSkill struct {
	Name          string
	Description   string
	Path          string
	CanonicalPath string
	Scope         string // "project" or "global"
	Agents        []string
}

type scopeEntry struct {
	isGlobal  bool
	path      string
	agentType string
}

func filterAgentsToCheck(detected []string, agentFilter []string) []string {
	if len(agentFilter) == 0 {
		return detected
	}
	var filtered []string
	for _, a := range detected {
		for _, f := range agentFilter {
			if a == f {
				filtered = append(filtered, a)
				break
			}
		}
	}
	return filtered
}

func agentDirForScope(agentName string, isGlobal bool, cwd string) string {
	a := agent.AllAgents[agentName]
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

func appendDetectedAgentScopes(scopes []scopeEntry, agentsToCheck []string, isGlobal bool, cwd string) []scopeEntry {
	for _, agentName := range agentsToCheck {
		a := agent.AllAgents[agentName]
		if a == nil || (isGlobal && a.GlobalSkillsDir == "") {
			continue
		}
		agentDir := agentDirForScope(agentName, isGlobal, cwd)
		if !scopeEntryExists(scopes, agentDir, isGlobal) {
			scopes = append(scopes, scopeEntry{isGlobal: isGlobal, path: agentDir, agentType: agentName})
		}
	}
	return scopes
}

func appendUndetectedAgentScopes(scopes []scopeEntry, agentsToCheck []string, isGlobal bool, cwd string) []scopeEntry {
	for agentName, a := range agent.AllAgents {
		if contains(agentsToCheck, agentName) {
			continue
		}
		if isGlobal && a.GlobalSkillsDir == "" {
			continue
		}
		agentDir := agentDirForScope(agentName, isGlobal, cwd)
		if scopeEntryExists(scopes, agentDir, isGlobal) {
			continue
		}
		if _, statErr := os.Stat(agentDir); statErr == nil {
			scopes = append(scopes, scopeEntry{isGlobal: isGlobal, path: agentDir, agentType: agentName})
		}
	}
	return scopes
}

func buildScopeEntries(agentsToCheck []string, scopeTypes []bool, cwd string) []scopeEntry {
	var scopes []scopeEntry
	for _, isGlobal := range scopeTypes {
		scopes = append(scopes, scopeEntry{isGlobal: isGlobal, path: getCanonicalSkillsDir(isGlobal, cwd)})
		scopes = appendDetectedAgentScopes(scopes, agentsToCheck, isGlobal, cwd)
		scopes = appendUndetectedAgentScopes(scopes, agentsToCheck, isGlobal, cwd)
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

func mergeAgentSkillIntoMap(skillsMap map[string]*InstalledSkill, mapKey, agentType string, s *skill.Skill, skillDir, scopeKey string) {
	if existing, ok := skillsMap[mapKey]; ok {
		if !contains(existing.Agents, agentType) {
			existing.Agents = append(existing.Agents, agentType)
		}
	} else {
		skillsMap[mapKey] = &InstalledSkill{
			Name: s.Name, Description: s.Description,
			Path: skillDir, CanonicalPath: skillDir,
			Scope: scopeKey, Agents: []string{agentType},
		}
	}
}

func agentHasSkill(agentBase, dirName, sName, skillName string) bool {
	for _, name := range []string{dirName, sName} {
		agentSkillDir := filepath.Join(agentBase, name)
		if !isPathSafe(agentBase, agentSkillDir) {
			continue
		}
		if _, err := os.Stat(agentSkillDir); err == nil {
			return true
		}
	}
	agentEntries, err := os.ReadDir(agentBase)
	if err != nil {
		return false
	}
	for _, ae := range agentEntries {
		if !ae.IsDir() {
			continue
		}
		candidateDir := filepath.Join(agentBase, ae.Name())
		candidateSkill, err := skill.ParseSkillMd(filepath.Join(candidateDir, "SKILL.md"), true)
		if err == nil && candidateSkill != nil && candidateSkill.Name == skillName {
			return true
		}
	}
	return false
}

func findAgentsForSkill(s *skill.Skill, dirName string, agentsToCheck []string, isGlobal bool, cwd string) []string {
	sName := sanitizeName(s.Name)
	var result []string
	for _, agentName := range agentsToCheck {
		a := agent.AllAgents[agentName]
		if a == nil || (isGlobal && a.GlobalSkillsDir == "") {
			continue
		}
		agentBase := agentDirForScope(agentName, isGlobal, cwd)
		if agentHasSkill(agentBase, dirName, sName, s.Name) {
			result = append(result, agentName)
		}
	}
	return result
}

func mergeCanonicalSkillIntoMap(skillsMap map[string]*InstalledSkill, mapKey string, agents []string, s *skill.Skill, skillDir, scopeKey string) {
	if existing, ok := skillsMap[mapKey]; ok {
		for _, ag := range agents {
			if !contains(existing.Agents, ag) {
				existing.Agents = append(existing.Agents, ag)
			}
		}
	} else {
		skillsMap[mapKey] = &InstalledSkill{
			Name: s.Name, Description: s.Description,
			Path: skillDir, CanonicalPath: skillDir,
			Scope: scopeKey, Agents: agents,
		}
	}
}

func populateScopeSkills(scope scopeEntry, agentsToCheck []string, cwd string, skillsMap map[string]*InstalledSkill) {
	entries, err := os.ReadDir(scope.path)
	if err != nil {
		return
	}
	scopeKey := "project"
	if scope.isGlobal {
		scopeKey = "global"
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(scope.path, e.Name())
		s := parseSkillInDir(skillDir)
		if s == nil {
			continue
		}
		mapKey := scopeKey + ":" + s.Name
		if scope.agentType != "" {
			mergeAgentSkillIntoMap(skillsMap, mapKey, scope.agentType, s, skillDir, scopeKey)
			continue
		}
		agents := findAgentsForSkill(s, e.Name(), agentsToCheck, scope.isGlobal, cwd)
		mergeCanonicalSkillIntoMap(skillsMap, mapKey, agents, s, skillDir, scopeKey)
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

func listInstalledSkills(global *bool, agentFilter []string) ([]*InstalledSkill, error) {
	cwd, _ := os.Getwd()
	agentsToCheck := filterAgentsToCheck(agent.DetectInstalledAgents(), agentFilter)

	scopeTypes := []bool{false, true}
	if global != nil {
		scopeTypes = []bool{*global}
	}
	scopes := buildScopeEntries(agentsToCheck, scopeTypes, cwd)

	skillsMap := map[string]*InstalledSkill{}
	for _, scope := range scopes {
		populateScopeSkills(scope, agentsToCheck, cwd, skillsMap)
	}

	var result []*InstalledSkill
	for _, s := range skillsMap {
		result = append(result, s)
	}
	return result, nil
}

// installWellKnownSkillForAgent installs a well-known skill for an agent.
// (moved from registry/wellknown.go since it depends on installSkillFilesForAgent)
func installWellKnownSkillForAgent(sk *registry.WellKnownSkill, agentName string, global bool, mode InstallMode) InstallResult {
	var files []struct{ Path, Contents string }
	for path, content := range sk.Files {
		files = append(files, struct{ Path, Contents string }{path, content})
	}
	return installSkillFilesForAgent(sk.InstallName, files, agentName, global, mode)
}

// Silence unused imports
var _ = fmt.Sprintf
var _ = lock.ReadSkillLock
