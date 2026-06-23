package devices

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
)

// SerialProbeConfig describes a one-off raw serial diagnostic: open a COM
// port with the given mode, optionally write data, and read back whatever
// the device sends within the timeout. Unlike ReadWeight, it does not assume
// any protocol or try to parse a weight — it is meant for reverse-engineering
// unknown device responses (e.g. when wiring up a new scale model).
type SerialProbeConfig struct {
	SerialPort    string `json:"serial_port"`
	BaudRate      int    `json:"baud_rate,omitempty"`
	DataBits      int    `json:"data_bits,omitempty"`
	Parity        string `json:"parity,omitempty"`
	StopBits      int    `json:"stop_bits,omitempty"`
	Data          string `json:"data,omitempty"`
	ReadTimeoutMs int    `json:"read_timeout_ms,omitempty"`
}

// ListSerialPorts returns the names of locally available serial (COM) ports.
func ListSerialPorts() ([]string, error) {
	return serial.GetPortsList()
}

// ProbeSerial opens the configured COM port, optionally writes Data, and
// returns the raw response received within ReadTimeoutMs.
func ProbeSerial(cfg SerialProbeConfig) (string, error) {
	if strings.TrimSpace(cfg.SerialPort) == "" {
		return "", errors.New("brak serial_port w konfiguracji")
	}

	timeout := time.Duration(cfg.ReadTimeoutMs) * time.Millisecond
	if cfg.ReadTimeoutMs <= 0 {
		timeout = 3 * time.Second
	}

	availablePorts, portsErr := serial.GetPortsList()
	resolvedPort := normalizeSerialPortName(cfg.SerialPort, availablePorts)

	mode := buildSerialMode(ScaleConfig{
		BaudRate: cfg.BaudRate,
		DataBits: cfg.DataBits,
		Parity:   cfg.Parity,
		StopBits: cfg.StopBits,
	})

	port, err := serial.Open(resolvedPort, mode)
	if err != nil {
		if portsErr == nil && len(availablePorts) > 0 {
			return "", fmt.Errorf("nie można otworzyć portu %s: %w (dostępne porty: %s)", resolvedPort, err, strings.Join(availablePorts, ", "))
		}
		return "", fmt.Errorf("nie można otworzyć portu %s: %w", resolvedPort, err)
	}
	defer func() {
		_ = port.Close()
	}()

	_ = port.SetReadTimeout(timeout)

	if cfg.Data != "" {
		if _, err := port.Write([]byte(cfg.Data)); err != nil {
			return "", fmt.Errorf("błąd zapisu do portu %s: %w", resolvedPort, err)
		}
	}

	buf := make([]byte, 4096)
	var response []byte
	for {
		n, readErr := port.Read(buf)
		if n > 0 {
			response = append(response, buf[:n]...)
		}
		if readErr != nil || n == 0 {
			break
		}
	}

	return string(response), nil
}
