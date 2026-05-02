package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const updateRepo = "sethcarney/mdm"
const releasesAPI = "https://api.github.com/repos/" + updateRepo + "/releases/latest"

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

func buildUpgradeCmd(ver string) *cobra.Command {
	return &cobra.Command{
		Use:     "upgrade",
		Short:   "Upgrade the " + appName + " CLI binary",
		Aliases: []string{"update-cli", "self-update"},
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runSelfUpdate(ver)
		},
	}
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

func runSelfUpdate(currentVersion string) {
	client := &http.Client{Timeout: 30 * time.Second}

	fmt.Printf("%sChecking for updates...%s\n", ansiDim, ansiReset)

	req, _ := http.NewRequest("GET", releasesAPI, nil)
	req.Header.Set("User-Agent", appName+"-cli/"+currentVersion)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to check for updates: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "GitHub API returned %d\n", resp.StatusCode)
		os.Exit(1)
	}

	body, _ := io.ReadAll(resp.Body)
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse release info: %v\n", err)
		os.Exit(1)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")

	if !isNewer(latestVersion, currentVersion) {
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

	var downloadURL string
	for _, a := range release.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		fmt.Fprintf(os.Stderr, "Binary for your platform (%s) not found in release %s.\n", assetName, latestVersion)
		os.Exit(1)
	}

	fmt.Printf("%sDownloading %s...%s\n", ansiDim, assetName, ansiReset)
	dlResp, err := client.Get(downloadURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "Download failed: HTTP %d\n", dlResp.StatusCode)
		os.Exit(1)
	}

	dlBody, _ := io.ReadAll(dlResp.Body)
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf(appName+"-update-%d", time.Now().UnixNano()))

	if err := os.WriteFile(tmpPath, dlBody, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write temp file: %v\n", err)
		os.Exit(1)
	}

	execPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not determine executable path: %v\n", err)
		os.Exit(1)
	}

	if runtime.GOOS != "windows" {
		if err := os.Rename(tmpPath, execPath); err != nil {
			// Cross-filesystem: unlink + copy
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
	} else {
		batchPath := filepath.Join(os.TempDir(), appName+"-update.bat")
		batchContent := fmt.Sprintf("@echo off\r\ntimeout /t 1 /nobreak > NUL\r\nmove /y \"%s\" \"%s\" > NUL\r\ndel \"%%~f0\"\r\n",
			tmpPath, execPath)
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
}
