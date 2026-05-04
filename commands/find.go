package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/registry"
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

func buildFindOptions(results []FindSkillResult) []ui.UIOption {
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
	return options
}

func installFindResult(r FindSkillResult) {
	src := r.Source
	if src == "" && r.Owner != "" && r.Repo != "" {
		src = r.Owner + "/" + r.Repo
	}
	if src == "" {
		return
	}
	fmt.Printf("%sAdding %s%s%s...\n", ansiDim, ansiText, r.Name, ansiReset)
	runAdd(src, AddOptions{Yes: false, PreselectedSkills: []string{r.Name}})
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
		fmt.Printf("%sSearch skills:%s ", ansiBold, ansiReset)
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			return
		}
		query = strings.TrimSpace(scanner.Text())
		if query == "" {
			return
		}
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

	indices, ok := ui.UiMultiselect("Select skills to install", buildFindOptions(results), false, nil, nil)
	if !ok || len(indices) == 0 {
		return
	}

	fmt.Println()
	for _, i := range indices {
		installFindResult(results[i])
	}
}

func fetchFindResults(query string) ([]FindSkillResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fetchURL := findAPIURL
	if query != "" {
		fetchURL += "?q=" + url.QueryEscape(query)
	}

	body, status, err := registry.HttpGetText(ctx, fetchURL)
	if err != nil || status != 200 {
		return nil, fmt.Errorf("search failed: status %d", status)
	}

	var wrapped struct {
		Skills []FindSkillResult `json:"skills"`
	}
	if err := json.Unmarshal([]byte(body), &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Skills, nil
}
