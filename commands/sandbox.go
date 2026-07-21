package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/agent"
	"github.com/sethcarney/mdm/internal/sandbox"
	"github.com/sethcarney/mdm/internal/ui"
)

func buildSandboxCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sandbox",
		Short: "Harden AI agent sandboxes so they can't read secrets or escape the project",
		Long: fmt.Sprintf(`Configure the sandbox and permission settings of your AI coding agents
so they cannot read secret files and stay confined to the project
working directory.

Supported agents: Claude Code, Codex, GitHub Copilot CLI.

Each tool is configured through its own native mechanism — permission
deny rules and the OS sandbox for Claude Code, sandbox_mode and network
policy for Codex, and a pre-tool-use secret guard hook plus bypass-flag
lockdown for GitHub Copilot CLI. mdm only adds or tightens settings; it
never removes entries you wrote yourself.

%sSubcommands:%s
  setup   Apply the recommended baseline (interactive, or -a/-y for scripts)
  status  Check current configuration against the baseline`, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			_ = cmd.Help()
		},
	}
	cmd.AddCommand(buildSandboxSetupCmd(), buildSandboxStatusCmd())
	return cmd
}

func sandboxAgentCompletion(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	agents := sandbox.Agents()
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.Name+"\t"+a.DisplayName)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

// resolveSandboxAgents picks the target agents: explicit --agent flags win,
// -y with no flags means all supported, otherwise an interactive picker
// pre-selects the agents detected on this machine.
func resolveSandboxAgents(agentFilter []string, yes bool) ([]sandbox.Agent, error) {
	all := sandbox.Agents()

	if len(agentFilter) > 0 {
		var selected []sandbox.Agent
		for _, name := range agentFilter {
			if !sandbox.Supported(name) {
				return nil, fmt.Errorf("unsupported agent %q; sandbox setup supports: claude-code, codex, github-copilot", name)
			}
			for _, a := range all {
				if a.Name == name {
					selected = append(selected, a)
				}
			}
		}
		return selected, nil
	}

	if yes {
		return all, nil
	}

	options := make([]ui.UIOption, len(all))
	var preSelected []int
	for i, a := range all {
		hint := ""
		if cfg := agent.AllAgents[a.Name]; cfg != nil && cfg.DetectInstalled != nil && cfg.DetectInstalled() {
			hint = "detected"
			preSelected = append(preSelected, i)
		}
		options[i] = ui.UIOption{Label: a.DisplayName, Value: a.Name, Hint: hint}
	}
	indices, ok := ui.UiMultiselect("Which agents should be sandboxed?", options, true, preSelected, nil)
	if !ok {
		return nil, nil
	}
	var selected []sandbox.Agent
	for _, i := range indices {
		selected = append(selected, all[i])
	}
	return selected, nil
}

// ─── setup ────────────────────────────────────────────────────────────────────

func buildSandboxSetupCmd() *cobra.Command {
	var agentFilter []string
	var dryRun, yes bool

	cmd := &cobra.Command{
		Use:     "setup",
		Aliases: []string{"init", "apply"},
		Short:   "Apply the recommended sandbox baseline to your agents",
		Long: fmt.Sprintf(`Apply the recommended sandbox baseline.

%sWhat gets configured:%s
  Claude Code    .claude/settings.json — Read() deny rules for secret paths,
                 OS sandbox enabled, bypass-permissions mode disabled
  Codex          $CODEX_HOME/config.toml — workspace-write sandbox, network
                 off for sandboxed commands, approval to leave the sandbox
  Copilot CLI    .github/hooks/ secret guard hook (committable) and
                 ~/.copilot/settings.json --yolo/--allow-all lockdown

Existing configuration is preserved: mdm only adds missing baseline
entries and never loosens a stricter setting you already have.

%sExamples:%s
  mdm sandbox setup
  mdm sandbox setup --agent claude-code codex
  mdm sandbox setup --dry-run
  mdm sandbox setup -y`, ansiBold, ansiReset, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSandboxSetup(agentFilter, dryRun, yes)
		},
	}

	f := cmd.Flags()
	f.StringArrayVarP(&agentFilter, "agent", "a", nil, "Configure specific agents (repeatable)")
	f.BoolVar(&dryRun, "dry-run", false, "Show what would change without writing anything")
	f.BoolVarP(&yes, "yes", "y", false, "Apply without prompting (targets all supported agents unless --agent is given)")
	_ = cmd.RegisterFlagCompletionFunc("agent", sandboxAgentCompletion)

	return cmd
}

func runSandboxSetup(agentFilter []string, dryRun, yes bool) error {
	cwd, _ := os.Getwd()
	agents, err := resolveSandboxAgents(agentFilter, yes)
	if err != nil {
		return err
	}
	if agents == nil {
		fmt.Println("Cancelled.")
		return nil
	}

	planned, anyChanges, err := printSandboxPlan(agents, cwd)
	if err != nil {
		return err
	}
	if !anyChanges {
		fmt.Println()
		ui.LogSuccess("All selected agents already match the sandbox baseline")
		fmt.Println()
		return nil
	}
	if dryRun {
		fmt.Printf("%sDry run — nothing written.%s\n\n", ansiDim, ansiReset)
		return nil
	}

	if !yes {
		confirmed, ok := ui.UiConfirm("Apply these changes?")
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return nil
		}
		fmt.Println()
	}

	return applySandboxPlan(planned, cwd)
}

// printSandboxPlan shows each agent's pending changes and returns the agents
// that have any.
func printSandboxPlan(agents []sandbox.Agent, cwd string) (planned []sandbox.Agent, anyChanges bool, err error) {
	fmt.Println()
	for _, a := range agents {
		changes, err := a.Plan(cwd)
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", a.DisplayName, err)
		}
		fmt.Printf("  %s%s%s\n", ansiBold+ansiText, a.DisplayName, ansiReset)
		if len(changes) == 0 {
			fmt.Printf("    %s✓ already configured%s\n\n", ansiGreen, ansiReset)
			continue
		}
		anyChanges = true
		planned = append(planned, a)
		for _, c := range changes {
			verb := "update"
			if c.Create {
				verb = "create"
			}
			fmt.Printf("    %s%s%s %s\n", ansiYellow, verb, ansiReset, c.Path)
			fmt.Printf("      %s%s%s\n", ansiDim, c.Description, ansiReset)
		}
		fmt.Println()
	}
	return planned, anyChanges, nil
}

func applySandboxPlan(agents []sandbox.Agent, cwd string) error {
	for _, a := range agents {
		changes, err := a.Apply(cwd)
		if err != nil {
			return fmt.Errorf("%s: %w", a.DisplayName, err)
		}
		for _, c := range changes {
			ui.LogSuccess(fmt.Sprintf("%s — %s", a.DisplayName, c.Path))
		}
	}
	fmt.Println()
	printSandboxNotes(agents)
	fmt.Printf("%sRestart any running agent sessions to pick up the new configuration.%s\n\n", ansiDim, ansiReset)
	return nil
}

func printSandboxNotes(agents []sandbox.Agent) {
	for _, a := range agents {
		if len(a.Notes) == 0 {
			continue
		}
		fmt.Printf("  %s%s%s\n", ansiBold+ansiText, a.DisplayName, ansiReset)
		for _, note := range a.Notes {
			fmt.Printf("    %s•%s %s%s%s\n", ansiDim, ansiReset, ansiDim, note, ansiReset)
		}
		fmt.Println()
	}
}

// ─── status ───────────────────────────────────────────────────────────────────

type sandboxStatusEntry struct {
	Agent       string          `json:"agent"`
	DisplayName string          `json:"displayName"`
	Checks      []sandbox.Check `json:"checks"`
}

func buildSandboxStatusCmd() *cobra.Command {
	var agentFilter []string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check agent sandbox configuration against the baseline",
		Long: fmt.Sprintf(`Check each agent's current configuration against the recommended
sandbox baseline.

%sExamples:%s
  mdm sandbox status
  mdm sandbox status --agent claude-code
  mdm sandbox status --json`, ansiBold, ansiReset),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSandboxStatus(agentFilter, jsonOutput)
		},
	}

	cmd.Flags().StringArrayVarP(&agentFilter, "agent", "a", nil, "Limit to specific agents (repeatable)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output status as a JSON array")
	_ = cmd.RegisterFlagCompletionFunc("agent", sandboxAgentCompletion)

	return cmd
}

func runSandboxStatus(agentFilter []string, jsonOutput bool) error {
	cwd, _ := os.Getwd()

	var entries []sandboxStatusEntry
	for _, a := range sandbox.Agents() {
		if len(agentFilter) > 0 && !contains(agentFilter, a.Name) {
			continue
		}
		checks, err := a.Status(cwd)
		if err != nil {
			return fmt.Errorf("%s: %w", a.DisplayName, err)
		}
		entries = append(entries, sandboxStatusEntry{Agent: a.Name, DisplayName: a.DisplayName, Checks: checks})
	}

	if jsonOutput {
		out, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(out))
		return nil
	}
	printSandboxStatusTable(entries)
	return nil
}

func sandboxStateLabel(state sandbox.CheckState) string {
	switch state {
	case sandbox.StateOK:
		return fmt.Sprintf("%s✓%s", ansiGreen, ansiReset)
	case sandbox.StateWarn:
		return fmt.Sprintf("%s!%s", ansiYellow, ansiReset)
	case sandbox.StateUnsupported:
		return fmt.Sprintf("%s–%s", ansiDim, ansiReset)
	default:
		return fmt.Sprintf("%s✗%s", ansiRed, ansiReset)
	}
}

func printSandboxStatusTable(entries []sandboxStatusEntry) {
	fmt.Println()
	missing := 0
	for _, e := range entries {
		fmt.Printf("  %s%s%s\n", ansiBold+ansiText, e.DisplayName, ansiReset)
		for _, c := range e.Checks {
			if c.State == sandbox.StateMissing {
				missing++
			}
			fmt.Printf("    %s %-28s %s%s%s\n", sandboxStateLabel(c.State), c.Name, ansiDim, c.Detail, ansiReset)
		}
		fmt.Println()
	}
	if missing > 0 {
		fmt.Printf("%s%d check(s) below baseline — run 'mdm sandbox setup' to fix.%s\n\n", ansiYellow, missing, ansiReset)
	}
}
