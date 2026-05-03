package update

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sethcarney/mdm/internal/version"
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
// or an empty string otherwise. The check never fails loudly.
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
	hit, latest := fromCache()
	if !hit {
		// Pre-write a sentinel ("") before making the network call.  If this
		// process is killed during the HTTP request (e.g. because the 500 ms
		// display window expired and main returned), the sentinel is already on
		// disk and the next invocation will see a cache hit, preventing repeated
		// API requests within the TTL window.
		saveCache("")
		latest = fromAPI(currentVersion)
		if latest != "" {
			// Overwrite the sentinel with the actual release tag so future
			// cache hits can show the update notice without another API call.
			saveCache(latest)
		}
	}
	if version.IsNewer(latest, currentVersion) {
		return latest
	}
	return ""
}

// fromCache returns (hit, latestVersion). hit is false when the cache is
// absent or older than cacheTTL; latestVersion may be "" (meaning the last
// check found no update or the API was unreachable).
func fromCache() (bool, string) {
	data, err := os.ReadFile(cacheFilePath())
	if err != nil {
		return false, ""
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return false, ""
	}
	if time.Since(entry.CheckedAt) > cacheTTL {
		return false, ""
	}
	return true, entry.LatestVersion
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
