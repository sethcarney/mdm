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

// firstUnsafeTemporalKey walks v looking for a TOML local date/time/datetime
// value, returning the dotted/indexed path to the first one found so a
// caller can name it in an error rather than silently reinterpreting it.
//
// It walks by reflect.Kind, not by a list of concrete types: a TOML array of
// tables decodes to []map[string]any, a different concrete type from the
// []any a plain array decodes to, and a hand-listed type switch is only ever
// as complete as whatever the decoder happens to produce today.
func firstUnsafeTemporalKey(v any, path string) (string, bool) {
	return walkUnsafeTemporal(reflect.ValueOf(v), path)
}

func walkUnsafeTemporal(rv reflect.Value, path string) (string, bool) {
	if !rv.IsValid() {
		return "", false
	}
	if t, ok := rv.Interface().(time.Time); ok {
		if tomlLocalZoneNames[t.Location().String()] {
			return path, true
		}
		return "", false
	}
	switch rv.Kind() {
	case reflect.Interface, reflect.Pointer:
		if rv.IsNil() {
			return "", false
		}
		return walkUnsafeTemporal(rv.Elem(), path)
	case reflect.Map:
		return walkUnsafeTemporalMap(rv, path)
	case reflect.Slice, reflect.Array:
		return walkUnsafeTemporalSlice(rv, path)
	default:
		return "", false
	}
}

// walkUnsafeTemporalMap checks every value in a map keyed by its map key, so
// a hit inside an mcp_servers-style nested table names that key, not just
// the table's own name.
func walkUnsafeTemporalMap(rv reflect.Value, path string) (string, bool) {
	for _, k := range rv.MapKeys() {
		if got, ok := walkUnsafeTemporal(rv.MapIndex(k), fmt.Sprintf("%s.%v", path, k.Interface())); ok {
			return got, true
		}
	}
	return "", false
}

// walkUnsafeTemporalSlice checks every element by index, so a hit inside a
// TOML array of tables names its position (e.g. servers[0].when), not just
// the array's own key.
func walkUnsafeTemporalSlice(rv reflect.Value, path string) (string, bool) {
	for i := 0; i < rv.Len(); i++ {
		if got, ok := walkUnsafeTemporal(rv.Index(i), fmt.Sprintf("%s[%d]", path, i)); ok {
			return got, true
		}
	}
	return "", false
}

// firstNilExtraKey returns the dotted/indexed path to the first nil value in
// extra, checking keys in sorted order so the key an error names does not
// depend on Go's map iteration.
func firstNilExtraKey(extra map[string]any) (string, bool) {
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if key, ok := walkNil(reflect.ValueOf(extra[k]), k); ok {
			return key, true
		}
	}
	return "", false
}

// walkNil finds a nil anywhere inside a value. TOML has no null, and
// BurntSushi's encoder answers a nil interface by writing nothing at all and
// returning no error (encode.go: `case reflect.Interface: if rv.IsNil() {
// return }`), so the key vanishes from the file with nobody told. It walks by
// reflect.Kind for the same reason walkUnsafeTemporal does: a TOML array of
// tables decodes to []map[string]any, not []any.
//
// An invalid reflect.Value is the untyped nil an `any` holding nothing gives,
// so it counts as a hit rather than as "nothing to look at".
func walkNil(rv reflect.Value, path string) (string, bool) {
	if !rv.IsValid() {
		return path, true
	}
	switch rv.Kind() {
	case reflect.Interface, reflect.Pointer:
		if rv.IsNil() {
			return path, true
		}
		return walkNil(rv.Elem(), path)
	case reflect.Map:
		return walkNilMap(rv, path)
	case reflect.Slice, reflect.Array:
		return walkNilSlice(rv, path)
	default:
		return "", false
	}
}

// walkNilMap checks every value in a map keyed by its map key, so a nil
// inside a nested table names that key rather than the table's own name.
func walkNilMap(rv reflect.Value, path string) (string, bool) {
	for _, k := range rv.MapKeys() {
		if got, ok := walkNil(rv.MapIndex(k), fmt.Sprintf("%s.%v", path, k.Interface())); ok {
			return got, true
		}
	}
	return "", false
}

// walkNilSlice checks every element by index, so a nil inside an array of
// tables names its position (e.g. servers[0].when).
func walkNilSlice(rv reflect.Value, path string) (string, bool) {
	for i := 0; i < rv.Len(); i++ {
		if got, ok := walkNil(rv.Index(i), fmt.Sprintf("%s[%d]", path, i)); ok {
			return got, true
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
		return nil, fmt.Errorf("agentfile: encoding TOML: %w", err)
	}
	return buf.Bytes(), nil
}
