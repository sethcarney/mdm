// Package agentfile discovers and parses agent-definition markdown files:
// single .md files with name and description frontmatter, such as Claude
// Code subagents. It is to agent definitions what internal/skill is to
// SKILL.md directories.
package agentfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sethcarney/mdm/internal/skill"
)

// AgentFile is one parsed agent-definition markdown file.
type AgentFile struct {
	Name        string
	Description string
	Path        string
}

// conventionalDirs are scanned after any manifest-declared agentsDirs.
var conventionalDirs = []string{"agents", "subagents", ".claude/agents", ".github/agents", ".agents/agents"}

// ParseAgentMd reads and parses one agent .md file. It returns (nil, nil)
// when the frontmatter has no name or no description: that file is not an
// agent definition, which is a normal thing to find in a source tree and
// not an error.
func ParseAgentMd(path string) (*AgentFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, _ := skill.ParseFrontmatter(string(raw))
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	if name == "" || desc == "" {
		return nil, nil
	}
	return &AgentFile{Name: name, Description: desc, Path: path}, nil
}

// isSafeRelDir reports whether d is a repo-relative directory that cannot
// escape the search root, judged purely from the path STRING. Both the
// caller's subpath and the manifest's agentsDirs go through it, because the
// manifest lives inside the source being installed and that source is
// third-party.
//
// This is a cheap first pass only. It cannot see that a directory entry
// which lexically looks fine (e.g. "custom-agents") is actually a symlink
// pointing somewhere else on disk; DiscoverAgentFiles additionally resolves
// every candidate directory and file against the real search root before
// reading it. filepath.IsAbs alone is not enough here either: on Windows
// it reports false for a rooted-but-driveless path like "/etc/passwd", so
// a naive absolute check would let that through. filepath.IsLocal covers
// that, plus "..", empty, and (on Windows) reserved device names in one
// lexical pass; "." is rejected on top because a source declaring "." would
// make the search root scan itself as a "declared" subdirectory, which is
// harmless but not what agentsDirs means.
func isSafeRelDir(d string) bool {
	if d == "" || d == "." {
		return false
	}
	return filepath.IsLocal(d)
}

// resolvedContains reports whether candidate, once symlinks are resolved,
// still lies inside resolvedRoot (itself already resolved by the caller).
//
// This data comes from the source being installed, which is untrusted: a
// directory or file inside it can be a symlink whose target lies anywhere
// on the victim's disk (e.g. a "custom-agents" entry that is really a link
// to the user's Documents folder). isSafeRelDir only inspects the path
// string declared in agentsDirs and can't see that; only resolving the
// entry on disk and comparing it against the real root closes that gap. Any
// resolution error, including a broken or looping symlink, is treated as
// unsafe rather than followed.
//
// This still leaves a check-then-use window: the caller resolves candidate
// here and then reopens the same unresolved path by name (os.ReadDir /
// os.ReadFile), so an attacker who can swap a real entry for a symlink
// between the two defeats it. That is accepted, not missed: closing it
// needs handle-based open-then-verify APIs Go does not expose portably,
// this is a one-shot scan at install time, and an attacker able to write
// into the source tree concurrently during an install can already just
// drop a hostile agent file in it directly, which is the threat this
// package already assumes.
func resolvedContains(resolvedRoot, candidate string) bool {
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// manifestAgentDirs reads agentsDirs from .claude-plugin/marketplace.json.
// An unsafe entry is dropped silently, the same treatment an invalid agent
// file gets: this data comes from the source being installed, not from
// mdm's own configuration, so a bad value is input to filter rather than a
// fault to report.
//
// resolvedRoot is the already-resolved search root; .claude-plugin or
// marketplace.json itself can be a symlink pointing outside the source, so
// the manifest path gets the same containment check as every other file
// this package opens before it is read. The impact of skipping that check
// would have been muted here specifically — these bytes are parsed only
// for the agentsDirs array and never surfaced to the user, so it would
// have been an unguarded read rather than an exfiltration path — but
// there's no reason to leave the one open this package doesn't guard.
func manifestAgentDirs(searchPath, resolvedRoot string) []string {
	manifestPath := filepath.Join(searchPath, ".claude-plugin", "marketplace.json")
	if !resolvedContains(resolvedRoot, manifestPath) {
		return nil
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	var m struct {
		AgentsDirs []string `json:"agentsDirs"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	var out []string
	for _, d := range m.AgentsDirs {
		if isSafeRelDir(d) {
			out = append(out, d)
		}
	}
	return out
}

// DiscoverAgentFiles scans basePath, optionally joined with subpath, for
// agent-definition files. Manifest-declared directories are scanned before
// the conventional ones, and the first occurrence of a name wins, so a
// source can say where its agents live.
func DiscoverAgentFiles(basePath, subpath string) ([]*AgentFile, error) {
	searchPath := basePath
	if subpath != "" {
		if !isSafeRelDir(subpath) {
			return nil, nil
		}
		searchPath = filepath.Join(basePath, subpath)
	}

	// Resolve the search root once. Every directory and file discovered
	// below must resolve to somewhere inside this real path (see
	// resolvedContains): the source tree being installed is untrusted, and
	// a symlinked entry inside it can otherwise point mdm at reading
	// arbitrary files elsewhere on the victim's disk.
	resolvedRoot, err := filepath.EvalSymlinks(searchPath)
	if err != nil {
		// The search root itself doesn't exist or can't be resolved (e.g. a
		// broken symlink at the root). There is nothing safe to scan.
		return nil, nil
	}

	seen := map[string]bool{}
	var out []*AgentFile
	for _, dir := range append(manifestAgentDirs(searchPath, resolvedRoot), conventionalDirs...) {
		dirPath := filepath.Join(searchPath, dir)
		if !resolvedContains(resolvedRoot, dirPath) {
			continue
		}
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			filePath := filepath.Join(dirPath, e.Name())
			// A directory can itself be safely contained while still
			// holding a symlinked FILE that points outside the root
			// (e.g. legitimate agents/ containing a linked "leak.md").
			// The directory-level check above does not catch that; each
			// file needs its own resolution.
			if !resolvedContains(resolvedRoot, filePath) {
				continue
			}
			a, err := ParseAgentMd(filePath)
			if err != nil {
				// A==nil-with-no-error from ParseAgentMd means "this file
				// has no name/description frontmatter", which is a normal,
				// expected thing to find and is skipped below. A non-nil
				// error here is different: the file exists as a directory
				// entry but couldn't be read (permission denied, I/O
				// fault). That is a real fault, not "not an agent", so it
				// is surfaced to the caller instead of silently vanishing.
				return nil, fmt.Errorf("agentfile: reading %s: %w", filePath, err)
			}
			if a == nil || seen[a.Name] {
				continue
			}
			seen[a.Name] = true
			out = append(out, a)
		}
	}
	return out, nil
}
