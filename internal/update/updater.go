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

// ProgressFunc is called periodically during a download with bytes received and
// total bytes (-1 when Content-Length is unknown).
type ProgressFunc func(received, total int64)

func DownloadLatestWindowsAsset(ctx context.Context, repo string) (string, LatestReleaseResponse, error) {
	return DownloadLatestWindowsAssetWithProgress(ctx, repo, nil)
}

func DownloadLatestWindowsAssetWithProgress(ctx context.Context, repo string, progress ProgressFunc) (string, LatestReleaseResponse, error) {
	release, err := GetLatestRelease(ctx, repo)
	if err != nil {
		if strings.Contains(err.Error(), "brak opublikowanego release") {
			return downloadFromTaggedRepoAssets(ctx, repo)
		}
		return "", LatestReleaseResponse{}, err
	}

	assetURL := ""
	assetName := ""
	var assetSize int64
	for _, asset := range release.Assets {
		if strings.EqualFold(asset.Name, "BizantiAgent.exe") {
			assetURL = asset.BrowserDownloadURL
			assetName = asset.Name
			assetSize = asset.Size
			break
		}
	}
	if assetURL == "" {
		for _, asset := range release.Assets {
			if strings.HasSuffix(strings.ToLower(asset.Name), ".exe") {
				assetURL = asset.BrowserDownloadURL
				assetName = asset.Name
				assetSize = asset.Size
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

	client := &http.Client{Timeout: 5 * time.Minute}
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

	// Prefer Content-Length from response over release metadata.
	total := assetSize
	if response.ContentLength > 0 {
		total = response.ContentLength
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

	var src io.Reader = response.Body
	if progress != nil {
		src = &progressReader{r: response.Body, total: total, fn: progress}
	}

	if _, err = io.Copy(tmpFile, src); err != nil {
		return "", LatestReleaseResponse{}, err
	}

	return tmpFile.Name(), release, nil
}

// progressReader wraps an io.Reader and calls fn after each read.
type progressReader struct {
	r        io.Reader
	total    int64
	received int64
	fn       ProgressFunc
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.received += int64(n)
		pr.fn(pr.received, pr.total)
	}
	return n, err
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

	var tags []githubTag
	if err = json.NewDecoder(response.Body).Decode(&tags); err != nil {
		return "", err
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("brak tagów w repozytorium")
	}

	return pickLatestTagName(tags), nil
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

// StartRollback replaces targetPath with previousBinaryPath using the same
// detached PowerShell mechanism as StartSelfUpdate.
func StartRollback(targetPath, previousBinaryPath string) error {
	if runtime.GOOS != "windows" {
		return errors.New("rollback wspierany tylko na Windows")
	}
	if strings.TrimSpace(previousBinaryPath) == "" {
		return errors.New("brak ścieżki do poprzedniej wersji")
	}
	return StartSelfUpdate(previousBinaryPath)
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

function Write-UpdateLog {
    param([string]$Message)
    $timestamp = Get-Date -Format 'yyyy-MM-dd HH:mm:ss.fff'
    try { Add-Content -Path $logPath -Value "[$timestamp] $Message" } catch {}
}

try {
    New-Item -ItemType Directory -Path (Split-Path -Parent $logPath) -Force | Out-Null
    Write-UpdateLog "=== Start self-update. target=$target staged=$staged ==="

    # --- Kill running instances ---
    Stop-Process -Name 'BizantiAgent'   -Force -ErrorAction SilentlyContinue
    Stop-Process -Name 'bizanti-agent'  -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 300
    & taskkill /F /IM 'BizantiAgent.exe' /T 2>&1 | Out-Null

    # --- Wait until process is fully gone (max 10 s) ---
    $deadline = (Get-Date).AddSeconds(10)
    while ((Get-Date) -lt $deadline) {
        if (-not (Get-Process -Name 'BizantiAgent' -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 300
    }
    Write-UpdateLog "Procesy zakończone. Rozpoczynam podmianę pliku."

    $replaced = $false
    for ($attempt = 1; $attempt -le 40; $attempt++) {
        Start-Sleep -Milliseconds 750

        try {
            # Remove old backup first — without silencing the error so the catch
            # block retries when antivirus still holds it.
            if (Test-Path $backup) {
                Remove-Item $backup -Force
            }

            # Move current exe → backup (Move-Item -Force overwrites on Windows).
            if (Test-Path $target) {
                Move-Item -Path $target -Destination $backup -Force
            }

            # Move staged → target.
            Move-Item -Path $staged -Destination $target -Force

            $replaced = $true
            Write-UpdateLog "OK: plik EXE podmieniony w próbie #$attempt."
            break
        } catch {
            Write-UpdateLog "Próba #$attempt nieudana: $($_.Exception.Message)"

            # If target was moved to backup but staged did not land, restore.
            if ((-not (Test-Path $target)) -and (Test-Path $backup)) {
                try {
                    Move-Item -Path $backup -Destination $target -Force
                    Write-UpdateLog "Przywrócono backup po nieudanej próbie #$attempt."
                } catch {
                    Write-UpdateLog "BŁĄD PRZYWRACANIA: $($_.Exception.Message)"
                }
            }
        }
    }

    if (-not $replaced) {
        # Update failed — restart the old binary so the agent is not left dead.
        Write-UpdateLog "BŁĄD: nie udało się podmienić EXE po 40 próbach. Wznawiam starą wersję."
        if (Test-Path $target) {
            Start-Process -FilePath $target | Out-Null
            Write-UpdateLog "Uruchomiono starą wersję agenta."
        }
        throw 'Nie udało się podmienić pliku BizantiAgent.exe po 40 próbach.'
    }

    # Cleanup temp download file.
    if (Test-Path $downloaded) {
        Remove-Item $downloaded -Force -ErrorAction SilentlyContinue
    }

    # Give the OS a moment to fully release file handles before launching.
    Start-Sleep -Milliseconds 1000
    Start-Process -FilePath $target | Out-Null
    Write-UpdateLog "Uruchomiono nową wersję agenta."
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
