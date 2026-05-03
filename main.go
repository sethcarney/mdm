package main

import (
	"fmt"
	"os"

	"github.com/sethcarney/mdm/commands"
	"github.com/sethcarney/mdm/internal/version"
)

func main() {
	root := commands.BuildRootCmd(version.Version)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
