package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginsGraduated(t *testing.T) {
	dir := t.TempDir()
	stdout, _, code := runMdmInDir(t, dir, freshEnv(t), "--help")
	if code != 0 {
		t.Fatalf("mdm --help exited %d", code)
	}
	if !strings.Contains(stdout, "plugins") {
		t.Errorf("plugins graduated and should appear in --help, got: %q", stdout)
	}

	_, stderr, code := runMdmInDir(t, dir, freshEnv(t), "plugins")
	if code != 0 {
		t.Fatalf("mdm plugins should run without any experimental gate, exited %d: %s", code, stderr)
	}
	if strings.Contains(stderr, "experimental") {
		t.Errorf("no experimental banner expected after graduation, got: %q", stderr)
	}
}

func TestPluginsInitValidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	_, stderr, code := runMdmInDir(t, dir, env, "plugins", "init", "my-plugin", "--with-mcp")
	if code != 0 {
		t.Fatalf("plugins init exited %d: %s", code, stderr)
	}
	for _, p := range []string{"plugin.json", "mcp.json", filepath.Join("skills", "example-skill", "SKILL.md")} {
		if _, err := os.Stat(filepath.Join(dir, "my-plugin", p)); err != nil {
			t.Errorf("init should create %s: %v", p, err)
		}
	}

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "validate", "./my-plugin")
	if code != 0 {
		t.Fatalf("validate should pass on a scaffolded plugin, exited %d: %s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "1 skill(s)") || !strings.Contains(stdout, "1 MCP server(s)") {
		t.Errorf("expected component counts in report, got: %q", stdout)
	}
}

func TestPluginsInitRejectsInvalidName(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	_, stderr, code := runMdmInDir(t, dir, env, "plugins", "init", "Bad--Name")
	if code == 0 {
		t.Fatal("expected non-zero exit for an invalid plugin name")
	}
	if !strings.Contains(stderr, "invalid plugin name") {
		t.Errorf("expected name error, got: %q", stderr)
	}
}

func TestPluginsValidateReportsErrors(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)

	pluginDir := filepath.Join(dir, "broken")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Missing $schema is a fatal manifest violation.
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name": "broken"}`), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runMdmInDir(t, dir, env, "plugins", "validate", "./broken")
	if code == 0 {
		t.Fatal("expected non-zero exit for an invalid manifest")
	}
	if !strings.Contains(stdout, "invalid-manifest") {
		t.Errorf("expected invalid-manifest issue in output, got: %q", stdout)
	}
}

// writePluginSource creates a valid plugin source directory with the given
// skills and returns its path.
func writePluginSource(t *testing.T, parent, name string, skills ...string) string {
	t.Helper()
	root := filepath.Join(parent, name+"-src")
	manifest := `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
  "name": "` + name + `",
  "version": "1.0.0",
  "description": "Test plugin."
}
`
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		dir := filepath.Join(root, "skills", skill)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		md := "---\nname: " + skill + "\ndescription: A test skill.\n---\n\n# " + skill + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// mustRunPlugins runs an mdm command and fails the test on a non-zero exit.
func mustRunPlugins(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	stdout, stderr, code := runMdmInDir(t, dir, env, args...)
	if code != 0 {
		t.Fatalf("mdm %s exited %d: %s%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout + stderr
}

func assertPathsExist(t *testing.T, dir string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func assertPathsGone(t *testing.T, dir string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be gone (err=%v)", p, err)
		}
	}
}

func assertToolkitLockEntry(t *testing.T, dir string) {
	t.Helper()
	var lockData struct {
		Plugins map[string]struct {
			Skills      []string `json:"skills"`
			InstallDir  string   `json:"installDir"`
			SpecVersion string   `json:"specVersion"`
		} `json:"plugins"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &lockData); err != nil {
		t.Fatal(err)
	}
	entry, ok := lockData.Plugins["toolkit"]
	if !ok || len(entry.Skills) != 2 || entry.SpecVersion != "1.0.0" {
		t.Fatalf("unexpected lock entry: %+v", lockData)
	}
}

func TestPluginsAddListRemoveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha", "beta")

	mustRunPlugins(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y")

	// The plugin directory is the PLUGIN_ROOT; skills link into canonical
	// and agent dirs; the data dir exists; the lock records it all.
	assertPathsExist(t, dir,
		filepath.Join(".agents", "plugins", "toolkit", "plugin.json"),
		filepath.Join(".agents", "plugins-data", "toolkit"),
		filepath.Join(".agents", "skills", "alpha", "SKILL.md"),
		filepath.Join(".claude", "skills", "alpha", "SKILL.md"),
		lockName,
	)
	// The canonical skill entry is a symlink into the plugin directory.
	if info, err := os.Lstat(filepath.Join(dir, ".agents", "skills", "alpha")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("canonical skill should be a symlink into the plugin dir (err=%v)", err)
	}
	// Plugin installs never write the legacy v1 lock file.
	assertPathsGone(t, dir, "plugins-lock.json", "skills-lock.json")
	assertToolkitLockEntry(t, dir)

	out := mustRunPlugins(t, dir, env, "plugins", "list")
	if !strings.Contains(out, "toolkit") || !strings.Contains(out, "2 skill(s)") {
		t.Errorf("unexpected list output: %q", out)
	}

	// skills list labels plugin-owned skills.
	out = mustRunPlugins(t, dir, env, "skills", "list", "-p")
	if !strings.Contains(out, "from plugin toolkit") {
		t.Errorf("skills list should label plugin-owned skills, got: %q", out)
	}

	mustRunPlugins(t, dir, env, "plugins", "remove", "toolkit", "-y")
	assertPathsGone(t, dir,
		filepath.Join(".agents", "plugins", "toolkit"),
		filepath.Join(".agents", "skills", "alpha"),
		filepath.Join(".claude", "skills", "alpha"),
		lockName,
	)
	// The data dir survives unless --purge-data is given.
	assertPathsExist(t, dir, filepath.Join(".agents", "plugins-data", "toolkit"))
}

func TestPluginsRemovePurgeData(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}
	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "remove", "toolkit", "-y", "--purge-data"); code != 0 {
		t.Fatalf("plugins remove exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins-data", "toolkit")); !os.IsNotExist(err) {
		t.Error("--purge-data should delete the data dir")
	}
}

func TestPluginsAddDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y", "--dry-run")
	if code != 0 {
		t.Fatalf("plugins add --dry-run exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Dry run") {
		t.Errorf("expected dry-run notice, got: %q", stdout)
	}
	for _, p := range []string{".agents", lockName} {
		if _, err := os.Stat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Errorf("dry run must not create %s", p)
		}
	}
}

func TestPluginsAddBlocksHiddenChars(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	root, err := findModRoot()
	if err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "tests", "testdata", "hidden-plugin")

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", fixture, "-a", "claude-code", "-y")
	combined := stdout + stderr
	if code == 0 {
		t.Fatalf("expected hidden character scan to block install:\n%s", combined)
	}
	if !strings.Contains(combined, "Hidden character") {
		t.Errorf("expected hidden character finding, got:\n%s", combined)
	}
	if _, err := os.Stat(filepath.Join(dir, lockName)); !os.IsNotExist(err) {
		t.Error("blocked install must not write the lock")
	}

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", fixture, "-a", "claude-code", "-y", "--allow-hidden-chars"); code != 0 {
		t.Fatalf("--allow-hidden-chars should permit the install, exited %d: %s", code, stderr)
	}
}

func TestPluginsAddRejectsInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := filepath.Join(dir, "broken-src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"name": "no-schema"}`), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./broken-src", "-a", "claude-code", "-y")
	if code == 0 {
		t.Fatal("expected non-zero exit for a rejected plugin")
	}
	if !strings.Contains(stdout+stderr, "No plugins found") {
		t.Errorf("expected rejection message, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSkillsRemoveRefusesPluginOwnedSkill(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}

	stdout, stderr, code := runMdmInDir(t, dir, env, "skills", "remove", "alpha", "-y")
	if code != 0 {
		t.Fatalf("skills remove exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "managed by plugin toolkit") {
		t.Errorf("expected plugin-ownership refusal, got stdout=%q stderr=%q", stdout, stderr)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".agents", "skills", "alpha")); err != nil {
		t.Errorf("plugin-owned skill must survive skills remove: %v", err)
	}
}

func TestPluginsAddSkipsCollidingStandaloneSkill(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	// A standalone skill already owns the canonical directory.
	standalone := filepath.Join(dir, ".agents", "skills", "alpha")
	if err := os.MkdirAll(standalone, 0755); err != nil {
		t.Fatal(err)
	}
	marker := []byte("---\nname: alpha\ndescription: Standalone skill.\n---\n")
	if err := os.WriteFile(filepath.Join(standalone, "SKILL.md"), marker, 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y")
	if code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "already installed standalone") {
		t.Errorf("expected collision warning, got stdout=%q stderr=%q", stdout, stderr)
	}
	got, err := os.ReadFile(filepath.Join(standalone, "SKILL.md"))
	if err != nil || string(got) != string(marker) {
		t.Errorf("standalone skill must be left untouched (err=%v)", err)
	}
}

const testMCPJSON = `{
  "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
  "mcpServers": {
    "api": {"type": "stdio", "command": "./bin/server", "args": ["--data", "${PLUGIN_DATA}"]},
    "remote": {"type": "streamable-http", "url": "https://api.example.com/mcp"}
  }
}
`

func readServers(t *testing.T, path, serversKey string) (map[string]json.RawMessage, map[string]json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	servers := map[string]json.RawMessage{}
	if err := json.Unmarshal(top[serversKey], &servers); err != nil {
		t.Fatalf("parsing %s of %s: %v", serversKey, path, err)
	}
	return top, servers
}

// assertWiredStdioServer checks the claude-code entry for toolkit--api:
// absolute command inside the installed plugin, expanded args, injected
// env, defaulted cwd.
func assertWiredStdioServer(t *testing.T, servers map[string]json.RawMessage) {
	t.Helper()
	var stdio struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		Cwd     string            `json:"cwd"`
	}
	if err := json.Unmarshal(servers["toolkit--api"], &stdio); err != nil {
		t.Fatalf("toolkit--api missing or invalid: %v (have %v)", err, servers)
	}
	if stdio.Type != "stdio" || !filepath.IsAbs(stdio.Command) || !strings.Contains(stdio.Command, filepath.Join("plugins", "toolkit")) {
		t.Errorf("stdio command should be absolute inside the installed plugin: %+v", stdio)
	}
	if !filepath.IsAbs(stdio.Args[1]) || !strings.Contains(stdio.Args[1], "plugins-data") {
		t.Errorf("args should expand PLUGIN_DATA to the data dir: %+v", stdio.Args)
	}
	if stdio.Env["PLUGIN_ROOT"] == "" || stdio.Env["PLUGIN_DATA"] == "" || stdio.Cwd == "" {
		t.Errorf("env must inject PLUGIN_ROOT/PLUGIN_DATA and cwd must default: %+v", stdio)
	}
}

// assertWiredLockMCP checks the lock records the wired ids per agent.
func assertWiredLockMCP(t *testing.T, dir string) {
	t.Helper()
	var lockData struct {
		Plugins map[string]struct {
			MCP map[string][]string `json:"mcp"`
		} `json:"plugins"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, lockName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &lockData); err != nil {
		t.Fatal(err)
	}
	mcp := lockData.Plugins["toolkit"].MCP
	if len(mcp["claude-code"]) != 2 || len(mcp["cursor"]) != 2 {
		t.Errorf("lock should record wired ids per agent: %+v", mcp)
	}
}

func TestPluginsAddWiresMCPConfigs(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")
	if err := os.WriteFile(filepath.Join(src, "mcp.json"), []byte(testMCPJSON), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing user config must survive the merge.
	existing := `{"mcpServers": {"user-server": {"command": "keep"}}, "custom": 1}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	mustRunPlugins(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-a", "cursor", "-y")

	top, servers := readServers(t, filepath.Join(dir, ".mcp.json"), "mcpServers")
	if _, ok := top["custom"]; !ok {
		t.Error("unknown top-level keys in .mcp.json must survive")
	}
	if _, ok := servers["user-server"]; !ok {
		t.Error("pre-existing user server must survive")
	}
	assertWiredStdioServer(t, servers)
	if _, ok := servers["toolkit--remote"]; !ok {
		t.Error("http server should be wired too")
	}

	// Cursor config: bare style, no type field.
	_, cursorServers := readServers(t, filepath.Join(dir, ".cursor", "mcp.json"), "mcpServers")
	var cursorEntry map[string]json.RawMessage
	if err := json.Unmarshal(cursorServers["toolkit--api"], &cursorEntry); err != nil {
		t.Fatalf("cursor entry missing: %v", err)
	}
	if _, hasType := cursorEntry["type"]; hasType {
		t.Error("cursor entries should omit the type field")
	}
	assertWiredLockMCP(t, dir)

	// Remove unwires only the plugin's entries.
	mustRunPlugins(t, dir, env, "plugins", "remove", "toolkit", "-y")
	_, servers = readServers(t, filepath.Join(dir, ".mcp.json"), "mcpServers")
	if _, ok := servers["user-server"]; !ok {
		t.Error("remove must keep user entries")
	}
	for id := range servers {
		if strings.HasPrefix(id, "toolkit--") {
			t.Errorf("remove should delete %s", id)
		}
	}
}

func TestPluginsAddSkipMCP(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")
	if err := os.WriteFile(filepath.Join(src, "mcp.json"), []byte(testMCPJSON), 0644); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y", "--skip-mcp"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("--skip-mcp must not write .mcp.json")
	}
}

func TestPluginsAddBrokenMCPStillInstallsSkills(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")
	broken := `{"$schema": "https://agent-plugins.org/schemas/9.0.0/mcp.schema.json", "mcpServers": {}}`
	if err := os.WriteFile(filepath.Join(src, "mcp.json"), []byte(broken), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y")
	if code != 0 {
		t.Fatalf("a broken mcp.json must not fail the install, exited %d: %s", code, stderr)
	}
	if !strings.Contains(stdout+stderr, "MCP disabled") {
		t.Errorf("expected an MCP-disabled warning, got stdout=%q stderr=%q", stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "alpha", "SKILL.md")); err != nil {
		t.Errorf("skills must still install when MCP is disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("no MCP config should be written when MCP is disabled")
	}
}

func TestPluginsUpdateRefetchesSourceAndPreservesData(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}

	// Simulate plugin state that must survive updates.
	dataFile := filepath.Join(dir, ".agents", "plugins-data", "toolkit", "state.db")
	if err := os.WriteFile(dataFile, []byte("precious"), 0644); err != nil {
		t.Fatal(err)
	}

	// The upstream source gains a skill.
	newSkill := filepath.Join(src, "skills", "gamma")
	if err := os.MkdirAll(newSkill, 0755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: gamma\ndescription: New skill.\n---\n"
	if err := os.WriteFile(filepath.Join(newSkill, "SKILL.md"), []byte(md), 0644); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "update", "toolkit"); code != 0 {
		t.Fatalf("plugins update exited %d: %s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills", "gamma", "SKILL.md")); err != nil {
		t.Errorf("update should pick up the new skill: %v", err)
	}
	if got, err := os.ReadFile(dataFile); err != nil || string(got) != "precious" {
		t.Errorf("update must preserve the data dir (err=%v)", err)
	}
}

func TestPluginsInstallRestoresFromLock(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}
	// A fresh checkout has the lock but no installed state.
	if err := os.RemoveAll(filepath.Join(dir, ".agents")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "install"); code != 0 {
		t.Fatalf("plugins install exited %d: %s", code, stderr)
	}
	for _, p := range []string{
		filepath.Join(".agents", "plugins", "toolkit", "plugin.json"),
		filepath.Join(".agents", "skills", "alpha", "SKILL.md"),
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("install should restore %s: %v", p, err)
		}
	}
}

func TestDoctorReportsPluginIssues(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}
	// Break the install so doctor has something to report.
	if err := os.RemoveAll(filepath.Join(dir, ".agents", "plugins", "toolkit")); err != nil {
		t.Fatal(err)
	}

	stdout, _, _ := runMdmInDir(t, dir, env, "doctor")
	if !strings.Contains(stdout, "plugin directory") {
		t.Errorf("doctor should report the missing plugin, got: %q", stdout)
	}
}

func TestSkillsInstallHintsAtPluginsLock(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	if _, stderr, code := runMdmInDir(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y"); code != 0 {
		t.Fatalf("plugins add exited %d: %s", code, stderr)
	}

	stdout, stderr, _ := runMdmInDir(t, dir, env, "skills", "install", "-y")
	if !strings.Contains(stdout+stderr, "mdm plugins install") {
		t.Errorf("skills install should hint at plugins install, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestPluginsAddSkipsEscapingSymlinks(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")

	// A secret outside the plugin root, reachable only through symlinks
	// planted inside it — the package boundary says neither may be read.
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("private key material"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(src, "skills", "alpha", "SKILL.md"), filepath.Join(src, "alias.md")); err != nil {
		t.Fatal(err)
	}

	out := mustRunPlugins(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y")
	if !strings.Contains(out, "outside the plugin root") {
		t.Errorf("expected a skipped-symlink warning, got: %q", out)
	}
	assertPathsGone(t, dir, filepath.Join(".agents", "plugins", "toolkit", "leak.txt"))
	assertPathsExist(t, dir,
		filepath.Join(".agents", "plugins", "toolkit", "alias.md"),
		filepath.Join(".agents", "plugins", "toolkit", "skills", "alpha", "SKILL.md"))
}

func TestSkillsUpdateHintsAtPluginOwnedSkill(t *testing.T) {
	dir := t.TempDir()
	env := freshEnv(t)
	src := writePluginSource(t, dir, "toolkit", "alpha")
	mustRunPlugins(t, dir, env, "plugins", "add", "./"+filepath.Base(src), "-a", "claude-code", "-y")

	out := mustRunPlugins(t, dir, env, "skills", "update", "alpha", "--project", "-y")
	if !strings.Contains(out, "mdm plugins update toolkit") {
		t.Errorf("expected a hint pointing at mdm plugins update, got: %q", out)
	}
}
