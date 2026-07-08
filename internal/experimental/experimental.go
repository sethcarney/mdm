// Package experimental gates features that may change or be removed in any
// release and are exempt from semantic versioning until they graduate.
//
// A feature is active when it is named in the MDM_EXPERIMENTAL environment
// variable (comma-separated feature names, or "all"), or when it has been
// persisted with `mdm experimental enable <feature>`.
package experimental

import (
	"os"
	"sort"
	"strings"

	"github.com/sethcarney/mdm/internal/lock"
)

// Feature is a named experimental capability.
type Feature string

const Knowledge Feature = "knowledge"

// EnvVar enables features for a single invocation without persisting
// anything, e.g. MDM_EXPERIMENTAL=knowledge or MDM_EXPERIMENTAL=all.
const EnvVar = "MDM_EXPERIMENTAL"

type Info struct {
	Feature     Feature
	Description string
	SpecURL     string
}

// All lists every known experimental feature, in display order.
var All = []Info{
	{
		Feature:     Knowledge,
		Description: "Manage OKF knowledge bundles (mdm knowledge)",
		SpecURL:     "https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf",
	},
}

func IsKnown(name string) bool {
	for _, info := range All {
		if string(info.Feature) == name {
			return true
		}
	}
	return false
}

// Enabled reports whether f is active via MDM_EXPERIMENTAL or a persisted opt-in.
func Enabled(f Feature) bool {
	return EnabledByEnv(f) || Persisted(f)
}

// EnabledByEnv reports whether f is named in the MDM_EXPERIMENTAL variable.
func EnabledByEnv(f Feature) bool {
	for _, part := range strings.Split(os.Getenv(EnvVar), ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "all" || part == string(f) {
			return true
		}
	}
	return false
}

// Persisted reports whether f was enabled with `mdm experimental enable`.
func Persisted(f Feature) bool {
	for _, name := range lock.ReadSkillLock().Experimental {
		if name == string(f) {
			return true
		}
	}
	return false
}

// Enable persists the opt-in for f in the global lock file.
func Enable(f Feature) error {
	lk := lock.ReadSkillLock()
	for _, name := range lk.Experimental {
		if name == string(f) {
			return nil
		}
	}
	lk.Experimental = append(lk.Experimental, string(f))
	sort.Strings(lk.Experimental)
	return lock.WriteSkillLock(lk)
}

// Disable removes the persisted opt-in for f. It does not affect
// MDM_EXPERIMENTAL, which always wins.
func Disable(f Feature) error {
	lk := lock.ReadSkillLock()
	kept := make([]string, 0, len(lk.Experimental))
	for _, name := range lk.Experimental {
		if name != string(f) {
			kept = append(kept, name)
		}
	}
	if len(kept) == len(lk.Experimental) {
		return nil
	}
	lk.Experimental = kept
	return lock.WriteSkillLock(lk)
}
