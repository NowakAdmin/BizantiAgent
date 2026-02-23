package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"os/exec"
	"encoding/json"
)

type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type LatestReleaseResponse struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Body    string         `json:"body"`
	Assets  []ReleaseAsset `json:"assets"`
}

func GetLatestRelease(ctx context.Context, repo string) (LatestReleaseResponse, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return LatestReleaseResponse{}, fmt.Errorf("repozytorium GitHub nie może być puste")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return LatestReleaseResponse{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return LatestReleaseResponse{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode == http.StatusNotFound {
		return LatestReleaseResponse{}, fmt.Errorf("brak opublikowanego release")
	}

	if response.StatusCode >= 300 {
		return LatestReleaseResponse{}, fmt.Errorf("github api zwróciło status: %d", response.StatusCode)
	}

	var release LatestReleaseResponse
	if err = json.NewDecoder(response.Body).Decode(&release); err != nil {
		return LatestReleaseResponse{}, err
	}

	return release, nil
}

func DownloadLatestWindowsAsset(ctx context.Context, repo string) (string, LatestReleaseResponse, error) {
	release, err := GetLatestRelease(ctx, repo)
	if err != nil {
		return "", LatestReleaseResponse{}, err
	}

	assetURL := ""
	assetName := ""
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, "BizantiAgent.exe") {
			assetURL = asset.BrowserDownloadURL
			assetName = asset.Name
			break
		}
	}
	if assetURL == "" {
		for _, asset := range release.Assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
				assetURL = asset.BrowserDownloadURL
				assetName = asset.Name
				break
			}
		}
	}
	if assetURL == "" {
		return "", LatestReleaseResponse{}, fmt.Errorf("brak pliku .exe w release")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return "", LatestReleaseResponse{}, err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", LatestReleaseResponse{}, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		return "", LatestReleaseResponse{}, fmt.Errorf("download status %d", response.StatusCode)
	}

	prefix := strings.TrimSuffix(assetName, ".exe")
	if prefix == "" {
		prefix = "BizantiAgent"
	}
	tmpFile, err := os.CreateTemp(os.TempDir(), prefix+"-*.exe")
	if err != nil {
		return "", LatestReleaseResponse{}, err
	}
	defer func() {
		_ = tmpFile.Close()
	}()

	if _, err = io.Copy(tmpFile, response.Body); err != nil {
		return "", LatestReleaseResponse{}, err
	}

	return tmpFile.Name(), release, nil
}

func StartSelfUpdate(newBinaryPath string) error {
	if runtime.GOOS != "windows" {
		return errors.New("self-update wspierany tylko na Windows")
	}
	if strings.TrimSpace(newBinaryPath) == "" {
		return errors.New("brak ścieżki do nowego pliku")
	}

	targetPath, err := os.Executable()
	if err != nil {
		return err
	}

	scriptPath := filepath.Join(os.TempDir(), "bizanti-agent-update.cmd")
	script := fmt.Sprintf("@echo off\r\nset \"TARGET=%s\"\r\nset \"NEW=%s\"\r\n:loop\r\nping 127.0.0.1 -n 2 > nul\r\ndel \"%%TARGET%%\" >nul 2>nul\r\nif exist \"%%TARGET%%\" goto loop\r\nmove /Y \"%%NEW%%\" \"%%TARGET%%\" >nul\r\nstart \"\" \"%%TARGET%%\"\r\ndel \"%%~f0\"\r\n", targetPath, newBinaryPath)

	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	return exec.Command("cmd.exe", "/C", "start", "", scriptPath).Start()
}
