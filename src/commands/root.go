package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
		Use:   appName,
		Short: "The markdown management CLI",
		Long: fmt.Sprintf("  %s✓%s %sNo telemetry%s   %s✓%s %sOSV vulnerability scanning%s   %s✓%s %sOpen source%s",
			ansiGreen, ansiReset, ansiDim, ansiReset,
			ansiGreen, ansiReset, ansiDim, ansiReset,
			ansiGreen, ansiReset, ansiDim, ansiReset),
		Example:       "  mdm skills add https://github.com/anthropics/skills --skill frontend-design\n  mdm skills add https://github.com/vercel-labs/agent-skills --skill vercel-react-best-practices",
		Version:       ver,
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			fmt.Printf("%s%s%s%s  %s%s%s\n", ansiBold, ansiText, appName, ansiReset, ansiDim, ver, ansiReset)
			fmt.Println()
			_ = cmd.Help()
		},
	}

	root.SetVersionTemplate(fmt.Sprintf("%s%s%s%s %s\n", ansiBold, ansiText, appName, ansiReset, ver))

	root.AddCommand(
		buildSkillsCmd(ver),
		buildRulesCmd(),
		buildDoctorCmd(),
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
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			valid := map[string]bool{"bash": true, "zsh": true, "fish": true, "powershell": true}
			if !valid[args[0]] {
				return fmt.Errorf("unknown shell %q; valid options: bash, zsh, fish, powershell", args[0])
			}
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
			return nil
		},
	}
	cmd.AddCommand(buildCompletionInstallCmd())
	return cmd
}

func buildCompletionInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install completion into your shell rc file",
		Long: fmt.Sprintf(`Auto-detect your shell and append the %s completion hook to your rc file.

Supports bash, zsh, fish, and PowerShell. Run once after installing %s.`, appName, appName),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell, rcFile, line, err := detectShellSetup()
			if err != nil {
				return err
			}

			rcFile = expandTilde(rcFile)

			existing, readErr := os.ReadFile(rcFile)
			if readErr != nil && !os.IsNotExist(readErr) {
				return fmt.Errorf("reading %s: %w", rcFile, readErr)
			}
			if strings.Contains(string(existing), appName+" completion") {
				fmt.Printf("%s%s completion already installed in %s%s\n", ansiGreen, appName, rcFile, ansiReset)
				return nil
			}

			if err := os.MkdirAll(filepath.Dir(rcFile), 0755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}

			f, err := os.OpenFile(rcFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("opening %s: %w", rcFile, err)
			}
			defer f.Close()

			if _, err = fmt.Fprintf(f, "\n# %s shell completion\n%s\n", appName, line); err != nil {
				return fmt.Errorf("writing to %s: %w", rcFile, err)
			}

			fmt.Printf("%s✓%s %s completion installed for %s\n", ansiGreen, ansiReset, appName, shell)
			if shell == "powershell" {
				fmt.Printf("  %sRestart PowerShell or run:%s . \"%s\"\n", ansiDim, ansiReset, rcFile)
			} else {
				fmt.Printf("  %sRestart your terminal or run:%s source %s\n", ansiDim, ansiReset, rcFile)
			}
			return nil
		},
	}
}

func detectShellSetup() (shell, rcFile, completionLine string, err error) {
	shellPath := os.Getenv("SHELL")
	switch {
	case strings.Contains(shellPath, "zsh"):
		return "zsh", "~/.zshrc", fmt.Sprintf("source <(%s completion zsh)", appName), nil
	case strings.Contains(shellPath, "fish"):
		return "fish", "~/.config/fish/config.fish", fmt.Sprintf("%s completion fish | source", appName), nil
	case strings.Contains(shellPath, "bash"):
		rc := "~/.bashrc"
		if runtime.GOOS == "darwin" {
			rc = "~/.bash_profile"
		}
		return "bash", rc, fmt.Sprintf("source <(%s completion bash)", appName), nil
	case os.Getenv("PSModulePath") != "":
		return "powershell", powershellProfile(), fmt.Sprintf("Invoke-Expression (& %s completion powershell | Out-String)", appName), nil
	default:
		if shellPath == "" {
			return "", "", "", fmt.Errorf("could not detect shell; run `%s completion [bash|zsh|fish|powershell]` to generate the script manually", appName)
		}
		return "", "", "", fmt.Errorf("unsupported shell %q; run `%s completion [bash|zsh|fish|powershell]` to generate the script manually", filepath.Base(shellPath), appName)
	}
}

func powershellProfile() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	}
	return filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
}

func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
