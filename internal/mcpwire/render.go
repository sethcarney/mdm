package mcpwire

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sethcarney/mdm/internal/plugin"
)

// RenderServer converts a validated plugin server into the agent-native
// JSON object for this target. mdm is a config writer, not the launcher -
// the agent is - so everything the spec asks the launcher to do is baked
// in here: ${PLUGIN_ROOT}/${PLUGIN_DATA} become absolute paths, both
// variables are injected into the subprocess env, ./-prefixed commands
// resolve inside the plugin root, and an omitted cwd defaults to it.
func (t MCPTarget) RenderServer(s plugin.Server, rootAbs, dataAbs string) (map[string]any, error) {
	switch s.Type {
	case plugin.ServerStdio:
		return t.renderStdio(s, rootAbs, dataAbs)
	case plugin.ServerStreamableHTTP, plugin.ServerSSE:
		return t.renderHTTP(s), nil
	default:
		return nil, fmt.Errorf("unsupported server type %q", s.Type)
	}
}

func (t MCPTarget) renderStdio(s plugin.Server, rootAbs, dataAbs string) (map[string]any, error) {
	command := s.Command
	if strings.HasPrefix(command, "./") {
		resolved, err := plugin.ResolveWithin(rootAbs, command)
		if err != nil {
			return nil, err
		}
		command = resolved
	}
	cwd, err := renderCwd(s.Cwd, rootAbs, dataAbs)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, len(s.Args))
	for _, a := range s.Args {
		args = append(args, plugin.ExpandVars(a, rootAbs, dataAbs))
	}
	env := map[string]string{}
	for k, v := range s.Env {
		env[k] = plugin.ExpandVars(v, rootAbs, dataAbs)
	}
	// The spec requires the launcher to provide both variables to plugin
	// subprocesses; a config entry is the only channel mdm has.
	env["PLUGIN_ROOT"] = rootAbs
	env["PLUGIN_DATA"] = dataAbs

	out := map[string]any{"command": command, "env": env, "cwd": cwd}
	if t.style == styleTyped {
		out["type"] = "stdio"
	}
	if len(args) > 0 {
		out["args"] = args
	}
	return out, nil
}

// renderCwd expands the spec's allowed cwd forms to an absolute path and
// re-checks containment after expansion. An omitted cwd defaults to the
// plugin root.
func renderCwd(cwd, rootAbs, dataAbs string) (string, error) {
	if cwd == "" {
		return rootAbs, nil
	}
	if strings.HasPrefix(cwd, "./") {
		return plugin.ResolveWithin(rootAbs, cwd)
	}
	expanded := filepath.Clean(plugin.ExpandVars(cwd, rootAbs, dataAbs))
	for _, base := range []string{rootAbs, dataAbs} {
		if expanded == filepath.Clean(base) || strings.HasPrefix(expanded, filepath.Clean(base)+string(filepath.Separator)) {
			return expanded, nil
		}
	}
	return "", fmt.Errorf("cwd %q escapes the plugin directories", cwd)
}

func (t MCPTarget) renderHTTP(s plugin.Server) map[string]any {
	out := map[string]any{"url": s.URL}
	if len(s.Headers) > 0 {
		out["headers"] = s.Headers
	}
	if t.style == styleTyped {
		if s.Type == plugin.ServerSSE {
			out["type"] = "sse"
		} else {
			out["type"] = "http"
		}
	}
	return out
}
