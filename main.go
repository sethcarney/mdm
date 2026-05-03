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
	updateCh := update.CheckForUpdate(version.Version)

	root := commands.BuildRootCmd(version.Version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if update.IsTerminal() && os.Getenv("NO_COLOR") == "" {
		select {
		case latest := <-updateCh:
			if latest != "" {
				fmt.Printf("\n%sA new version of mdm is available: %s%s%s\n", ui.Yellow, ui.Bold, latest, ui.Reset)
				fmt.Printf("%sUpdate now with: mdm upgrade%s\n", ui.Dim, ui.Reset)
			}
		case <-time.After(500 * time.Millisecond):
		}
	}
}
