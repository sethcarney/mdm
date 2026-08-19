package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/plugin"
)

func buildPluginsInitCmd() *cobra.Command {
	var withMCP bool

	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new Agent Plugin",
		Long: fmt.Sprintf(`Initialize a minimal, conformant Agent Plugin in the current directory.

Creates <name>/plugin.json and an example skill, or scaffolds into the
current directory if no name is given. The name must follow the spec's
rules: lowercase alphanumerics, hyphens, and periods.

%sExamples:%s
  mdm plugins init
  mdm plugins init my-plugin
  mdm plugins init my-plugin --with-mcp`, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsInit(args, withMCP)
		},
	}

	cmd.Flags().BoolVar(&withMCP, "with-mcp", false, "Also scaffold an mcp.json with an example stdio server")
	return cmd
}

func pluginManifestTemplate(name string) string {
	return fmt.Sprintf(`{
  "$schema": %q,
  "name": %q,
  "version": "0.1.0",
  "description": "Describe what this plugin provides."
}
`, plugin.ManifestSchema, name)
}

const pluginSkillTemplate = `---
name: example-skill
description: Replace with what this skill teaches an agent to do and when to use it.
---

# Example skill

Describe the task this skill covers: the steps to follow, the pitfalls
to avoid, and any scripts or references bundled alongside it.
`

func pluginMCPTemplate() string {
	return fmt.Sprintf(`{
  "$schema": %q,
  "mcpServers": {
    "example": {
      "type": "stdio",
      "command": "./scripts/server",
      "args": ["--data", "${PLUGIN_DATA}"]
    }
  }
}
`, plugin.MCPSchema)
}

func runPluginsInit(args []string, withMCP bool) {
	cwd, _ := os.Getwd()
	pluginDir := cwd
	name := filepath.Base(cwd)
	if len(args) > 0 && args[0] != "" {
		name = args[0]
		pluginDir = filepath.Join(cwd, name)
	}
	if err := plugin.ValidateName(name); err != nil {
		fmt.Fprintf(os.Stderr, "%sError:%s invalid plugin name %q: %s\n", ansiText, ansiReset, name, err)
		os.Exit(1)
	}

	manifestPath := filepath.Join(pluginDir, plugin.ManifestFile)
	if _, err := os.Stat(manifestPath); err == nil {
		fmt.Printf("%sPlugin already exists at %s%s%s\n", ansiText, ansiDim, manifestPath, ansiReset)
		return
	}

	skillDir := filepath.Join(pluginDir, plugin.SkillsDir, "example-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
		os.Exit(1)
	}
	created := []string{manifestPath, filepath.Join(skillDir, "SKILL.md")}
	writes := map[string]string{
		manifestPath: pluginManifestTemplate(name),
		created[1]:   pluginSkillTemplate,
	}
	if withMCP {
		mcpPath := filepath.Join(pluginDir, plugin.MCPFile)
		writes[mcpPath] = pluginMCPTemplate()
		created = append(created, mcpPath)
	}
	for _, p := range created {
		if err := os.WriteFile(p, []byte(writes[p]), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
	}

	rel := func(p string) string {
		if r, err := filepath.Rel(cwd, p); err == nil {
			return r
		}
		return p
	}
	fmt.Printf("%sInitialized plugin: %s%s%s\n", ansiText, ansiDim, name, ansiReset)
	fmt.Println()
	fmt.Printf("%sCreated:%s\n", ansiDim, ansiReset)
	for _, p := range created {
		fmt.Printf("  %s\n", rel(p))
	}
	fmt.Println()
	fmt.Printf("%sNext steps:%s\n", ansiDim, ansiReset)
	fmt.Printf("  1. Replace the example skill with real skills (one directory per skill)\n")
	if withMCP {
		fmt.Printf("  2. Point %smcp.json%s at your server command or URL\n", ansiText, ansiReset)
		fmt.Printf("  3. Check conformance with %smdm plugins validate %s%s\n", ansiText, rel(pluginDir), ansiReset)
	} else {
		fmt.Printf("  2. Check conformance with %smdm plugins validate %s%s\n", ansiText, rel(pluginDir), ansiReset)
	}
	fmt.Println()
}
