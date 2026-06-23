package devices

import (
	"fmt"
	"net"
	"time"
)

// ProbeRawTCP connects to host:port, optionally writes data, and returns
// whatever the remote side sends back within readTimeoutMs. It is a generic
// diagnostic primitive (no protocol assumptions) used to test raw printer/
// scale TCP communication without printing a label.
func ProbeRawTCP(host string, port int, data string, readTimeoutMs int) (string, error) {
	if host == "" || port <= 0 {
		return "", fmt.Errorf("tcp_probe: wymagane pola 'host' i 'port'")
	}

	timeout := time.Duration(readTimeoutMs) * time.Millisecond
	if readTimeoutMs <= 0 {
		timeout = 3 * time.Second
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if data != "" {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte(data)); err != nil {
			return "", err
		}
	}

	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 4096)
	var response []byte
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			response = append(response, buf[:n]...)
		}
		if err != nil {
			break // timeout or EOF — return whatever we got, not an error
		}
	}

	return string(response), nil
}
