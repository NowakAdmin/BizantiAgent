package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NowakAdmin/BizantiAgent/internal/config"
)

// IsFirstRun checks if the binary is running from the correct location.
// Returns true if binary is NOT in the expected %PROGRAMDATA%\BizantiAgent\ folder.
func IsFirstRun() bool {
	if runtime.GOOS != "windows" {
		return false
	}

	currentExe, err := os.Executable()
	if err != nil {
		return false
	}

	expectedDir := config.Dir()
	expectedPath := filepath.Join(expectedDir, "BizantiAgent.exe")

	currentAbsPath, _ := filepath.Abs(currentExe)
	expectedAbsPath, _ := filepath.Abs(expectedPath)

	// Compare as lowercase to avoid case-sensitivity issues on Windows
	return strings.ToLower(currentAbsPath) != strings.ToLower(expectedAbsPath)
}

// MoveToAppData moves the binary from current location to %PROGRAMDATA%\BizantiAgent\
// and updates autostart registry.
func MoveToAppData() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("only supported on Windows")
	}

	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	expectedDir := config.Dir()
	expectedPath := filepath.Join(expectedDir, "BizantiAgent.exe")

	// Create directory if not exists
	if err := os.MkdirAll(expectedDir, 0o755); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Copy file
	sourceData, err := os.ReadFile(currentExe)
	if err != nil {
		return fmt.Errorf("failed to read source binary: %w", err)
	}

	if err := os.WriteFile(expectedPath, sourceData, 0o755); err != nil {
		return fmt.Errorf("failed to write target binary: %w", err)
	}

	// Update autostart registry to point to new location
	updateAutostart(expectedPath)

	return nil
}

// VerifyAutostart checks if autostart registry points to the correct binary location.
// If not, updates it to the current location.
func VerifyAutostart() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	expectedDir := config.Dir()
	expectedPath := filepath.Join(expectedDir, "BizantiAgent.exe")

	// Get current registry autostart path
	currentRegistryPath, err := getAutostartPath()
	if err != nil {
		// If no registry entry exists, don't error - just return
		return nil
	}

	// Normalize paths for comparison
	currentReg := strings.ToLower(filepath.Clean(currentRegistryPath))
	expected := strings.ToLower(filepath.Clean(expectedPath))

	if currentReg != expected {
		// Update registry to correct path
		return setAutostart(expectedPath)
	}

	return nil
}

// getAutostartPath retrieves the autostart binary path from Windows registry.
func getAutostartPath() (string, error) {
	// Query HKCU\Software\Microsoft\Windows\CurrentVersion\Run\BizantiAgent
	cmd := exec.Command(
		"reg", "query",
		"HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
		"/v", "BizantiAgent",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	outputStr := string(output)
	lines := strings.Split(outputStr, "\n")

	for _, line := range lines {
		if strings.Contains(line, "BizantiAgent") && strings.Contains(line, "REG_SZ") {
			parts := strings.Split(line, "REG_SZ")
			if len(parts) > 1 {
				path := strings.TrimSpace(parts[1])
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("autostart entry not found")
}

// setAutostart sets the autostart registry entry to the specified binary path.
func setAutostart(binaryPath string) error {
	cmd := exec.Command(
		"reg", "add",
		"HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run",
		"/v", "BizantiAgent",
		"/d", binaryPath,
		"/f",
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to update autostart: %w", err)
	}

	return nil
}

// updateAutostart is a convenience wrapper that silently updates autostart
// without returning errors (used during setup).
func updateAutostart(binaryPath string) {
	_ = setAutostart(binaryPath)
}

// RestartApp restarts the application from the specified binary path.
// If binaryPath is empty, uses current executable.
func RestartApp(binaryPath string) {
	if binaryPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		binaryPath = exe
	}

	cmd := exec.Command(binaryPath)
	_ = cmd.Start()
	os.Exit(0)
}
