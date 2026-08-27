package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sethcarney/mdm/internal/fork"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/skill"
	"github.com/sethcarney/mdm/internal/source"
)

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

const mitText = "MIT License\n\nPermission is hereby granted, free of charge, to any person obtaining a copy\n"

// newTestSkill writes a SKILL.md under root/rel and returns the parsed skill.
func newTestSkill(t *testing.T, root, rel, frontmatter string) *skill.Skill {
	t.Helper()
	dir := filepath.Join(root, rel)
	writeTestFile(t, filepath.Join(dir, "SKILL.md"), frontmatter)
	s, err := skill.ParseSkillMd(filepath.Join(dir, "SKILL.md"), true)
	if err != nil || s == nil {
		t.Fatalf("could not parse the test skill: %v", err)
	}
	return s
}

func TestResolveLicensePrefersTheSkillsOwnFile(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "LICENSE"), "Apache License\nVersion 2.0, January 2004\n")
	s := newTestSkill(t, root, "skills/a", "---\nname: a\ndescription: b\n---\n")
	writeTestFile(t, filepath.Join(s.Path, "LICENSE.md"), mitText)

	lic := resolveLicense(s, upstreamContext{root: root})
	if !lic.InSkill {
		t.Error("expected the skill's own license file to win over the repository root")
	}
	if lic.File != "LICENSE.md" {
		t.Errorf("File = %q, want LICENSE.md - a file already inside the skill keeps its name", lic.File)
	}
	if lic.ID != "MIT" {
		t.Errorf("ID = %q, want MIT", lic.ID)
	}
}

func TestResolveLicenseFallsBackToTheRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "LICENSE"), mitText)
	s := newTestSkill(t, root, "skills/a", "---\nname: a\ndescription: b\n---\n")

	lic := resolveLicense(s, upstreamContext{root: root})
	if lic.InSkill {
		t.Error("a root license is not inside the skill directory")
	}
	if lic.File != fork.UpstreamLicenseFileName {
		t.Errorf("File = %q, want %q", lic.File, fork.UpstreamLicenseFileName)
	}
	if lic.ID != "MIT" || lic.Declared {
		t.Errorf("expected a detected (not declared) MIT license, got %+v", lic)
	}
}

func TestResolveLicenseKeepsADeclaredIdentifier(t *testing.T) {
	root := t.TempDir()
	s := newTestSkill(t, root, "skills/a", "---\nname: a\ndescription: b\nlicense: CC-BY-4.0\n---\n")

	lic := resolveLicense(s, upstreamContext{root: root})
	if lic.ID != "CC-BY-4.0" || !lic.Declared {
		t.Errorf("expected the frontmatter license to be kept, got %+v", lic)
	}
	if lic.Path != "" {
		t.Errorf("expected no license file, got %q", lic.Path)
	}
}

func TestConfirmUnlicensedPassesWhenTermsAreKnown(t *testing.T) {
	root := t.TempDir()
	s := newTestSkill(t, root, "a", "---\nname: a\ndescription: b\n---\n")
	licenses := map[string]licenseInfo{"a": {ID: "MIT", Path: filepath.Join(root, "LICENSE"), File: "LICENSE"}}

	// No prompt is reachable here: a licensed fork must not ask anything.
	if !confirmUnlicensed([]*skill.Skill{s}, licenses, CherryPickOptions{}) {
		t.Error("expected a licensed fork to proceed without confirmation")
	}
}

func TestUpstreamRecordUsesTheSourceAsTyped(t *testing.T) {
	root := t.TempDir()
	s := newTestSkill(t, root, "skills/code-review", "---\nname: code-review\ndescription: b\n---\n")
	up := upstreamContext{
		parsed:     source.ParsedSource{Type: source.SourceTypeGitHub, URL: "https://github.com/owner/repo"},
		input:      "owner/repo",
		root:       root,
		searchRoot: root,
		ref:        "v1.2.0",
		commit:     "abc123",
	}

	rec := upstreamRecord(s, up, licenseInfo{ID: "MIT", Path: "x", File: fork.UpstreamLicenseFileName}, t.TempDir())
	if rec.Source != "owner/repo" || rec.SourceURL != "https://github.com/owner/repo" {
		t.Errorf("unexpected source: %+v", rec)
	}
	if rec.Ref != "v1.2.0" || rec.Commit != "abc123" {
		t.Errorf("unexpected ref/commit: %+v", rec)
	}
	if rec.SkillPath != "skills/code-review/SKILL.md" {
		t.Errorf("SkillPath = %q, want the repo-relative path", rec.SkillPath)
	}
	if rec.LicenseFile != fork.UpstreamLicenseFileName || rec.Via != "" {
		t.Errorf("unexpected license/via: %+v", rec)
	}
}

// Forking a skill that mdm installed earlier must credit the repository it came
// from, not the local directory the files were copied out of.
func TestUpstreamRecordCreditsTheOriginalUpstream(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cwd := t.TempDir()
	installed := filepath.Join(cwd, ".agents", "skills")
	s := newTestSkill(t, installed, "code-review", "---\nname: code-review\ndescription: b\n---\n")

	if err := lock.AddSkillToLocalLock("code-review", lock.LocalSkillLockEntry{
		Source:     "owner/repo",
		Ref:        "v2.0.0",
		SourceType: string(source.SourceTypeGitHub),
		SkillPath:  "skills/code-review/SKILL.md",
	}, cwd); err != nil {
		t.Fatal(err)
	}

	up := upstreamContext{
		parsed: source.ParsedSource{Type: source.SourceTypeLocal, LocalPath: s.Path},
		input:  s.Path,
		root:   s.Path,
	}
	rec := upstreamRecord(s, up, licenseInfo{}, cwd)

	if rec.Source != "owner/repo" || rec.SourceType != string(source.SourceTypeGitHub) {
		t.Errorf("expected the recorded upstream, got %+v", rec)
	}
	if rec.Ref != "v2.0.0" || rec.SkillPath != "skills/code-review/SKILL.md" {
		t.Errorf("expected the lock's ref and path, got %+v", rec)
	}
	if rec.Via != "./.agents/skills/code-review" {
		t.Errorf("Via = %q, want the local path the copy came through", rec.Via)
	}
}

func TestUpstreamRecordKeepsLocalSourcesLocal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cwd := t.TempDir()
	s := newTestSkill(t, cwd, "vendor/a", "---\nname: a\ndescription: b\n---\n")

	up := upstreamContext{
		parsed: source.ParsedSource{Type: source.SourceTypeLocal, LocalPath: s.Path},
		input:  s.Path,
		root:   s.Path,
	}
	rec := upstreamRecord(s, up, licenseInfo{}, cwd)

	if rec.Source != "./vendor/a" {
		t.Errorf("Source = %q, want a path relative to the project", rec.Source)
	}
	if rec.SourceURL != "" || rec.Via != "" {
		t.Errorf("a plain local fork has no URL or via: %+v", rec)
	}
}

func TestClearForkDestRefusesToDiscardEditsWithoutForce(t *testing.T) {
	cwd := t.TempDir()
	dest := filepath.Join(cwd, "skills", "a")
	writeTestFile(t, filepath.Join(dest, "SKILL.md"), "ours\n")

	if clearForkDest(dest, "a", cwd, false) {
		t.Error("expected an existing fork to be left alone without --force")
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); err != nil {
		t.Error("the existing fork must survive a refused overwrite")
	}

	if !clearForkDest(dest, "a", cwd, true) {
		t.Fatal("expected --force to clear the destination")
	}
	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); !os.IsNotExist(err) {
		t.Error("--force should have emptied the destination")
	}
}

func TestRewriteForkName(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: code-review\ndescription: b\n---\nbody\n")

	if err := rewriteForkName(dir, "our-review"); err != nil {
		t.Fatal(err)
	}
	s, err := skill.ParseSkillMd(filepath.Join(dir, "SKILL.md"), true)
	if err != nil || s == nil {
		t.Fatalf("rewritten SKILL.md no longer parses: %v", err)
	}
	if s.Name != "our-review" {
		t.Errorf("name = %q, want our-review", s.Name)
	}

	writeTestFile(t, filepath.Join(dir, "SKILL.md"), "no frontmatter here\n")
	if err := rewriteForkName(dir, "x"); err == nil {
		t.Error("expected an error when there is no name to rewrite")
	}
}

func TestForkLabels(t *testing.T) {
	o := fork.Origin{
		Skill: "our-review",
		Upstream: fork.Upstream{
			Name: "code-review", Source: "owner/repo",
			SourceURL: "https://github.com/owner/repo", Ref: "v1", License: "MIT",
		},
	}
	label := forkUpstreamLabel(o)
	for _, want := range []string{"https://github.com/owner/repo", "#v1", "code-review"} {
		if !strings.Contains(label, want) {
			t.Errorf("label %q missing %q", label, want)
		}
	}
	if got := forkLicenseLabel(o); got != "MIT" {
		t.Errorf("license label = %q, want MIT", got)
	}
	if got := forkLicenseLabel(fork.Origin{}); got != "none declared" {
		t.Errorf("license label = %q, want 'none declared'", got)
	}

	if got := forkStateLabel(fork.Fork{Origin: o}); got != "" {
		t.Errorf("state label = %q, want empty when nothing can be compared", got)
	}
	o.ContentHash = "sha256:a"
	if got := forkStateLabel(fork.Fork{Origin: o, Hash: "sha256:a"}); got != "unmodified" {
		t.Errorf("state label = %q, want unmodified", got)
	}
	if got := forkStateLabel(fork.Fork{Origin: o, Hash: "sha256:b"}); got != "edited since fork" {
		t.Errorf("state label = %q, want 'edited since fork'", got)
	}
}

// ─── Guards against destroying a fork ──────────────────────────────────────────
//
// A fork lives in ./skills, which is also OpenClaw's project skills directory.
// Every command that treats an agent's skills directory as disposable can reach
// a fork through it, so each guard is pinned here.

func writeTestFork(t *testing.T, dir string) {
	t.Helper()
	writeTestFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: forked\ndescription: b\n---\nour edits\n")
	if err := fork.WriteOrigin(dir, fork.NewOrigin("forked", fork.Upstream{Name: "theirs", Source: "owner/repo"})); err != nil {
		t.Fatal(err)
	}
}

func TestIsCherryPickedSource(t *testing.T) {
	root := t.TempDir()

	forked := filepath.Join(root, "forked")
	writeTestFork(t, forked)
	if !isCherryPickedSource(forked) {
		t.Error("a directory with an origin file is a fork")
	}

	plain := filepath.Join(root, "plain")
	writeTestFile(t, filepath.Join(plain, "SKILL.md"), "---\nname: plain\ndescription: b\n---\n")
	if isCherryPickedSource(plain) {
		t.Error("an installed skill is not a fork")
	}

	// A symlink into a fork is an install, and removing it must stay possible.
	link := filepath.Join(root, "link")
	if err := os.Symlink(forked, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if isCherryPickedSource(link) {
		t.Error("a symlink pointing at a fork is an install, not the fork itself")
	}
}

func TestRemoveAgentSkillsDirKeepsForks(t *testing.T) {
	skillsDir := t.TempDir()
	writeTestFork(t, filepath.Join(skillsDir, "forked"))
	writeTestFile(t, filepath.Join(skillsDir, "installed", "SKILL.md"), "---\nname: installed\ndescription: b\n---\n")

	kept, removed := removeAgentSkillsDir(skillsDir)
	if !removed || kept != 1 {
		t.Fatalf("kept = %d, removed = %v; want 1, true", kept, removed)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "forked", "SKILL.md")); err != nil {
		t.Error("the fork must survive an agent removal")
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "installed")); !os.IsNotExist(err) {
		t.Error("the installed skill should have been removed")
	}
}

func TestRemoveAgentSkillsDirRemovesEverythingWhenNoForks(t *testing.T) {
	parent := t.TempDir()
	skillsDir := filepath.Join(parent, "skills")
	writeTestFile(t, filepath.Join(skillsDir, "installed", "SKILL.md"), "---\nname: installed\ndescription: b\n---\n")

	kept, removed := removeAgentSkillsDir(skillsDir)
	if !removed || kept != 0 {
		t.Fatalf("kept = %d, removed = %v; want 0, true", kept, removed)
	}
	if _, err := os.Stat(skillsDir); !os.IsNotExist(err) {
		t.Error("a directory with no forks in it should be removed whole, as before")
	}
}

// Installing a fork into an agent that reads the forks directory would replace
// the fork with a symlink to a copy of itself - destroying the source.
func TestClobbersForks(t *testing.T) {
	cwd := t.TempDir()
	forksRoot := filepath.Join(cwd, defaultForksDir)

	if !clobbersForks("openclaw", false, cwd, forksRoot) {
		t.Error("OpenClaw reads ./skills - installing there would overwrite the fork")
	}
	if clobbersForks("claude-code", false, cwd, forksRoot) {
		t.Error("claude-code has its own directory and is safe to install to")
	}
	// A fork directory pointed at the canonical tree is the same hazard.
	if !clobbersForks("claude-code", false, cwd, filepath.Join(cwd, ".agents", "skills")) {
		t.Error("the canonical skills directory must be treated as clobbering too")
	}

	kept := dropClobberingAgents([]string{"openclaw", "claude-code"}, false, cwd, forksRoot)
	if len(kept) != 1 || kept[0] != "claude-code" {
		t.Errorf("kept = %v, want only claude-code", kept)
	}
}
