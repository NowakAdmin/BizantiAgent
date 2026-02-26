package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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
		if !acquireSingleInstance() {
			showInfoMessage("Bizanti Agent", "Agent jest już uruchomiony.")
			return
		}
		defer releaseSingleInstance()
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

// setConsoleTitle ustawia title okna konsoli na Windows
func setConsoleTitle(title string) {
	if runtime.GOOS != "windows" {
		return
	}
	setConsoleTitleFn := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleTitleW")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	setConsoleTitleFn.Call(uintptr(unsafe.Pointer(titlePtr)))
}

const errorAlreadyExists = 183

var instanceMutexHandle syscall.Handle

func acquireSingleInstance() bool {
	createMutex := syscall.NewLazyDLL("kernel32.dll").NewProc("CreateMutexW")
	getLastError := syscall.NewLazyDLL("kernel32.dll").NewProc("GetLastError")
	closeHandle := syscall.NewLazyDLL("kernel32.dll").NewProc("CloseHandle")

	namePtr, _ := syscall.UTF16PtrFromString("Local\\BizantiAgent")
	handle, _, _ := createMutex.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	if handle == 0 {
		return false
	}

	errCode, _, _ := getLastError.Call()
	if errCode == errorAlreadyExists {
		closeHandle.Call(handle)
		return false
	}

	instanceMutexHandle = syscall.Handle(handle)
	return true
}

func releaseSingleInstance() {
	if runtime.GOOS != "windows" {
		return
	}
	if instanceMutexHandle == 0 {
		return
	}

	closeHandle := syscall.NewLazyDLL("kernel32.dll").NewProc("CloseHandle")
	closeHandle.Call(uintptr(instanceMutexHandle))
	instanceMutexHandle = 0
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


