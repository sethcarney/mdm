// `mdm install`: restore everything mdm.lock records - skills, agent
// definitions, knowledge bundles and plugins - in one command.
package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

// installAllOptions is the union of what the four per-section restores take.
// Nothing here is new: --skip-mcp comes from the plugin restore, the embedded
// restoreOptions from the skill and agent ones.
type installAllOptions struct {
	restoreOptions
	global  bool
	project bool
	skipMCP bool
}

// installScope is what resolveInstallScope decided. The two non-scope results
// are distinct because they end the run differently: scopeNone reports an
// empty lock, scopeCancelled has already said why it stopped.
type installScope int

const (
	scopeProject installScope = iota
	scopeGlobal
	scopeNone
	scopeCancelled
)

func buildInstallAllCmd(ver string) *cobra.Command {
	var opts installAllOptions

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Restore everything " + lockName + " records",
		Long: `Restore every skill, agent definition, knowledge bundle and plugin
recorded in ` + lockName + `, each re-fetched from its original source and
ref. Intended for CI and onboarding - run it after cloning a repo to get
the whole project back without remembering each package source.

Run in a directory holding a ` + lockName + ` it restores that project, with
no prompt. Run anywhere else it offers the globally recorded skills and
agent definitions instead; --project and --global choose explicitly.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			runInstallAll(opts)
			// A restore that could not install everything the lock describes
			// has to say so in its exit code, or CI treats a half-provisioned
			// checkout as a good one. Every section sets the same flag, so one
			// check here covers all four.
			if restoreFailed {
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&opts.allowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")
	cmd.Flags().BoolVar(&opts.copy, "copy", false, "Copy files instead of symlinking (switches the scope to copy mode)")
	cmd.Flags().BoolVar(&opts.symlink, "symlink", false, "Symlink files from .agents (the default; switches a scope back from copy mode)")
	cmd.Flags().BoolVar(&opts.skipMCP, "skip-mcp", false, "Do not wire MCP server config for restored plugins")
	cmd.Flags().BoolVar(&opts.project, "project", false, "Restore the project's "+lockName+" (the default when one is present)")
	cmd.Flags().BoolVar(&opts.global, "global", false, "Restore the globally recorded skills and agent definitions")
	// Each pair is one switch with two settings, so asking for both is a
	// contradiction rather than a precedence puzzle.
	cmd.MarkFlagsMutuallyExclusive("copy", "symlink")
	cmd.MarkFlagsMutuallyExclusive("project", "global")
	return cmd
}

// resolveInstallScope decides which lock the run restores. A project lock in
// the working directory is the whole answer when there is one: `mdm install`
// in a checkout restores that checkout, and asking a user standing in a repo
// which lock they meant is a question with one sensible answer. Global scope
// is the fallback for running it outside a project, never a silent one.
func resolveInstallScope(opts installAllOptions, cwd string) installScope {
	switch {
	case opts.global:
		return scopeGlobal
	case opts.project:
		return scopeProject
	}

	if fileExists(lock.GetProjectLockPath(cwd)) {
		return scopeProject
	}

	g := lock.ReadGlobalState()
	if len(g.Skills) == 0 && len(g.Agents) == 0 {
		return scopeNone
	}

	fmt.Printf("\n%sNo %s in %s.%s\n", ansiDim, lockName, cwd, ansiReset)
	fmt.Printf("%sThe global state file (%s) records %d skill(s) and %d agent definition(s).%s\n\n",
		ansiDim, lock.GetGlobalStatePath(), len(g.Skills), len(g.Agents), ansiReset)

	// --yes must not reach global scope on its own. A CI step that meant to
	// restore a checkout and found no lock would otherwise install into the
	// runner's home directory and report success for it.
	if opts.yes {
		fmt.Printf("Pass %s--global%s to restore them.\n\n", ansiText, ansiReset)
		return scopeCancelled
	}

	confirmed, ok := ui.UiConfirm("Restore from the global state file?")
	if !ok || !confirmed {
		fmt.Println("Cancelled.")
		return scopeCancelled
	}
	return scopeGlobal
}

// sectionCounts is how much each lock section has to restore in the chosen
// scope. The counts gate the four steps: a section with nothing in it is
// skipped silently rather than printing its own "nothing found" block, which
// for a skills-only project would otherwise be three paragraphs of noise.
type sectionCounts struct {
	skills    int
	agents    int
	knowledge int
	plugins   int
}

func (c sectionCounts) total() int { return c.skills + c.agents + c.knowledge + c.plugins }

// countSections tallies the chosen scope. Knowledge bundles and plugins are
// recorded in the project lock only, so they stay zero in global scope -
// noteProjectOnlySections reports them there rather than restoring them.
func countSections(global bool, cwd string) sectionCounts {
	if global {
		g := lock.ReadGlobalState()
		return sectionCounts{skills: len(g.Skills), agents: len(g.Agents)}
	}
	pl := lock.ReadProjectLock(cwd)
	return sectionCounts{
		skills:    len(pl.Skills),
		agents:    len(pl.Agents),
		knowledge: len(pl.Knowledge),
		plugins:   len(pl.Plugins),
	}
}

// installAllSteps are runInstallAll's four restores, as swappable vars. None
// of them leaves an on-disk trace a unit test can tell apart from the others',
// so tests swap these for recorders to pin down the order and the options.
var (
	installSkillsStep    = restoreSkillsInScope
	installAgentsStep    = restoreAgentsInScope
	installKnowledgeStep = runKnowledgeInstall
	installPluginsStep   = runPluginsInstall
)

func runInstallAll(opts installAllOptions) {
	cwd, _ := os.Getwd()

	scope := resolveInstallScope(opts, cwd)
	switch scope {
	case scopeCancelled:
		return
	case scopeNone:
		reportNoLockAnywhere(cwd)
		return
	}
	global := scope == scopeGlobal

	counts := countSections(global, cwd)
	vlog(verboseFlag, "install: scope global=%v skills=%d agents=%d knowledge=%d plugins=%d",
		global, counts.skills, counts.agents, counts.knowledge, counts.plugins)
	// Reporting an empty scope does not end the run: the zero counts below
	// skip every step anyway, and a global run still owes the note about the
	// project sections it is not the scope for.
	if counts.total() == 0 {
		reportEmptyScope(global, cwd)
	}

	// Plugins go last on purpose. A plugin skill whose name collides with a
	// standalone one is refused by installPluginSkillLink with a warning that
	// names both; the reverse order has refuseIfPluginOwned refuse the skills
	// entry instead, and that leaves a canonical directory - the plugin's own
	// symlink - which skillRestoredOnDisk would count as a successful restore.
	// Both orders are safe; only this one reports the collision honestly.
	if counts.skills > 0 {
		installSkillsStep(global, opts.restoreOptions, cwd)
	}
	if counts.agents > 0 {
		installAgentsStep(global, opts.restoreOptions, cwd)
	}
	if global {
		noteProjectOnlySections(cwd)
		return
	}
	if counts.knowledge > 0 {
		installKnowledgeStep(opts.allowHiddenChars)
	}
	if counts.plugins > 0 {
		installPluginsStep(opts.allowHiddenChars, opts.skipMCP)
	}
}

// reportNoLockAnywhere closes a run that found no project lock in the working
// directory and nothing recorded globally either. It is not a failure: a
// directory that records nothing is a normal one.
func reportNoLockAnywhere(cwd string) {
	fmt.Printf("\n%sNothing to restore - no %s in %s, and nothing recorded globally.%s\n\n",
		ansiDim, lockName, cwd, ansiReset)
	fmt.Printf("Add skills with %smdm skills add <package>%s\n\n", ansiText, ansiReset)
}

// reportEmptyScope closes a run whose chosen scope records nothing. The two
// scopes get different text because "no lock here" is the wrong thing to tell
// someone who asked for --global - their project lock is not what was read.
func reportEmptyScope(global bool, cwd string) {
	if global {
		fmt.Printf("\n%sNothing to restore - the global state file (%s) records no skills or agent definitions.%s\n\n",
			ansiDim, lock.GetGlobalStatePath(), ansiReset)
		return
	}
	if fileExists(lock.GetProjectLockPath(cwd)) {
		fmt.Printf("\n%sNothing to restore - %s records nothing yet.%s\n\n", ansiDim, lockName, ansiReset)
	} else {
		fmt.Printf("\n%sNothing to restore - no %s in %s.%s\n\n", ansiDim, lockName, cwd, ansiReset)
	}
	fmt.Printf("Add skills with %smdm skills add <package>%s\n\n", ansiText, ansiReset)
}

// noteProjectOnlySections says what a global run did not restore. Knowledge
// bundles and plugins are project-scoped, so a --global run standing in a
// project holding them would otherwise finish looking complete.
func noteProjectOnlySections(cwd string) {
	pl := lock.ReadProjectLock(cwd)
	if len(pl.Knowledge) == 0 && len(pl.Plugins) == 0 {
		return
	}
	fmt.Printf("%sThis project's %s also records %d knowledge bundle(s) and %d plugin(s). "+
		"Both are project-scoped - restore them with 'mdm install --project'.%s\n\n",
		ansiDim, lockName, len(pl.Knowledge), len(pl.Plugins), ansiReset)
}
