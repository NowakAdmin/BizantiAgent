package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

type UpdateConfig struct {
	GitHubRepo         string `json:"github_repo"`
	CheckIntervalHours int    `json:"check_interval_hours"`
}

type DibalServerConfig struct {
	Name     string `json:"name,omitempty"`
	BindHost string `json:"bind_host,omitempty"`
	RXPort   int    `json:"rx_port,omitempty"`
	TXPort   int    `json:"tx_port,omitempty"`
	Addr     byte   `json:"addr,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type Config struct {
	ServerURL        string              `json:"server_url"`
	WebSocketURL     string              `json:"websocket_url"`
	AgentToken       string              `json:"agent_token"`
	TenantID         string              `json:"tenant_id,omitempty"`
	HeartbeatSeconds int                 `json:"heartbeat_seconds"`
	Update           UpdateConfig        `json:"update"`
	DibalServers     []DibalServerConfig `json:"dibal_servers,omitempty"`
}

func Default() *Config {
	return &Config{
		ServerURL:        "https://bizanti.pl",
		WebSocketURL:     "wss://bizanti.pl/agent/ws",
		AgentToken:       "",
		TenantID:         "",
		HeartbeatSeconds: 30,
		Update: UpdateConfig{
			GitHubRepo:         "NowakAdmin/BizantiAgent",
			CheckIntervalHours: 6,
		},
	}
}

func LoadOrCreateDefault() (*Config, error) {
	if _, err := os.Stat(Path()); errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		if errSave := Save(cfg); errSave != nil {
			return nil, errSave
		}
		return cfg, nil
	}

	return Load()
}

func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		return nil, err
	}

	cfg := Default()
	if err = json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.HeartbeatSeconds <= 0 {
		cfg.HeartbeatSeconds = 30
	}

	if cfg.Update.CheckIntervalHours <= 0 {
		cfg.Update.CheckIntervalHours = 6
	}

	for i := range cfg.DibalServers {
		if cfg.DibalServers[i].BindHost == "" {
			cfg.DibalServers[i].BindHost = "0.0.0.0"
		}
		if cfg.DibalServers[i].RXPort <= 0 {
			cfg.DibalServers[i].RXPort = 3000
		}
		if cfg.DibalServers[i].TXPort <= 0 {
			cfg.DibalServers[i].TXPort = 3001
		}
		if cfg.DibalServers[i].Addr == 0 {
			cfg.DibalServers[i].Addr = 1
		}
	}

	return cfg, nil
}

func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(Path(), data, 0o600)
}

func Dir() string {
	programData := os.Getenv("ProgramData")
	if runtime.GOOS == "windows" {
		if programData == "" {
			programData = "C:\\ProgramData"
		}
		return filepath.Join(programData, "BizantiAgent")
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "."
	}

	return filepath.Join(configDir, "bizanti-agent")
}

func LogDir() string {
	return filepath.Join(Dir(), "logs")
}

func Path() string {
	return filepath.Join(Dir(), "config.json")
}
