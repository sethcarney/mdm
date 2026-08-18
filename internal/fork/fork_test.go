package fork

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestHashDirIgnoresTheOriginFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: a\ndescription: b\n---\n")

	before, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOrigin(dir, NewOrigin("a", Upstream{Name: "a", Source: "o/r"})); err != nil {
		t.Fatal(err)
	}
	after, err := HashDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("writing %s changed the content hash: %q → %q", OriginFileName, before, after)
	}
}

func TestHashDirDetectsEdits(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: a\ndescription: b\n---\n")
	before, _ := HashDir(dir)

	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: a\ndescription: b\n---\nour own words\n")
	if after, _ := HashDir(dir); after == before {
		t.Error("expected an edited skill to hash differently")
	}

	writeFile(t, filepath.Join(dir, "references", "extra.md"), "more\n")
	if after, _ := HashDir(dir); after == before {
		t.Error("expected an added file to hash differently")
	}
}

func TestOriginRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if IsFork(dir) {
		t.Fatal("a bare directory should not read as a fork")
	}

	want := NewOrigin("our-review", Upstream{
		Name: "code-review", Source: "owner/repo", SourceType: "github",
		SourceURL: "https://github.com/owner/repo", Ref: "v1.2.0", Commit: "abc123",
		SkillPath: "skills/code-review/SKILL.md", License: "MIT", LicenseFile: UpstreamLicenseFileName,
	})
	want.ContentHash = "sha256:deadbeef"
	if err := WriteOrigin(dir, want); err != nil {
		t.Fatal(err)
	}
	if !IsFork(dir) {
		t.Error("expected IsFork to be true after WriteOrigin")
	}

	got, err := ReadOrigin(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Skill != "our-review" || got.Upstream.Ref != "v1.2.0" || got.ContentHash != "sha256:deadbeef" {
		t.Errorf("round trip lost data: %+v", got)
	}
	if got.ForkedAt == "" {
		t.Error("expected ForkedAt to be stamped")
	}
}

func TestListForksSkipsNonForksAndFlagsEdits(t *testing.T) {
	root := t.TempDir()

	forked := filepath.Join(root, "our-review")
	writeFile(t, filepath.Join(forked, "SKILL.md"), "---\nname: our-review\ndescription: b\n---\n")
	o := NewOrigin("our-review", Upstream{Name: "code-review", Source: "owner/repo"})
	hash, err := HashDir(forked)
	if err != nil {
		t.Fatal(err)
	}
	o.ContentHash = hash
	if err := WriteOrigin(forked, o); err != nil {
		t.Fatal(err)
	}

	// A hand-written skill in the same directory is not a fork.
	writeFile(t, filepath.Join(root, "ours", "SKILL.md"), "---\nname: ours\ndescription: b\n---\n")

	forks, err := ListForks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(forks) != 1 || forks[0].Name != "our-review" {
		t.Fatalf("expected only the forked skill, got %+v", forks)
	}
	if forks[0].Modified() {
		t.Error("an untouched fork should not read as modified")
	}

	writeFile(t, filepath.Join(forked, "SKILL.md"), "---\nname: our-review\ndescription: b\n---\nedited\n")
	forks, _ = ListForks(root)
	if !forks[0].Modified() {
		t.Error("expected an edited fork to read as modified")
	}
}

func TestModifiedIsFalseWithoutARecordedHash(t *testing.T) {
	f := Fork{Hash: "sha256:abc"}
	if f.Modified() {
		t.Error("an unknown fork-point hash must not read as modified")
	}
}

func TestFindLicenseFile(t *testing.T) {
	dir := t.TempDir()
	if got := FindLicenseFile(dir); got != "" {
		t.Errorf("expected no license file, got %q", got)
	}
	writeFile(t, filepath.Join(dir, "license.md"), "MIT")
	if got := FindLicenseFile(dir); got != "license.md" {
		t.Errorf("expected case-insensitive match, got %q", got)
	}
}

func TestDetectLicenseID(t *testing.T) {
	cases := map[string]string{
		"Permission is hereby granted, free of charge, to any person":             "MIT",
		"Apache License\nVersion 2.0, January 2004":                               "Apache-2.0",
		"Redistribution and use in source and binary forms, with or":              "BSD",
		"This is free and unencumbered software released into the public domain.": "Unlicense",
		"Some private terms of our own":                                           "",
	}
	for text, want := range cases {
		if got := DetectLicenseID(text); got != want {
			t.Errorf("DetectLicenseID(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestRenderAttributionStatesTheLicensePosition(t *testing.T) {
	licensed := RenderAttribution(NewOrigin("ours", Upstream{
		Name: "theirs", Source: "owner/repo", SourceURL: "https://github.com/owner/repo",
		Ref: "v1", License: "MIT", LicenseFile: UpstreamLicenseFileName,
	}))
	for _, want := range []string{"theirs", "https://github.com/owner/repo", "MIT", UpstreamLicenseFileName} {
		if !strings.Contains(licensed, want) {
			t.Errorf("attribution missing %q:\n%s", want, licensed)
		}
	}

	unlicensed := RenderAttribution(NewOrigin("ours", Upstream{Name: "theirs", Source: "owner/repo"}))
	if !strings.Contains(unlicensed, "redistribution rights are granted") {
		t.Errorf("an unlicensed fork must say so:\n%s", unlicensed)
	}
}
