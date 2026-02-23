package tray

import (
	"context"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

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
	settingsItem := systray.AddMenuItem("Ustawienia", "Otwórz plik konfiguracji")
	versionItem := systray.AddMenuItem("Wersja: "+version.Version, "Wersja agenta: "+version.Version)
	versionItem.Disable()

	systray.AddSeparator()
	quit := systray.AddMenuItem("Zamknij", "Zamknij BizantiAgent")

	ctx := context.Background()
	updateTicker := time.NewTicker(time.Duration(a.cfg.Update.CheckIntervalHours) * time.Hour)
	if a.cfg.Update.CheckIntervalHours <= 0 {
		updateTicker = time.NewTicker(6 * time.Hour)
	}

	go func() {
		defer updateTicker.Stop()

		for {
			select {
			case <-start.ClickedCh:
				if a.agent.IsRunning() {
					continue
				}

				if startErr := a.agent.Start(ctx); startErr != nil {
					a.logger.Printf("Błąd startu agenta: %v", startErr)
					status.SetTitle("Status: błąd")
					continue
				}

				status.SetTitle("Status: łączenie...")
				start.Disable()
				stop.Enable()

			case <-stop.ClickedCh:
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
					continue
				}

				if result.HasUpdate {
					a.logger.Printf("Dostępna aktualizacja %s: %s", result.Version, result.URL)
					_ = openURL(result.URL)
				} else {
					a.logger.Printf("Brak nowszej wersji")
				}
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
			case <-updateTicker.C:
				checkCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				result, updateErr := update.CheckGitHubRelease(checkCtx, a.cfg.Update.GitHubRepo)
				cancel()

				if updateErr != nil {
					a.logger.Printf("Auto-check update error: %v", updateErr)
					continue
				}

				if result.HasUpdate {
					a.logger.Printf("Dostępna aktualizacja %s: %s", result.Version, result.URL)
				}

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

// loadIcon wczytuje ikonę bizanti logo z dysku i zwraca raw bytes dla systray
func loadIcon() []byte {
	// Spróbuj wczytać z assets w executable directory
	exePath, err := os.Executable()
	if err != nil {
		return nil
	}
	exeDir := filepath.Dir(exePath)

	logoPath := filepath.Join(exeDir, "assets", "bizanti_logo.png")

	// Fallback - jeśli nie ma w exe dir, spróbuj z bieżącego repo
	if _, err := os.Stat(logoPath); err != nil {
		logoPath = "assets/bizanti_logo.png"
	}

	data, err := os.ReadFile(logoPath)
	if err != nil {
		return nil
	}

	return data
}


