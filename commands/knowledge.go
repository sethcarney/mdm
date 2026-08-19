package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// knowledgeSpecVersion is the OKF spec revision this build implements.
// See docs/specs/knowledge.md and the upstream spec:
// https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf
const knowledgeSpecVersion = "0.1"

func buildKnowledgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Manage OKF knowledge bundles",
		Long: fmt.Sprintf(`Manage Open Knowledge Format (OKF) bundles — directories of markdown
documents that give AI agents durable reference context.

Tracks OKF spec v%s.

%sExamples:%s
  mdm knowledge validate ./knowledge/my-bundle
  mdm knowledge init my-bundle`, knowledgeSpecVersion, ansiBold, ansiReset),
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
		},
	}
	cmd.AddCommand(
		buildKnowledgeAddCmd(),
		buildKnowledgeListCmd(),
		buildKnowledgeRemoveCmd(),
		buildKnowledgeUpdateCmd(),
		buildKnowledgeValidateCmd(),
		buildKnowledgeInitCmd(),
		buildKnowledgeInstallCmd(),
	)
	return cmd
}
