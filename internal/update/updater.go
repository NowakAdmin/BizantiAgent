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

	"github.com/NowakAdmin/BizantiAgent/internal/config"
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

	stagedBinaryPath, err := stageUpdateBinary(targetPath, newBinaryPath)
	if err != nil {
		return fmt.Errorf("nie udało się przygotować pliku aktualizacji: %w", err)
	}

	updateLogPath := filepath.Join(config.LogDir(), "update.log")
	if err := os.MkdirAll(filepath.Dir(updateLogPath), 0o755); err != nil {
		return fmt.Errorf("nie udało się przygotować katalogu logów aktualizacji: %w", err)
	}

	tmpScript, err := os.CreateTemp(os.TempDir(), "bizanti-agent-update-*.ps1")
	if err != nil {
		return err
	}
	scriptPath := tmpScript.Name()
	_ = tmpScript.Close()

	script := buildWindowsUpdateScript(targetPath, stagedBinaryPath, newBinaryPath, updateLogPath)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}

	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath)
	cmd.Dir = filepath.Dir(targetPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	return cmd.Start()
}

func stageUpdateBinary(targetPath string, newBinaryPath string) (string, error) {
	targetDir := filepath.Dir(targetPath)
	stagedBinaryPath := filepath.Join(targetDir, "BizantiAgent.update.exe")

	if err := copyFile(newBinaryPath, stagedBinaryPath); err != nil {
		return "", err
	}

	return stagedBinaryPath, nil
}

func copyFile(sourcePath string, destinationPath string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(destinationPath) == "" {
		return fmt.Errorf("ścieżki źródła i celu nie mogą być puste")
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		_ = sourceFile.Close()
	}()

	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}

	if err := os.Remove(destinationPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	destinationFile, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	_, err = io.Copy(destinationFile, sourceFile)
	closeErr := destinationFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	return nil
}

func buildWindowsUpdateScript(targetPath string, stagedBinaryPath string, downloadedBinaryPath string, updateLogPath string) string {
	targetPath = escapePowerShellSingleQuoted(targetPath)
	stagedBinaryPath = escapePowerShellSingleQuoted(stagedBinaryPath)
	downloadedBinaryPath = escapePowerShellSingleQuoted(downloadedBinaryPath)
	updateLogPath = escapePowerShellSingleQuoted(updateLogPath)

	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$target = '%s'
$staged = '%s'
$downloaded = '%s'
$logPath = '%s'
$scriptPath = $MyInvocation.MyCommand.Path
$backup = Join-Path (Split-Path -Parent $target) 'BizantiAgent.previous.exe'
$backupLeaf = Split-Path -Leaf $backup

function Write-UpdateLog {
    param([string]$Message)

    $timestamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss.fff'
    Add-Content -Path $logPath -Value "[$timestamp] $Message"
}

try {
    New-Item -ItemType Directory -Path (Split-Path -Parent $logPath) -Force | Out-Null
    Write-UpdateLog "Start self-update. target=$target staged=$staged downloaded=$downloaded"

    Stop-Process -Name 'BizantiAgent' -Force -ErrorAction SilentlyContinue
    Stop-Process -Name 'bizanti-agent' -Force -ErrorAction SilentlyContinue

    $replaced = $false
    for ($attempt = 1; $attempt -le 40; $attempt++) {
        Start-Sleep -Milliseconds 750

        try {
            if (Test-Path $backup) {
                Remove-Item $backup -Force -ErrorAction SilentlyContinue
            }

            if (Test-Path $target) {
                Rename-Item -Path $target -NewName $backupLeaf -Force
            }

            Move-Item -Path $staged -Destination $target -Force
            if (Test-Path $backup) {
                Remove-Item $backup -Force -ErrorAction SilentlyContinue
            }

            $replaced = $true
            Write-UpdateLog "Podmieniono plik EXE w próbie #$attempt."
            break
        } catch {
            if ((-not (Test-Path $target)) -and (Test-Path $backup)) {
                try {
                    Move-Item -Path $backup -Destination $target -Force
                } catch {
                    Write-UpdateLog "Nie udało się przywrócić poprzedniej wersji: $($_.Exception.Message)"
                }
            }

            Write-UpdateLog "Próba #$attempt nieudana: $($_.Exception.Message)"
        }
    }

    if (-not $replaced) {
        throw 'Nie udało się podmienić pliku BizantiAgent.exe po 40 próbach.'
    }

    if (Test-Path $downloaded) {
        Remove-Item $downloaded -Force -ErrorAction SilentlyContinue
    }

    Start-Process -FilePath $target | Out-Null
    Write-UpdateLog 'Uruchomiono nową wersję agenta.'
} catch {
    Write-UpdateLog "BŁĄD AKTUALIZACJI: $($_.Exception.Message)"
} finally {
    Start-Sleep -Milliseconds 500
    Remove-Item $scriptPath -Force -ErrorAction SilentlyContinue
}
`, targetPath, stagedBinaryPath, downloadedBinaryPath, updateLogPath)
}

func escapePowerShellSingleQuoted(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
