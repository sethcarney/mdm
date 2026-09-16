package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSkill drops a minimal SKILL.md at dir.
func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	body := "---\nname: " + name + "\ndescription: fixture\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// lockSources reads mdm.lock and returns section -> name -> source.
func lockSources(t *testing.T, proj string) map[string]map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(proj, "mdm.lock"))
	if err != nil {
		t.Fatalf("reading mdm.lock: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing mdm.lock: %v", err)
	}
	out := map[string]map[string]string{}
	for _, section := range []string{"skills", "agents", "knowledge", "plugins"} {
		body, ok := raw[section]
		if !ok {
			continue
		}
		var entries map[string]struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(body, &entries); err != nil {
			continue
		}
		out[section] = map[string]string{}
		for name, e := range entries {
			out[section][name] = e.Source
		}
	}
	return out
}

// TestLocalSourcesRecordedRelativeToProject pins the lock's whole point: a
// teammate restores from their own clone, which sits at a path this machine
// never sees. Every section that can record a local source therefore has to
// write it relative to the project root, even when the user typed an absolute
// one. Knowledge bundles and plugins used to store the absolute path, so a
// clone anywhere else failed with "Path not found" pointing at a stranger's
// home directory.
func TestLocalSourcesRecordedRelativeToProject(t *testing.T) {
	proj := t.TempDir()
	env := freshEnv(t)

	writeSkill(t, filepath.Join(proj, "src", "myskill"), "myskill")
	agents := filepath.Join(proj, "agentsrc")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agents, "myagent.md"),
		[]byte("---\nname: myagent\ndescription: fixture\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	if _, stderr, code := runMdmInDir(t, proj, env, "knowledge", "init", "kb"); code != 0 {
		t.Fatalf("knowledge init exited %d: %s", code, stderr)
	}
	if _, stderr, code := runMdmInDir(t, proj, env, "plugins", "init", "myplug"); code != 0 {
		t.Fatalf("plugins init exited %d: %s", code, stderr)
	}

	// Absolute paths on the way in - exactly what a shell tab-completion or a
	// copied path produces, and what used to leak into the lock.
	steps := [][]string{
		{"skills", "add", filepath.Join(proj, "src", "myskill"), "--harness", "claude-code", "--project", "-y"},
		{"agents", "add", agents, "--harness", "claude-code", "--project", "-y"},
		{"knowledge", "add", filepath.Join(proj, "kb")},
		{"plugins", "add", filepath.Join(proj, "myplug"), "--harness", "claude-code"},
	}
	for _, args := range steps {
		if stdout, stderr, code := runMdmInDir(t, proj, env, args...); code != 0 {
			t.Fatalf("mdm %s exited %d: %s%s", strings.Join(args, " "), code, stdout, stderr)
		}
	}

	want := map[string]map[string]string{
		"skills":    {"myskill": "./src/myskill"},
		"agents":    {"myagent": "./agentsrc"},
		"knowledge": {"kb": "./kb"},
		"plugins":   {"myplug": "./myplug"},
	}
	got := lockSources(t, proj)
	for section, entries := range want {
		for name, source := range entries {
			if got[section][name] != source {
				t.Errorf("%s/%s source = %q, want %q (an absolute path does not survive a clone)",
					section, name, got[section][name], source)
			}
		}
	}
}

// TestInstallSkipsUnreachableLocalSource covers the other half: an entry
// recorded from outside the project cannot follow the repository, so a restore
// has to name it, install everything else anyway, and still fail the run. It
// used to exit on the first such entry, taking every later skill with it.
func TestInstallSkipsUnreachableLocalSource(t *testing.T) {
	proj := t.TempDir()
	env := freshEnv(t)

	writeSkill(t, filepath.Join(proj, "skills", "good"), "good")
	if stdout, stderr, code := runMdmInDir(t, proj, env,
		"skills", "add", "./skills/good", "--harness", "claude-code", "--project", "-y"); code != 0 {
		t.Fatalf("skills add exited %d: %s%s", code, stdout, stderr)
	}

	// Add an entry whose source left with the machine that wrote it. Skills
	// restore grouped by source in sorted order, and "../" sorts before "./",
	// so the unreachable group is reached first and the good one is what the
	// old code abandoned.
	lockPath := filepath.Join(proj, "mdm.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock: %v", err)
	}
	var lk map[string]any
	if err := json.Unmarshal(data, &lk); err != nil {
		t.Fatalf("parsing lock: %v", err)
	}
	lk["skills"].(map[string]any)["aa-gone"] = map[string]any{
		"source":     "../nowhere/aa-gone",
		"sourceType": "local",
	}
	patched, _ := json.MarshalIndent(lk, "", "  ")
	if err := os.WriteFile(lockPath, patched, 0o644); err != nil {
		t.Fatalf("writing lock: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(proj, ".agents")); err != nil {
		t.Fatalf("clearing installs: %v", err)
	}

	stdout, stderr, code := runMdmInDir(t, proj, env, "skills", "install", "-y")
	out := stdout + stderr
	if code == 0 {
		t.Errorf("exit code = 0, want non-zero: a half-restored checkout must not look green to CI\n%s", out)
	}
	if !strings.Contains(out, "aa-gone") {
		t.Errorf("output does not name the unrestorable skill:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(proj, ".agents", "skills", "good", "SKILL.md")); err != nil {
		t.Errorf("the reachable skill was not restored: %v\n%s", err, out)
	}
}

// TestKnowledgeAndPluginsInstallSkipUnreachableLocalSource is the same
// guarantee for the two restores that used to stop dead: one entry recorded
// from a path this machine does not have must not abandon the entries queued
// behind it.
func TestKnowledgeAndPluginsInstallSkipUnreachableLocalSource(t *testing.T) {
	proj := t.TempDir()
	env := freshEnv(t)

	for _, args := range [][]string{
		{"knowledge", "init", "kb"},
		{"plugins", "init", "myplug"},
		{"knowledge", "add", "./kb"},
		{"plugins", "add", "./myplug", "--harness", "claude-code"},
	} {
		if stdout, stderr, code := runMdmInDir(t, proj, env, args...); code != 0 {
			t.Fatalf("mdm %s exited %d: %s%s", strings.Join(args, " "), code, stdout, stderr)
		}
	}

	// "aa-" sorts first, so the unreachable entry is reached before the good
	// one. That ordering is the whole point: exiting there abandoned every
	// entry queued behind it. With the bad entry last there is nothing left to
	// lose and the old code looks fine.
	lockPath := filepath.Join(proj, "mdm.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("reading lock: %v", err)
	}
	var lk map[string]any
	if err := json.Unmarshal(data, &lk); err != nil {
		t.Fatalf("parsing lock: %v", err)
	}
	for section, dir := range map[string]string{"knowledge": "knowledge", "plugins": ".agents/plugins"} {
		lk[section].(map[string]any)["aa-gone"] = map[string]any{
			"source":     "../nowhere/aa-gone",
			"sourceType": "local",
			"installDir": dir + "/aa-gone",
		}
	}
	patched, _ := json.MarshalIndent(lk, "", "  ")
	if err := os.WriteFile(lockPath, patched, 0o644); err != nil {
		t.Fatalf("writing lock: %v", err)
	}

	for _, tc := range []struct {
		args     []string
		survivor string
	}{
		{[]string{"knowledge", "install"}, filepath.Join("knowledge", "kb")},
		{[]string{"plugins", "install"}, filepath.Join(".agents", "plugins", "myplug")},
	} {
		if err := os.RemoveAll(filepath.Join(proj, tc.survivor)); err != nil {
			t.Fatalf("clearing %s: %v", tc.survivor, err)
		}
		stdout, stderr, code := runMdmInDir(t, proj, env, tc.args...)
		out := stdout + stderr
		if code == 0 {
			t.Errorf("mdm %s exit code = 0, want non-zero\n%s", strings.Join(tc.args, " "), out)
		}
		if !strings.Contains(out, "aa-gone") {
			t.Errorf("mdm %s did not name the unrestorable entry:\n%s", strings.Join(tc.args, " "), out)
		}
		if _, err := os.Stat(filepath.Join(proj, tc.survivor)); err != nil {
			t.Errorf("mdm %s did not restore the reachable entry: %v\n%s", strings.Join(tc.args, " "), err, out)
		}
	}
}
