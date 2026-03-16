package devices

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func TestReadWeightTCPServerTXOnly(t *testing.T) {
	txPort := reserveFreePort(t)

	cfg := ScaleConfig{
		Transport:     "tcp_server",
		BindHost:      "127.0.0.1",
		TXPort:        txPort,
		ReadTimeoutMs: 2000,
	}

	go func() {
		conn := dialWithRetry(t, "127.0.0.1", txPort, 1500*time.Millisecond)
		defer func() {
			_ = conn.Close()
		}()

		_, _ = conn.Write([]byte("12.34 kg\r\n"))
	}()

	weight, raw, err := ReadWeight(cfg)
	if err != nil {
		t.Fatalf("ReadWeight returned error: %v", err)
	}

	if raw != "12.34 kg" {
		t.Fatalf("unexpected raw payload: %s", raw)
	}

	if weight != 12.34 {
		t.Fatalf("unexpected weight: %f", weight)
	}
}

func TestReadWeightTCPServerWithRequestCommand(t *testing.T) {
	txPort := reserveFreePort(t)
	rxPort := reserveFreePort(t)

	cfg := ScaleConfig{
		Transport:      "dibal_tcp_server",
		BindHost:       "127.0.0.1",
		TXPort:         txPort,
		RXPort:         rxPort,
		RequestCommand: "RW\r\n",
		ReadTimeoutMs:  2500,
	}

	rxCommandCh := make(chan string, 1)

	go func() {
		conn := dialWithRetry(t, "127.0.0.1", rxPort, 2*time.Second)
		defer func() {
			_ = conn.Close()
		}()

		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buffer := make([]byte, 16)
		n, err := conn.Read(buffer)
		if err != nil {
			rxCommandCh <- ""
			return
		}

		rxCommandCh <- string(buffer[:n])
	}()

	go func() {
		conn := dialWithRetry(t, "127.0.0.1", txPort, 2*time.Second)
		defer func() {
			_ = conn.Close()
		}()

		_, _ = conn.Write([]byte("7.500 kg\n"))
	}()

	weight, raw, err := ReadWeight(cfg)
	if err != nil {
		t.Fatalf("ReadWeight returned error: %v", err)
	}

	if raw != "7.500 kg" {
		t.Fatalf("unexpected raw payload: %s", raw)
	}

	if weight != 7.5 {
		t.Fatalf("unexpected weight: %f", weight)
	}

	command := <-rxCommandCh
	if command != "RW\r\n" {
		t.Fatalf("unexpected request command: %q", command)
	}
}

func reserveFreePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("cannot reserve free port: %v", err)
	}
	defer func() {
		_ = listener.Close()
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("cannot parse reserved TCP address")
	}

	return addr.Port
}

func dialWithRetry(t *testing.T, host string, port int, maxWait time.Duration) net.Conn {
	t.Helper()

	deadline := time.Now().Add(maxWait)
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	for {
		conn, err := net.DialTimeout("tcp", addr, 250*time.Millisecond)
		if err == nil {
			return conn
		}

		if time.Now().After(deadline) {
			t.Fatalf("cannot connect to %s in time: %v", addr, err)
		}

		time.Sleep(50 * time.Millisecond)
	}
}
