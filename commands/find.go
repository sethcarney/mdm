package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/blob"
	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/lock"
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

// RemoteSkillEntry is the JSON shape returned by `skills find --source --json`.
type RemoteSkillEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func buildFindCmd() *cobra.Command {
	var jsonMode bool
	var sourceFlag string
	cmd := &cobra.Command{
		Use:     "find [query]",
		Short:   "Search the skills registry",
		Aliases: []string{"search", "f", "s"},
		Long: fmt.Sprintf(`Search the skills registry and install interactively.

%sExamples:%s
  mdm skills find typescript
  mdm skills find git
  mdm skills find --source owner/repo --json`, ansiBold, ansiReset),
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if sourceFlag != "" {
				runFindSource(sourceFlag, jsonMode)
				return
			}
			if jsonMode {
				runFindJSON(args)
				return
			}
			fmt.Println()
			runFind(args)
		},
	}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "Output results as JSON without installing")
	cmd.Flags().StringVar(&sourceFlag, "source", "", "List skills available at a remote source without installing")
	return cmd
}

func runFindJSON(args []string) {
	query := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			query = a
			break
		}
	}
	results, err := fetchFindResults(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Search failed: %v\n", err)
		os.Exit(1)
	}
	if results == nil {
		results = []FindSkillResult{}
	}
	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))
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

	indices, ok := ui.UiSearchMultiselect("Select skills to install", buildFindOptions(results), nil, nil, false)
	if !ok || len(indices) == 0 {
		return
	}

	fmt.Println()
	for _, i := range indices {
		installFindResult(results[i])
	}
}

func runFindSource(sourceInput string, jsonMode bool) {
	parsed := source.ParseSource(sourceInput)
	entries, err := fetchSourceSkillEntries(parsed, sourceInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch skills: %v\n", err)
		os.Exit(1)
	}
	if len(entries) == 0 {
		if jsonMode {
			fmt.Println("[]")
		} else {
			fmt.Fprintf(os.Stderr, "%sNo skills found at %s%s\n", ansiDim, sourceInput, ansiReset)
		}
		return
	}
	if jsonMode {
		out, _ := json.MarshalIndent(entries, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Println()
	options := make([]ui.UIOption, len(entries))
	for i, s := range entries {
		options[i] = ui.UIOption{Label: s.Name, Value: s.Name, Hint: s.Description}
	}
	indices, ok := ui.UiSearchMultiselect("Select skills to install", options, nil, nil, false)
	if !ok || len(indices) == 0 {
		return
	}
	var names []string
	for _, i := range indices {
		names = append(names, entries[i].Name)
	}
	fmt.Println()
	runAdd(sourceInput, AddOptions{PreselectedSkills: names})
}

func fetchSourceSkillEntries(parsed source.ParsedSource, sourceInput string) ([]RemoteSkillEntry, error) {
	switch parsed.Type {
	case source.SourceTypeWellKnown:
		spin := ui.NewSpinner("Fetching skills...")
		skills, err := registry.FetchAllWellKnownSkills(parsed.URL)
		spin.Stop("")
		if err != nil {
			return nil, err
		}
		var entries []RemoteSkillEntry
		for _, s := range skills {
			entries = append(entries, RemoteSkillEntry{Name: s.Name, Description: s.Description})
		}
		return entries, nil
	case source.SourceTypeLocal:
		skills := discoverSkillsInDir(parsed.LocalPath, false, "")
		var entries []RemoteSkillEntry
		for _, s := range skills {
			entries = append(entries, RemoteSkillEntry{Name: s.Name, Description: s.Description})
		}
		return entries, nil
	case source.SourceTypeGitHub:
		ownerRepo := source.GetOwnerRepo(parsed)
		spin := ui.NewSpinner("Fetching skills...")
		metas, err := blob.FetchRemoteSkillList(ownerRepo, parsed.Ref, parsed.Subpath, lock.GetGitHubToken())
		spin.Stop("")
		if err != nil {
			return nil, err
		}
		var entries []RemoteSkillEntry
		for _, m := range metas {
			entries = append(entries, RemoteSkillEntry{Name: m.Name, Description: m.Description})
		}
		return entries, nil
	default:
		spin := ui.NewSpinner("Cloning " + parsed.URL + "...")
		tmpDir, err := git.CloneRepo(parsed.URL, parsed.Ref)
		spin.Stop("")
		if err != nil {
			return nil, err
		}
		defer git.CleanupTempDir(tmpDir)
		searchRoot := tmpDir
		if parsed.Subpath != "" {
			searchRoot = filepath.Join(tmpDir, parsed.Subpath)
		}
		skills := discoverSkillsInDir(searchRoot, false, "")
		var entries []RemoteSkillEntry
		for _, s := range skills {
			entries = append(entries, RemoteSkillEntry{Name: s.Name, Description: s.Description})
		}
		return entries, nil
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
