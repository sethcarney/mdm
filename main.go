package main

import (
	"fmt"
	"os"
	"time"

	"github.com/sethcarney/mdm/commands"
	"github.com/sethcarney/mdm/internal/ui"
	"github.com/sethcarney/mdm/internal/update"
	"github.com/sethcarney/mdm/internal/version"
)

func main() {
	// Only start the background update check when the notice would actually be
	// shown: interactive terminal, NO_COLOR not set, and not an upgrade command
	// (the in-memory version.Version is stale after the binary is replaced,
	// which would produce a false-positive "new version available" notice).
	var updateCh <-chan string
	if !isUpgradeCmd() && update.IsTerminal() && os.Getenv("NO_COLOR") == "" {
		updateCh = update.CheckForUpdate(version.Version)
	}

	root := commands.BuildRootCmd(version.Version)
	cmdErr := root.Execute()

	// Print the notice after command output (success or failure) so the user
	// always sees it.
	if updateCh != nil {
		select {
		case latest := <-updateCh:
			if latest != "" {
				fmt.Printf("\n%sA new version of mdm is available: %s%s%s\n", ui.Yellow, ui.Bold, latest, ui.Reset)
				fmt.Printf("%sUpdate now with: mdm upgrade%s\n", ui.Dim, ui.Reset)
			}
		case <-time.After(500 * time.Millisecond):
		}
	}

	if cmdErr != nil {
		fmt.Fprintln(os.Stderr, cmdErr)
		os.Exit(1)
	}
}

// isUpgradeCmd reports whether the invocation is an upgrade/self-update command.
func isUpgradeCmd() bool {
	upgradeAliases := map[string]bool{
		"upgrade":     true,
		"update-cli":  true,
		"self-update": true,
	}
	for _, arg := range os.Args[1:] {
		if upgradeAliases[arg] {
			return true
		}
	}
	return false
}
