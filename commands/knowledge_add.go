package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/git"
	"github.com/sethcarney/mdm/internal/lock"
	"github.com/sethcarney/mdm/internal/okf"
	"github.com/sethcarney/mdm/internal/security/markdownscan"
	"github.com/sethcarney/mdm/internal/source"
	"github.com/sethcarney/mdm/internal/ui"
)

// defaultKnowledgeDir is where bundles land, relative to the project root.
const defaultKnowledgeDir = "knowledge"

type KnowledgeAddOptions struct {
	Dir              string
	Bundles          []string
	Yes              bool
	DryRun           bool
	AllowHiddenChars bool
}

func buildKnowledgeAddCmd() *cobra.Command {
	var opts KnowledgeAddOptions

	cmd := &cobra.Command{
		Use:     "add <source>",
		Short:   "Install an OKF bundle from GitHub, GitLab, URL, or local path",
		Aliases: []string{"a"},
		Long: fmt.Sprintf(`Install a knowledge bundle into the project's knowledge directory
(default ./%s) and record it in knowledge-lock.json.

Sources use the same forms as skills: owner/repo shorthand, full URLs,
and local paths, with an optional #ref for version pinning.

%sExamples:%s
  mdm knowledge add acme/sales-knowledge
  mdm knowledge add acme/sales-knowledge#v2.1.0
  mdm knowledge add https://github.com/acme/sales-knowledge
  mdm knowledge add ./local-bundle`, defaultKnowledgeDir, ansiBold, ansiReset),
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			runKnowledgeAdd(args[0], opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.Dir, "dir", defaultKnowledgeDir, "Directory to install bundles into (relative to the project root)")
	f.StringArrayVarP(&opts.Bundles, "bundle", "b", nil, "Bundle names to install (repeatable, use '*' for all)")
	f.BoolVarP(&opts.Yes, "yes", "y", false, "Skip confirmation prompts and install all discovered bundles")
	f.BoolVar(&opts.DryRun, "dry-run", false, "Show what would be installed without writing anything")
	f.BoolVar(&opts.AllowHiddenChars, "allow-hidden-chars", false, "Allow markdown files with hidden Unicode characters")

	return cmd
}

// knowledgeCandidate is a discovered bundle root awaiting install.
type knowledgeCandidate struct {
	Name string
	Root string
	Docs int
}

func runKnowledgeAdd(sourceInput string, opts KnowledgeAddOptions) {
	cwd, _ := os.Getwd()
	parsed := source.ParseSource(sourceInput)
	vlog(verboseFlag, "source %q → type=%s url=%s ref=%q subpath=%q",
		sourceInput, parsed.Type, parsed.URL, parsed.Ref, parsed.Subpath)
	fmt.Println()

	searchRoot, cleanup := fetchKnowledgeSource(parsed)
	defer cleanup()

	candidates := discoverKnowledgeCandidates(searchRoot, parsed)
	if len(candidates) == 0 {
		fmt.Fprintf(os.Stderr, "%sNo knowledge bundles found in %s%s\n", ansiText, sourceInput, ansiReset)
		os.Exit(1)
	}

	selected, ok := selectKnowledgeCandidates(candidates, opts)
	if !ok {
		return
	}

	if !scanKnowledgeCandidates(selected, opts.AllowHiddenChars) {
		os.Exit(1)
	}

	baseEntry := knowledgeLockEntry(parsed, sourceInput)
	installed := 0
	for _, c := range selected {
		if installKnowledgeCandidate(c, baseEntry, opts, cwd) {
			installed++
		}
	}
	fmt.Println()
	if opts.DryRun {
		fmt.Printf("%sDry run — nothing was written.%s\n\n", ansiDim, ansiReset)
		return
	}
	fmt.Printf("%sInstalled %d bundle(s) to ./%s%s\n\n", ansiText, installed, opts.Dir, ansiReset)
}

// fetchKnowledgeSource materializes the source on disk and returns the
// directory to search plus a cleanup func for any temp clone.
func fetchKnowledgeSource(parsed source.ParsedSource) (string, func()) {
	noop := func() {}
	switch parsed.Type {
	case source.SourceTypeLocal:
		if _, err := os.Stat(parsed.LocalPath); err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s Path not found: %s\n", ansiText, ansiReset, parsed.LocalPath)
			os.Exit(1)
		}
		return parsed.LocalPath, noop
	case source.SourceTypeWellKnown:
		fmt.Fprintf(os.Stderr, "%sError:%s well-known registries are not supported for knowledge bundles\n", ansiText, ansiReset)
		os.Exit(1)
		return "", noop
	default:
		spin := ui.NewSpinner("Cloning " + parsed.URL + "...")
		tmpDir, err := git.CloneRepo(parsed.URL, parsed.Ref)
		spin.Stop("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%sError:%s %s\n", ansiText, ansiReset, err.Error())
			os.Exit(1)
		}
		searchRoot := tmpDir
		if parsed.Subpath != "" {
			searchRoot = filepath.Join(tmpDir, parsed.Subpath)
			if _, err := os.Stat(searchRoot); err != nil {
				git.CleanupTempDir(tmpDir)
				fmt.Fprintf(os.Stderr, "%sSubpath not found:%s %s\n", ansiText, ansiReset, parsed.Subpath)
				os.Exit(1)
			}
		}
		return searchRoot, func() { git.CleanupTempDir(tmpDir) }
	}
}

// discoverKnowledgeCandidates finds bundle roots and names them: nested
// bundles after their directory, a root-level bundle after the repo or
// source directory.
func discoverKnowledgeCandidates(searchRoot string, parsed source.ParsedSource) []knowledgeCandidate {
	var candidates []knowledgeCandidate
	for _, root := range okf.FindBundleRoots(searchRoot) {
		name := filepath.Base(root)
		if root == searchRoot {
			if ownerRepo := source.GetOwnerRepo(parsed); ownerRepo != "" {
				name = filepath.Base(ownerRepo)
			}
		}
		docs := 0
		if b, err := okf.LoadBundle(root); err == nil {
			docs = len(b.Docs)
		}
		candidates = append(candidates, knowledgeCandidate{Name: sanitizeName(name), Root: root, Docs: docs})
	}
	return candidates
}

func filterKnowledgeCandidates(candidates []knowledgeCandidate, filters []string) []knowledgeCandidate {
	if len(filters) == 1 && filters[0] == "*" {
		return candidates
	}
	var keep []knowledgeCandidate
	for _, c := range candidates {
		for _, f := range filters {
			if skillNameMatches(c.Name, f) {
				keep = append(keep, c)
				break
			}
		}
	}
	return keep
}

func selectKnowledgeCandidates(candidates []knowledgeCandidate, opts KnowledgeAddOptions) ([]knowledgeCandidate, bool) {
	if len(opts.Bundles) > 0 {
		keep := filterKnowledgeCandidates(candidates, opts.Bundles)
		if len(keep) == 0 {
			fmt.Fprintf(os.Stderr, "%sNo matching bundles found.%s\n", ansiText, ansiReset)
			os.Exit(1)
		}
		return keep, true
	}
	if opts.Yes || len(candidates) == 1 {
		return candidates, true
	}
	options := make([]ui.UIOption, len(candidates))
	for i, c := range candidates {
		options[i] = ui.UIOption{Label: c.Name, Value: c.Name, Hint: fmt.Sprintf("%d document(s)", c.Docs)}
	}
	indices, ok := ui.UiMultiselect("Which bundles would you like to install?", options, true, nil, nil)
	if !ok {
		fmt.Println("Cancelled.")
		return nil, false
	}
	var selected []knowledgeCandidate
	for _, i := range indices {
		selected = append(selected, candidates[i])
	}
	return selected, true
}

// scanKnowledgeCandidates runs the hidden-character scan over every selected
// bundle. Bundles are agent-bound markdown, so this gate is mandatory.
func scanKnowledgeCandidates(selected []knowledgeCandidate, allow bool) bool {
	ok := true
	for _, c := range selected {
		findings, err := markdownscan.ScanMarkdownFiles(c.Root)
		if err != nil {
			fmt.Printf("%sHidden character scan failed for %s: %s%s\n", ansiRed, c.Name, err, ansiReset)
			ok = false
			continue
		}
		if !checkSkillMarkdownForHiddenChars(c.Name, findings, allow) {
			ok = false
		}
	}
	return ok
}

// knowledgeLockEntry builds the source-level part of the lock entry shared by
// every bundle installed from this invocation.
func knowledgeLockEntry(parsed source.ParsedSource, sourceInput string) lock.KnowledgeLockEntry {
	entry := lock.KnowledgeLockEntry{
		Source:      stripSourceRef(sourceInput),
		SourceType:  string(parsed.Type),
		SourceURL:   parsed.URL,
		Subpath:     parsed.Subpath,
		SpecVersion: knowledgeSpecVersion,
	}
	if parsed.Type == source.SourceTypeLocal {
		entry.Source = parsed.LocalPath
		entry.SourceURL = ""
	} else {
		entry.Ref = parsed.Ref
		if entry.Ref == "" {
			entry.Ref = git.DefaultBranch(parsed.URL)
		}
	}
	return entry
}

func installKnowledgeCandidate(c knowledgeCandidate, baseEntry lock.KnowledgeLockEntry, opts KnowledgeAddOptions, cwd string) bool {
	issues := validateKnowledgeCandidate(c)

	if opts.DryRun {
		fmt.Printf("  %s%s%s  %d document(s) → ./%s\n", ansiText, c.Name, ansiReset, c.Docs, filepath.ToSlash(filepath.Join(opts.Dir, c.Name)))
		return true
	}

	destBase := filepath.Join(cwd, opts.Dir)
	destDir := filepath.Join(destBase, c.Name)
	if !isPathSafe(destBase, destDir) || !isPathSafe(cwd, destBase) {
		ui.LogError(fmt.Sprintf("%s: potential path traversal detected", c.Name))
		return false
	}
	if err := cleanAndCreateDir(destDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", c.Name, err))
		return false
	}
	if err := copyDirectory(c.Root, destDir); err != nil {
		ui.LogError(fmt.Sprintf("%s: %v", c.Name, err))
		return false
	}

	entry := baseEntry
	entry.InstallDir = filepath.ToSlash(filepath.Join(opts.Dir, c.Name))
	if hash, err := okf.HashBundleDir(destDir); err == nil {
		entry.ContentHash = hash
	}
	if err := lock.AddBundleToKnowledgeLock(c.Name, entry, cwd); err != nil {
		ui.LogWarn(fmt.Sprintf("could not update knowledge-lock.json: %v", err))
	}

	msg := fmt.Sprintf("%s (%d document(s))", c.Name, c.Docs)
	if n := len(issues); n > 0 {
		msg += fmt.Sprintf(" — %d validation issue(s), run 'mdm knowledge validate %s'", n, entry.InstallDir)
	}
	ui.LogSuccess(msg)
	return true
}

// validateKnowledgeCandidate reports conformance issues without blocking the
// install; an imperfect bundle is still useful and `validate` exists for
// strict checking.
func validateKnowledgeCandidate(c knowledgeCandidate) []okf.Issue {
	b, err := okf.LoadBundle(c.Root)
	if err != nil {
		return nil
	}
	return okf.Validate(b)
}
