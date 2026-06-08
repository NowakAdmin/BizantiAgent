package tray

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/getlantern/systray"

	"github.com/NowakAdmin/BizantiAgent/internal/agent"
	"github.com/NowakAdmin/BizantiAgent/internal/autostart"
	"github.com/NowakAdmin/BizantiAgent/internal/config"
	"github.com/NowakAdmin/BizantiAgent/internal/update"
	"github.com/NowakAdmin/BizantiAgent/internal/version"
)

const appName = "BizantiAgent"

//go:embed app.ico
var embeddedTrayIcon []byte

type App struct {
	cfg    *config.Config
	agent  *agent.Agent
	logger *log.Logger
}

func New(cfg *config.Config, agentInstance *agent.Agent, logger *log.Logger) *App {
	return &App{
		cfg:    cfg,
		agent:  agentInstance,
		logger: logger,
	}
}

func (a *App) Run() {
	systray.Run(a.onReady, a.onExit)
}

func (a *App) onReady() {
	normalIcon := loadIcon()
	if len(normalIcon) > 0 {
		systray.SetIcon(normalIcon)
	}

	systray.SetTitle("Bizanti Agent")
	systray.SetTooltip("Bizanti Agent - local device bridge")
	a.logger.Printf("Tray uruchomiony, log: %s", filepath.Join(config.LogDir(), "agent.log"))

	status := systray.AddMenuItem("Status: offline", "Status połączenia")
	status.Disable()

	start := systray.AddMenuItem("Połącz", "Połącz z Bizanti")
	stop := systray.AddMenuItem("Rozłącz", "Rozłącz agenta")
	stop.Disable()

	autostartItem := systray.AddMenuItemCheckbox("Autostart (Windows)", "Uruchamiaj przy logowaniu", false)
	enabled, err := autostart.IsEnabled(appName)
	if err == nil && enabled {
		autostartItem.Check()
	}

	systray.AddSeparator()
	updateItem := systray.AddMenuItem("Sprawdź aktualizacje", "Sprawdź nowszą wersję")
	updateStatusItem := systray.AddMenuItem("Aktualizacje: nie sprawdzono", "Status aktualizacji")
	updateStatusItem.Disable()
	rollbackItem := systray.AddMenuItem("Przywróć poprzednią wersję", "Przywróć BizantiAgent.previous.exe")

	// Hide rollback unless a previous backup exists.
	if !previousVersionExists() {
		rollbackItem.Hide()
	}

	reloadItem := systray.AddMenuItem("Przeładuj ustawienia", "Przeładuj config.json bez restartu")
	settingsItem := systray.AddMenuItem("Ustawienia", "Otwórz plik konfiguracji")
	logsItem := systray.AddMenuItem("Pokaż log", "Otwórz agent.log")
	logsFolderItem := systray.AddMenuItem("Folder logów", "Otwórz folder z logami")
	versionItem := systray.AddMenuItem("Wersja: "+version.Version, "Wersja agenta: "+version.Version)
	versionItem.Disable()

	systray.AddSeparator()
	quit := systray.AddMenuItem("Zamknij", "Zamknij BizantiAgent")

	ctx := context.Background()

	a.logger.Printf("Auto-start agenta na starcie tryski")
	if err := a.agent.Start(ctx); err != nil {
		a.logger.Printf("Błąd auto-startu agenta: %v", err)
		status.SetTitle("Status: błąd")
	} else {
		status.SetTitle("Status: łączenie...")
		start.Disable()
		stop.Enable()
	}

	updateTicker := time.NewTicker(6 * time.Hour)
	if a.cfg.Update.CheckIntervalHours > 0 {
		updateTicker = time.NewTicker(time.Duration(a.cfg.Update.CheckIntervalHours) * time.Hour)
	}

	statusTicker := time.NewTicker(500 * time.Millisecond)

	go func() {
		defer updateTicker.Stop()
		defer statusTicker.Stop()

		for {
			select {
			case <-start.ClickedCh:
				if a.agent.IsRunning() {
					continue
				}
				a.logger.Printf("Start agenta: żądanie połączenia")
				if startErr := a.agent.Start(ctx); startErr != nil {
					a.logger.Printf("Błąd startu agenta: %v", startErr)
					status.SetTitle("Status: błąd")
					continue
				}
				status.SetTitle("Status: łączenie...")
				start.Disable()
				stop.Enable()

			case <-stop.ClickedCh:
				a.logger.Printf("Stop agenta: rozłączanie")
				a.agent.Stop()
				status.SetTitle("Status: offline")
				start.Enable()
				stop.Disable()

			case <-autostartItem.ClickedCh:
				if autostartItem.Checked() {
					if disableErr := autostart.Disable(appName); disableErr != nil {
						a.logger.Printf("Błąd wyłączenia autostartu: %v", disableErr)
						continue
					}
					autostartItem.Uncheck()
					continue
				}
				executablePath, pathErr := os.Executable()
				if pathErr != nil {
					a.logger.Printf("Błąd ścieżki EXE: %v", pathErr)
					continue
				}
				if enableErr := autostart.Enable(appName, executablePath); enableErr != nil {
					a.logger.Printf("Błąd autostartu: %v", enableErr)
					continue
				}
				autostartItem.Check()

			case <-updateItem.ClickedCh:
				checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				result, updateErr := update.CheckGitHubRelease(checkCtx, a.cfg.Update.GitHubRepo)
				cancel()

				if updateErr != nil {
					a.logger.Printf("Błąd sprawdzania aktualizacji: %v", updateErr)
					updateStatusItem.SetTitle("Aktualizacje: błąd")
					showMessageBox("Bizanti Agent", "Nie udało się sprawdzić aktualizacji.", mbOK|mbIconError)
					continue
				}

				if result.HasUpdate {
					a.logger.Printf("Dostępna aktualizacja %s: %s", result.Version, result.URL)
					updateStatusItem.SetTitle(fmt.Sprintf("Aktualizacje: nowa %s", result.Version))
					if showMessageBox("Bizanti Agent", fmt.Sprintf("Dostępna aktualizacja %s. Zainstalować teraz?", result.Version), mbYesNo|mbIconInfo) == idYes {
						a.performUpdate(result.Version, updateStatusItem, rollbackItem, normalIcon)
					}
				} else {
					a.logger.Printf("Używasz najnowszej wersji %s", version.Version)
					updateStatusItem.SetTitle(fmt.Sprintf("Aktualizacje: najnowsza %s", version.Version))
					systray.SetIcon(normalIcon)
					showMessageBox("Bizanti Agent", fmt.Sprintf("Masz najnowszą wersję %s", version.Version), mbOK|mbIconInfo)
				}

			case <-rollbackItem.ClickedCh:
				a.performRollback(rollbackItem)

			case <-reloadItem.ClickedCh:
				newCfg, reloadErr := config.Load()
				if reloadErr != nil {
					a.logger.Printf("Błąd przeładowania config: %v", reloadErr)
					continue
				}
				a.cfg = newCfg
				a.logger.Printf("Ustawienia przeładowane bez restartu")

			case <-settingsItem.ClickedCh:
				cfgPath := config.Path()
				if _, statErr := os.Stat(cfgPath); statErr != nil {
					if errCreate := config.Save(a.cfg); errCreate != nil {
						a.logger.Printf("Błąd tworzenia konfiguracji: %v", errCreate)
						continue
					}
				}
				if runtime.GOOS == "windows" {
					if err := exec.Command("notepad.exe", cfgPath).Start(); err != nil {
						a.logger.Printf("Błąd otwarcia edytora: %v", err)
					}
				}

			case <-logsItem.ClickedCh:
				logPath := filepath.Join(config.LogDir(), "agent.log")
				if runtime.GOOS == "windows" {
					if err := ensureLogFile(logPath); err != nil {
						a.logger.Printf("Błąd przygotowania logu: %v", err)
						continue
					}
					if err := exec.Command("notepad.exe", logPath).Start(); err != nil {
						a.logger.Printf("Błąd otwarcia logu: %v", err)
					}
				}

			case <-logsFolderItem.ClickedCh:
				if runtime.GOOS == "windows" {
					if err := exec.Command("explorer.exe", config.LogDir()).Start(); err != nil {
						a.logger.Printf("Błąd otwarcia folderu logów: %v", err)
					}
				}

			case <-updateTicker.C:
				checkCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				result, updateErr := update.CheckGitHubRelease(checkCtx, a.cfg.Update.GitHubRepo)
				cancel()

				if updateErr != nil {
					a.logger.Printf("Auto-check update error: %v", updateErr)
					updateStatusItem.SetTitle("Aktualizacje: błąd")
					continue
				}

				if result.HasUpdate {
					a.logger.Printf("Dostępna aktualizacja %s: %s", result.Version, result.URL)
					updateStatusItem.SetTitle(fmt.Sprintf("Aktualizacje: nowa %s", result.Version))
					systray.SetTooltip(fmt.Sprintf("Bizanti Agent v%s — dostępna aktualizacja %s", version.Version, result.Version))
				} else {
					updateStatusItem.SetTitle(fmt.Sprintf("Aktualizacje: najnowsza %s", version.Version))
					systray.SetIcon(normalIcon)
				}

			case <-statusTicker.C:
				statusStr := a.agent.GetStatus()
				status.SetTitle("Status: " + statusStr)
				systray.SetTooltip(fmt.Sprintf("Bizanti Agent v%s - %s", version.Version, statusStr))

			case <-quit.ClickedCh:
				a.agent.Stop()
				systray.Quit()
				return
			}
		}
	}()
}

// performUpdate downloads and installs the update, showing progress in the status label.
func (a *App) performUpdate(newVersion string, statusItem *systray.MenuItem, rollbackItem *systray.MenuItem, normalIcon []byte) {
	statusItem.SetTitle("Aktualizacje: pobieranie 0%")

	downloadCtx, cancelDownload := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelDownload()

	progress := func(received, total int64) {
		if total > 0 {
			pct := received * 100 / total
			statusItem.SetTitle(fmt.Sprintf("Aktualizacje: pobieranie %d%%", pct))
		} else {
			statusItem.SetTitle(fmt.Sprintf("Aktualizacje: pobieranie %s", formatBytes(received)))
		}
	}

	newBinaryPath, _, downloadErr := update.DownloadLatestWindowsAssetWithProgress(downloadCtx, a.cfg.Update.GitHubRepo, progress)
	if downloadErr != nil {
		a.logger.Printf("Błąd pobierania aktualizacji: %v", downloadErr)
		statusItem.SetTitle("Aktualizacje: błąd pobierania")
		showMessageBox("Bizanti Agent", "Nie udało się pobrać aktualizacji.", mbOK|mbIconError)
		return
	}

	statusItem.SetTitle("Aktualizacje: instalowanie...")

	if updateErr := update.StartSelfUpdate(newBinaryPath); updateErr != nil {
		a.logger.Printf("Błąd aktualizacji: %v", updateErr)
		statusItem.SetTitle("Aktualizacje: błąd instalacji")
		showMessageBox("Bizanti Agent", "Nie udało się zainstalować aktualizacji.", mbOK|mbIconError)
		return
	}

	a.logger.Printf("Aktualizacja pobrana, restart agenta do wersji %s", newVersion)
	systray.SetIcon(normalIcon)
	a.agent.Stop()
	systray.Quit()
	os.Exit(0)
}

// performRollback swaps BizantiAgent.exe back to BizantiAgent.previous.exe.
func (a *App) performRollback(rollbackItem *systray.MenuItem) {
	if showMessageBox("Bizanti Agent", "Przywrócić poprzednią wersję agenta?", mbYesNo|mbIconInfo) != idYes {
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		showMessageBox("Bizanti Agent", fmt.Sprintf("Błąd: %v", err), mbOK|mbIconError)
		return
	}

	previousPath := filepath.Join(filepath.Dir(exePath), "BizantiAgent.previous.exe")
	if _, err := os.Stat(previousPath); err != nil {
		showMessageBox("Bizanti Agent", "Brak poprzedniej wersji do przywrócenia.", mbOK|mbIconError)
		rollbackItem.Hide()
		return
	}

	if rollbackErr := update.StartRollback(exePath, previousPath); rollbackErr != nil {
		a.logger.Printf("Błąd rollback: %v", rollbackErr)
		showMessageBox("Bizanti Agent", fmt.Sprintf("Błąd przywracania: %v", rollbackErr), mbOK|mbIconError)
		return
	}

	a.logger.Printf("Rollback zainicjowany, restart agenta")
	a.agent.Stop()
	systray.Quit()
	os.Exit(0)
}

func previousVersionExists() bool {
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	previousPath := filepath.Join(filepath.Dir(exePath), "BizantiAgent.previous.exe")
	_, err = os.Stat(previousPath)
	return err == nil
}

func formatBytes(b int64) string {
	const kb = 1024
	const mb = kb * 1024
	switch {
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/mb)
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (a *App) onExit() {
	a.agent.Stop()
}

func openURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	return cmd.Start()
}

const (
	mbOK        = 0x00000000
	mbYesNo     = 0x00000004
	mbIconInfo  = 0x00000040
	mbIconError = 0x00000010
	idYes       = 6
)

func showMessageBox(title, message string, flags uintptr) int {
	if runtime.GOOS != "windows" {
		return 0
	}

	messageBox := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	textPtr, _ := syscall.UTF16PtrFromString(message)
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	ret, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(textPtr)), uintptr(unsafe.Pointer(titlePtr)), flags)
	return int(ret)
}

func ensureLogFile(logPath string) error {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

// loadIcon reads the embedded tray icon, falling back to disk.
func loadIcon() []byte {
	if len(embeddedTrayIcon) > 0 {
		return embeddedTrayIcon
	}
	exePath, err := os.Executable()
	if err != nil {
		return nil
	}
	logoPath := filepath.Join(filepath.Dir(exePath), "assets", "app.ico")
	if _, err := os.Stat(logoPath); err != nil {
		logoPath = "assets/app.ico"
	}
	data, err := os.ReadFile(logoPath)
	if err != nil {
		return nil
	}
	return data
}
