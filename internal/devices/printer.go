package devices

import (
	"fmt"
	"net"
	"strings"
	"time"
)

func SendToPrinter(cfg PrinterConfig, content string) error {
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

func RenderTemplate(template string, values map[string]string) string {
	rendered := template
	for key, value := range values {
		placeholder := "{{" + key + "}}"
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}

	return rendered
}
