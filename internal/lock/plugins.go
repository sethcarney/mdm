package lock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ──────────────────────────────────────────────────────────
// Agent Plugins lock — the plugins section of mdm.lock
//
// v1 kept plugin entries in their own plugins-lock.json so a stable binary
// rewriting the skills lock could not drop them. In v2 the unified
// mdm.lock preserves unknown top-level keys on every write (see
// project.go), so the sections share one file safely.
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

// PluginLockFile is a view of the plugins section of the project lock.
type PluginLockFile struct {
	Version int                        `json:"version"`
	Plugins map[string]PluginLockEntry `json:"plugins"`
}

// readLegacyPluginsLockE reads the v1 plugins-lock.json directly. It is only
// consulted when mdm.lock does not exist. Corrupt or newer-versioned
// files are an error, matching the final v1 patch releases.
func readLegacyPluginsLockE(cwd string) (PluginLockFile, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	path := filepath.Join(cwd, "plugins-lock.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return EmptyPluginsLock(), nil
		}
		return EmptyPluginsLock(), errUnreadableLock(path, err)
	}
	var lk PluginLockFile
	if err := json.Unmarshal(data, &lk); err != nil {
		return EmptyPluginsLock(), errUnreadableLock(path, err)
	}
	if lk.Version > pluginsLockVersion {
		return EmptyPluginsLock(), errNewerLock(path, lk.Version, pluginsLockVersion)
	}
	if lk.Plugins == nil || lk.Version < pluginsLockVersion {
		return EmptyPluginsLock(), nil
	}
	return lk, nil
}

func ReadPluginsLock(cwd string) PluginLockFile {
	return PluginLockFile{Version: pluginsLockVersion, Plugins: ReadProjectLock(cwd).Plugins}
}

func WritePluginsLock(lk PluginLockFile, cwd string) error {
	pl := ReadProjectLock(cwd)
	pl.Plugins = lk.Plugins
	return WriteProjectLock(pl, cwd)
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
	return WritePluginsLock(lk, cwd)
}
