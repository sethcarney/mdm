// Shared helpers for the `--json` output modes.
//
// The contract every `--json` command holds to: the top-level value is an
// array even for a single result, an empty result is `[]` with exit 0 rather
// than the human "nothing installed" text, the JSON is the only thing on
// stdout, and it carries no ANSI escapes. Field names are a public contract
// once released - the VS Code extension parses them - so a rename keeps the
// old key.
package commands

import (
	"encoding/json"
	"fmt"
)

// printJSON writes v to stdout as indented JSON. Marshaling a value built
// from plain strings, ints and bools cannot fail, so the error is dropped the
// same way the other --json paths drop it.
func printJSON(v any) {
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
}

// emptyIfNil returns a non-nil slice, so an absent list marshals to `[]`
// rather than `null`. A caller iterating the field should not have to
// special-case the two.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
