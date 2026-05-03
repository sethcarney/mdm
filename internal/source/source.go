package source

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type SourceType string

const (
	SourceTypeGitHub    SourceType = "github"
	SourceTypeGitLab    SourceType = "gitlab"
	SourceTypeGit       SourceType = "git"
	SourceTypeLocal     SourceType = "local"
	SourceTypeWellKnown SourceType = "well-known"
)

type ParsedSource struct {
	Type        SourceType
	URL         string
	LocalPath   string
	Subpath     string
	Ref         string
	SkillFilter string
}

var sourceAliases = map[string]string{
	"coinbase/agentWallet": "coinbase/agentic-wallet-skills",
}

func IsLocalPath(input string) bool {
	if filepath.IsAbs(input) {
		return true
	}
	if strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return true
	}
	if input == "." || input == ".." {
		return true
	}
	if matched, _ := regexp.MatchString(`^[a-zA-Z]:[/\\]`, input); matched {
		return true
	}
	return false
}

func SanitizeSubpath(subpath string) (string, error) {
	normalized := strings.ReplaceAll(subpath, "\\", "/")
	segments := strings.Split(normalized, "/")
	for _, seg := range segments {
		if seg == ".." {
			return "", fmt.Errorf("unsafe subpath: %q contains path traversal segments", subpath)
		}
	}
	return subpath, nil
}

func looksLikeGitSource(input string) bool {
	if strings.HasPrefix(input, "github:") || strings.HasPrefix(input, "gitlab:") || strings.HasPrefix(input, "git@") {
		return true
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err == nil {
			path := u.Path
			if u.Hostname() == "github.com" {
				matched, _ := regexp.MatchString(`^/[^/]+/[^/]+(?:\.git)?(?:/tree/[^/]+(?:/.*)?)?/?$`, path)
				return matched
			}
			if u.Hostname() == "gitlab.com" {
				matched, _ := regexp.MatchString(`^/.+?/[^/]+(?:\.git)?(?:/-/tree/[^/]+(?:/.*)?)?/?$`, path)
				return matched
			}
		}
	}
	matched, _ := regexp.MatchString(`(?i)^https?://.+\.git(?:$|[/?])`, input)
	if matched {
		return true
	}
	matched, _ = regexp.MatchString(`^([^/]+)/([^/]+)(?:/(.+)|@(.+))?$`, input)
	return matched && !strings.Contains(input, ":") && !strings.HasPrefix(input, ".") && !strings.HasPrefix(input, "/")
}

type fragmentRefResult struct {
	inputWithoutFragment string
	ref                  string
	skillFilter          string
}

func parseFragmentRef(input string) fragmentRefResult {
	hashIdx := strings.Index(input, "#")
	if hashIdx < 0 {
		return fragmentRefResult{inputWithoutFragment: input}
	}
	inputWithoutFragment := input[:hashIdx]
	fragment := input[hashIdx+1:]
	if fragment == "" || !looksLikeGitSource(inputWithoutFragment) {
		return fragmentRefResult{inputWithoutFragment: input}
	}
	atIdx := strings.Index(fragment, "@")
	if atIdx == -1 {
		return fragmentRefResult{
			inputWithoutFragment: inputWithoutFragment,
			ref:                  decodeFragment(fragment),
		}
	}
	ref := fragment[:atIdx]
	skillFilter := fragment[atIdx+1:]
	return fragmentRefResult{
		inputWithoutFragment: inputWithoutFragment,
		ref:                  decodeFragment(ref),
		skillFilter:          decodeFragment(skillFilter),
	}
}

func decodeFragment(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}

func AppendFragmentRef(input, ref, skillFilter string) string {
	if ref == "" {
		return input
	}
	if skillFilter != "" {
		return input + "#" + ref + "@" + skillFilter
	}
	return input + "#" + ref
}

func IsWellKnownURL(input string) bool {
	if !strings.HasPrefix(input, "http://") && !strings.HasPrefix(input, "https://") {
		return false
	}
	u, err := url.Parse(input)
	if err != nil {
		return false
	}
	excluded := map[string]bool{
		"github.com":                true,
		"gitlab.com":                true,
		"raw.githubusercontent.com": true,
	}
	if excluded[u.Hostname()] {
		return false
	}
	if strings.HasSuffix(input, ".git") {
		return false
	}
	return true
}

func ParseSource(input string) ParsedSource {
	if IsLocalPath(input) {
		resolved, _ := filepath.Abs(input)
		return ParsedSource{
			Type:      SourceTypeLocal,
			URL:       resolved,
			LocalPath: resolved,
		}
	}

	fragResult := parseFragmentRef(input)
	input = fragResult.inputWithoutFragment
	fragmentRef := fragResult.ref
	fragmentSkillFilter := fragResult.skillFilter

	if alias, ok := sourceAliases[input]; ok {
		input = alias
	}

	// github: prefix
	if m := regexp.MustCompile(`^github:(.+)$`).FindStringSubmatch(input); m != nil {
		return ParseSource(AppendFragmentRef(m[1], fragmentRef, fragmentSkillFilter))
	}

	// gitlab: prefix
	if m := regexp.MustCompile(`^gitlab:(.+)$`).FindStringSubmatch(input); m != nil {
		return ParseSource(AppendFragmentRef("https://gitlab.com/"+m[1], fragmentRef, fragmentSkillFilter))
	}

	// GitHub URL with tree + path
	if m := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)/(.+)`).FindStringSubmatch(input); m != nil {
		ref := m[3]
		if fragmentRef != "" {
			ref = fragmentRef
		}
		sub, _ := SanitizeSubpath(m[4])
		return ParsedSource{
			Type:    SourceTypeGitHub,
			URL:     "https://github.com/" + m[1] + "/" + m[2] + ".git",
			Ref:     ref,
			Subpath: sub,
		}
	}

	// GitHub URL with tree only
	if m := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/tree/([^/]+)$`).FindStringSubmatch(input); m != nil {
		ref := m[3]
		if fragmentRef != "" {
			ref = fragmentRef
		}
		return ParsedSource{
			Type: SourceTypeGitHub,
			URL:  "https://github.com/" + m[1] + "/" + m[2] + ".git",
			Ref:  ref,
		}
	}

	// GitHub URL
	if m := regexp.MustCompile(`github\.com/([^/]+)/([^/]+)`).FindStringSubmatch(input); m != nil {
		cleanRepo := strings.TrimSuffix(m[2], ".git")
		return ParsedSource{
			Type: SourceTypeGitHub,
			URL:  "https://github.com/" + m[1] + "/" + cleanRepo + ".git",
			Ref:  fragmentRef,
		}
	}

	// GitLab URL with path
	if m := regexp.MustCompile(`^(https?):\/\/([^/]+)/(.+?)\/-\/tree/([^/]+)/(.+)`).FindStringSubmatch(input); m != nil {
		if m[2] != "github.com" && m[3] != "" {
			ref := m[4]
			if fragmentRef != "" {
				ref = fragmentRef
			}
			sub, _ := SanitizeSubpath(m[5])
			repoPath := strings.TrimSuffix(m[3], ".git")
			return ParsedSource{
				Type:    SourceTypeGitLab,
				URL:     m[1] + "://" + m[2] + "/" + repoPath + ".git",
				Ref:     ref,
				Subpath: sub,
			}
		}
	}

	// GitLab URL with branch only
	if m := regexp.MustCompile(`^(https?):\/\/([^/]+)/(.+?)\/-\/tree/([^/]+)$`).FindStringSubmatch(input); m != nil {
		if m[2] != "github.com" && m[3] != "" {
			ref := m[4]
			if fragmentRef != "" {
				ref = fragmentRef
			}
			repoPath := strings.TrimSuffix(m[3], ".git")
			return ParsedSource{
				Type: SourceTypeGitLab,
				URL:  m[1] + "://" + m[2] + "/" + repoPath + ".git",
				Ref:  ref,
			}
		}
	}

	// GitLab.com URL
	if m := regexp.MustCompile(`gitlab\.com/(.+?)(?:\.git)?/?$`).FindStringSubmatch(input); m != nil {
		repoPath := m[1]
		if strings.Contains(repoPath, "/") {
			return ParsedSource{
				Type: SourceTypeGitLab,
				URL:  "https://gitlab.com/" + repoPath + ".git",
				Ref:  fragmentRef,
			}
		}
	}

	// GitHub shorthand with @skill: owner/repo@skill
	if m := regexp.MustCompile(`^([^/]+)/([^/@]+)@(.+)$`).FindStringSubmatch(input); m != nil {
		if !strings.Contains(input, ":") && !strings.HasPrefix(input, ".") && !strings.HasPrefix(input, "/") {
			sf := fragmentSkillFilter
			if sf == "" {
				sf = m[3]
			}
			return ParsedSource{
				Type:        SourceTypeGitHub,
				URL:         "https://github.com/" + m[1] + "/" + m[2] + ".git",
				Ref:         fragmentRef,
				SkillFilter: sf,
			}
		}
	}

	// GitHub shorthand: owner/repo or owner/repo/subpath
	if m := regexp.MustCompile(`^([^/]+)/([^/]+)(?:/(.+?))?/?$`).FindStringSubmatch(input); m != nil {
		if !strings.Contains(input, ":") && !strings.HasPrefix(input, ".") && !strings.HasPrefix(input, "/") {
			sub := ""
			if m[3] != "" {
				sub, _ = SanitizeSubpath(m[3])
			}
			sf := fragmentSkillFilter
			return ParsedSource{
				Type:        SourceTypeGitHub,
				URL:         "https://github.com/" + m[1] + "/" + m[2] + ".git",
				Ref:         fragmentRef,
				Subpath:     sub,
				SkillFilter: sf,
			}
		}
	}

	// Well-known URL
	if IsWellKnownURL(input) {
		return ParsedSource{
			Type: SourceTypeWellKnown,
			URL:  input,
		}
	}

	// Fallback: direct git URL
	return ParsedSource{
		Type: SourceTypeGit,
		URL:  input,
		Ref:  fragmentRef,
	}
}

func GetOwnerRepo(parsed ParsedSource) string {
	if parsed.Type == SourceTypeLocal {
		return ""
	}
	// SSH URL: git@github.com:owner/repo.git
	if m := regexp.MustCompile(`^git@[^:]+:(.+)$`).FindStringSubmatch(parsed.URL); m != nil {
		path := strings.TrimSuffix(m[1], ".git")
		if strings.Contains(path, "/") {
			return path
		}
		return ""
	}
	if !strings.HasPrefix(parsed.URL, "http://") && !strings.HasPrefix(parsed.URL, "https://") {
		return ""
	}
	u, err := url.Parse(parsed.URL)
	if err != nil {
		return ""
	}
	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	if strings.Contains(path, "/") {
		return path
	}
	return ""
}

func FormatSourceInput(sourceURL, ref string) string {
	if ref == "" {
		return sourceURL
	}
	return sourceURL + "#" + ref
}
