//go:build debugtools

// tcp_capture is a discovery/diagnostic primitive gated behind the `debugtools`
// build tag: it is compiled only into the internal -debug binary, never into
// the production build. It passively listens on one or more TCP ports and dumps
// whatever a device (e.g. a Dibal scale performing a backup / end-of-day send)
// transmits, so an undocumented wire protocol can be reverse-engineered from a
// known-good record.
package devices

import (
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CaptureTCPResult is one accepted connection's captured payload.
type CaptureTCPResult struct {
	Port       int    `json:"port"`
	RemoteAddr string `json:"remote_addr"`
	ByteCount  int    `json:"byte_count"`
	Hex        string `json:"hex"`
	ASCII      string `json:"ascii"`
}

// CaptureTCP listens on each of ports (on bindHost) for the given duration,
// accepting every inbound connection and recording all bytes it sends. It is
// purely passive — it never writes to the peer — so it observes exactly what a
// scale pushes when the operator triggers a backup or end-of-day transfer.
func CaptureTCP(bindHost string, ports []int, durationMs int) ([]CaptureTCPResult, error) {
	if bindHost == "" {
		bindHost = "0.0.0.0"
	}
	if len(ports) == 0 {
		ports = []int{3000, 3001}
	}
	duration := time.Duration(durationMs) * time.Millisecond
	if durationMs <= 0 {
		duration = 60 * time.Second
	}
	if duration > 5*time.Minute {
		duration = 5 * time.Minute
	}

	deadline := time.Now().Add(duration)

	var (
		mu       sync.Mutex
		captured []CaptureTCPResult
		wg       sync.WaitGroup
		bindErrs []string
	)

	for _, port := range ports {
		addr := net.JoinHostPort(bindHost, strconv.Itoa(port))
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			mu.Lock()
			bindErrs = append(bindErrs, fmt.Sprintf("%s: %v", addr, err))
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(port int, l net.Listener) {
			defer wg.Done()
			defer func() { _ = l.Close() }()

			for time.Now().Before(deadline) {
				if tcpL, ok := l.(*net.TCPListener); ok {
					_ = tcpL.SetDeadline(time.Now().Add(1 * time.Second))
				}
				conn, acceptErr := l.Accept()
				if acceptErr != nil {
					if isTimeout(acceptErr) {
						continue
					}
					return
				}

				wg.Add(1)
				go func(conn net.Conn) {
					defer wg.Done()
					defer func() { _ = conn.Close() }()

					remote := conn.RemoteAddr().String()
					var buf []byte
					tmp := make([]byte, 4096)
					for time.Now().Before(deadline) {
						_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
						n, readErr := conn.Read(tmp)
						if n > 0 {
							buf = append(buf, tmp[:n]...)
						}
						if readErr != nil {
							if isTimeout(readErr) {
								continue
							}
							break // EOF / closed
						}
					}

					if len(buf) == 0 {
						return
					}

					mu.Lock()
					captured = append(captured, CaptureTCPResult{
						Port:       port,
						RemoteAddr: remote,
						ByteCount:  len(buf),
						Hex:        hex.EncodeToString(buf),
						ASCII:      asciiPreview(buf),
					})
					mu.Unlock()
				}(conn)
			}
		}(port, listener)
	}

	wg.Wait()

	if len(captured) == 0 && len(bindErrs) > 0 {
		return nil, fmt.Errorf("tcp_capture: nie udało się nasłuchiwać: %s", strings.Join(bindErrs, "; "))
	}

	return captured, nil
}

// asciiPreview renders bytes as printable ASCII, non-printables as '.'.
func asciiPreview(data []byte) string {
	var b strings.Builder
	for _, c := range data {
		if c >= 0x20 && c < 0x7f {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}
	return b.String()
}
