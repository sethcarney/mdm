package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func buildInitCmd(ver string) *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new skill",
		Long: fmt.Sprintf(`Initialize a new skill in the current directory.

Creates <name>/SKILL.md or ./SKILL.md if no name is given.

%sExamples:%s
  mdm skills init
  mdm skills init my-skill`, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			showLogo(ver)
			fmt.Println()
			runInitCmd(args)
		},
	}
}

func runInitCmd(args []string) {
	cwd, _ := os.Getwd()
	var skillName string
	hasName := len(args) > 0 && args[0] != ""
	if hasName {
		skillName = args[0]
	} else {
		skillName = filepath.Base(cwd)
	}

	var skillDir string
	if hasName {
		skillDir = filepath.Join(cwd, skillName)
	} else {
		skillDir = cwd
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")

	var displayPath string
	if hasName {
		displayPath = skillName + "/SKILL.md"
	} else {
		displayPath = "SKILL.md"
	}

	if _, err := os.Stat(skillFile); err == nil {
		fmt.Printf("%sSkill already exists at %s%s%s\n", ansiText, ansiDim, displayPath, ansiReset)
		return
	}

	if hasName {
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
			os.Exit(1)
		}
	}

	content := fmt.Sprintf(`---
name: %s
description: A brief description of what this skill does
---

# %s

Instructions for the agent to follow when this skill is activated.

## When to use

Describe when this skill should be used.

## Instructions

1. First step
2. Second step
3. Additional steps as needed
`, skillName, skillName)

	if err := os.WriteFile(skillFile, []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%sInitialized skill: %s%s%s\n", ansiText, ansiDim, skillName, ansiReset)
	fmt.Println()
	fmt.Printf("%sCreated:%s\n", ansiDim, ansiReset)
	fmt.Printf("  %s\n", displayPath)
	fmt.Println()
	fmt.Printf("%sNext steps:%s\n", ansiDim, ansiReset)
	fmt.Printf("  1. Edit %s%s%s to define your skill instructions\n", ansiText, displayPath, ansiReset)
	fmt.Printf("  2. Update the %sname%s and %sdescription%s in the frontmatter\n", ansiText, ansiReset, ansiText, ansiReset)
	fmt.Println()
	fmt.Printf("%sPublishing:%s\n", ansiDim, ansiReset)
	fmt.Printf("  %sGitHub:%s  Push to a repo, then %smdm skills add <owner>/<repo>%s\n", ansiDim, ansiReset, ansiText, ansiReset)
	fmt.Println()
	fmt.Printf("Browse existing skills for inspiration at %shttps://skills.sh/%s\n", ansiText, ansiReset)
	fmt.Println()
}
