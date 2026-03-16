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
	case "tcp_server", "server_tcp", "dibal_tcp_server", "dibal_server":
		return readWeightTCPServer(cfg, timeout)
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

func readWeightTCPServer(cfg ScaleConfig, timeout time.Duration) (float64, string, error) {
	bindHost := strings.TrimSpace(cfg.BindHost)
	if bindHost == "" {
		bindHost = "0.0.0.0"
	}

	txPort := cfg.TXPort
	if txPort <= 0 {
		if cfg.TCPPort > 0 {
			txPort = cfg.TCPPort
		} else {
			txPort = 3001
		}
	}

	rxPort := cfg.RXPort
	if rxPort <= 0 {
		rxPort = 3000
	}

	txAddr := net.JoinHostPort(bindHost, strconv.Itoa(txPort))
	txListener, err := net.Listen("tcp", txAddr)
	if err != nil {
		return 0, "", fmt.Errorf("nie można uruchomić nasłuchu TX na %s: %w", txAddr, err)
	}
	defer func() {
		_ = txListener.Close()
	}()

	var rxListener net.Listener
	if cfg.RequestCommand != "" {
		rxAddr := net.JoinHostPort(bindHost, strconv.Itoa(rxPort))
		rxListener, err = net.Listen("tcp", rxAddr)
		if err != nil {
			return 0, "", fmt.Errorf("nie można uruchomić nasłuchu RX na %s: %w", rxAddr, err)
		}
		defer func() {
			_ = rxListener.Close()
		}()
	}

	txConnCh := make(chan net.Conn, 1)
	txErrCh := make(chan error, 1)
	go func() {
		conn, acceptErr := acceptSingleConnection(txListener, timeout)
		if acceptErr != nil {
			txErrCh <- acceptErr
			return
		}

		txConnCh <- conn
	}()

	rxResultCh := make(chan error, 1)
	if cfg.RequestCommand != "" {
		go func() {
			rxConn, rxAcceptErr := acceptSingleConnection(rxListener, timeout)
			if rxAcceptErr != nil {
				rxResultCh <- rxAcceptErr
				return
			}
			defer func() {
				_ = rxConn.Close()
			}()

			_ = rxConn.SetWriteDeadline(time.Now().Add(timeout))
			if _, writeErr := rxConn.Write([]byte(cfg.RequestCommand)); writeErr != nil {
				rxResultCh <- fmt.Errorf("błąd wysyłania request_command do RX: %w", writeErr)
				return
			}

			rxResultCh <- nil
		}()
	}

	var txConn net.Conn
	select {
	case conn := <-txConnCh:
		txConn = conn
	case acceptErr := <-txErrCh:
		return 0, "", fmt.Errorf("błąd połączenia TX: %w", acceptErr)
	}
	defer func() {
		_ = txConn.Close()
	}()

	if cfg.RequestCommand != "" {
		rxErr := <-rxResultCh
		if rxErr != nil {
			return 0, "", fmt.Errorf("błąd połączenia RX: %w", rxErr)
		}
	}

	line, err := readLineFromConn(txConn, timeout)
	if err != nil {
		return 0, "", err
	}

	weight, err := parseWeight(line)
	if err != nil {
		return 0, line, err
	}

	return weight, line, nil
}

func acceptSingleConnection(listener net.Listener, timeout time.Duration) (net.Conn, error) {
	if tcpListener, ok := listener.(*net.TCPListener); ok {
		_ = tcpListener.SetDeadline(time.Now().Add(timeout))
	}

	conn, err := listener.Accept()
	if err != nil {
		return nil, err
	}

	return conn, nil
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
