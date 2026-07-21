package sandbox

import (
	"fmt"
	"path/filepath"
)

// Cursor's CLI agent (cursor-agent) reads permission tokens from a JSON file
// with the same allow/deny shape as Claude Code — Read(pathOrGlob),
// Shell(commandBase), Write(pathOrGlob) — where deny always beats allow. mdm
// writes project-scoped deny rules for secret paths into .cursor/cli.json.
//
// Cursor's OS-level command sandbox (filesystem/network isolation) is a
// separate mechanism enabled through the agent's run mode; these rules harden
// the permission layer, which is the part that is file-configurable and
// committable.

func cursorAgent() Agent {
	return Agent{
		Name:        "cursor",
		DisplayName: "Cursor",
		Notes: []string{
			"Project permissions live in .cursor/cli.json (committable); Cursor also reads a global ~/.cursor/cli-config.json.",
			"Deny rules block the agent from reading secret files, and deny always beats allow.",
			"Cursor's per-command OS sandbox (filesystem/network isolation) is enabled separately via the agent run mode.",
		},
		Plan:   func(dir string) ([]Change, error) { return cursorSetup(dir, false) },
		Apply:  func(dir string) ([]Change, error) { return cursorSetup(dir, true) },
		Status: cursorStatus,
		HasProjectConfig: func(dir string) bool {
			_, exists, err := readJSONMap(cursorConfigPath(dir))
			return err == nil && exists
		},
	}
}

func cursorConfigPath(projectDir string) string {
	return filepath.Join(projectDir, ".cursor", "cli.json")
}

func cursorSetup(projectDir string, write bool) ([]Change, error) {
	path := cursorConfigPath(projectDir)
	settings, exists, err := readJSONMap(path)
	if err != nil {
		return nil, err
	}

	perms := ensureMap(settings, "permissions")
	deny, added := mergeDenyRules(stringSlice(perms["deny"]), secretReadDenyRules())
	if added == 0 {
		return nil, nil
	}
	perms["deny"] = toAnySlice(deny)

	change := Change{
		Path:        path,
		Description: fmt.Sprintf("add %d Read deny rule(s) for secret paths", added),
		Create:      !exists,
	}
	if write {
		if err := writeJSONMap(path, settings); err != nil {
			return nil, err
		}
	}
	return []Change{change}, nil
}

func cursorStatus(projectDir string) ([]Check, error) {
	path := cursorConfigPath(projectDir)
	settings, exists, err := readJSONMap(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return []Check{{Name: "Secret read deny rules", State: StateMissing, Detail: "no .cursor/cli.json — run 'mdm sandbox setup'", Path: path}}, nil
	}

	perms, _ := settings["permissions"].(map[string]any)
	haveKeys := map[string]bool{}
	for _, r := range stringSlice(perms["deny"]) {
		haveKeys[denyRuleKey(r)] = true
	}
	baseline := secretReadDenyRules()
	missing := 0
	for _, rule := range baseline {
		if !haveKeys[denyRuleKey(rule)] {
			missing++
		}
	}
	return []Check{
		boolCheck("Secret read deny rules", missing == 0,
			"all baseline rules present",
			fmt.Sprintf("%d of %d baseline rules missing", missing, len(baseline)), path),
	}, nil
}
