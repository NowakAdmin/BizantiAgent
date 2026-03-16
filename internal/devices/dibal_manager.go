package devices

// DibalManager maintains persistent TCP server connections to a Dibal K-series
// scale equipped with a Lantronix ETS-1 Ethernet-RS232 converter.
//
// # Why persistent connections?
//
// The Lantronix adapter is configured in "connected mode": it dials from the
// scale to the PC at power-on and keeps the TCP session alive indefinitely.
// Creating a fresh listener per print job misses the already-established
// connection and times out.
//
// # Architecture
//
//   - One background goroutine listens on the RX port (default 3000).
//     The scale connects here; the agent sends Dibal commands over this socket.
//   - One background goroutine listens on the TX port (default 3001).
//     The scale connects here; weight data is streamed to the agent.
//   - Both listeners are re-opened automatically when the scale reconnects.
//   - SendLines() and ReadWeight() use the cached connections under a mutex.

import (
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DibalManagerConfig configures the persistent Dibal TCP server.
type DibalManagerConfig struct {
	BindHost string // default "0.0.0.0"
	RXPort   int    // Port scale connects to for receiving commands (default 3000)
	TXPort   int    // Port scale connects to for sending weight data (default 3001)
	Addr     byte   // Scale K-series address (default 1)
	Logger   *log.Logger
}

// DibalManager holds live TCP connections to the Dibal scale.
type DibalManager struct {
	cfg DibalManagerConfig

	mu     sync.Mutex
	rxConn net.Conn // connection on RX port (we send commands here)
	txConn net.Conn // connection on TX port (we read weight here)

	done chan struct{}
	wg   sync.WaitGroup
}

// NewDibalManager creates and starts the persistent server.
// Call Close() to shut down background goroutines.
func NewDibalManager(cfg DibalManagerConfig) *DibalManager {
	if cfg.BindHost == "" {
		cfg.BindHost = "0.0.0.0"
	}
	if cfg.RXPort <= 0 {
		cfg.RXPort = 3000
	}
	if cfg.TXPort <= 0 {
		cfg.TXPort = 3001
	}
	if cfg.Addr == 0 {
		cfg.Addr = DibalDefaultAddr
	}
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}

	m := &DibalManager{
		cfg:  cfg,
		done: make(chan struct{}),
	}

	m.wg.Add(2)
	go m.acceptLoop(cfg.RXPort, true)
	go m.acceptLoop(cfg.TXPort, false)

	return m
}

// Close shuts down all background goroutines and closes open connections.
func (m *DibalManager) Close() {
	close(m.done)
	m.wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rxConn != nil {
		_ = m.rxConn.Close()
		m.rxConn = nil
	}
	if m.txConn != nil {
		_ = m.txConn.Close()
		m.txConn = nil
	}
}

// acceptLoop listens on the given port and stores accepted connections.
// isRX=true => store as rxConn; false => txConn.
func (m *DibalManager) acceptLoop(port int, isRX bool) {
	defer m.wg.Done()

	label := "TX"
	if isRX {
		label = "RX"
	}
	addr := net.JoinHostPort(m.cfg.BindHost, strconv.Itoa(port))

	for {
		// Check if we're shutting down.
		select {
		case <-m.done:
			return
		default:
		}

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			m.cfg.Logger.Printf("Dibal %s: nie można nasłuchiwać na %s: %v — ponawiam za 5s", label, addr, err)
			select {
			case <-m.done:
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		m.cfg.Logger.Printf("Dibal %s: nasłuch na %s", label, addr)

		// Accept one connection at a time.
		for {
			select {
			case <-m.done:
				_ = listener.Close()
				return
			default:
			}

			// Set a short accept deadline so we can check done channel.
			if tcpL, ok := listener.(*net.TCPListener); ok {
				_ = tcpL.SetDeadline(time.Now().Add(2 * time.Second))
			}

			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				if isTimeout(acceptErr) {
					continue // deadline expired — loop and check done
				}
				m.cfg.Logger.Printf("Dibal %s: accept error: %v", label, acceptErr)
				_ = listener.Close()
				break // reopen listener
			}

			remote := conn.RemoteAddr().String()
			m.cfg.Logger.Printf("Dibal %s: waga podłączona z %s", label, remote)

			// Store connection; close previous if any.
			m.mu.Lock()
			if isRX {
				if m.rxConn != nil {
					_ = m.rxConn.Close()
				}
				m.rxConn = conn
			} else {
				if m.txConn != nil {
					_ = m.txConn.Close()
				}
				m.txConn = conn
			}
			m.mu.Unlock()

			// Wait until this connection closes (EOF/error), then re-accept.
			waitForConnectionClose(conn, m.done)

			m.cfg.Logger.Printf("Dibal %s: połączenie z %s zakończone", label, remote)
			m.mu.Lock()
			if isRX {
				if m.rxConn == conn {
					m.rxConn = nil
				}
			} else {
				if m.txConn == conn {
					m.txConn = nil
				}
			}
			m.mu.Unlock()
		}

		// Wait before reopening listener.
		select {
		case <-m.done:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// waitForConnectionClose blocks until conn is closed or manager is shutting down.
func waitForConnectionClose(conn net.Conn, done chan struct{}) {
	buf := make([]byte, 1)
	for {
		select {
		case <-done:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, err := conn.Read(buf)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return // connection closed
		}
		// Discard any unsolicited bytes from scale.
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}

// SendLines sends semicolon-delimited Dibal register lines over the RX
// connection. Returns an error if the scale is not connected.
func (m *DibalManager) SendLines(lines []string, timeout time.Duration) error {
	m.mu.Lock()
	conn := m.rxConn
	m.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("waga Dibal nie jest połączona na porcie RX %d — sprawdź konfigurację Lantronix (Remote IP = IP tego komputera)", m.cfg.RXPort)
	}

	err := SendDibalLines(conn, m.cfg.Addr, lines, timeout)
	if err != nil {
		// Mark connection as dead so acceptLoop can re-accept.
		m.mu.Lock()
		if m.rxConn == conn {
			_ = conn.Close()
			m.rxConn = nil
		}
		m.mu.Unlock()
	}
	return err
}

// ReadWeightFromTX reads one weight line from the TX connection.
func (m *DibalManager) ReadWeightFromTX(timeout time.Duration) (float64, string, error) {
	m.mu.Lock()
	conn := m.txConn
	m.mu.Unlock()

	if conn == nil {
		return 0, "", fmt.Errorf("waga Dibal nie jest połączona na porcie TX %d", m.cfg.TXPort)
	}

	line, err := readLineFromConn(conn, timeout)
	if err != nil {
		if !isTimeout(err) {
			m.mu.Lock()
			if m.txConn == conn {
				_ = conn.Close()
				m.txConn = nil
			}
			m.mu.Unlock()
		}
		return 0, "", err
	}

	weight, parseErr := parseWeight(line)
	return weight, line, parseErr
}

// IsRXConnected reports whether the scale has an active RX connection.
func (m *DibalManager) IsRXConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rxConn != nil
}

// IsTXConnected reports whether the scale has an active TX connection.
func (m *DibalManager) IsTXConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.txConn != nil
}

// SendDibalContentPersistent calls manager.SendLines with newline-split content.
func SendDibalContentPersistent(manager *DibalManager, content string, timeout time.Duration) error {
	if manager == nil {
		return fmt.Errorf("brak DibalManager — skonfiguruj dibal_manager w agencie")
	}
	lines := strings.Split(content, "\n")
	return manager.SendLines(lines, timeout)
}

// SendDibalPLUPersistent programs a PLU via the persistent manager connection.
func SendDibalPLUPersistent(manager *DibalManager, plu DibalPLU, timeout time.Duration) error {
	if manager == nil {
		return fmt.Errorf("brak DibalManager — skonfiguruj dibal_manager w agencie")
	}

	mode := plu.Mode
	if mode == "" {
		mode = "M"
	}
	group := dibalPad(plu.Group, 2)
	code := dibalPad(plu.Code, 6)
	price := dibalPad(plu.Price, 6)

	barcode := strings.TrimSpace(plu.Barcode)
	if barcode == "" || barcode == "0" {
		barcode = "000000000000"
	}
	barcode = dibalPad(barcode, 12)

	labelNum := dibalPad(plu.LabelNum, 2)
	if plu.LabelNum == "" {
		labelNum = "01"
	}

	pluLine := strings.Join([]string{
		"X1", group, mode, code, plu.Name, price,
		barcode, "0", "000000", "000000", "00000",
		labelNum, "000000", "0000", "00", "0", "0", "0", "00", "00000",
	}, ";")

	lines := []string{
		"KB;00;02;01",
		pluLine,
		"KB;00;02;03",
	}

	return manager.SendLines(lines, timeout)
}

// ReadWeightPersistent reads weight using the persistent TX connection.
// Falls back to single-shot tcp_server if manager is nil (backward compat).
func ReadWeightPersistent(manager *DibalManager, cfg ScaleConfig) (float64, string, error) {
	if manager == nil {
		return ReadWeight(cfg)
	}
	timeout := time.Duration(cfg.ReadTimeoutMs) * time.Millisecond
	if cfg.ReadTimeoutMs <= 0 {
		timeout = 5 * time.Second
	}

	// Send request command via RX if configured.
	if cfg.RequestCommand != "" && manager.IsRXConnected() {
		_ = manager.SendLines([]string{cfg.RequestCommand}, timeout)
	}

	return manager.ReadWeightFromTX(timeout)
}

// Ensure io is used (for future weight streaming).
var _ = io.EOF
