package update

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
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
		if strings.Contains(err.Error(), "brak opublikowanego release") {
			return downloadFromTaggedRepoAssets(ctx, repo)
		}
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

func downloadFromTaggedRepoAssets(ctx context.Context, repo string) (string, LatestReleaseResponse, error) {
	latestTag, err := getLatestTag(ctx, repo)
	if err != nil {
		return "", LatestReleaseResponse{}, err
	}

	normalized := normalizeVersion(latestTag)

	zipURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/releases/bizanti-agent-v%s-win64.zip", repo, latestTag, normalized)
	zipPath, err := downloadToTemp(ctx, zipURL, "bizanti-agent-release-*.zip")
	if err == nil {
		exePath, extractErr := extractExeFromZip(zipPath)
		_ = os.Remove(zipPath)
		if extractErr == nil {
			return exePath, LatestReleaseResponse{TagName: latestTag}, nil
		}
	}

	exeURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/BizantiAgent.exe", repo, latestTag)
	exePath, exeErr := downloadToTemp(ctx, exeURL, "BizantiAgent-*.exe")
	if exeErr != nil {
		return "", LatestReleaseResponse{}, fmt.Errorf("brak pliku aktualizacji dla tagu %s", latestTag)
	}

	return exePath, LatestReleaseResponse{TagName: latestTag}, nil
}

func getLatestTag(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/tags", repo)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		return "", fmt.Errorf("github api zwróciło status: %d", response.StatusCode)
	}

	var tags []struct {
		Name string `json:"name"`
	}
	if err = json.NewDecoder(response.Body).Decode(&tags); err != nil {
		return "", err
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("brak tagów w repozytorium")
	}

	return tags[0].Name, nil
}

func downloadToTemp(ctx context.Context, url, pattern string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		return "", fmt.Errorf("download status %d", response.StatusCode)
	}

	tmpFile, err := os.CreateTemp(os.TempDir(), pattern)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmpFile.Close()
	}()

	if _, err = io.Copy(tmpFile, response.Body); err != nil {
		return "", err
	}

	return tmpFile.Name(), nil
}

func extractExeFromZip(zipPath string) (string, error) {
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = zipReader.Close()
	}()

	for _, f := range zipReader.File {
		if !strings.EqualFold(filepath.Base(f.Name), "BizantiAgent.exe") {
			continue
		}

		src, openErr := f.Open()
		if openErr != nil {
			return "", openErr
		}

		tmpExe, createErr := os.CreateTemp(os.TempDir(), "BizantiAgent-*.exe")
		if createErr != nil {
			_ = src.Close()
			return "", createErr
		}

		_, copyErr := io.Copy(tmpExe, src)
		_ = src.Close()
		_ = tmpExe.Close()
		if copyErr != nil {
			return "", copyErr
		}

		return tmpExe.Name(), nil
	}

	return "", fmt.Errorf("brak BizantiAgent.exe w archiwum")
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
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

	tmpScript, err := os.CreateTemp(os.TempDir(), "bizanti-agent-update-*.cmd")
	if err != nil {
		return err
	}
	scriptPath := tmpScript.Name()
	_ = tmpScript.Close()

	script := fmt.Sprintf("@echo off\r\nset \"TARGET=%s\"\r\nset \"NEW=%s\"\r\n:loop\r\nping 127.0.0.1 -n 2 > nul\r\ndel \"%%TARGET%%\" >nul 2>nul\r\nif exist \"%%TARGET%%\" goto loop\r\nmove /Y \"%%NEW%%\" \"%%TARGET%%\" >nul\r\nstart \"\" \"%%TARGET%%\"\r\ndel \"%%~f0\"\r\n", targetPath, newBinaryPath)

	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	cmd := exec.Command("cmd.exe", "/D", "/C", scriptPath)
	cmd.Dir = filepath.Dir(targetPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	return cmd.Start()
}
