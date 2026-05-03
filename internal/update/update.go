package update

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/sethcarney/mdm/releases/latest"
const cacheTTL = 24 * time.Hour

type cacheEntry struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

type githubRelease struct {
	TagName string `json:"tag_name"`
}

// CheckForUpdate starts a background goroutine that checks for a newer release.
// It returns a channel that receives the latest version tag if one is available,
// or an empty string if no update is found or the check fails.
func CheckForUpdate(currentVersion string) <-chan string {
	ch := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- ""
			}
		}()
		ch <- check(currentVersion)
	}()
	return ch
}

// IsTerminal reports whether stdout is an interactive terminal.
func IsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func check(currentVersion string) string {
	latest := fromCache()
	if latest == "" {
		latest = fromAPI(currentVersion)
		if latest != "" {
			saveCache(latest)
		}
	}
	if isNewer(latest, currentVersion) {
		return latest
	}
	return ""
}

func fromCache() string {
	data, err := os.ReadFile(cacheFilePath())
	if err != nil {
		return ""
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return ""
	}
	if time.Since(entry.CheckedAt) > cacheTTL {
		return ""
	}
	return entry.LatestVersion
}

func saveCache(latest string) {
	path := cacheFilePath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	data, err := json.Marshal(cacheEntry{LatestVersion: latest, CheckedAt: time.Now()})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0644)
}

func cacheFilePath() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "mdm-update-check.json")
	}
	return filepath.Join(cacheDir, "mdm", "update-check.json")
}

func fromAPI(currentVersion string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", releasesAPI, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "mdm-cli/"+currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return ""
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return ""
	}
	return release.TagName
}

func isNewer(latest, current string) bool {
	lParts := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	cParts := strings.Split(strings.TrimPrefix(current, "v"), ".")
	for i := 0; i < 3; i++ {
		var l, c int
		if i < len(lParts) {
			l, _ = strconv.Atoi(lParts[i])
		}
		if i < len(cParts) {
			c, _ = strconv.Atoi(cParts[i])
		}
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}
	return false
}
