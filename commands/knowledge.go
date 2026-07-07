package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/experimental"
)

// knowledgeSpecVersion is the OKF spec revision this build implements.
// See docs/specs/knowledge.md and the upstream spec:
// https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf
const knowledgeSpecVersion = "0.1"

func printKnowledgeBanner() {
	fmt.Fprintf(os.Stderr, "%s⚠ experimental:%s %sOKF support tracks spec v%s — commands and file formats may change%s\n",
		ansiYellow, ansiReset, ansiDim, knowledgeSpecVersion, ansiReset)
}

func buildKnowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "knowledge",
		Short:  "Manage OKF knowledge bundles [experimental]",
		Hidden: !experimental.Enabled(experimental.Knowledge),
		Long: fmt.Sprintf(`Manage Open Knowledge Format (OKF) bundles — directories of markdown
documents that give AI agents durable reference context.

This command group is experimental: it tracks OKF spec v%s and may
change or be removed in any release.

%sExamples:%s
  mdm knowledge validate ./knowledge/my-bundle
  mdm knowledge init my-bundle`, knowledgeSpecVersion, ansiBold, ansiReset),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !experimental.Enabled(experimental.Knowledge) {
				return fmt.Errorf("knowledge is experimental — enable it with 'mdm experimental enable knowledge' or %s=knowledge", experimental.EnvVar)
			}
			printKnowledgeBanner()
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}
	cmd.AddCommand(
		buildKnowledgeValidateCmd(),
		buildKnowledgeInitCmd(),
	)
	return cmd
}
