package devices

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func SendToPrinter(cfg PrinterConfig, content string) error {
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))

	if transport == "" {
		transport = "raw_tcp"
	}

	switch transport {
	case "raw_tcp", "tcp", "network", "jetdirect":
		return sendRawTCP(cfg, content)
	case "windows", "windows_spooler", "spooler", "windows_printer":
		return sendWindowsSpooler(cfg, content)
	default:
		return fmt.Errorf("nieobsługiwany transport drukarki: %s", transport)
	}
}

func sendRawTCP(cfg PrinterConfig, content string) error {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return fmt.Errorf("brak host dla drukarki %s", cfg.Model)
	}

	port := cfg.Port
	if port <= 0 {
		port = 9100
	}

	timeout := time.Duration(cfg.WriteTimeoutS) * time.Second
	if cfg.WriteTimeoutS <= 0 {
		timeout = 5 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	_, err = conn.Write([]byte(content))
	return err
}

func sendWindowsSpooler(cfg PrinterConfig, content string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("windows_spooler jest dostępny tylko na Windows")
	}

	tmpFile, err := os.CreateTemp("", "bizanti-print-*.txt")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err = tmpFile.WriteString(content); err != nil {
		return err
	}

	printerName := strings.TrimSpace(cfg.PrinterName)
	pathArg := strings.ReplaceAll(tmpPath, "'", "''")

	var script string
	if printerName == "" {
		script = fmt.Sprintf("$ErrorActionPreference='Stop'; Get-Content -LiteralPath '%s' -Raw | Out-Printer", pathArg)
	} else {
		printerArg := strings.ReplaceAll(printerName, "'", "''")
		script = fmt.Sprintf("$ErrorActionPreference='Stop'; Get-Content -LiteralPath '%s' -Raw | Out-Printer -Name '%s'", pathArg, printerArg)
	}

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("błąd windows spooler: %s", msg)
	}

	return nil
}

func RenderTemplate(template string, values map[string]string) string {
	rendered := template
	for key, value := range values {
		doubleBracketPlaceholder := "{{" + key + "}}"
		singleBracketPlaceholder := "{" + key + "}"
		rendered = strings.ReplaceAll(rendered, doubleBracketPlaceholder, value)
		rendered = strings.ReplaceAll(rendered, singleBracketPlaceholder, value)
	}

	return rendered
}
