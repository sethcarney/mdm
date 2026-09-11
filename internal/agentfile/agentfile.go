// Package agentfile discovers and parses agent-definition files: markdown
// files with name and description frontmatter, such as Claude Code subagents,
// and Codex's standalone TOML files. It is to agent definitions what
// internal/skill is to SKILL.md.
package agentfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

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

// ConventionalDirs are scanned after any manifest-declared agentsDirs, and
// before the search path itself. Exported so a caller can name them when a
// search finds nothing.
var ConventionalDirs = []string{"agents", "subagents", ".claude/agents", ".github/agents", ".agents/agents"}

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
		// A file that carries the keys with the wrong shape meant to be a
		// definition; discovery reports it. One with no such keys is an
		// ordinary file and is passed over in silence.
		if key, ok := nonStringKey(data, "name", "description"); ok {
			return nil, &NotADefinitionError{Path: path, Reason: fmt.Sprintf("frontmatter %s is not a string", key)}
		}
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

// Encode renders a in format f. It refuses a definition whose Extra carries a
// TOML local date, local time, or local date-time: neither target format can
// express that value's "no UTC offset" meaning, so re-encoding it would
// silently change what it says rather than merely reformat it. The error names
// the offending key and carries no package prefix: the installer prints it to
// the user verbatim, under a line that already names the definition.
func Encode(a *AgentFile, f Format) ([]byte, error) {
	fromMarkdown := sourceFormat(a) == FormatMarkdown
	for k, v := range a.Extra {
		if key, ok := firstUnsafeTemporalKey(v, k); ok {
			return nil, fmt.Errorf("cannot encode %q: TOML local date/time values cannot be re-encoded without changing their meaning", key)
		}
		// yaml.v3 resolves an unquoted date to a time.Time, which TOML writes
		// as an offset datetime and YAML as an RFC 3339 timestamp: the same
		// class of silent change the guard above refuses the other way.
		if fromMarkdown {
			if key, ok := firstTemporalKey(v, k); ok {
				return nil, fmt.Errorf("cannot encode %q: YAML read it as a timestamp, and neither format writes that back as the source spelled it - quote the value in the source", key)
			}
		}
		if key, ok := firstNonStringKey(v, k); ok {
			return nil, fmt.Errorf("cannot encode %q: the key is not a string, and neither format can keep it as written - quote it in the source", key)
		}
	}
	if f == FormatTOML {
		return encodeTOML(a)
	}
	return encodeMarkdown(a)
}

// sourceFormat is the format a's bytes came in: Format when set, otherwise
// what the source extension says. A definition assembled in code can leave
// both empty, which reads as markdown.
func sourceFormat(a *AgentFile) Format {
	if a.Format != "" {
		return a.Format
	}
	return FormatForExt(filepath.Ext(a.Path))
}

// encodeMarkdown renders a as a markdown file: YAML frontmatter holding name,
// description and every Extra key, followed by the body. It refuses an Extra
// key named name or description: the loop below writes into the same map, so
// the key would silently replace what the definition itself says.
// developer_instructions is not reserved here, since in markdown the body is
// not a frontmatter key and the two sit side by side.
func encodeMarkdown(a *AgentFile) ([]byte, error) {
	for _, r := range reservedTOMLKeys {
		if r.key == "developer_instructions" {
			continue
		}
		if _, ok := a.Extra[r.key]; ok {
			return nil, fmt.Errorf("cannot encode %q: markdown writes the definition's %s under that frontmatter key, so a key of the same name would replace it - rename or remove the key", r.key, r.holds)
		}
	}
	fm := map[string]any{
		"name":        a.Name,
		"description": a.Description,
	}
	for k, v := range a.Extra {
		fm[k] = yamlEncodable(v)
	}
	yamlBytes, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("encoding frontmatter: %w", err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(yamlBytes)
	buf.WriteString("---\n")
	buf.WriteString(a.Instructions)
	return buf.Bytes(), nil
}

// yamlEncodable prepares v for YAML frontmatter encoding. Left alone, a TOML
// float whose value happens to be whole (3.0) marshals through yaml.v3 as a
// bare, integer-looking scalar and comes back as a TOML integer, silently
// changing what the source said. This walks v by reflect.Kind, the same
// approach as firstUnsafeTemporalKey, and wraps every such float, wherever
// it is nested, in a node that keeps its decimal point; everything else
// passes through unchanged.
func yamlEncodable(v any) any {
	return walkYAMLEncodable(reflect.ValueOf(v))
}

func walkYAMLEncodable(rv reflect.Value) any {
	if !rv.IsValid() {
		return nil
	}
	if f, ok := rv.Interface().(float64); ok {
		return wholeFloatNode(f)
	}
	switch rv.Kind() {
	case reflect.Interface, reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		return walkYAMLEncodable(rv.Elem())
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			// Encode has already refused a non-string key; fmt.Sprint keeps
			// the walker honest for an interface-kind key, where
			// Value.String would emit "<interface {} Value>" as the key.
			out[fmt.Sprint(k.Interface())] = walkYAMLEncodable(rv.MapIndex(k))
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = walkYAMLEncodable(rv.Index(i))
		}
		return out
	default:
		return rv.Interface()
	}
}

// wholeFloatNode returns f unchanged when it has a fractional part, which
// yaml.v3 already renders with a decimal point; a whole value instead gets
// an explicit *yaml.Node tagged !!float, so the emitted scalar (e.g. "3.0")
// reparses as a float rather than an int.
func wholeFloatNode(f float64) any {
	if f != math.Trunc(f) {
		return f
	}
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!float",
		Value: strconv.FormatFloat(f, 'f', -1, 64) + ".0",
	}
}

// NotADefinitionError says a file has a definition's shape but not its
// content: a name or description that is present and not a string.
// DiscoverAgentFiles notes it and moves on; a caller that only wants to know
// whether a file is a definition can treat it as "no".
type NotADefinitionError struct {
	Path   string
	Reason string
}

func (e *NotADefinitionError) Error() string { return e.Reason }

// nonStringKey returns the first of keys whose value is present in data and
// is not a string.
func nonStringKey(data map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := data[k]; ok {
			if _, isString := v.(string); !isString {
				return k, true
			}
		}
	}
	return "", false
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

// noteSkippedFile reports a candidate definition discovery could not read. A
// skip that says nothing leaves someone staring at a source whose definition
// never appeared, with no way to tell a typo in their TOML from mdm ignoring
// the file on purpose. It is a variable so a test can capture the note without
// reading os.Stderr. No program prefix: nothing else mdm prints carries one.
var noteSkippedFile = func(path string, err error) {
	fmt.Fprintf(os.Stderr, "  skipping %s: %v\n", path, err)
}

// DiscoverAgentFiles scans basePath, optionally joined with subpath, for
// agent-definition files: manifest-declared directories first, then the
// conventional ones, then searchPath itself. The first occurrence of a name
// wins, so a source can say where its agents live.
func DiscoverAgentFiles(basePath, subpath string) ([]*AgentFile, error) {
	searchPath := basePath
	if subpath != "" {
		// The subpath is the user's own #fragment, so an escape is a mistake
		// to report, as DiscoverSkills does; a manifest entry is third-party
		// and is dropped in silence.
		if !isSafeRelDir(subpath) {
			return nil, fmt.Errorf("invalid subpath: %q escapes the source directory", subpath)
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
	// The search path itself comes last: `mdm agents add ./my-agents` names a
	// directory of definitions, and a name a declared or conventional
	// directory already claimed still wins.
	dirs := append(manifestAgentDirs(searchPath, resolvedRoot), ConventionalDirs...)
	dirs = append(dirs, ".")
	for _, dir := range dirs {
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
				// A definition that does not parse is skipped, not fatal:
				// one malformed file must not block every valid definition
				// beside it in the same source. Markdown already behaves this
				// way, because skill.ParseFrontmatter falls back to "no
				// frontmatter" when the YAML will not unmarshal; TOML's
				// decoder reports the syntax error instead, which used to
				// abort the whole scan. The skip is announced rather than
				// silent, so the file's owner learns why it never appeared.
				noteSkippedFile(filePath, err)
				continue
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
