package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/mcpwire"
	"github.com/sethcarney/mdm/internal/plugin"
)

// checkInstalledPlugins diagnoses installed plugins against
// the plugins lock section: missing or invalid plugin directories, content drift,
// broken skill links, and MCP config that fell out of sync. Reports nothing
// when no plugins are installed, so doctor output is unchanged for everyone
// else.
func checkInstalledPlugins(cwd string) []doctorIssue {
	lk := lock.ReadPluginsLock(cwd)
	if len(lk.Plugins) == 0 {
		return nil
	}

	names := make([]string, 0, len(lk.Plugins))
	for name := range lk.Plugins {
		names = append(names, name)
	}
	sort.Strings(names)

	var issues []doctorIssue
	for _, name := range names {
		issues = append(issues, diagnoseInstalledPlugin(name, lk.Plugins[name], cwd)...)
	}
	if hint := pluginsGitignoreHint(cwd); hint != nil {
		issues = append(issues, *hint)
	}
	return issues
}

func diagnoseInstalledPlugin(name string, entry lock.PluginLockEntry, cwd string) []doctorIssue {
	dir := filepath.Join(cwd, filepath.FromSlash(entry.InstallDir))
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return []doctorIssue{{
			Level:   "error",
			Message: fmt.Sprintf("%s: plugin directory ./%s not found — run `mdm plugins install` to restore", name, entry.InstallDir),
		}}
	}

	var issues []doctorIssue
	if _, _, err := plugin.LoadManifest(dir); err != nil {
		issues = append(issues, doctorIssue{
			Level:   "error",
			Message: fmt.Sprintf("%s: %v — run `mdm plugins validate ./%s`", name, err, entry.InstallDir),
		})
	}
	if entry.ContentHash != "" {
		if hash, err := plugin.HashPluginDir(dir); err == nil && hash != entry.ContentHash {
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s: modified since install (content hash mismatch) — run `mdm plugins update %s` to re-fetch", name, name),
			})
		}
	}
	issues = append(issues, diagnosePluginSkillLinks(name, entry, cwd)...)
	issues = append(issues, diagnosePluginMCP(name, entry, cwd)...)
	return issues
}

// diagnosePluginSkillLinks flags recorded skills whose canonical link is
// missing or no longer owned by this plugin.
func diagnosePluginSkillLinks(name string, entry lock.PluginLockEntry, cwd string) []doctorIssue {
	var issues []doctorIssue
	canonicalBase := getCanonicalSkillsDir(false, cwd)
	for _, skillName := range entry.Skills {
		canonicalDir := filepath.Join(canonicalBase, skillName)
		if _, err := os.Lstat(canonicalDir); os.IsNotExist(err) {
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s: skill link %s missing — run `mdm plugins update %s` to re-link", name, skillName, name),
			})
			continue
		}
		if owner, _ := skillDirOwner(canonicalDir, cwd); owner != "" && owner != name {
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s: skill %s is now owned by plugin %s", name, skillName, owner),
			})
		}
	}
	return issues
}

// diagnosePluginMCP flags recorded server ids missing from an agent's MCP
// config, and mdm-managed ids present in config but absent from the lock.
func diagnosePluginMCP(name string, entry lock.PluginLockEntry, cwd string) []doctorIssue {
	var issues []doctorIssue
	for _, agentName := range sortedStringKeys(entry.MCP) {
		target, ok := mcpwire.Targets[agentName]
		if !ok {
			continue
		}
		managed, err := target.ListManaged(cwd, name)
		if err != nil {
			issues = append(issues, doctorIssue{
				Level:   "warn",
				Message: fmt.Sprintf("%s: could not read %s: %v", name, target.ConfigPath, err),
			})
			continue
		}
		managedSet := map[string]bool{}
		for _, id := range managed {
			managedSet[id] = true
		}
		recorded := map[string]bool{}
		for _, id := range entry.MCP[agentName] {
			recorded[id] = true
			if !managedSet[id] {
				issues = append(issues, doctorIssue{
					Level:   "warn",
					Message: fmt.Sprintf("%s: server %s missing from %s — run `mdm plugins update %s` to re-wire", name, id, target.ConfigPath, name),
				})
			}
		}
		for _, id := range managed {
			if !recorded[id] {
				issues = append(issues, doctorIssue{
					Level:   "warn",
					Message: fmt.Sprintf("%s: orphaned server %s in %s — remove it or re-run `mdm plugins update %s`", name, id, target.ConfigPath, name),
				})
			}
		}
	}
	return issues
}

// pluginsGitignoreHint suggests ignoring the plugin data directory once it
// holds anything, since its contents are machine-local state.
func pluginsGitignoreHint(cwd string) *doctorIssue {
	dataBase := pluginsDataBaseDir(cwd)
	entries, err := os.ReadDir(dataBase)
	if err != nil || len(entries) == 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git")); err != nil {
		return nil
	}
	ignore, err := os.ReadFile(filepath.Join(cwd, ".gitignore"))
	if err == nil && strings.Contains(string(ignore), pluginsDataSubdir) {
		return nil
	}
	rel := filepath.ToSlash(filepath.Join(agent.AgentsDir, pluginsDataSubdir))
	return &doctorIssue{
		Level:   "warn",
		Message: fmt.Sprintf("plugin data is machine-local state — add %s/ to .gitignore", rel),
	}
}

func sortedStringKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
