package devices

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

var weightPattern = regexp.MustCompile(`([0-9]+(?:[\.,][0-9]+)?)\s?kg`)

func ReadWeight(cfg ScaleConfig) (float64, string, error) {
	timeout := time.Duration(cfg.ReadTimeoutMs) * time.Millisecond
	if cfg.ReadTimeoutMs <= 0 {
		timeout = 3 * time.Second
	}

	transport := strings.ToLower(strings.TrimSpace(cfg.Transport))

	switch transport {
	case "serial", "rs232", "com":
		return readWeightSerial(cfg, timeout)
	case "tcp", "ethernet":
		return readWeightTCP(cfg, timeout)
	default:
		return 0, "", fmt.Errorf("nieobsługiwany transport wagi: %s", cfg.Transport)
	}
}

func readWeightSerial(cfg ScaleConfig, timeout time.Duration) (float64, string, error) {
	if strings.TrimSpace(cfg.SerialPort) == "" {
		return 0, "", errors.New("brak serial_port w konfiguracji")
	}

	requestedPort := strings.TrimSpace(cfg.SerialPort)
	availablePorts, portsErr := serial.GetPortsList()
	resolvedPort := normalizeSerialPortName(requestedPort, availablePorts)

	mode := buildSerialMode(cfg)
	port, err := serial.Open(resolvedPort, mode)
	if err != nil {
		if portsErr != nil {
			return 0, "", fmt.Errorf("nie można otworzyć portu %s: %w (nie udało się pobrać listy portów: %v)", resolvedPort, err, portsErr)
		}

		if len(availablePorts) == 0 {
			return 0, "", fmt.Errorf("nie można otworzyć portu %s: %w (brak dostępnych portów szeregowych)", resolvedPort, err)
		}

		return 0, "", fmt.Errorf("nie można otworzyć portu %s: %w (dostępne porty: %s)", resolvedPort, err, strings.Join(availablePorts, ", "))
	}
	defer func() {
		_ = port.Close()
	}()

	_ = port.SetReadTimeout(timeout)

	if cfg.RequestCommand != "" {
		_, _ = port.Write([]byte(cfg.RequestCommand))
	}

	line, err := readLineFromReader(port)
	if err != nil {
		return 0, "", err
	}

	weight, err := parseWeight(line)
	if err != nil {
		return 0, line, err
	}

	return weight, line, nil
}

func buildSerialMode(cfg ScaleConfig) *serial.Mode {
	baud := cfg.BaudRate
	if baud <= 0 {
		baud = 9600
	}

	dataBits := cfg.DataBits
	if dataBits < 5 || dataBits > 8 {
		dataBits = 8
	}

	parity := serial.NoParity
	switch strings.ToLower(strings.TrimSpace(cfg.Parity)) {
	case "odd", "o":
		parity = serial.OddParity
	case "even", "e":
		parity = serial.EvenParity
	case "mark", "m":
		parity = serial.MarkParity
	case "space", "s":
		parity = serial.SpaceParity
	}

	stopBits := serial.OneStopBit
	switch cfg.StopBits {
	case 2:
		stopBits = serial.TwoStopBits
	}

	return &serial.Mode{
		BaudRate: baud,
		DataBits: dataBits,
		Parity:   parity,
		StopBits: stopBits,
	}
}

func normalizeSerialPortName(requested string, available []string) string {
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		return trimmed
	}

	for _, candidate := range available {
		if strings.EqualFold(strings.TrimSpace(candidate), trimmed) {
			return candidate
		}
	}

	if runtime.GOOS == "windows" {
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "COM") {
			return upper
		}
	}

	return trimmed
}

func readWeightTCP(cfg ScaleConfig, timeout time.Duration) (float64, string, error) {
	if strings.TrimSpace(cfg.TCPHost) == "" || cfg.TCPPort <= 0 {
		return 0, "", errors.New("brak tcp_host/tcp_port w konfiguracji")
	}

	addr := net.JoinHostPort(cfg.TCPHost, strconv.Itoa(cfg.TCPPort))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return 0, "", err
	}
	defer func() {
		_ = conn.Close()
	}()

	_ = conn.SetDeadline(time.Now().Add(timeout))

	if cfg.RequestCommand != "" {
		_, _ = conn.Write([]byte(cfg.RequestCommand))
	}

	line, err := readLineFromConn(conn, timeout)
	if err != nil {
		return 0, "", err
	}

	weight, err := parseWeight(line)
	if err != nil {
		return 0, line, err
	}

	return weight, line, nil
}

func readLineFromConn(conn net.Conn, timeout time.Duration) (string, error) {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	return readLineFromReader(conn)
}

func readLineFromReader(r io.Reader) (string, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadString('\n')
	if err != nil {
		buffer := make([]byte, 128)
		n, _ := reader.Read(buffer)
		line = string(buffer[:n])
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return "", errors.New("pusta odpowiedź z wagi")
	}

	return trimmed, nil
}

func parseWeight(raw string) (float64, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return 0, errors.New("brak danych do parsowania")
	}

	if match := weightPattern.FindStringSubmatch(strings.ToLower(clean)); len(match) == 2 {
		normalized := strings.ReplaceAll(match[1], ",", ".")
		return strconv.ParseFloat(normalized, 64)
	}

	normalized := strings.ReplaceAll(clean, ",", ".")
	normalized = strings.TrimSuffix(strings.ToLower(normalized), "kg")
	normalized = strings.TrimSpace(normalized)

	return strconv.ParseFloat(normalized, 64)
}
