package commands

import (
	"fmt"
	"strings"

	"github.com/sethcarney/mdm/internal/harness"
)

// symlinkFallbacks collects the installs in one run that were copied because a
// symlink could not be created (see performSymlinkInstall). The fallback records
// nothing: the scope stays in symlink mode, so the next install tries to link
// again. One warning per run names `--copy` as the way to make copies deliberate.
type symlinkFallbacks struct {
	harnesses []string
	seen      map[string]bool
}

// note records the result of one install for one harness. Results that did
// not fall back are ignored, so every install loop can call it unconditionally.
func (f *symlinkFallbacks) note(harnessName string, r InstallResult) {
	if !r.SymlinkFailed {
		return
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	if f.seen[harnessName] {
		return
	}
	f.seen[harnessName] = true
	f.harnesses = append(f.harnesses, harnessName)
}

// any reports whether at least one install fell back to a copy.
func (f *symlinkFallbacks) any() bool {
	return f != nil && len(f.harnesses) > 0
}

// warn prints the one-per-run warning. It prints nothing when no install
// fell back, so callers need not check first.
func (f *symlinkFallbacks) warn() {
	if !f.any() {
		return
	}
	names := make([]string, 0, len(f.harnesses))
	for _, a := range f.harnesses {
		if cfg := harness.AllHarnesses[a]; cfg != nil {
			names = append(names, cfg.DisplayName)
		} else {
			names = append(names, a)
		}
	}
	fmt.Printf("%s▲ Could not create symlinks for %s; those skills were copied instead.%s\n", ansiYellow, strings.Join(names, ", "), ansiReset)
	fmt.Printf("%s  The scope is still in symlink mode, so mdm skills install and mdm skills update will try to symlink again.%s\n", ansiDim, ansiReset)
	fmt.Printf("%s  If copies are what you want on this machine, run with --copy once to record it.%s\n", ansiDim, ansiReset)
	fmt.Println()
}

// materializedInstalls collects the harnesses in one run that received a real
// file by design: they read another format, or their directory is one people
// commit. It is deliberately not symlinkFallbacks. That type records a link
// that was attempted and refused by the machine, which --copy can settle;
// this one records a decision the harness itself forces, which nothing can.
type materializedInstalls struct {
	harnesses []string
	seen      map[string]bool
}

// note records one install. Results that were not materialized are ignored,
// so every install loop can call it unconditionally.
func (m *materializedInstalls) note(harnessName string, r InstallResult) {
	if !r.Materialized {
		return
	}
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	if m.seen[harnessName] {
		return
	}
	m.seen[harnessName] = true
	m.harnesses = append(m.harnesses, harnessName)
}

// any reports whether at least one install was materialized.
func (m *materializedInstalls) any() bool {
	return m != nil && len(m.harnesses) > 0
}

// materializeReason says why one harness received a real file, in the same
// order installAgentFile decides it.
func materializeReason(harnessName string) string {
	h := harness.AllHarnesses[harnessName]
	if h == nil {
		return "it does not take symlinks"
	}
	if h.AgentAlwaysMaterialize {
		return "its agents directory is committed to the repository"
	}
	return "it reads " + strings.ToUpper(string(harness.AgentFormat(harnessName)))
}

// explain prints the one-per-run note. Nothing went wrong, so it offers no
// remedy: --copy would change nothing, because the reason is the harness and
// not the machine. It prints nothing when no install was materialized.
func (m *materializedInstalls) explain() {
	if !m.any() {
		return
	}
	fmt.Printf("%sSome harnesses received a real file rather than a symlink:%s\n", ansiDim, ansiReset)
	for _, a := range m.harnesses {
		name := a
		if cfg := harness.AllHarnesses[a]; cfg != nil {
			name = cfg.DisplayName
		}
		fmt.Printf("%s  %s: %s.%s\n", ansiDim, name, materializeReason(a), ansiReset)
	}
	fmt.Println()
}
