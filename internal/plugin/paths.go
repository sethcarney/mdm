package plugin

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

const (
	rootVar = "${PLUGIN_ROOT}"
	dataVar = "${PLUGIN_DATA}"
)

// escapes reports whether a cleaned slash-separated relative path points
// outside its base.
func escapes(rel string) bool {
	cleaned := path.Clean(rel)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned)
}

// EscapesRoot reports whether an existing path inside the plugin directory
// resolves outside the filesystem-resolved plugin root - the spec's package
// boundary for every file a client reads. Nonexistent paths report false:
// there is nothing to read through them.
func EscapesRoot(root, target string) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false
	}
	return resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator))
}

// ResolveWithin resolves a "./"-prefixed plugin-relative path against root
// and verifies it stays inside root, following symlinks when the target
// exists. Symlinks may resolve to targets within the root; anything
// resolving outside is rejected.
func ResolveWithin(root, rel string) (string, error) {
	if !strings.HasPrefix(rel, "./") {
		return "", fmt.Errorf("plugin-relative path %q must begin with ./", rel)
	}
	if escapes(strings.TrimPrefix(rel, "./")) {
		return "", fmt.Errorf("path %q escapes the plugin root", rel)
	}
	abs := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(rel, "./")))
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// A target that does not exist yet cannot be symlink-checked;
		// the lexical containment check above still holds.
		return abs, nil //nolint:nilerr // lexically contained is the contract here
	}
	if resolved != resolvedRoot && !strings.HasPrefix(resolved, resolvedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the plugin root", rel)
	}
	return resolved, nil
}

// ValidateCwd enforces the spec's allowed cwd forms: a "./"-prefixed
// plugin-relative path, ${PLUGIN_ROOT} or ${PLUGIN_DATA} alone, or either
// placeholder followed by a non-escaping relative path.
func ValidateCwd(cwd string) error {
	if cwd == "" || cwd == rootVar || cwd == dataVar {
		return nil
	}
	for _, prefix := range []string{"./", rootVar + "/", dataVar + "/"} {
		if strings.HasPrefix(cwd, prefix) {
			if escapes(strings.TrimPrefix(cwd, prefix)) {
				return fmt.Errorf("cwd %q escapes its base directory", cwd)
			}
			return nil
		}
	}
	return fmt.Errorf("cwd %q must be ./-prefixed, ${PLUGIN_ROOT}[/...], or ${PLUGIN_DATA}[/...]", cwd)
}

// ExpandVars performs the spec's single, non-recursive textual replacement
// of ${PLUGIN_ROOT} and ${PLUGIN_DATA}. Replaced text is never rescanned,
// and unrecognized placeholder-like text remains literal.
func ExpandVars(s, rootAbs, dataAbs string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], rootVar):
			b.WriteString(rootAbs)
			i += len(rootVar)
		case strings.HasPrefix(s[i:], dataVar):
			b.WriteString(dataAbs)
			i += len(dataVar)
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}
