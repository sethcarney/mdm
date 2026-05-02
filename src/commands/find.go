package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/registry"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

const findAPIURL = "https://skills.sh/api/search"

type FindSkillResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Stars       int    `json:"stars,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Repo        string `json:"repo,omitempty"`
}

func buildFindCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "find [query]",
		Short:   "Search the skills registry",
		Aliases: []string{"search", "f", "s"},
		Long: fmt.Sprintf(`Search the skills registry and install interactively.

%sExamples:%s
  mdm skills find typescript
  mdm skills find git`, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println()
			runFind(args)
		},
	}
}

func runFind(args []string) {
	query := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			query = a
			break
		}
	}

	if query == "" {
		fmt.Printf("%sUsage:%s mdm skills find %s<query>%s\n", ansiBold, ansiReset, ansiDim, ansiReset)
		return
	}

	spin := ui.NewSpinner("Searching skills...")
	results, err := fetchFindResults(query)
	spin.Stop("")

	if err != nil {
		fmt.Fprintf(os.Stderr, "Search failed: %v\n", err)
		return
	}
	if len(results) == 0 {
		fmt.Printf("%sNo skills found for \"%s\"%s\n", ansiDim, query, ansiReset)
		return
	}

	// Build options
	options := make([]ui.UIOption, len(results))
	for i, r := range results {
		hint := r.Source
		if r.Description != "" {
			hint = r.Description
		}
		if r.Stars > 0 {
			hint = fmt.Sprintf("%s ★%d", hint, r.Stars)
		}
		options[i] = ui.UIOption{Label: r.Name, Value: r.Source, Hint: hint}
	}

	indices, ok := ui.UiMultiselect("Select skills to install", options, false, nil, nil)
	if !ok || len(indices) == 0 {
		return
	}

	fmt.Println()

	// Install each selected skill
	for _, i := range indices {
		r := results[i]
		src := r.Source
		if src == "" && r.Owner != "" && r.Repo != "" {
			src = r.Owner + "/" + r.Repo
		}
		if src == "" {
			continue
		}
		fmt.Printf("%sAdding %s%s%s...\n", ansiDim, ansiText, r.Name, ansiReset)
		parsed := source.ParseSource(src)
		skillFilter := ""
		if r.Name != "" {
			skillFilter = r.Name
		}
		_ = skillFilter

		opts := AddOptions{Yes: false}
		runAdd(src, opts)
		_ = parsed
	}
}

func fetchFindResults(query string) ([]FindSkillResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := findAPIURL
	if query != "" {
		url += "?q=" + query
	}

	body, status, err := registry.HttpGetText(ctx, url)
	if err != nil || status != 200 {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	var wrapped struct {
		Skills []FindSkillResult `json:"skills"`
	}
	if err := json.Unmarshal([]byte(body), &wrapped); err != nil {
		return nil, err
	}
	results := wrapped.Skills

	_ = os.Stderr
	return results, nil
}
