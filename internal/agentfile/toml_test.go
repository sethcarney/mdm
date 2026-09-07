package agentfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// valuesEqual compares two decoded values for the round-trip test. A plain
// reflect.DeepEqual is too strict in exactly one place and too loose
// everywhere else that matters here:
//
//   - Too loose: DeepEqual cannot tell int64(3) from float64(3), and
//     json.Marshal comparison (tried and reverted) can't either — both would
//     wave through a TOML float silently becoming a TOML integer. This
//     compares numeric values by kind, so an integer and a float holding the
//     same number are never equal.
//   - Too strict: a TOML array of tables decodes to []map[string]any from
//     BurntSushi but []any (of map[string]any elements) from yaml.v3 — same
//     value, different concrete Go slice type. Map and slice comparisons
//     here recurse by reflect.Kind, indifferent to that difference, rather
//     than requiring identical concrete types.
func valuesEqual(a, b any) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if !av.IsValid() || !bv.IsValid() {
		return !av.IsValid() && !bv.IsValid()
	}
	aNum, bNum := isNumericKind(av.Kind()), isNumericKind(bv.Kind())
	if aNum || bNum {
		return aNum && bNum && numericValuesEqual(av, bv)
	}
	if av.Kind() == reflect.Map && bv.Kind() == reflect.Map {
		return mapValuesEqual(av, bv)
	}
	if isSliceKind(av.Kind()) && isSliceKind(bv.Kind()) {
		return sliceValuesEqual(av, bv)
	}
	return reflect.DeepEqual(a, b)
}

func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func isFloatKind(k reflect.Kind) bool {
	return k == reflect.Float32 || k == reflect.Float64
}

func isSliceKind(k reflect.Kind) bool {
	return k == reflect.Slice || k == reflect.Array
}

// numericValuesEqual is the one check that must distinguish kinds rather
// than just values: TOML tells an integer and a float apart, so 3 and 3.0
// are not equal here even though they are the same number.
func numericValuesEqual(av, bv reflect.Value) bool {
	aFloat, bFloat := isFloatKind(av.Kind()), isFloatKind(bv.Kind())
	if aFloat != bFloat {
		return false
	}
	if aFloat {
		return av.Float() == bv.Float()
	}
	return intValue(av) == intValue(bv)
}

func intValue(v reflect.Value) int64 {
	if v.Kind() >= reflect.Uint && v.Kind() <= reflect.Uint64 {
		return int64(v.Uint())
	}
	return v.Int()
}

func mapValuesEqual(av, bv reflect.Value) bool {
	if av.Len() != bv.Len() {
		return false
	}
	for _, k := range av.MapKeys() {
		bVal := bv.MapIndex(k)
		if !bVal.IsValid() || !valuesEqual(av.MapIndex(k).Interface(), bVal.Interface()) {
			return false
		}
	}
	return true
}

func sliceValuesEqual(av, bv reflect.Value) bool {
	if av.Len() != bv.Len() {
		return false
	}
	for i := 0; i < av.Len(); i++ {
		if !valuesEqual(av.Index(i).Interface(), bv.Index(i).Interface()) {
			return false
		}
	}
	return true
}

// A TOML definition carries the same three required ideas as a markdown one,
// with developer_instructions standing in for the body.
func TestParseAgentFileReadsTOML(t *testing.T) {
	got, err := ParseAgentFile("testdata/repo/agents/codex-style.toml")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil, want a parsed definition")
	}
	if got.Name != "codex-style" {
		t.Errorf("Name = %q, want codex-style", got.Name)
	}
	if got.Format != FormatTOML {
		t.Errorf("Format = %q, want %q", got.Format, FormatTOML)
	}
	if got.Instructions == "" {
		t.Error("Instructions is empty; developer_instructions did not reach it")
	}
	if got.Extra["model"] != "gpt-5" {
		t.Errorf("Extra[model] = %v, want gpt-5; non-required keys must pass through", got.Extra["model"])
	}
	if _, ok := got.Extra["mcp_servers"]; !ok {
		t.Error("Extra[mcp_servers] missing; a nested table must survive")
	}
	if _, ok := got.Extra["name"]; ok {
		t.Error("Extra must not repeat name; it has its own field")
	}
}

// A TOML file with no name or no description is not a definition. That is
// normal in a source tree, matching ParseAgentMd's contract for markdown, so
// it must come back as (nil, nil) rather than an error.
func TestParseAgentFileTOMLSkipsNonDefinition(t *testing.T) {
	got, err := ParseAgentFile("testdata/repo/agents/no-name.toml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil for a TOML file with no name", got)
	}
}

// A markdown definition keeps working and reports its format.
func TestParseAgentFileReadsMarkdown(t *testing.T) {
	got, err := ParseAgentFile("testdata/repo/agents/critic.md")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("got nil, want a parsed definition")
	}
	if got.Format != FormatMarkdown {
		t.Errorf("Format = %q, want %q", got.Format, FormatMarkdown)
	}
	if got.Instructions == "" {
		t.Error("Instructions is empty; the markdown body did not reach it")
	}
}

// Converting a definition and converting it back must not change it. This is
// the property the whole feature rests on: a TOML source installed to a
// markdown harness and back is the same definition.
func TestEncodeRoundTripsBothDirections(t *testing.T) {
	src, err := ParseAgentFile("testdata/repo/agents/codex-style.toml")
	if err != nil || src == nil {
		t.Fatalf("fixture did not parse: %v", err)
	}

	for _, via := range []Format{FormatMarkdown, FormatTOML} {
		t.Run(string(via), func(t *testing.T) {
			mid, err := Encode(src, via)
			if err != nil {
				t.Fatal(err)
			}
			ext := ".md"
			if via == FormatTOML {
				ext = ".toml"
			}
			p := filepath.Join(t.TempDir(), "a"+ext)
			if err := os.WriteFile(p, mid, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ParseAgentFile(p)
			if err != nil || got == nil {
				t.Fatalf("re-parse failed: %v", err)
			}
			if got.Name != src.Name || got.Description != src.Description {
				t.Errorf("name/description changed: %q/%q", got.Name, got.Description)
			}
			if got.Instructions != src.Instructions {
				t.Errorf("Instructions changed:\n got %q\nwant %q", got.Instructions, src.Instructions)
			}
			if !valuesEqual(got.Extra, src.Extra) {
				t.Errorf("Extra changed:\n got %#v\nwant %#v", got.Extra, src.Extra)
			}
		})
	}
}

// TOML's local date and local time values carry a "no UTC offset" meaning
// that neither a re-encoded TOML file nor YAML frontmatter can express: the
// synthetic zone BurntSushi uses to remember that meaning is TOML-internal,
// so re-encoding one of these values would silently turn it into an ordinary
// offset datetime. Encode refuses instead, naming the offending key.
func TestEncodeRejectsTOMLLocalTemporalValues(t *testing.T) {
	src, err := ParseAgentFile("testdata/repo/agents/codex-local-time.toml")
	if err != nil || src == nil {
		t.Fatalf("fixture did not parse: %v", err)
	}

	for _, f := range []Format{FormatMarkdown, FormatTOML} {
		t.Run(string(f), func(t *testing.T) {
			_, err := Encode(src, f)
			if err == nil {
				t.Fatal("Encode succeeded, want an error naming the local date/time key")
			}
			if !strings.Contains(err.Error(), "release_date") && !strings.Contains(err.Error(), "daily_check_in") {
				t.Errorf("error %q does not name the offending local date/time key", err.Error())
			}
		})
	}
}

// A TOML array of tables decodes to []map[string]any, a different concrete
// type from the []any a plain array decodes to. A detector that switches on
// concrete types (rather than reflect.Kind) misses this shape entirely, so a
// local time nested inside one would round-trip corrupted instead of being
// rejected. The error must name the nested key, not just the array's own key,
// so it is actionable in a large file.
func TestEncodeRejectsLocalTemporalInArrayOfTables(t *testing.T) {
	src, err := ParseAgentFile("testdata/repo/agents/codex-array-of-tables.toml")
	if err != nil || src == nil {
		t.Fatalf("fixture did not parse: %v", err)
	}

	for _, f := range []Format{FormatMarkdown, FormatTOML} {
		t.Run(string(f), func(t *testing.T) {
			_, err := Encode(src, f)
			if err == nil {
				t.Fatal("Encode succeeded, want an error naming the nested local time key")
			}
			if !strings.Contains(err.Error(), "servers[0].when") {
				t.Errorf("error %q does not name the nested key servers[0].when", err.Error())
			}
		})
	}
}

// An array of tables holding only safe values (strings, ints, bools) must
// not be rejected. This guards against over-fixing the missing-shape hole by
// rejecting every array of tables regardless of what is inside it.
func TestEncodeAllowsArrayOfTablesWithSafeValues(t *testing.T) {
	src, err := ParseAgentFile("testdata/repo/agents/codex-style.toml")
	if err != nil || src == nil {
		t.Fatalf("fixture did not parse: %v", err)
	}

	for _, f := range []Format{FormatMarkdown, FormatTOML} {
		t.Run(string(f), func(t *testing.T) {
			if _, err := Encode(src, f); err != nil {
				t.Fatalf("Encode rejected a safe array of tables: %v", err)
			}
		})
	}
}

// A TOML float whose value happens to be whole (3.0) must round-trip through
// markdown as a float, not silently become a TOML integer: yaml.v3 writes a
// whole float64 as a bare, integer-looking scalar unless told otherwise, and
// TOML tells an integer and a float apart. Both the emitted text and the
// re-parsed type are checked, since either one alone could pass by accident.
func TestEncodeMarkdownPreservesWholeFloat(t *testing.T) {
	src, err := ParseAgentFile("testdata/repo/agents/codex-style.toml")
	if err != nil || src == nil {
		t.Fatalf("fixture did not parse: %v", err)
	}
	want, ok := src.Extra["rating"].(float64)
	if !ok {
		t.Fatalf("fixture's rating key is %T, want float64", src.Extra["rating"])
	}

	mid, err := Encode(src, FormatMarkdown)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mid), "3.0") {
		t.Errorf("encoded markdown does not spell the whole float with a decimal point:\n%s", mid)
	}

	p := filepath.Join(t.TempDir(), "a.md")
	if err := os.WriteFile(p, mid, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseAgentFile(p)
	if err != nil || got == nil {
		t.Fatalf("re-parse failed: %v", err)
	}
	gotVal, ok := got.Extra["rating"].(float64)
	if !ok {
		t.Fatalf("Extra[rating] came back as %T, want float64 (a TOML integer, not the source's TOML float)", got.Extra["rating"])
	}
	if gotVal != want {
		t.Errorf("Extra[rating] = %v, want %v", gotVal, want)
	}
}
