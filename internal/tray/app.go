package tray

import (
	"context"
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
	// Załaduj ikonę bizanti logo
	if iconData := loadIcon(); len(iconData) > 0 {
		systray.SetIcon(iconData)
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
	reloadItem := systray.AddMenuItem("Przeładuj ustawienia", "Przeładuj config.json bez restartu")
	settingsItem := systray.AddMenuItem("Ustawienia", "Otwórz plik konfiguracji")
	logsItem := systray.AddMenuItem("Pokaż log", "Otwórz agent.log")
	logsFolderItem := systray.AddMenuItem("Folder logów", "Otwórz folder z logami")
	versionItem := systray.AddMenuItem("Wersja: "+version.Version, "Wersja agenta: "+version.Version)
	versionItem.Disable()

	systray.AddSeparator()
	quit := systray.AddMenuItem("Zamknij", "Zamknij BizantiAgent")

	ctx := context.Background()

	// Auto-start agent on tray creation
	a.logger.Printf("Auto-start agenta na starcie tryski")
	if err := a.agent.Start(ctx); err != nil {
		a.logger.Printf("Błąd auto-startu agenta: %v", err)
		status.SetTitle("Status: błąd")
	} else {
		status.SetTitle("Status: łączenie...")
		start.Disable()
		stop.Enable()
	}

	updateTicker := time.NewTicker(time.Duration(a.cfg.Update.CheckIntervalHours) * time.Hour)
	if a.cfg.Update.CheckIntervalHours <= 0 {
		updateTicker = time.NewTicker(6 * time.Hour)
	}

	// Status update ticker - update status display every 500ms
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
						updateStatusItem.SetTitle("Aktualizacje: pobieranie...")
						downloadCtx, cancelDownload := context.WithTimeout(context.Background(), 60*time.Second)
						newBinaryPath, _, downloadErr := update.DownloadLatestWindowsAsset(downloadCtx, a.cfg.Update.GitHubRepo)
						cancelDownload()
						if downloadErr != nil {
							a.logger.Printf("Błąd pobierania aktualizacji: %v", downloadErr)
							updateStatusItem.SetTitle("Aktualizacje: błąd")
							showMessageBox("Bizanti Agent", "Nie udało się pobrać aktualizacji.", mbOK|mbIconError)
							continue
						}

						if updateErr := update.StartSelfUpdate(newBinaryPath); updateErr != nil {
							a.logger.Printf("Błąd aktualizacji: %v", updateErr)
							updateStatusItem.SetTitle("Aktualizacje: błąd")
							showMessageBox("Bizanti Agent", "Nie udało się zainstalować aktualizacji.", mbOK|mbIconError)
							continue
						}

						a.logger.Printf("Aktualizacja pobrana, restart")
						a.agent.Stop()
						systray.Quit()
						os.Exit(0)
					}
				} else {
					a.logger.Printf("✓ Używasz najnowszej wersji %s", version.Version)
					updateStatusItem.SetTitle(fmt.Sprintf("Aktualizacje: najnowsza %s", version.Version))
					showMessageBox("Bizanti Agent", fmt.Sprintf("Masz najnowszą wersję %s", version.Version), mbOK|mbIconInfo)
				}
				case <-reloadItem.ClickedCh:
					newCfg, err := config.Load()
					if err != nil {
						a.logger.Printf("Błąd przeładowania config: %v", err)
						continue
					}
					a.cfg = newCfg
					a.logger.Printf("✓ Ustawienia przeładowane bez restartu")

				case <-settingsItem.ClickedCh:
					cfgPath := config.Path()
					if _, err := os.Stat(cfgPath); err != nil {
						// Jeśli plik nie istnieje, utwórz go
						if errCreate := config.Save(a.cfg); errCreate != nil {
							a.logger.Printf("Błąd tworzenia konfiguracji: %v", errCreate)
							continue
						}
					}
					// Otwórz plik w edytorze
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
					logDir := config.LogDir()
					if runtime.GOOS == "windows" {
						if err := exec.Command("explorer.exe", logDir).Start(); err != nil {
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
				} else {
					updateStatusItem.SetTitle(fmt.Sprintf("Aktualizacje: najnowsza %s", version.Version))
				}

			case <-statusTicker.C:
				// Update status item and tooltip with current connection state
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

func (a *App) onExit() {
	a.agent.Stop()
}

func openURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	return cmd.Start()
}

const (
	mbOK       = 0x00000000
	mbYesNo    = 0x00000004
	mbIconInfo = 0x00000040
	mbIconError = 0x00000010
	idYes      = 6
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

// loadIcon wczytuje ikonę bizanti logo z dysku i zwraca raw bytes dla systray
func loadIcon() []byte {
	// Spróbuj wczytać z assets w executable directory
	exePath, err := os.Executable()
	if err != nil {
		return nil
	}
	exeDir := filepath.Dir(exePath)

	logoPath := filepath.Join(exeDir, "assets", "app.ico")

	// Fallback - jeśli nie ma w exe dir, spróbuj z bieżącego repo
	if _, err := os.Stat(logoPath); err != nil {
		logoPath = "assets/app.ico"
	}

	data, err := os.ReadFile(logoPath)
	if err != nil {
		return nil
	}

	return data
}


