// Package agentfile discovers and parses agent-definition files: markdown
// files with name and description frontmatter, such as Claude Code subagents,
// and Codex's standalone TOML files. It is to agent definitions what
// internal/skill is to SKILL.md.
package agentfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sethcarney/mdm/internal/pathsafe"
	"github.com/sethcarney/mdm/internal/skill"
)

// Format is the on-disk shape of a definition. Codex reads TOML; every other
// supported harness reads markdown.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatTOML     Format = "toml"
)

// AgentFile is one parsed agent definition. Instructions is the markdown body
// or the TOML developer_instructions. Extra carries every other key so a
// conversion does not drop what it does not understand.
type AgentFile struct {
	Name         string
	Description  string
	Instructions string
	Path         string
	Format       Format
	Extra        map[string]any
}

// conventionalDirs are scanned after any manifest-declared agentsDirs.
var conventionalDirs = []string{"agents", "subagents", ".claude/agents", ".github/agents", ".agents/agents"}

// ParseAgentMd reads and parses one agent .md file. It returns (nil, nil) when
// the frontmatter has no name or no description: that file is not an agent
// definition, which is normal in a source tree and not an error.
func ParseAgentMd(path string) (*AgentFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, body := skill.ParseFrontmatter(string(raw))
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	if name == "" || desc == "" {
		return nil, nil
	}
	extra := map[string]any{}
	for k, v := range data {
		if k == "name" || k == "description" {
			continue
		}
		extra[k] = v
	}
	return &AgentFile{
		Name:         name,
		Description:  desc,
		Instructions: body,
		Path:         path,
		Format:       FormatMarkdown,
		Extra:        extra,
	}, nil
}

// FormatForExt maps a file extension to its format. Anything that is not TOML
// is markdown, which is what every harness but Codex reads.
func FormatForExt(ext string) Format {
	if strings.EqualFold(ext, ".toml") {
		return FormatTOML
	}
	return FormatMarkdown
}

// ParseAgentFile reads one definition in whichever format its extension names.
// It returns (nil, nil) when the file is not a definition, which is a normal
// thing to find in a source tree.
func ParseAgentFile(path string) (*AgentFile, error) {
	if FormatForExt(filepath.Ext(path)) == FormatTOML {
		return parseAgentTOML(path)
	}
	return ParseAgentMd(path)
}

// isDefinitionExt reports whether name has an extension DiscoverAgentFiles
// should hand to ParseAgentFile. Anything else is not a candidate definition.
func isDefinitionExt(name string) bool {
	ext := filepath.Ext(name)
	return strings.EqualFold(ext, ".md") || strings.EqualFold(ext, ".toml")
}

// The containment checks this package applies live in internal/pathsafe, which
// documents what each one covers.
var (
	isSafeRelDir     = pathsafe.IsSafeRelDir
	resolvedContains = pathsafe.ResolvedContains
)

// manifestAgentDirs reads agentsDirs from .claude-plugin/marketplace.json. An
// unsafe entry is dropped silently, like an invalid agent file: this data comes
// from the source being installed. resolvedRoot is the resolved search root, so
// the manifest path gets the same containment check as every other file read.
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
// agent-definition files. Manifest-declared directories are scanned first and
// the first occurrence of a name wins, so a source can say where its agents live.
func DiscoverAgentFiles(basePath, subpath string) ([]*AgentFile, error) {
	searchPath := basePath
	if subpath != "" {
		if !isSafeRelDir(subpath) {
			return nil, nil
		}
		searchPath = filepath.Join(basePath, subpath)
	}

	// Resolve the search root once. Every directory and file discovered below
	// must resolve inside this real path: a symlinked entry in the untrusted
	// source tree can otherwise point mdm at files elsewhere on disk.
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
			if e.IsDir() || !isDefinitionExt(e.Name()) {
				continue
			}
			filePath := filepath.Join(dirPath, e.Name())
			// A contained directory can hold a symlinked FILE pointing
			// outside the root (agents/ with a linked "leak.md" or
			// "leak.toml"). The directory-level check above misses that.
			if !resolvedContains(resolvedRoot, filePath) {
				continue
			}
			a, err := ParseAgentFile(filePath)
			if err != nil {
				// A nil AgentFile with no error means "no name or description
				// frontmatter", which is skipped below. An error means the
				// entry exists but could not be read, which is a real fault.
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
