package mcpwire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (t MCPTarget) configFile(projectRoot string) string {
	return filepath.Join(projectRoot, filepath.FromSlash(t.ConfigPath))
}

// readConfig loads the agent config file into a top-level raw map plus the
// parsed server map, preserving every key mdm does not manage. A missing
// file yields empty maps.
func (t MCPTarget) readConfig(projectRoot string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	top := map[string]json.RawMessage{}
	servers := map[string]json.RawMessage{}
	data, err := os.ReadFile(t.configFile(projectRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return top, servers, nil
		}
		return nil, nil, err
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, fmt.Errorf("%s is not a JSON object: %w", t.ConfigPath, err)
	}
	if raw, ok := top[t.ServersKey]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return nil, nil, fmt.Errorf("%s: %s is not an object", t.ConfigPath, t.ServersKey)
		}
	}
	return top, servers, nil
}

func (t MCPTarget) writeConfig(projectRoot string, top map[string]json.RawMessage, servers map[string]json.RawMessage) error {
	raw, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	top[t.ServersKey] = raw
	data, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	path := t.configFile(projectRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

// Install merges the rendered entries into the target's config file,
// creating it if needed and leaving every foreign key untouched. It
// returns the namespaced ids written, sorted.
func (t MCPTarget) Install(projectRoot string, entries map[string]map[string]any) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	top, servers, err := t.readConfig(projectRoot)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for id, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		servers[id] = raw
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, t.writeConfig(projectRoot, top, servers)
}

// Remove deletes exactly the given namespaced ids, leaving other entries
// and unknown top-level keys in place. The file itself is never deleted.
func (t MCPTarget) Remove(projectRoot string, ids []string) error {
	top, servers, err := t.readConfig(projectRoot)
	if err != nil {
		return err
	}
	removed := false
	for _, id := range ids {
		if _, ok := servers[id]; ok {
			delete(servers, id)
			removed = true
		}
	}
	if !removed {
		return nil
	}
	return t.writeConfig(projectRoot, top, servers)
}

// ListManaged returns the namespaced ids in the config file that belong to
// the given plugin, sorted.
func (t MCPTarget) ListManaged(projectRoot, pluginName string) ([]string, error) {
	_, servers, err := t.readConfig(projectRoot)
	if err != nil {
		return nil, err
	}
	var ids []string
	for id := range servers {
		if strings.HasPrefix(id, pluginName+idSeparator) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}
