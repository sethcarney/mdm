package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func buildKnowledgeInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new OKF bundle",
		Long: fmt.Sprintf(`Initialize a minimal, conformant OKF bundle in the current directory.

Creates <name>/index.md and an example concept document, or scaffolds
into the current directory if no name is given.

%sExamples:%s
  mdm knowledge init
  mdm knowledge init sales`, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeInit(args)
		},
	}
}

func knowledgeIndexTemplate(name string) string {
	return fmt.Sprintf(`---
title: %s
description: Knowledge bundle for %s.
---

# %s

Curated context for AI agents. Each concept lives in its own document;
link related concepts with ordinary markdown links.

## Concepts

- [Example concept](concepts/example-concept.md)
`, name, name, name)
}

const knowledgeConceptTemplate = `---
type: Concept
title: Example concept
description: Replace with one fact, definition, or playbook worth remembering.
tags: [example]
---

# Example concept

Describe one thing an agent should know about this domain: what it is,
where it lives, and how it relates to other concepts.

Link related documents with markdown links — root-relative from the
bundle, like [the index](/index.md), or relative to this file.
`

func runKnowledgeInit(args []string) {
	cwd, _ := os.Getwd()
	bundleDir := cwd
	name := filepath.Base(cwd)
	if len(args) > 0 && args[0] != "" {
		name = args[0]
		bundleDir = filepath.Join(cwd, name)
	}

	indexPath := filepath.Join(bundleDir, "index.md")
	if _, err := os.Stat(indexPath); err == nil {
		fmt.Printf("%sBundle already exists at %s%s%s\n", ansiText, ansiDim, indexPath, ansiReset)
		return
	}

	conceptDir := filepath.Join(bundleDir, "concepts")
	if err := os.MkdirAll(conceptDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(indexPath, []byte(knowledgeIndexTemplate(name)), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}
	conceptPath := filepath.Join(conceptDir, "example-concept.md")
	if err := os.WriteFile(conceptPath, []byte(knowledgeConceptTemplate), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
		os.Exit(1)
	}

	rel := func(p string) string {
		if r, err := filepath.Rel(cwd, p); err == nil {
			return r
		}
		return p
	}
	fmt.Printf("%sInitialized knowledge bundle: %s%s%s\n", ansiText, ansiDim, name, ansiReset)
	fmt.Println()
	fmt.Printf("%sCreated:%s\n", ansiDim, ansiReset)
	fmt.Printf("  %s\n", rel(indexPath))
	fmt.Printf("  %s\n", rel(conceptPath))
	fmt.Println()
	fmt.Printf("%sNext steps:%s\n", ansiDim, ansiReset)
	fmt.Printf("  1. Replace the example concept with real documents (one concept per file)\n")
	fmt.Printf("  2. Keep %sindex.md%s linking to every top-level concept\n", ansiText, ansiReset)
	fmt.Printf("  3. Check conformance with %smdm knowledge validate %s%s\n", ansiText, rel(bundleDir), ansiReset)
	fmt.Println()
}
