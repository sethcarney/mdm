package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeSkill(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\ndescription: d\n---\nbody\n")
}

func writeManifest(t *testing.T, searchPath, contents string) {
	t.Helper()
	writeFile(t, filepath.Join(searchPath, ".claude-plugin", "marketplace.json"), contents)
}

// symlinkOrSkip creates a symlink, skipping the test where the host cannot.
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
}

func discoveredNames(skills []*Skill) []string {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	return names
}

func hasName(skills []*Skill, name string) bool {
	for _, n := range discoveredNames(skills) {
		if n == name {
			return true
		}
	}
	return false
}

func TestIsSafeRelDir(t *testing.T) {
	cases := []struct {
		desc string
		dir  string
		want bool
	}{
		{"a plain subdirectory", "extra", true},
		{"a nested subdirectory", "packages/extra", true},
		{"empty", "", false},
		{"the search root itself", ".", false},
		{"a parent escape", "../victim", false},
		{"a parent escape after a descent", "extra/../../victim", false},
		{"a rooted path with no drive letter", "/Users/victim", false},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			if got := isSafeRelDir(c.dir); got != c.want {
				t.Errorf("isSafeRelDir(%q) = %v, want %v", c.dir, got, c.want)
			}
		})
	}
}

// A manifest that declares an in-tree directory must keep working: the guard
// is meant to be invisible to honest sources.
func TestManifestKeepsInTreeSkillDirs(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeSkill(t, filepath.Join(src, "extra", "helper"), "helper")
	writeManifest(t, src, `{"name":"pack","skillDirs":["extra"],"plugins":[{"name":"pack","skillDir":"extra/helper"}]}`)

	paths := GetPluginSkillPaths(src)
	if len(paths) != 1 || paths[0] != filepath.Join(src, "extra") {
		t.Fatalf("GetPluginSkillPaths = %v, want [%s]", paths, filepath.Join(src, "extra"))
	}

	abs, _ := filepath.Abs(filepath.Join(src, "extra", "helper"))
	if got := GetPluginGroupings(src)[abs]; got != "pack" {
		t.Errorf("grouping for %s = %q, want %q", abs, got, "pack")
	}

	skills, err := DiscoverSkills(src, "", DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !hasName(skills, "helper") {
		t.Errorf("discovered %v, want the manifest-declared helper skill", discoveredNames(skills))
	}
}

// A manifest lives inside the third-party source being installed, so a
// declared directory that walks out of the source must never be scanned.
func TestManifestCannotDeclarePathsOutsideTheSource(t *testing.T) {
	cases := []struct {
		desc     string
		skillDir string
	}{
		{"a parent escape", "../victim-docs"},
		{"a parent escape after a descent", "extra/../../victim-docs"},
		{"a rooted path with no drive letter", "/victim-docs"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			root := t.TempDir()
			src := filepath.Join(root, "src")
			writeSkill(t, filepath.Join(root, "victim-docs", "stolen"), "victim-secret")
			writeSkill(t, filepath.Join(src, "skills", "legit"), "legit")
			writeManifest(t, src, `{"name":"evil","skillDirs":["`+c.skillDir+
				`"],"plugins":[{"name":"evil","skillDir":"`+c.skillDir+`/stolen"}]}`)

			if paths := GetPluginSkillPaths(src); len(paths) != 0 {
				t.Errorf("GetPluginSkillPaths = %v, want none", paths)
			}
			if g := GetPluginGroupings(src); len(g) != 0 {
				t.Errorf("GetPluginGroupings = %v, want none", g)
			}
			skills, err := DiscoverSkills(src, "", DiscoverOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if hasName(skills, "victim-secret") {
				t.Errorf("discovered %v, want no skill from outside the source", discoveredNames(skills))
			}
			if !hasName(skills, "legit") {
				t.Errorf("discovered %v, want the in-tree skill to survive", discoveredNames(skills))
			}
		})
	}
}

// A declared directory can look local and still be a symlink out of the
// source, which no lexical check can see.
func TestManifestSkillDirCannotEscapeThroughASymlink(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeSkill(t, filepath.Join(root, "victim-docs", "stolen"), "victim-secret")
	writeManifest(t, src, `{"name":"evil","skillDirs":["linked"],"plugins":[{"name":"evil","skillDir":"linked/stolen"}]}`)
	symlinkOrSkip(t, filepath.Join(root, "victim-docs"), filepath.Join(src, "linked"))

	if paths := GetPluginSkillPaths(src); len(paths) != 0 {
		t.Errorf("GetPluginSkillPaths = %v, want none", paths)
	}
	if g := GetPluginGroupings(src); len(g) != 0 {
		t.Errorf("GetPluginGroupings = %v, want none", g)
	}
	skills, err := DiscoverSkills(src, "", DiscoverOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hasName(skills, "victim-secret") {
		t.Errorf("discovered %v, want no skill from behind the symlink", discoveredNames(skills))
	}
}

// The manifest itself is read before anything has checked where it came from,
// so a symlinked .claude-plugin must be refused too.
func TestManifestBehindASymlinkIsNotRead(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeSkill(t, filepath.Join(src, "skills", "legit"), "legit")
	writeSkill(t, filepath.Join(root, "victim-docs", "stolen"), "victim-secret")
	writeFile(t, filepath.Join(root, "attacker", "marketplace.json"),
		`{"name":"evil","skillDirs":["extra"],"plugins":[{"name":"evil","skillDir":"extra"}]}`)
	writeSkill(t, filepath.Join(src, "extra", "reached"), "reached")
	symlinkOrSkip(t, filepath.Join(root, "attacker"), filepath.Join(src, ".claude-plugin"))

	// "extra" is in-tree and would be honored from a real manifest; it is
	// dropped here only because the manifest declaring it is not in the source.
	if paths := GetPluginSkillPaths(src); len(paths) != 0 {
		t.Errorf("GetPluginSkillPaths = %v, want none", paths)
	}
	if g := GetPluginGroupings(src); len(g) != 0 {
		t.Errorf("GetPluginGroupings = %v, want none", g)
	}
}

// A broken or looping symlink resolves to an error, which must count as unsafe
// rather than as "nothing to check".
func TestManifestSkillDirWithAnUnresolvableTargetIsDropped(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeManifest(t, src, `{"name":"evil","skillDirs":["dangling"]}`)
	symlinkOrSkip(t, filepath.Join(root, "does-not-exist"), filepath.Join(src, "dangling"))

	if paths := GetPluginSkillPaths(src); len(paths) != 0 {
		t.Errorf("GetPluginSkillPaths = %v, want none", paths)
	}
}
