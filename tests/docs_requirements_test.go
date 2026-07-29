package tests_test

import (
	"regexp"
	"strings"
	"testing"
)

// The docs site installs its Python dependencies with
// `pip install --require-hashes`, which is what makes the pip command count as
// pinned for OpenSSF Scorecard and what stops a compromised or re-uploaded
// artifact from reaching the build. That guarantee has three moving parts:
//
//   - docs/requirements.in     → the direct dependencies (source of truth)
//   - docs/requirements.txt    → the generated lock: every dependency, == pinned, hashed
//   - .github/workflows/docs.yml → installs the lock with --require-hashes
//
// Any one of them drifting silently drops the protection: a hand-edited
// requirement without a hash makes pip fail the whole install, and dropping
// --require-hashes makes it succeed while pinning nothing. These tests keep the
// three in step.

const (
	docsRequirementsIn  = "docs/requirements.in"
	docsRequirementsTxt = "docs/requirements.txt"
	docsWorkflow        = ".github/workflows/docs.yml"
	docsLockTarget      = "docs-lock"
)

// pinnedRequirement matches a fully pinned requirement line, e.g.
// `mkdocs-material==9.7.7 \`. Anything looser (>=, ~=, bare name) does not match
// and is reported as unpinned.
var pinnedRequirement = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)==([^\s;\\]+)`)

// requirement is one entry in a requirements file plus the hashes attached to it.
type requirement struct {
	name    string
	version string
	hashes  int
	line    int
}

// parseRequirements splits a requirements file into its entries. Continuation
// lines (`--hash=...`, `# via ...`) are indented in pip-compile output, so a
// non-indented, non-comment line starts a new entry.
func parseRequirements(t *testing.T, rel string) []requirement {
	t.Helper()

	var (
		out  []requirement
		cur  *requirement
		body = readRepoFile(t, rel)
	)
	for i, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			continue
		case strings.HasPrefix(trimmed, "--hash="):
			if cur == nil {
				t.Fatalf("%s:%d: hash with no requirement above it", rel, i+1)
			}
			cur.hashes++
		case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
			// Some other continuation of the current entry; nothing to record.
			continue
		default:
			m := pinnedRequirement.FindStringSubmatch(trimmed)
			if m == nil {
				out = append(out, requirement{name: trimmed, line: i + 1})
				cur = nil
				continue
			}
			out = append(out, requirement{name: strings.ToLower(m[1]), version: m[2], line: i + 1})
			cur = &out[len(out)-1]
		}
	}
	return out
}

// TestDocsInstallRequiresHashes guards the flag itself. Without it pip happily
// installs the lock file while ignoring every hash in it.
func TestDocsInstallRequiresHashes(t *testing.T) {
	workflow := readRepoFile(t, docsWorkflow)

	install := regexp.MustCompile(`(?m)^\s*run:\s*(.*pip\s+install.*)$`).FindAllStringSubmatch(workflow, -1)
	if len(install) == 0 {
		t.Fatalf("%s: no `pip install` step found", docsWorkflow)
	}
	for _, m := range install {
		cmd := m[1]
		if !strings.Contains(cmd, "--require-hashes") {
			t.Errorf("%s: pip install without --require-hashes: %q\n"+
				"the hashes in %s are inert unless pip is told to enforce them",
				docsWorkflow, cmd, docsRequirementsTxt)
		}
		if !strings.Contains(cmd, docsRequirementsTxt) {
			t.Errorf("%s: pip install does not install %s: %q",
				docsWorkflow, docsRequirementsTxt, cmd)
		}
	}
}

// TestDocsLockIsFullyHashed checks the lock file holds up its end: pip rejects
// the entire install if a single requirement is missing a hash or an == pin, so
// a hand-edited entry breaks the docs build rather than weakening it quietly.
func TestDocsLockIsFullyHashed(t *testing.T) {
	reqs := parseRequirements(t, docsRequirementsTxt)
	if len(reqs) == 0 {
		t.Fatalf("%s: no requirements found", docsRequirementsTxt)
	}

	for _, r := range reqs {
		if r.version == "" {
			t.Errorf("%s:%d: %q is not pinned with ==; regenerate with `make %s`",
				docsRequirementsTxt, r.line, r.name, docsLockTarget)
			continue
		}
		if r.hashes == 0 {
			t.Errorf("%s:%d: %s==%s has no --hash; regenerate with `make %s`",
				docsRequirementsTxt, r.line, r.name, r.version, docsLockTarget)
		}
	}
}

// TestDocsLockMatchesDirectPins catches the lock going stale: a bump in
// requirements.in that was never compiled would otherwise leave CI installing
// the old version, including when the bump exists to clear an advisory.
func TestDocsLockMatchesDirectPins(t *testing.T) {
	locked := make(map[string]string)
	for _, r := range parseRequirements(t, docsRequirementsTxt) {
		locked[r.name] = r.version
	}

	direct := parseRequirements(t, docsRequirementsIn)
	if len(direct) == 0 {
		t.Fatalf("%s: no requirements found", docsRequirementsIn)
	}

	for _, r := range direct {
		if r.version == "" {
			continue // a range in the source file is fine; the lock resolves it
		}
		got, ok := locked[r.name]
		if !ok {
			t.Errorf("%s:%d: %s is missing from %s; regenerate with `make %s`",
				docsRequirementsIn, r.line, r.name, docsRequirementsTxt, docsLockTarget)
			continue
		}
		if got != r.version {
			t.Errorf("%s pins %s==%s but %s has %s; regenerate with `make %s`",
				docsRequirementsIn, r.name, r.version, docsRequirementsTxt, got, docsLockTarget)
		}
	}
}

// TestDocsLockPythonVersionMatchesWorkflow keeps the resolution honest. Markers
// like `python_version < "3.13"` mean a lock compiled for one interpreter can
// omit a dependency another one needs — and under --require-hashes a missing
// dependency fails the install instead of being fetched.
func TestDocsLockPythonVersionMatchesWorkflow(t *testing.T) {
	workflow := readRepoFile(t, docsWorkflow)
	m := regexp.MustCompile(`python-version:\s*"?(\d+\.\d+)"?`).FindStringSubmatch(workflow)
	if m == nil {
		t.Fatalf("%s: no python-version found for setup-python", docsWorkflow)
	}
	want := m[1]

	makefile := readRepoFile(t, "Makefile")
	target := regexp.MustCompile(`(?ms)^` + docsLockTarget + `:\n(.*?)(?:\n\n|\z)`).FindStringSubmatch(makefile)
	if target == nil {
		t.Fatalf("Makefile: no %s target found", docsLockTarget)
	}
	got := regexp.MustCompile(`--python-version[ =](\d+\.\d+)`).FindStringSubmatch(target[1])
	if got == nil {
		t.Fatalf("Makefile: %s target does not pass --python-version; the lock would be "+
			"resolved for whatever interpreter happens to be installed", docsLockTarget)
	}
	if got[1] != want {
		t.Errorf("Makefile: %s resolves for Python %s but %s builds on %s",
			docsLockTarget, got[1], docsWorkflow, want)
	}
}
