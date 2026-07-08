package commands

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/experimental"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildExperimentalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experimental",
		Short: "Manage experimental features",
		Long: fmt.Sprintf(`Manage experimental features.

Experimental features may change or be removed in any release and are
exempt from semantic versioning until they graduate.

Features can also be enabled for a single invocation with the %s
environment variable (comma-separated feature names, or "all"):

  %s=knowledge mdm knowledge list`, experimental.EnvVar, experimental.EnvVar),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}
	cmd.AddCommand(
		buildExperimentalListCmd(),
		buildExperimentalEnableCmd(),
		buildExperimentalDisableCmd(),
	)
	return cmd
}

func experimentalFeatureCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	names := make([]string, 0, len(experimental.All))
	for _, info := range experimental.All {
		names = append(names, string(info.Feature)+"\t"+info.Description)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func buildExperimentalListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "Show experimental features and their status",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			for _, info := range experimental.All {
				status := ansiDim + "disabled" + ansiReset
				switch {
				case experimental.EnabledByEnv(info.Feature):
					status = ansiGreen + "enabled" + ansiReset + ansiDim + " (via " + experimental.EnvVar + ")" + ansiReset
				case experimental.Persisted(info.Feature):
					status = ansiGreen + "enabled" + ansiReset
				}
				fmt.Printf("  %s%s%s  %s\n", ansiBold+ansiText, info.Feature, ansiReset, status)
				fmt.Printf("      %s%s%s\n", ansiDim, info.Description, ansiReset)
				fmt.Printf("      %sSpec: %s%s\n", ansiDim, info.SpecURL, ansiReset)
			}
			fmt.Println()
			fmt.Printf("%sEnable with:%s mdm experimental enable <feature>\n\n", ansiDim, ansiReset)
		},
	}
}

func validateExperimentalFeature(name string) error {
	if !experimental.IsKnown(name) {
		return fmt.Errorf("unknown experimental feature %q; run 'mdm experimental list' to see available features", name)
	}
	return nil
}

func buildExperimentalEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "enable <feature>",
		Short:             "Enable an experimental feature",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: experimentalFeatureCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateExperimentalFeature(name); err != nil {
				return err
			}
			if err := experimental.Enable(experimental.Feature(name)); err != nil {
				return fmt.Errorf("could not persist experimental opt-in: %w", err)
			}
			ui.LogSuccess(fmt.Sprintf("%s enabled — this feature may change or be removed in any release", name))
			return nil
		},
	}
}

func buildExperimentalDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "disable <feature>",
		Short:             "Disable an experimental feature",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: experimentalFeatureCompletion,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateExperimentalFeature(name); err != nil {
				return err
			}
			if err := experimental.Disable(experimental.Feature(name)); err != nil {
				return fmt.Errorf("could not persist experimental opt-out: %w", err)
			}
			ui.LogSuccess(fmt.Sprintf("%s disabled", name))
			if experimental.EnabledByEnv(experimental.Feature(name)) {
				ui.LogWarn(fmt.Sprintf("%s is still active via %s in this environment", name, experimental.EnvVar))
			}
			return nil
		},
	}
}
