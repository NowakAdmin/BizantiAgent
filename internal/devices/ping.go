package devices

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

const defaultPingTimeout = 3 * time.Second

// PingResult describes the reachability of a single device.
type PingResult struct {
	Reachable bool   `json:"reachable"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

// PingPrinter tests connectivity to the given printer configuration.
func PingPrinter(cfg PrinterConfig) PingResult {
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	switch transport {
	case "windows_spooler", "spooler", "windows":
		return pingWindowsSpooler(cfg.PrinterName)
	case "dibal_direct", "dibal", "dibal_tcp_server", "dibal_server":
		// Dibal uses persistent inbound connections managed by DibalManager.
		// The agent pre-starts listeners; if we reach here, the listener is up.
		return PingResult{Reachable: true}
	default:
		host := strings.TrimSpace(cfg.Host)
		port := cfg.Port
		if port <= 0 {
			port = 9100
		}
		return pingTCP(host, port)
	}
}

// PingScale tests connectivity to the given scale configuration.
func PingScale(cfg ScaleConfig) PingResult {
	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))
	switch transport {
	case "serial", "rs232", "com":
		return pingSerial(cfg.SerialPort)
	case "tcp_server", "server_tcp", "dibal_tcp_server", "dibal_server":
		// Scale connects inbound to the agent; listener is always up.
		return PingResult{Reachable: true}
	default:
		host := strings.TrimSpace(cfg.TCPHost)
		port := cfg.TCPPort
		if port <= 0 {
			port = 9100
		}
		return pingTCP(host, port)
	}
}

const maxConcurrentPortScans = 50

// ScanPorts tests TCP connectivity to host on each of the given ports
// concurrently and returns a reachability/latency result per port. It is a
// generic diagnostic primitive used to discover which port a device (e.g. a
// printer's auxiliary serial pass-through) actually listens on.
func ScanPorts(host string, ports []int, timeoutMs int) map[int]PingResult {
	results := make(map[int]PingResult, len(ports))
	if strings.TrimSpace(host) == "" || len(ports) == 0 {
		return results
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeoutMs <= 0 {
		timeout = defaultPingTimeout
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, maxConcurrentPortScans)
	)

	for _, port := range ports {
		port := port
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			result := pingTCPWithTimeout(host, port, timeout)

			mu.Lock()
			results[port] = result
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}

func pingTCP(host string, port int) PingResult {
	return pingTCPWithTimeout(host, port, defaultPingTimeout)
}

func pingTCPWithTimeout(host string, port int, timeout time.Duration) PingResult {
	if host == "" {
		return PingResult{Error: "brak adresu hosta"}
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return PingResult{Error: err.Error()}
	}
	_ = conn.Close()
	return PingResult{Reachable: true, LatencyMs: latency}
}

func pingSerial(portName string) PingResult {
	portName = strings.TrimSpace(portName)
	if portName == "" {
		return PingResult{Error: "brak nazwy portu szeregowego"}
	}
	mode := &serial.Mode{BaudRate: 9600}
	port, err := serial.Open(portName, mode)
	if err != nil {
		return PingResult{Error: err.Error()}
	}
	_ = port.Close()
	return PingResult{Reachable: true}
}

func pingWindowsSpooler(printerName string) PingResult {
	if runtime.GOOS != "windows" {
		return PingResult{Error: "windows_spooler dostępny tylko na Windows"}
	}
	printerName = strings.TrimSpace(printerName)
	if printerName == "" {
		return PingResult{Error: "brak nazwy drukarki"}
	}
	printers, err := listWindowsPrinters()
	if err != nil {
		return PingResult{Error: fmt.Sprintf("błąd listy drukarek: %v", err)}
	}
	for _, name := range printers {
		if strings.EqualFold(name, printerName) {
			return PingResult{Reachable: true}
		}
	}
	return PingResult{Error: fmt.Sprintf("drukarka '%s' nie znaleziona w systemie", printerName)}
}

func listWindowsPrinters() ([]string, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-Command", "Get-Printer | Select-Object -ExpandProperty Name | ConvertTo-Json -Compress")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return []string{}, nil
	}
	var printers []string
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &printers); unmarshalErr == nil {
		return printers, nil
	}
	var single string
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &single); unmarshalErr == nil {
		return []string{single}, nil
	}
	return nil, fmt.Errorf("nieoczekiwana odpowiedź z Get-Printer: %s", trimmed)
}
