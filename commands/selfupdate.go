package commands

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sethcarney/mdm/internal/ui"
	"github.com/sethcarney/mdm/internal/version"
)

const maxBinaryBytes = 100 * 1024 * 1024 // 100 MB hard cap

const updateRepo = "sethcarney/mdm"
const releasesAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"
const releasesListAPI = "https://api.github.com/repos/" + updateRepo + "/releases"

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Prerelease bool          `json:"prerelease"`
	Assets     []githubAsset `json:"assets"`
}

func buildUpgradeCmd(ver string) *cobra.Command {
	var beta, stable bool
	cmd := &cobra.Command{
		Use:     "upgrade",
		Short:   "Upgrade the " + appName + " CLI binary",
		Aliases: []string{"update-cli", "self-update"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			useBeta := beta
			if !beta && !stable {
				useBeta = promptChannel()
			}
			runSelfUpdate(ver, useBeta)
		},
	}
	cmd.Flags().BoolVar(&beta, "beta", false, "Upgrade to the latest beta/prerelease version")
	cmd.Flags().BoolVar(&stable, "stable", false, "Upgrade to the latest stable version (default)")
	return cmd
}

func getBinaryAssetName() string {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "mdm-linux-x64"
		case "arm64":
			return "mdm-linux-arm64"
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "mdm-macos-x64"
		case "arm64":
			return "mdm-macos-arm64"
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "mdm-windows-x64.exe"
		}
	}
	return ""
}

// isGitHubURL returns true only for github.com and its CDN hostnames.
func isGitHubURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" ||
		strings.HasSuffix(h, ".github.com") ||
		strings.HasSuffix(h, ".githubusercontent.com")
}

// verifyChecksums returns true if the SHA256 of data matches the entry for
// assetName in a sha256sum-format checksum file.
func verifyChecksums(data []byte, checksumText, assetName string) bool {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, line := range strings.Split(checksumText, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName && strings.EqualFold(fields[0], sum) {
			return true
		}
	}
	return false
}

func fetchLatestRelease(client *http.Client, currentVersion string) (*githubRelease, error) {
	vlog(verboseFlag, "fetching latest stable release: GET %s", releasesAPI)
	req, _ := http.NewRequest("GET", releasesAPI, nil)
	req.Header.Set("User-Agent", appName+"-cli/"+currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "GitHub API returned %d\n", resp.StatusCode)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read release response: %v\n", err)
		return nil, fmt.Errorf("reading release response body: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse release info: %v\n", err)
		return nil, err
	}
	return &release, nil
}

func fetchLatestPrerelease(client *http.Client, currentVersion string) (*githubRelease, error) {
	vlog(verboseFlag, "fetching release list for prereleases: GET %s", releasesListAPI)
	req, _ := http.NewRequest("GET", releasesListAPI, nil)
	req.Header.Set("User-Agent", appName+"-cli/"+currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "GitHub API returned %d\n", resp.StatusCode)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read releases response: %v\n", err)
		return nil, fmt.Errorf("reading releases response body: %w", err)
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse releases: %v\n", err)
		return nil, err
	}
	for i := range releases {
		if releases[i].Prerelease {
			return &releases[i], nil
		}
	}
	return nil, nil
}

func findReleaseURLs(release *githubRelease, assetName, latestVersion string) (string, string, bool) {
	var downloadURL, checksumsURL string
	for _, a := range release.Assets {
		switch a.Name {
		case assetName:
			downloadURL = a.BrowserDownloadURL
		case "sha256sums.txt":
			checksumsURL = a.BrowserDownloadURL
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "Binary for your platform (%s) not found in release %s.\n", assetName, latestVersion)
		return "", "", false
	}
	vlog(verboseFlag, "selected asset %q → %s (checksums=%q)", assetName, downloadURL, checksumsURL)
	if !isGitHubURL(downloadURL) {
		fmt.Fprintf(os.Stderr, "Unexpected download host in release asset — aborting.\n")
		return "", "", false
	}
	return downloadURL, checksumsURL, true
}

func downloadAndVerify(client *http.Client, downloadURL, checksumsURL, assetName string) ([]byte, error) {
	var checksumText string
	if checksumsURL != "" && isGitHubURL(checksumsURL) {
		csResp, csErr := client.Get(checksumsURL)
		if csErr == nil && csResp.StatusCode == 200 {
			csBody, _ := io.ReadAll(io.LimitReader(csResp.Body, 1024*1024))
			csResp.Body.Close()
			checksumText = string(csBody)
		}
	}

	fmt.Printf("%sDownloading %s...%s\n", ansiDim, assetName, ansiReset)
	dlResp, err := client.Get(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		return nil, err
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "Download failed: HTTP %d\n", dlResp.StatusCode)
		return nil, fmt.Errorf("HTTP %d", dlResp.StatusCode)
	}
	dlBody, err := io.ReadAll(io.LimitReader(dlResp.Body, maxBinaryBytes+1))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Download read failed: %v\n", err)
		return nil, err
	}
	if int64(len(dlBody)) > maxBinaryBytes {
		fmt.Fprintf(os.Stderr, "Downloaded binary exceeds %d MB limit — aborting.\n", maxBinaryBytes/1024/1024)
		return nil, fmt.Errorf("binary too large")
	}
	vlog(verboseFlag, "downloaded %d bytes", len(dlBody))
	if checksumText != "" {
		if !verifyChecksums(dlBody, checksumText, assetName) {
			fmt.Fprintf(os.Stderr, "SHA256 checksum mismatch for %s — aborting update.\n", assetName)
			return nil, fmt.Errorf("checksum mismatch")
		}
		fmt.Printf("%sSHA256 verified.%s\n", ansiDim, ansiReset)
	} else {
		vlog(verboseFlag, "no checksums.txt asset found; skipping SHA256 verification")
	}
	return dlBody, nil
}

func writeTempExecutable(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", appName+"-update-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp file: %v\n", err)
		return "", err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Failed to write temp file: %v\n", err)
		return "", err
	}
	tmpFile.Close()
	if err := os.Chmod(tmpPath, 0700); err != nil {
		os.Remove(tmpPath)
		fmt.Fprintf(os.Stderr, "Failed to set permissions on temp file: %v\n", err)
		return "", err
	}
	return tmpPath, nil
}

func replaceBinary(tmpPath, execPath, latestVersion string) {
	vlog(verboseFlag, "replacing binary: %s → %s", tmpPath, execPath)
	if runtime.GOOS != "windows" {
		if err := os.Rename(tmpPath, execPath); err != nil {
			vlog(verboseFlag, "rename failed (%v); falling back to copy", err)
			os.Remove(execPath)
			if err2 := copyFile(tmpPath, execPath); err2 != nil {
				fmt.Fprintf(os.Stderr, "Failed to update binary: %v\n", err2)
				os.Exit(1)
			}
			os.Remove(tmpPath)
		}
		fmt.Printf("%sUpdated to %s successfully.%s\n", ansiText, latestVersion, ansiReset)
		fmt.Printf("%sRestart your shell or run %s%s --version%s%s to confirm.%s\n",
			ansiDim, ansiText, appName, ansiDim, ansiReset, ansiReset)
		return
	}
	batchPath := filepath.Join(os.TempDir(), appName+"-update.bat")
	escapedTmp := strings.ReplaceAll(tmpPath, "%", "%%")
	escapedExec := strings.ReplaceAll(execPath, "%", "%%")
	batchContent := fmt.Sprintf("@echo off\r\ntimeout /t 1 /nobreak > NUL\r\nmove /y \"%s\" \"%s\" > NUL\r\ndel \"%%~f0\"\r\n",
		escapedTmp, escapedExec)
	if err := os.WriteFile(batchPath, []byte(batchContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write update script: %v\n", err)
		os.Exit(1)
	}
	if err := exec.Command("cmd", "/c", "start", "/b", "", batchPath).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to launch update script: %v\n", err)
		fmt.Printf("%sTo apply the update manually, run:%s\n  %s%s%s\n",
			ansiDim, ansiReset, ansiText, batchPath, ansiReset)
		return
	}
	fmt.Printf("%sUpdated to %s successfully.%s\n", ansiText, latestVersion, ansiReset)
	fmt.Printf("%sApplying update as this process exits...%s\n", ansiDim, ansiReset)
	os.Exit(0)
}

func promptChannel() bool {
	idx, ok := ui.UiSelect("Select upgrade channel", []ui.UIOption{
		{Label: "Stable (recommended)", Hint: "Latest stable release"},
		{Label: "Beta / Prerelease", Hint: "Latest beta or release-candidate build"},
	})
	if !ok {
		os.Exit(0)
	}
	return idx == 1
}

func runSelfUpdate(currentVersion string, useBeta bool) {
	client := &http.Client{Timeout: 30 * time.Second}
	vlog(verboseFlag, "self-update: current=%s channel=%s", currentVersion, map[bool]string{true: "beta", false: "stable"}[useBeta])
	fmt.Printf("%sChecking for updates...%s\n", ansiDim, ansiReset)

	var release *githubRelease
	var err error
	if useBeta {
		release, err = fetchLatestPrerelease(client, currentVersion)
		if err != nil {
			os.Exit(1)
		}
		if release == nil {
			fmt.Printf("%sNo beta releases found.%s\n", ansiText, ansiReset)
			return
		}
	} else {
		release, err = fetchLatestRelease(client, currentVersion)
		if err != nil {
			os.Exit(1)
		}
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if !version.IsNewer(latestVersion, currentVersion) {
		fmt.Printf("%sAlready up to date%s %s(%s)%s\n", ansiText, ansiReset, ansiDim, currentVersion, ansiReset)
		return
	}
	fmt.Printf("%sNew version available:%s %s %s(current: %s)%s\n",
		ansiText, ansiReset, latestVersion, ansiDim, currentVersion, ansiReset)
	fmt.Println()

	assetName := getBinaryAssetName()
	if assetName == "" {
		fmt.Fprintf(os.Stderr, "Unsupported platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		fmt.Fprintf(os.Stderr, "Download manually from: https://github.com/%s/releases/latest\n", updateRepo)
		os.Exit(1)
	}

	downloadURL, checksumsURL, ok := findReleaseURLs(release, assetName, latestVersion)
	if !ok {
		os.Exit(1)
	}

	dlBody, err := downloadAndVerify(client, downloadURL, checksumsURL, assetName)
	if err != nil {
		os.Exit(1)
	}

	tmpPath, err := writeTempExecutable(dlBody)
	if err != nil {
		os.Exit(1)
	}

	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not determine executable path: %v\n", err)
		os.Exit(1)
	}

	replaceBinary(tmpPath, execPath, latestVersion)
}
