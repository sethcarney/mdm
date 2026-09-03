package commands

import (
	"fmt"
	"strings"

	"github.com/sethcarney/mdm/internal/agent"
)

// symlinkFallbacks collects the installs in one run that were copied because
// a symlink could not be created (see performSymlinkInstall). The fallback is
// per install and records nothing: the scope stays in symlink mode, so the
// next `mdm skills install` or `mdm skills update` tries to link again. One
// warning per run says so, rather than one per skill and agent, and gives
// `--copy` as the way to make the copies deliberate.
type symlinkFallbacks struct {
	agents []string
	seen   map[string]bool
}

// note records the result of one install for one agent. Results that did
// not fall back are ignored, so every install loop can call it unconditionally.
func (f *symlinkFallbacks) note(agentName string, r InstallResult) {
	if !r.SymlinkFailed {
		return
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	if f.seen[agentName] {
		return
	}
	f.seen[agentName] = true
	f.agents = append(f.agents, agentName)
}

// any reports whether at least one install fell back to a copy.
func (f *symlinkFallbacks) any() bool {
	return f != nil && len(f.agents) > 0
}

// warn prints the one-per-run warning. It prints nothing when no install
// fell back, so callers need not check first.
func (f *symlinkFallbacks) warn() {
	if !f.any() {
		return
	}
	names := make([]string, 0, len(f.agents))
	for _, a := range f.agents {
		if cfg := agent.AllAgents[a]; cfg != nil {
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
