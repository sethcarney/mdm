package commands

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/experimental"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildMigrateCmd() *cobra.Command {
	var dryRun, yes, noTombstone, force bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Move v1 lock files into mdm-lock.json and mdm-state.json",
		Long: fmt.Sprintf(`Migrate a project (and this machine's global state) from the v1 lock
files to the v2 layout:

  skills-lock.json + knowledge-lock.json + plugins-lock.json → mdm-lock.json
  ~/.agents/skills-lock.json                                 → ~/.agents/mdm-state.json

skills-lock.json is replaced with a tombstone by default, so anyone
still running a v1 mdm against the project finds a pointer to
mdm-lock.json instead of silence. Interactive runs offer to delete it
outright instead; --no-tombstone does the same without asking. Commit
mdm-lock.json and the removals together.

v2 reads the v1 files transparently, so migrating is not urgent — but
writes only ever go to the new files, and mdm doctor will keep pointing
here until the old ones are gone.

%sExamples:%s
  mdm migrate --dry-run
  mdm migrate -y`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return runMigrate(dryRun, yes, noTombstone, force)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be migrated without changing anything")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&noTombstone, "no-tombstone", false, "Delete skills-lock.json instead of leaving a tombstone for v1 binaries")
	cmd.Flags().BoolVar(&force, "force", false, "Retire legacy files even when they hold entries missing from mdm-lock.json (discards those entries)")
	return cmd
}

func runMigrate(dryRun, yes, noTombstone, force bool) error {
	cwd, _ := os.Getwd()

	plan, err := lock.PlanProjectMigration(cwd)
	if err != nil {
		return fmt.Errorf("cannot migrate: %w — fix or remove the file and re-run", err)
	}
	globalLegacyPath, globalNeeded := lock.LegacyGlobalLockExists()

	if !plan.Needed() && !globalNeeded {
		fmt.Printf("\n%sNothing to migrate — no v1 lock files found.%s\n\n", ansiDim, ansiReset)
		return nil
	}

	fmt.Println()
	if plan.Needed() {
		if err := printProjectMigrationPlan(plan, noTombstone, force); err != nil {
			return err
		}
	}
	if globalNeeded {
		fmt.Printf("%sGlobal:%s\n", ansiText, ansiReset)
		fmt.Printf("  %s → %s\n", globalLegacyPath, lock.GetGlobalStatePath())
	}
	fmt.Println()

	if dryRun {
		fmt.Printf("%sDry run — nothing was changed.%s\n\n", ansiDim, ansiReset)
		return nil
	}
	if !yes {
		confirmed, ok := ui.UiConfirm("Migrate now?")
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return nil
		}
		noTombstone = promptTombstoneCleanup(plan, noTombstone)
	}
	return executeMigration(cwd, plan.Needed(), globalNeeded, noTombstone)
}

// promptTombstoneCleanup offers to delete skills-lock.json outright instead
// of leaving the default tombstone. Only reached interactively; --yes keeps
// the tombstone and --no-tombstone skips the question.
func promptTombstoneCleanup(plan lock.ProjectMigration, noTombstone bool) bool {
	if noTombstone {
		return true
	}
	if _, ok := plan.Legacy["skills-lock.json"]; !ok {
		return false
	}
	idx, ok := ui.UiSelect("What should happen to the old skills-lock.json?", []ui.UIOption{
		{Label: "Leave a tombstone (recommended)", Hint: "points anyone still on v1 mdm at mdm-lock.json"},
		{Label: "Delete it", Hint: "clean tree; a v1 mdm in this project would quietly find no skills"},
	})
	return ok && idx == 1
}

// printProjectMigrationPlan renders the project half of the plan and
// enforces the --force requirement for discarding orphaned entries.
func printProjectMigrationPlan(plan lock.ProjectMigration, noTombstone, force bool) error {
	fmt.Printf("%sProject:%s\n", ansiText, ansiReset)
	for _, fname := range lock.LegacyProjectLockNames {
		count, ok := plan.Legacy[fname]
		if !ok {
			continue
		}
		fate := "delete"
		if fname == "skills-lock.json" && !noTombstone {
			fate = "leave a tombstone"
		}
		fmt.Printf("  %s — %d entr%s → mdm-lock.json, then %s\n",
			fname, count, map[bool]string{true: "ies", false: "y"}[count != 1], fate)
	}
	if plan.TargetExists {
		fmt.Printf("  %smdm-lock.json already exists; the legacy files above are leftovers%s\n", ansiDim, ansiReset)
	}
	if len(plan.Orphaned) > 0 {
		sort.Strings(plan.Orphaned)
		fmt.Printf("\n%sThese legacy entries are NOT in mdm-lock.json and would be discarded:%s\n", ansiYellow, ansiReset)
		for _, o := range plan.Orphaned {
			fmt.Printf("  %s\n", o)
		}
		if !force {
			fmt.Println()
			return fmt.Errorf("refusing to discard entries — re-add them with 'mdm skills add' (or knowledge/plugins add), or re-run with --force to drop them")
		}
	}
	return nil
}

func executeMigration(cwd string, projectNeeded, globalNeeded, noTombstone bool) error {
	if projectNeeded {
		if err := lock.ExecuteProjectMigration(cwd, !noTombstone); err != nil {
			return err
		}
		fmt.Printf("%s✓%s Project migrated to mdm-lock.json — commit it together with the removed files.\n", ansiGreen, ansiReset)
	}
	if globalNeeded {
		if err := lock.ExecuteGlobalMigration(); err != nil {
			return err
		}
		fmt.Printf("%s✓%s Global state migrated to %s.\n", ansiGreen, ansiReset, lock.GetGlobalStatePath())
	}
	clearGraduatedOptIns()
	fmt.Println()
	return nil
}

// clearGraduatedOptIns drops persisted experimental opt-ins for features
// that no longer exist (e.g. knowledge and plugins, which graduated in v2).
func clearGraduatedOptIns() {
	state := lock.ReadGlobalState()
	if len(state.Experimental) == 0 {
		return
	}
	kept := state.Experimental[:0]
	for _, name := range state.Experimental {
		if experimental.IsKnown(name) {
			kept = append(kept, name)
		}
	}
	if len(kept) == len(state.Experimental) {
		return
	}
	dropped := len(state.Experimental) - len(kept)
	state.Experimental = kept
	if err := lock.WriteGlobalState(state); err != nil {
		ui.LogWarn(fmt.Sprintf("could not update the global state file: %v", err))
		return
	}
	fmt.Printf("%s✓%s Cleared %d stale experimental opt-in(s) for graduated features.\n", ansiGreen, ansiReset, dropped)
}
