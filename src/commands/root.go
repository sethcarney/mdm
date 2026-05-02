package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/ui"
	"github.com/sethcarney/mdm/internal/version"
)

const appName = version.AppName

// ANSI shorthands — alias to ui constants so command files can keep using ansiXxx unchanged
const (
	ansiReset  = ui.Reset
	ansiBold   = ui.Bold
	ansiDim    = ui.Dim
	ansiText   = ui.Text
	ansiCyan   = ui.Cyan
	ansiYellow = ui.Yellow
	ansiGreen  = ui.Green
	ansiRed    = ui.Red
)

func showLogo(ver string) {
	fmt.Printf("\n%s%s%s%s %s%s%s\n\n", ansiBold, ansiText, appName, ansiReset, ansiDim, ver, ansiReset)
}

// multiValueFlags are flags that accept multiple space-separated values after a
// single flag instance (-a claude cursor) in addition to the repeated-flag form
// (-a claude -a cursor). Both styles are supported.
var multiValueFlags = map[string]bool{
	"agent": true, "a": true,
	"skill": true, "s": true,
}

// normalizeMultiFlags rewrites space-separated multi-value flags into the
// repeated-flag form that cobra/pflag expects.
// e.g. ["-a", "claude", "cursor"] → ["-a", "claude", "-a", "cursor"]
func normalizeMultiFlags(args []string) []string {
	result := make([]string, 0, len(args))
	i := 0
	for i < len(args) {
		arg := args[i]
		i++
		result = append(result, arg)

		var flagName string
		switch {
		case strings.HasPrefix(arg, "--") && !strings.Contains(arg, "="):
			flagName = arg[2:]
		case len(arg) == 2 && arg[0] == '-' && arg[1] != '-':
			flagName = string(arg[1])
		}

		if !multiValueFlags[flagName] {
			continue
		}
		// consume first value
		if i >= len(args) || strings.HasPrefix(args[i], "-") {
			continue
		}
		result = append(result, args[i])
		i++
		// expand extra space-separated values into repeated flag+value pairs
		for i < len(args) && !strings.HasPrefix(args[i], "-") {
			result = append(result, arg, args[i])
			i++
		}
	}
	return result
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// BuildRootCmd builds and returns the root cobra command.
func BuildRootCmd(ver string) *cobra.Command {
	root := &cobra.Command{
		Use:           appName,
		Short:         "The markdown management CLI",
		Long:          fmt.Sprintf("%s%s%s%s — The markdown management CLI. No telemetry · Fully open source.", ansiBold, ansiText, appName, ansiReset),
		Version:       ver,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			fmt.Printf("%s%s%s%s\n", ansiBold, ansiText, appName, ansiReset)
			fmt.Printf("%sThe markdown management CLI. No telemetry · Fully open source. (%s)%s\n", ansiDim, ver, ansiReset)
			fmt.Println()
			_ = cmd.Help()
		},
	}

	root.SetVersionTemplate(fmt.Sprintf("%s%s%s%s %s\n", ansiBold, ansiText, appName, ansiReset, ver))

	root.AddCommand(
		buildSkillsCmd(ver),
		buildUpgradeCmd(ver),
		buildCompletionCmd(root),
	)

	root.SetArgs(normalizeMultiFlags(os.Args[1:]))

	return root
}

// ─── completion ────────────────────────────────────────────────────────────────

func buildCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: fmt.Sprintf(`Generate a shell completion script for `+appName+`.

%sUsage:%s
  # Bash
  source <(`+appName+` completion bash)

  # Zsh
  `+appName+` completion zsh > "${fpath[1]}/_`+appName+`"

  # Fish
  `+appName+` completion fish | source`, ansiBold, ansiReset),
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		DisableFlagsInUseLine: true,
		Run: func(cmd *cobra.Command, args []string) {
			switch args[0] {
			case "bash":
				_ = root.GenBashCompletion(os.Stdout)
			case "zsh":
				_ = root.GenZshCompletion(os.Stdout)
			case "fish":
				_ = root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				_ = root.GenPowerShellCompletionWithDesc(os.Stdout)
			}
		},
	}
	return cmd
}
