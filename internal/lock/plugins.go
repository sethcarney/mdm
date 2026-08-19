package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ──────────────────────────────────────────────────────────
// Agent Plugins lock (plugins-lock.json, project scope)
//
// Plugin entries deliberately live in their own file rather than in
// skills-lock.json, for the same reason knowledge entries do: the skill
// locks are read into fixed structs and rewritten wholesale, so an older
// mdm binary touching skills would silently drop unknown keys. A separate
// file keeps the experimental plugins feature invisible to — and
// incorruptible by — stable binaries.
// ──────────────────────────────────────────────────────────

const pluginsLockVersion = 1

type PluginLockEntry struct {
	Source      string `json:"source"`
	SourceType  string `json:"sourceType"`
	SourceURL   string `json:"sourceUrl,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Subpath     string `json:"subpath,omitempty"`
	InstallDir  string `json:"installDir"` // slash-separated, relative to the project root
	DataDir     string `json:"dataDir"`    // persistent PLUGIN_DATA dir, preserved across updates
	SpecVersion string `json:"specVersion"`
	Version     string `json:"version,omitempty"` // the manifest's version field
	ContentHash string `json:"contentHash,omitempty"`
	// Skills lists the sanitized names installed into agent skill
	// directories; SkillAgents lists the agents they were installed for.
	Skills      []string `json:"skills,omitempty"`
	SkillAgents []string `json:"skillAgents,omitempty"`
	// MCP maps an agent name to the namespaced server ids written into
	// that agent's MCP config file.
	MCP         map[string][]string `json:"mcp,omitempty"`
	InstalledAt string              `json:"installedAt"`
	UpdatedAt   string              `json:"updatedAt"`
}

type PluginLockFile struct {
	Version int                        `json:"version"`
	Plugins map[string]PluginLockEntry `json:"plugins"`
}

func GetPluginsLockPath(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return filepath.Join(cwd, "plugins-lock.json")
}

func ReadPluginsLock(cwd string) PluginLockFile {
	data, err := os.ReadFile(GetPluginsLockPath(cwd))
	if err != nil {
		return EmptyPluginsLock()
	}
	var lk PluginLockFile
	if err := json.Unmarshal(data, &lk); err != nil {
		return EmptyPluginsLock()
	}
	if lk.Plugins == nil || lk.Version < pluginsLockVersion {
		return EmptyPluginsLock()
	}
	return lk
}

func WritePluginsLock(lk PluginLockFile, cwd string) error {
	// Sort keys for deterministic output
	sorted := PluginLockFile{Version: lk.Version, Plugins: map[string]PluginLockEntry{}}
	keys := make([]string, 0, len(lk.Plugins))
	for k := range lk.Plugins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sorted.Plugins[k] = lk.Plugins[k]
	}
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(GetPluginsLockPath(cwd), append(data, '\n'), 0600)
}

func EmptyPluginsLock() PluginLockFile {
	return PluginLockFile{Version: pluginsLockVersion, Plugins: map[string]PluginLockEntry{}}
}

func AddPluginToLock(name string, entry PluginLockEntry, cwd string) error {
	lk := ReadPluginsLock(cwd)
	now := time.Now().UTC().Format(time.RFC3339)
	if existing, ok := lk.Plugins[name]; ok {
		entry.InstalledAt = existing.InstalledAt
	} else {
		entry.InstalledAt = now
	}
	entry.UpdatedAt = now
	lk.Plugins[name] = entry
	return WritePluginsLock(lk, cwd)
}

func RemovePluginFromLock(name, cwd string) error {
	lk := ReadPluginsLock(cwd)
	if _, ok := lk.Plugins[name]; !ok {
		return nil
	}
	delete(lk.Plugins, name)
	if len(lk.Plugins) == 0 {
		return os.Remove(GetPluginsLockPath(cwd))
	}
	return WritePluginsLock(lk, cwd)
}
