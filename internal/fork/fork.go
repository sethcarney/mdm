// Package fork owns the provenance bookkeeping for cherry-picked skills —
// third-party skills copied into a project so the project can edit and ship
// them as its own.
//
// A fork deliberately keeps no link to a lock file. Everything needed to say
// where the material came from, and whether it has been changed since, lives
// inside the forked directory itself: a machine-readable origin file and a
// human-readable attribution notice. That is what makes a fork survive being
// committed, moved, renamed, or published in someone else's repository.
package fork

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// OriginFileName holds the machine-readable provenance record. The leading
	// dot keeps it out of copyDirectory's install path, so agent-facing copies
	// of the skill carry the notice but not the bookkeeping.
	OriginFileName = ".mdm-origin.json"

	// AttributionFileName is the notice that travels with the skill. It is not
	// dot-prefixed on purpose: it must follow the material when the fork is
	// installed or redistributed.
	AttributionFileName = "ATTRIBUTION.md"

	// UpstreamLicenseFileName is where upstream license text is copied when the
	// skill directory itself did not carry one.
	UpstreamLicenseFileName = "LICENSE.upstream"

	originVersion = 1
)

// Upstream records where a fork's material came from.
type Upstream struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	SourceType string `json:"sourceType"`
	SourceURL  string `json:"sourceUrl,omitempty"`
	// Via records the local path the material was copied through, when the
	// fork was taken from an already-installed copy rather than fetched from
	// the source directly.
	Via         string `json:"via,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Commit      string `json:"commit,omitempty"`
	SkillPath   string `json:"skillPath,omitempty"`
	License     string `json:"license,omitempty"`
	LicenseFile string `json:"licenseFile,omitempty"`
}

// Origin is the content of OriginFileName.
type Origin struct {
	Version     int      `json:"version"`
	Skill       string   `json:"skill"`
	Upstream    Upstream `json:"upstream"`
	ContentHash string   `json:"contentHash,omitempty"`
	ForkedAt    string   `json:"forkedAt"`
}

// Fork is a forked skill directory found on disk.
type Fork struct {
	Name    string // directory name under the forks directory
	Dir     string // absolute path
	Origin  Origin
	Hash    string // hash of the directory as it stands now
	HashErr error
}

// Modified reports whether the directory has changed since it was forked. It is
// false when no hash was recorded, since "unknown" should not read as "dirty".
func (f Fork) Modified() bool {
	return f.Origin.ContentHash != "" && f.Hash != "" && f.Origin.ContentHash != f.Hash
}

// NewOrigin builds an origin record stamped with the current time.
func NewOrigin(skillName string, up Upstream) Origin {
	return Origin{
		Version:  originVersion,
		Skill:    skillName,
		Upstream: up,
		ForkedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// WriteOrigin writes the provenance record into a forked skill directory.
func WriteOrigin(dir string, o Origin) error {
	if o.Version == 0 {
		o.Version = originVersion
	}
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, OriginFileName), append(data, '\n'), 0644)
}

// ReadOrigin reads the provenance record from a forked skill directory.
func ReadOrigin(dir string) (Origin, error) {
	data, err := os.ReadFile(filepath.Join(dir, OriginFileName))
	if err != nil {
		return Origin{}, err
	}
	var o Origin
	if err := json.Unmarshal(data, &o); err != nil {
		return Origin{}, fmt.Errorf("%s: %w", OriginFileName, err)
	}
	return o, nil
}

// IsFork reports whether dir carries a provenance record.
func IsFork(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, OriginFileName))
	return err == nil && !info.IsDir()
}

var skipDirs = map[string]bool{
	"node_modules": true,
	"__pycache__":  true,
}

// HashDir returns a deterministic sha256 over every file's relative path and
// contents, skipping the origin file itself so that recording the hash inside
// the directory does not change it. WalkDir visits in lexical order, which
// makes the digest stable across runs and platforms.
func HashDir(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if name == OriginFileName {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(h, filepath.ToSlash(rel))
		h.Write([]byte{0})
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		_, err = io.Copy(h, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

// ListForks returns every forked skill directly under root, sorted by name.
// Directories without an origin file are skipped: the forks directory is a
// normal source tree that may also hold hand-written skills.
func ListForks(root string) ([]Fork, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var forks []Fork
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !IsFork(dir) {
			continue
		}
		o, err := ReadOrigin(dir)
		if err != nil {
			continue
		}
		f := Fork{Name: e.Name(), Dir: dir, Origin: o}
		f.Hash, f.HashErr = HashDir(dir)
		forks = append(forks, f)
	}
	sort.Slice(forks, func(i, j int) bool { return forks[i].Name < forks[j].Name })
	return forks, nil
}

// licenseFileNames are the file names checked, in order, when looking for the
// terms a skill was published under.
var licenseFileNames = []string{
	"LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "LICENCE.md", "LICENCE.txt",
	"COPYING", "COPYING.md", "COPYING.txt", "UNLICENSE", "UNLICENSE.md",
	"LICENSE-MIT", "LICENSE-APACHE", "LICENSE.upstream",
}

// FindLicenseFile returns the name of the license file in dir, or "" when the
// directory declares no terms. Matching is case-insensitive because upstream
// repositories are inconsistent about it.
func FindLicenseFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	present := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			present[strings.ToUpper(e.Name())] = e.Name()
		}
	}
	for _, want := range licenseFileNames {
		if name, ok := present[strings.ToUpper(want)]; ok {
			return name
		}
	}
	return ""
}

// licenseMarkers maps a distinctive phrase to the SPDX identifier it implies.
// The list is deliberately short: guessing an identifier is a convenience for
// the attribution notice, and a wrong guess is worse than none.
var licenseMarkers = []struct{ marker, id string }{
	{"apache license", "Apache-2.0"},
	{"gnu affero general public license", "AGPL-3.0"},
	{"gnu lesser general public license", "LGPL-3.0"},
	{"gnu general public license", "GPL-3.0"},
	{"mozilla public license", "MPL-2.0"},
	{"redistribution and use in source and binary forms", "BSD"},
	{"permission is hereby granted, free of charge", "MIT"},
	{"permission to use, copy, modify, and/or distribute", "ISC"},
	{"this is free and unencumbered software released into the public domain", "Unlicense"},
	{"creative commons attribution", "CC-BY"},
}

// DetectLicenseID guesses an SPDX-style identifier from license text.
func DetectLicenseID(text string) string {
	lower := strings.ToLower(text)
	for _, m := range licenseMarkers {
		if strings.Contains(lower, m.marker) {
			return m.id
		}
	}
	return ""
}

// RenderAttribution builds the notice written into a forked skill directory.
func RenderAttribution(o Origin) string {
	var b strings.Builder
	b.WriteString("# Attribution\n\n")
	fmt.Fprintf(&b, "`%s` is a fork of a third-party skill, cherry-picked with [mdm](https://github.com/sethcarney/mdm).\n", o.Skill)
	b.WriteString("Everything below describes the material this copy started from. Changes made\n")
	b.WriteString("after the fork date are the work of this repository's maintainers, not of the\n")
	b.WriteString("original author.\n\n")

	rows := [][2]string{{"Upstream skill", "`" + o.Upstream.Name + "`"}}
	src := o.Upstream.SourceURL
	if src == "" {
		src = o.Upstream.Source
	}
	rows = append(rows, [2]string{"Source", src})
	if o.Upstream.Ref != "" {
		rows = append(rows, [2]string{"Ref", "`" + o.Upstream.Ref + "`"})
	}
	if o.Upstream.Commit != "" {
		rows = append(rows, [2]string{"Commit", "`" + o.Upstream.Commit + "`"})
	}
	if o.Upstream.SkillPath != "" {
		rows = append(rows, [2]string{"Path in source", "`" + o.Upstream.SkillPath + "`"})
	}
	if o.Upstream.Via != "" {
		rows = append(rows, [2]string{"Copied from", "`" + o.Upstream.Via + "`"})
	}
	rows = append(rows, [2]string{"License", licenseCell(o.Upstream)})
	rows = append(rows, [2]string{"Forked on", o.ForkedAt})

	b.WriteString("| | |\n| --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n", r[0], r[1])
	}
	b.WriteString("\n")

	if o.Upstream.LicenseFile == "" && o.Upstream.License == "" {
		b.WriteString("The source declared no license. Absent a license, the default is that no\n")
		b.WriteString("redistribution rights are granted — check with the original author before\n")
		b.WriteString("publishing this fork.\n")
		return b.String()
	}
	b.WriteString("The upstream license governs the material this fork started from. Keep this\n")
	b.WriteString("file — and the upstream license text alongside it — with the skill when you\n")
	b.WriteString("redistribute it.\n")
	return b.String()
}

func licenseCell(u Upstream) string {
	switch {
	case u.License != "" && u.LicenseFile != "":
		return fmt.Sprintf("%s (see `%s`)", u.License, u.LicenseFile)
	case u.License != "":
		return u.License + " (declared in the skill's frontmatter; no license file found)"
	case u.LicenseFile != "":
		return fmt.Sprintf("see `%s`", u.LicenseFile)
	default:
		return "**not declared upstream**"
	}
}
