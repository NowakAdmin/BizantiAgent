package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/NowakAdmin/BizantiAgent/internal/agent"
	"github.com/NowakAdmin/BizantiAgent/internal/config"
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
	agentID := fs.String("agent-id", cfg.AgentID, "ID konta agenta")
	token := fs.String("token", cfg.AgentToken, "Token API agenta")
	tenantID := fs.String("tenant-id", cfg.TenantID, "Opcjonalny tenant ID")
	deviceName := fs.String("name", cfg.DeviceName, "Nazwa agenta widoczna w Bizanti")
	githubRepo := fs.String("github-repo", cfg.Update.GitHubRepo, "Repo do auto-update, np. NowakAdmin/BizantiAgent")
	checkHours := fs.Int("update-hours", cfg.Update.CheckIntervalHours, "Co ile godzin sprawdzać aktualizacje")

	_ = fs.Parse(os.Args[2:])

	cfg.ServerURL = *serverURL
	cfg.WebSocketURL = *wsURL
	cfg.AgentID = *agentID
	cfg.AgentToken = *token
	cfg.TenantID = *tenantID
	cfg.DeviceName = *deviceName
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
	t := tray.New(cfg, a, logger)
	t.Run()
}

func buildLogger() (*log.Logger, func(), error) {
	logPath := filepath.Join(config.LogDir(), "agent.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, nil, err
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}

	w := io.MultiWriter(os.Stdout, f)
	logger := log.New(w, "[bizanti-agent] ", log.LstdFlags|log.Lmicroseconds)

	return logger, func() {
		_ = f.Close()
	}, nil
}
