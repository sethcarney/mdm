package agentfile

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/BurntSushi/toml"
)

// parseAgentTOML reads and parses one Codex-style TOML definition. It returns
// (nil, nil) when name or description is missing: that file is not a
// definition, which is normal in a source tree and not an error. Every key
// besides the three required ones lands in Extra, nested tables included, so
// a later conversion does not silently drop a model or an mcp_servers table.
func parseAgentTOML(path string) (*AgentFile, error) {
	var data map[string]any
	if _, err := toml.DecodeFile(path, &data); err != nil {
		return nil, err
	}
	name, _ := data["name"].(string)
	desc, _ := data["description"].(string)
	if name == "" || desc == "" {
		return nil, nil
	}
	instructions, _ := data["developer_instructions"].(string)
	extra := map[string]any{}
	for k, v := range data {
		if k == "name" || k == "description" || k == "developer_instructions" {
			continue
		}
		extra[k] = v
	}
	return &AgentFile{
		Name:         name,
		Description:  desc,
		Instructions: instructions,
		Path:         path,
		Format:       FormatTOML,
		Extra:        extra,
	}, nil
}

// tomlLocalZoneNames are the synthetic time.Location names BurntSushi gives a
// decoded TOML local date, local time, or local date-time, so it can remember
// that the value carries no UTC offset. Nothing outside TOML understands that
// tag: re-encoding one of these values, in TOML or in YAML, turns it into an
// ordinary offset datetime, which is a real change of meaning.
var tomlLocalZoneNames = map[string]bool{
	"date-local":     true,
	"time-local":     true,
	"datetime-local": true,
}

// firstUnsafeTemporalKey returns the dotted/indexed path to the first TOML
// local date, time or date-time in v, so a caller can name it in an error
// rather than silently reinterpreting it.
func firstUnsafeTemporalKey(v any, path string) (string, bool) {
	return walk(reflect.ValueOf(v), path, visitor{value: func(rv reflect.Value, _ string) bool {
		t, ok := timeValue(rv)
		return ok && tomlLocalZoneNames[t.Location().String()]
	}})
}

// firstTemporalKey returns the path to the first time.Time of any kind in v.
// A markdown source reaches it through yaml.v3, which resolves an unquoted
// date to a time.Time; neither encoder writes that back as the source spelled
// it, so the caller refuses by name.
func firstTemporalKey(v any, path string) (string, bool) {
	return walk(reflect.ValueOf(v), path, visitor{value: func(rv reflect.Value, _ string) bool {
		_, ok := timeValue(rv)
		return ok
	}})
}

// firstNilExtraKey returns the dotted/indexed path to the first nil value in
// extra, checking top-level keys in sorted order so the key an error names
// does not depend on Go's map iteration. TOML has no null, and BurntSushi's
// encoder answers a nil interface by writing nothing at all and returning no
// error (encode.go: `case reflect.Interface: if rv.IsNil() { return }`), so
// the key would vanish from the file with nobody told.
func firstNilExtraKey(extra map[string]any) (string, bool) {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if key, ok := walk(reflect.ValueOf(extra[k]), k, visitor{value: isNilValue}); ok {
			return key, true
		}
	}
	return "", false
}

// firstNonStringKey returns the dotted/indexed path to the first map key in v
// that is not a string. yaml.v3 decodes a mapping such as {8080: web} into
// map[interface{}]interface{} with an int key; quoting it would change its
// type, and TOML has no other way to write it, so the caller refuses by name.
func firstNonStringKey(v any, path string) (string, bool) {
	return walk(reflect.ValueOf(v), path, visitor{key: func(k reflect.Value, _ string) bool {
		if k.Kind() == reflect.Interface && !k.IsNil() {
			k = k.Elem()
		}
		return k.Kind() != reflect.String
	}})
}

// timeValue returns the time.Time rv holds, if it holds one.
func timeValue(rv reflect.Value) (time.Time, bool) {
	if !rv.IsValid() || !rv.CanInterface() {
		return time.Time{}, false
	}
	t, ok := rv.Interface().(time.Time)
	return t, ok
}

// isNilValue reports a nil: the invalid reflect.Value an `any` holding
// nothing gives, or a nil interface or pointer.
func isNilValue(rv reflect.Value, _ string) bool {
	if !rv.IsValid() {
		return true
	}
	return (rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer) && rv.IsNil()
}

// visitor is what walk calls on the way down: value for every node, with its
// path, and key for every map key, with the path of the entry it names.
// Returning true stops the walk at that path.
type visitor struct {
	value func(rv reflect.Value, path string) bool
	key   func(k reflect.Value, path string) bool
}

// walk visits v pre-order and returns the path where a visitor first answered
// true. It walks by reflect.Kind, not by a list of concrete types: a TOML
// array of tables decodes to []map[string]any, a different concrete type from
// the []any a plain array decodes to, and a hand-listed type switch is only
// ever as complete as whatever the decoder happens to produce today. A map
// entry's path is the parent's plus ".key"; an element's is the parent's
// plus "[index]", so a hit inside mcp_servers names the server, and one
// inside an array of tables names its position.
func walk(rv reflect.Value, path string, v visitor) (string, bool) {
	if v.value != nil && v.value(rv, path) {
		return path, true
	}
	if !rv.IsValid() {
		return "", false
	}
	switch rv.Kind() {
	case reflect.Interface, reflect.Pointer:
		if rv.IsNil() {
			return "", false
		}
		return walk(rv.Elem(), path, v)
	case reflect.Map:
		for _, k := range rv.MapKeys() {
			entry := fmt.Sprintf("%s.%v", path, k.Interface())
			if v.key != nil && v.key(k, entry) {
				return entry, true
			}
			if got, ok := walk(rv.MapIndex(k), entry, v); ok {
				return got, true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if got, ok := walk(rv.Index(i), fmt.Sprintf("%s[%d]", path, i), v); ok {
				return got, true
			}
		}
	}
	return "", false
}

// reservedTOMLKeys are the keys encodeTOML writes from the definition's own
// fields, named in the order an error should try them so the message a user
// sees does not depend on Go's map iteration. The value describes what the
// key holds, for that message.
//
// parseAgentTOML already keeps all three out of Extra, so a TOML source can
// never carry one. A markdown source can: ParseAgentMd only filters name and
// description, because in markdown the body, not a frontmatter key, is the
// instructions - so frontmatter may legitimately hold a key of that name and
// markdown encoding keeps it beside the body.
var reservedTOMLKeys = []struct{ key, holds string }{
	{"name", "name"},
	{"description", "description"},
	{"developer_instructions", "body"},
}

// encodeTOML renders a as a Codex-style TOML definition: name, description,
// developer_instructions, then every Extra key.
//
// It refuses a definition whose Extra carries one of those three key names.
// The Extra loop below writes into the same table, so such a key would
// silently replace what the definition itself says - a frontmatter
// developer_instructions would reach Codex as the whole of the agent's
// instructions with the real body dropped. It refuses a nil value for the
// same reason: TOML has no null, and the encoder writes nothing for one, so
// the key would leave the file unannounced. Both errors name the key and
// carry no package prefix: the installer prints them verbatim under a line
// that already names the definition.
func encodeTOML(a *AgentFile) ([]byte, error) {
	if key, ok := firstNilExtraKey(a.Extra); ok {
		return nil, fmt.Errorf("cannot encode %q: TOML has no null, so the key would be dropped from the file with nothing reported", key)
	}
	for _, r := range reservedTOMLKeys {
		if _, ok := a.Extra[r.key]; ok {
			return nil, fmt.Errorf("cannot encode %q: TOML writes the definition's %s under that key, so a frontmatter key of the same name would replace it - rename or remove the key", r.key, r.holds)
		}
	}
	doc := map[string]any{
		"name":                   a.Name,
		"description":            a.Description,
		"developer_instructions": a.Instructions,
	}
	for k, v := range a.Extra {
		doc[k] = v
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding TOML: %w", err)
	}
	return buf.Bytes(), nil
}
