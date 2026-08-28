package tests_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Toolchain and tool versions are written down in several places: go.mod, the
// CI workflows, the Makefile, the dev container, and the Pre-PR checklist in
// AGENTS.md. Dependabot cannot keep those in step - it never rewrites go.mod's
// `go` directive, it does not parse GOTOOLCHAIN strings or `go install ...@v`
// lines inside workflow steps, and a group never spans package ecosystems.
//
// These tests are the backstop instead: whoever bumps a version, CI stays red
// until every copy agrees.
//
// Sources of truth:
//   - Go toolchain       → the `go` directive in go.mod
//   - lint/release tools → the *_VERSION assignments in .devcontainer/post-create.sh

// toolModulePath maps each tracked tool to the module path used to install it.
// Any `<module path>@<version>` reference anywhere in the scanned files has to
// name the version pinned in post-create.sh.
var toolModulePath = map[string]string{
	"golangci-lint": "github.com/golangci/golangci-lint/v2/cmd/golangci-lint",
	"govulncheck":   "golang.org/x/vuln/cmd/govulncheck",
	"goreleaser":    "github.com/goreleaser/goreleaser/v2",
}

// toolPinPattern extracts each tool's canonical version from post-create.sh.
var toolPinPattern = map[string]*regexp.Regexp{
	"golangci-lint": regexp.MustCompile(`GOLANGCI_LINT_VERSION="(v[^"]+)"`),
	"govulncheck":   regexp.MustCompile(`GOVULNCHECK_VERSION="(v[^"]+)"`),
	"goreleaser":    regexp.MustCompile(`GORELEASER_VERSION="(v[^"]+)"`),
}

const postCreateScript = ".devcontainer/post-create.sh"
const devcontainerJSON = ".devcontainer/devcontainer.json"

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	root, err := findModRoot()
	if err != nil {
		t.Fatalf("could not locate module root: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// versionScanFiles returns every file that may pin a toolchain or tool version,
// keyed by its repo-relative path. Add new ones here as they appear.
func versionScanFiles(t *testing.T) map[string]string {
	t.Helper()

	root, err := findModRoot()
	if err != nil {
		t.Fatalf("could not locate module root: %v", err)
	}

	rels := []string{"Makefile", "AGENTS.md", devcontainerJSON, postCreateScript}

	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatalf("glob workflows: %v", err)
	}
	for _, w := range workflows {
		rel, err := filepath.Rel(root, w)
		if err != nil {
			t.Fatalf("relative path for %s: %v", w, err)
		}
		rels = append(rels, filepath.ToSlash(rel))
	}

	out := make(map[string]string, len(rels))
	for _, rel := range rels {
		out[rel] = readRepoFile(t, rel)
	}
	return out
}

var goDirectivePattern = regexp.MustCompile(`(?m)^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)

func goModVersion(t *testing.T) string {
	t.Helper()
	m := goDirectivePattern.FindStringSubmatch(readRepoFile(t, "go.mod"))
	if m == nil {
		t.Fatal("no `go` directive found in go.mod")
	}
	return m[1]
}

func canonicalToolVersions(t *testing.T) map[string]string {
	t.Helper()

	script := readRepoFile(t, postCreateScript)
	out := make(map[string]string, len(toolPinPattern))
	for tool, re := range toolPinPattern {
		m := re.FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("%s: no version assignment found for %s", postCreateScript, tool)
		}
		out[tool] = m[1]
	}
	return out
}

// gotoolchainPin matches both the shell form (GOTOOLCHAIN=go1.26.5) and the
// JSON form ("GOTOOLCHAIN": "go1.26.5"). It deliberately requires a digit after
// "go" so post-create.sh's own GOTOOLCHAIN=go${GO_VERSION} - which derives the
// value rather than hardcoding it - is not treated as a pin.
var gotoolchainPin = regexp.MustCompile(`GOTOOLCHAIN["']?\s*[:=]\s*["']?go(\d+\.\d+(?:\.\d+)?)`)

func TestGoToolchainPinsMatchGoMod(t *testing.T) {
	want := goModVersion(t)

	found := 0
	for rel, content := range versionScanFiles(t) {
		for i, line := range strings.Split(content, "\n") {
			m := gotoolchainPin.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			found++
			if m[1] != want {
				t.Errorf("%s:%d pins GOTOOLCHAIN=go%s but go.mod requires go%s\n\t%s",
					rel, i+1, m[1], want, strings.TrimSpace(line))
			}
		}
	}

	if found == 0 {
		t.Errorf("no GOTOOLCHAIN pins found - versionScanFiles is probably missing a file")
	}
}

func TestToolInstallPinsMatchDevContainer(t *testing.T) {
	canonical := canonicalToolVersions(t)
	files := versionScanFiles(t)

	for tool, modPath := range toolModulePath {
		re := regexp.MustCompile(regexp.QuoteMeta(modPath) + `@(v[0-9][^\s"']*)`)
		for rel, content := range files {
			for _, m := range re.FindAllStringSubmatch(content, -1) {
				if m[1] != canonical[tool] {
					t.Errorf("%s installs %s@%s but %s pins %s@%s",
						rel, modPath, m[1], postCreateScript, tool, canonical[tool])
				}
			}
		}
	}
}

// TestPinnedVersionsInFixedLocations covers the places a version is written in
// a form the module-path scan cannot see: the GoReleaser action's `version`
// input, and the dev container table in AGENTS.md.
func TestPinnedVersionsInFixedLocations(t *testing.T) {
	canonical := canonicalToolVersions(t)

	docTableRow := func(tool string) *regexp.Regexp {
		return regexp.MustCompile(`(?m)^\|\s*` + regexp.QuoteMeta(tool) + `\s*\|\s*(v[0-9][^\s|]*)\s*\|`)
	}

	sites := []struct {
		what    string
		file    string
		pattern *regexp.Regexp
		want    string
	}{
		{
			what:    "goreleaser-action version input",
			file:    ".github/workflows/release.yml",
			pattern: regexp.MustCompile(`(?m)^\s*version:\s*'(v[0-9][^']*)'`),
			want:    canonical["goreleaser"],
		},
		{
			what:    "AGENTS.md dev container table (golangci-lint)",
			file:    "AGENTS.md",
			pattern: docTableRow("golangci-lint"),
			want:    canonical["golangci-lint"],
		},
		{
			what:    "AGENTS.md dev container table (govulncheck)",
			file:    "AGENTS.md",
			pattern: docTableRow("govulncheck"),
			want:    canonical["govulncheck"],
		},
		{
			what:    "AGENTS.md dev container table (goreleaser)",
			file:    "AGENTS.md",
			pattern: docTableRow("goreleaser"),
			want:    canonical["goreleaser"],
		},
	}

	for _, s := range sites {
		matches := s.pattern.FindAllStringSubmatch(readRepoFile(t, s.file), -1)
		if len(matches) != 1 {
			t.Errorf("%s: expected exactly one match in %s, found %d - the file moved or the pattern is stale",
				s.what, s.file, len(matches))
			continue
		}
		if got := matches[0][1]; got != s.want {
			t.Errorf("%s: %s says %s, want %s", s.what, s.file, got, s.want)
		}
	}
}

// TestDevContainerDerivesGoVersion locks in the single-source-of-truth design:
// the dev container must read the Go version from go.mod rather than repeat it.
func TestDevContainerDerivesGoVersion(t *testing.T) {
	goVersion := goModVersion(t)

	if strings.Contains(readRepoFile(t, devcontainerJSON), goVersion) {
		t.Errorf("%s hardcodes Go %s; the version must come from go.mod via %s",
			devcontainerJSON, goVersion, postCreateScript)
	}

	script := readRepoFile(t, postCreateScript)
	if strings.Contains(script, "go"+goVersion) {
		t.Errorf("%s hardcodes go%s; it must derive the version from go.mod", postCreateScript, goVersion)
	}
	if !strings.Contains(script, `go env -w "GOTOOLCHAIN=go${GO_VERSION}"`) {
		t.Errorf("%s no longer pins GOTOOLCHAIN from the go.mod directive", postCreateScript)
	}
}

// dockerfileFrom matches a FROM line, capturing the tag and the digest
// separately so each can be checked on its own.
var dockerfileFrom = regexp.MustCompile(`(?m)^FROM\s+(\S+?)(?::([^\s@]+))?(?:@(sha256:[0-9a-f]{64}))?[ \t]*$`)

// TestDevContainerBaseImageIsPinnedByDigest guards the base image pin. A tag
// alone is not a pin: `bookworm` moves whenever upstream rebuilds it, so two
// builds of the same commit can start from different images, and the second one
// is the one that fails. Nothing here breaks when the digest is dropped - the
// container still builds, just not reproducibly - so this test is the only
// thing standing between a tag-only FROM and a silently unpinned base.
func TestDevContainerBaseImageIsPinnedByDigest(t *testing.T) {
	const dockerfile = ".devcontainer/Dockerfile"

	matches := dockerfileFrom.FindAllStringSubmatch(readRepoFile(t, dockerfile), -1)
	if len(matches) == 0 {
		t.Fatalf("%s has no FROM line", dockerfile)
	}

	for _, m := range matches {
		image, tag, digest := m[1], m[2], m[3]
		ref := image
		if tag != "" {
			ref += ":" + tag
		}
		if digest == "" {
			t.Errorf("%s: FROM %s is pinned by tag only; resolve it with\n"+
				"\tdocker buildx imagetools inspect %s\n"+
				"and write it as %s@sha256:<digest>", dockerfile, ref, ref, ref)
		}
		// The tag is redundant to the daemon but not to the reader: it is the
		// only thing saying which Debian release the digest points at.
		if tag == "" {
			t.Errorf("%s: FROM %s carries a digest but no tag; keep both so the pin "+
				"stays readable", dockerfile, image)
		}
	}
}

// commentLine matches a whole-line // comment, which is the only comment style
// used in devcontainer.json. Stripping those makes the file parseable as JSON.
var commentLine = regexp.MustCompile(`(?m)^\s*//.*$`)

// TestClaudeCodeCredentialsPersist guards the volume that keeps Claude Code
// signed in across rebuilds. The failure mode is silent - a mount target that
// no longer matches remoteUser's home just quietly stops persisting anything,
// and the only symptom is being asked to log in again after every rebuild.
func TestClaudeCodeCredentialsPersist(t *testing.T) {
	var dc struct {
		RemoteUser   string                     `json:"remoteUser"`
		Mounts       []string                   `json:"mounts"`
		Features     map[string]json.RawMessage `json:"features"`
		ContainerEnv map[string]string          `json:"containerEnv"`
	}

	raw := commentLine.ReplaceAllString(readRepoFile(t, devcontainerJSON), "")
	if err := json.Unmarshal([]byte(raw), &dc); err != nil {
		t.Fatalf("parse %s: %v", devcontainerJSON, err)
	}

	hasFeature := false
	for id := range dc.Features {
		if strings.HasPrefix(id, "ghcr.io/anthropics/devcontainer-features/claude-code") {
			hasFeature = true
			break
		}
	}
	if !hasFeature {
		t.Errorf("%s no longer installs the claude-code feature", devcontainerJSON)
	}

	if dc.RemoteUser == "" {
		t.Fatalf("%s must set remoteUser: Claude Code refuses to run as root, and the "+
			"credential volume is mounted into that user's home", devcontainerJSON)
	}

	// Mounting the volume is only half of it. Claude Code keeps its OAuth
	// session in ~/.claude.json, which sits beside ~/.claude rather than inside
	// it, so a volume on the directory alone still loses the login on every
	// rebuild. CLAUDE_CONFIG_DIR pulls .claude.json into the config directory.
	wantConfigDir := "/home/" + dc.RemoteUser + "/.claude"
	if got := dc.ContainerEnv["CLAUDE_CONFIG_DIR"]; got != wantConfigDir {
		t.Errorf("%s sets CLAUDE_CONFIG_DIR=%q, want %q: without it ~/.claude.json "+
			"stays outside the mounted volume and the OAuth session is discarded on rebuild",
			devcontainerJSON, got, wantConfigDir)
	}

	wantTarget := "target=/home/" + dc.RemoteUser + "/.claude"
	for _, m := range dc.Mounts {
		if !strings.Contains(m, "/.claude") {
			continue
		}
		if !strings.Contains(m, wantTarget) {
			t.Errorf("%s mounts the Claude Code volume at a path that is not remoteUser %q's home:\n\t%s\n\twant %s",
				devcontainerJSON, dc.RemoteUser, m, wantTarget)
		}
		if !strings.Contains(m, "type=volume") {
			t.Errorf("%s should use a named volume, not a host bind mount, so no host "+
				"credential file is exposed to the container:\n\t%s", devcontainerJSON, m)
		}
		return
	}

	t.Errorf("%s has no ~/.claude mount; Claude Code credentials will be discarded on every rebuild",
		devcontainerJSON)
}
