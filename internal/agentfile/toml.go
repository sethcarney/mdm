package agentfile

import "github.com/BurntSushi/toml"

// parseAgentTOML reads and parses one Codex-style TOML definition. It returns
// (nil, nil) when name or description is missing: that file is not a
// definition, which is normal in a source tree and not an error. Every key
// besides the three required ones lands in Extra, nested tables included, so
// a later conversion does not silently drop a model or an mcp_servers table.
func parseAgentTOML(path string) (*AgentFile, error) {
	var data map[string]any
	if _, err := toml.DecodeFile(path, &data); err != nil {
		return nil, err
	}
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	if name == "" || desc == "" {
		return nil, nil
	}
	instructions, _ := data["developer_instructions"].(string)
	extra := map[string]any{}
	for k, v := range data {
		if k == "name" || k == "description" || k == "developer_instructions" {
			continue
		}
		extra[k] = v
	}
	return &AgentFile{
		Name:         name,
		Description:  desc,
		Instructions: instructions,
		Path:         path,
		Format:       FormatTOML,
		Extra:        extra,
	}, nil
}
