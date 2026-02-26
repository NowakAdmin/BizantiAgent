package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/NowakAdmin/BizantiAgent/internal/agent"
	"github.com/NowakAdmin/BizantiAgent/internal/config"
	"github.com/NowakAdmin/BizantiAgent/internal/setup"
	"github.com/NowakAdmin/BizantiAgent/internal/tray"
	"github.com/NowakAdmin/BizantiAgent/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "configure":
			runConfigure()
			return
		case "headless":
			runHeadless()
			return
		case "version":
			fmt.Printf("BizantiAgent %s\n", version.Version)
			return
		}
	}

	runTray()
}

func runConfigure() {
	cfg, err := config.LoadOrCreateDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Błąd odczytu konfiguracji: %v\n", err)
		os.Exit(1)
	}

	fs := flag.NewFlagSet("configure", flag.ExitOnError)
	serverURL := fs.String("server", cfg.ServerURL, "Base URL API Bizanti, np. https://bizanti.pl")
	wsURL := fs.String("ws", cfg.WebSocketURL, "URL WebSocket agenta, np. wss://bizanti.pl/agent/ws")
	token := fs.String("token", cfg.AgentToken, "Token API agenta")
	tenantID := fs.String("tenant-id", cfg.TenantID, "Opcjonalny tenant ID")
	githubRepo := fs.String("github-repo", cfg.Update.GitHubRepo, "Repo do auto-update, np. NowakAdmin/BizantiAgent")
	checkHours := fs.Int("update-hours", cfg.Update.CheckIntervalHours, "Co ile godzin sprawdzać aktualizacje")

	_ = fs.Parse(os.Args[2:])

	cfg.ServerURL = *serverURL
	cfg.WebSocketURL = *wsURL
	cfg.AgentToken = *token
	cfg.TenantID = *tenantID
	cfg.Update.GitHubRepo = *githubRepo
	cfg.Update.CheckIntervalHours = *checkHours

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Błąd zapisu konfiguracji: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Konfiguracja zapisana: %s\n", config.Path())
}

func runHeadless() {
	cfg, err := config.LoadOrCreateDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Błąd konfiguracji: %v\n", err)
		os.Exit(1)
	}

	logger, closeFn, err := buildLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Błąd loggera: %v\n", err)
		os.Exit(1)
	}
	defer closeFn()

	a := agent.New(cfg, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := a.Start(ctx); err != nil {
		logger.Fatalf("Nie udało się wystartować agenta: %v", err)
	}

	<-ctx.Done()
	a.Stop()
}

func runTray() {
	// Sprawdź first-run setup (czy plik jest w poprawnym miejscu)
	if runtime.GOOS == "windows" && setup.IsFirstRun() {
		if showYesNoMessage("Bizanti Agent", fmt.Sprintf(
			"Rekomendujemy przeniesienie pliku do:\n%s\\BizantiAgent\\\n\nCzy chcesz to zrobić teraz?",
			os.Getenv("PROGRAMDATA"),
		)) {
			if err := setup.MoveToAppData(); err != nil {
				showInfoMessage("Bizanti Agent", fmt.Sprintf("Błąd przenoszenia: %v", err))
			} else {
				showInfoMessage("Bizanti Agent", "Instalacja zakończona. Agent uruchomi się ponownie.")
				setup.RestartApp(filepath.Join(config.Dir(), "BizantiAgent.exe"))
				return
			}
		}
	}

	cfg, err := config.LoadOrCreateDefault()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Błąd konfiguracji: %v\n", err)
		os.Exit(1)
	}

	if runtime.GOOS == "windows" {
		if err := ensureSingleInstanceByProcessCleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "Ostrzeżenie: nie udało się wykonać cleanup innych instancji: %v\n", err)
		}
	}

	// Dla trybu tray ukryj i odłącz konsolę od razu, żeby nie zostawało puste okno na pasku.
	if runtime.GOOS == "windows" {
		hideAndDetachConsole()
	}

	logger, closeFn, err := buildLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Błąd loggera: %v\n", err)
		os.Exit(1)
	}
	defer closeFn()

	a := agent.New(cfg, logger)
	t := tray.New(cfg, a, logger)

	t.Run()
}

// hideAndDetachConsole ukrywa i odłącza bieżącą konsolę na Windows,
// dzięki czemu tray działa bez pustego okna na pasku zadań.
func hideAndDetachConsole() {
	if runtime.GOOS != "windows" {
		return
	}

	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleFn := kernel32.NewProc("GetConsoleWindow")
	freeConsoleFn := kernel32.NewProc("FreeConsole")
	hwnd, _, _ := getConsoleFn.Call()

	if hwnd != 0 {
		showWindowFn := syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow")
		showWindowFn.Call(hwnd, 0) // 0 = SW_HIDE
	}

	freeConsoleFn.Call()
}

func buildLogger() (*log.Logger, func(), error) {
	logPath := filepath.Join(config.LogDir(), "agent.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, nil, err
	}
	_ = os.Remove(logPath)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, nil, err
	}

	logger := log.New(f, "[bizanti-agent] ", log.LstdFlags|log.Lmicroseconds)
	logger.Printf("Logger uruchomiony, log: %s", logPath)

	return logger, func() {
		_ = f.Close()
	}, nil
}

type processInfo struct {
	ProcessID      int    `json:"ProcessId"`
	Name           string `json:"Name"`
	ExecutablePath string `json:"ExecutablePath"`
}

func ensureSingleInstanceByProcessCleanup() error {
	if runtime.GOOS != "windows" {
		return nil
	}

	currentPID := os.Getpid()
	processes, err := listAgentProcesses()
	if err != nil {
		return err
	}

	for _, process := range processes {
		if process.ProcessID == 0 || process.ProcessID == currentPID {
			continue
		}

		otherVersion := detectAgentVersion(process.ExecutablePath)
		fmt.Fprintf(os.Stderr, "Znaleziono inną instancję agenta (PID %d, wersja %s). Zamykanie...\n", process.ProcessID, otherVersion)

		if terminateErr := terminateProcess(process.ProcessID); terminateErr != nil {
			fmt.Fprintf(os.Stderr, "Nie udało się zamknąć PID %d: %v\n", process.ProcessID, terminateErr)
		}
	}

	return nil
}

func listAgentProcesses() ([]processInfo, error) {
	script := "$procs = Get-CimInstance Win32_Process | Where-Object { $_.Name -in @('BizantiAgent.exe','bizanti-agent.exe') } | Select-Object ProcessId,Name,ExecutablePath; if ($procs) { $procs | ConvertTo-Json -Compress }"
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return []processInfo{}, nil
	}

	if strings.HasPrefix(trimmed, "[") {
		var processes []processInfo
		if unmarshalErr := json.Unmarshal([]byte(trimmed), &processes); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		return processes, nil
	}

	var single processInfo
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &single); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return []processInfo{single}, nil
}

func detectAgentVersion(executablePath string) string {
	path := strings.TrimSpace(executablePath)
	if path == "" {
		return "unknown"
	}

	cmd := exec.Command(path, "version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	versionOutput := strings.TrimSpace(string(output))
	versionOutput = strings.TrimPrefix(versionOutput, "BizantiAgent ")
	if versionOutput == "" {
		return "unknown"
	}

	return versionOutput
}

func terminateProcess(pid int) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	return cmd.Run()
}

const mbOK = 0x00000000
const mbYesNo = 0x00000004
const mbIconInfo = 0x00000040
const idYes = 6

func showInfoMessage(title, message string) {
	if runtime.GOOS != "windows" {
		return
	}

	messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	textPtr, _ := syscall.UTF16PtrFromString(message)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	messageBox.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), mbOK|mbIconInfo)
}

// showYesNoMessage displays a Yes/No dialog and returns true if user clicked Yes
func showYesNoMessage(title, message string) bool {
	if runtime.GOOS != "windows" {
		return false
	}

	messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	textPtr, _ := syscall.UTF16PtrFromString(message)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	ret, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), mbYesNo|mbIconInfo)
	return int(ret) == idYes
}


