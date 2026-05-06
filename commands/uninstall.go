package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/ui"
)

func buildUninstallCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:     "uninstall",
		Short:   "Remove the " + appName + " binary from your system",
		Aliases: []string{"remove-cli"},
		Args:    cobra.NoArgs,
		Long: fmt.Sprintf(`Uninstall the `+appName+` CLI by deleting its binary.

%sExamples:%s
  mdm uninstall
  mdm uninstall -y`, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			runUninstall(yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runUninstall(yes bool) {
	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not determine binary path: %v\n", err)
		os.Exit(1)
	}
	// Resolve any symlinks so we remove the real binary.
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not resolve binary path: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%sBinary location:%s %s\n\n", ansiDim, ansiReset, execPath)

	if !yes {
		confirmed, ok := ui.UiConfirm(fmt.Sprintf("Remove %s from your system?", appName))
		if !ok || !confirmed {
			fmt.Println("Cancelled.")
			return
		}
	}

	fmt.Println()

	if runtime.GOOS == "windows" {
		uninstallWindows(execPath)
		return
	}

	if err := os.Remove(execPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s✗%s Failed to remove %s: %v\n", ansiRed, ansiReset, execPath, err)
		os.Exit(1)
	}
	fmt.Printf("%s✓%s %s removed from %s\n", ansiText, ansiReset, appName, filepath.Dir(execPath))
}

// uninstallWindows schedules deletion via a batch script because Windows locks
// the executable while it is running.
func uninstallWindows(execPath string) {
	batchPath := filepath.Join(os.TempDir(), appName+"-uninstall.bat")
	escapedExec := strings.ReplaceAll(execPath, "%", "%%")
	escapedBatch := strings.ReplaceAll(batchPath, "%", "%%")
	batchContent := fmt.Sprintf(
		"@echo off\r\ntimeout /t 1 /nobreak > NUL\r\ndel /f /q \"%s\"\r\ndel /f /q \"%s\"\r\n",
		escapedExec, escapedBatch,
	)
	if err := os.WriteFile(batchPath, []byte(batchContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write uninstall script: %v\n", err)
		os.Exit(1)
	}
	if err := exec.Command("cmd", "/c", "start", "/b", "", batchPath).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to launch uninstall script: %v\n", err)
		fmt.Printf("%sTo uninstall manually, delete:%s\n  %s\n", ansiDim, ansiReset, execPath)
		return
	}
	fmt.Printf("%s✓%s %s will be removed after this process exits.\n", ansiText, ansiReset, appName)
	os.Exit(0)
}
