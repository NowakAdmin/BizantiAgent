package devices

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// FetchPrinterWebSettings connects to the printer's built-in HTTP web interface
// and returns a map with the raw response body and any parsed settings fields.
// Designed for Intermec PM43c and compatible Honeywell industrial printers.
func FetchPrinterWebSettings(cfg PrinterConfig) (map[string]any, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		return nil, fmt.Errorf("brak adresu IP drukarki w konfiguracji")
	}

	webPort := cfg.WebPort
	if webPort <= 0 {
		webPort = 80
	}

	baseURL := fmt.Sprintf("http://%s:%d", host, webPort)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Try paths in order; stop at the first useful response.
	paths := []string{"/", "/deviceInfo", "/home"}

	var (
		respondedURL string
		body         string
	)

	for _, path := range paths {
		url := baseURL + path
		b, err := doGet(client, url, cfg.WebUser, cfg.WebPass)
		if err != nil {
			continue
		}
		if strings.TrimSpace(b) == "" {
			continue
		}
		respondedURL = url
		body = b
		break
	}

	if respondedURL == "" {
		return nil, fmt.Errorf("drukarka nie odpowiada na żadnym ze znanych adresów HTTP (%s, porty sprawdzone: %d)", host, webPort)
	}

	rawBody := body
	if len(rawBody) > 4000 {
		rawBody = rawBody[:4000]
	}

	return map[string]any{
		"url":      respondedURL,
		"raw_body": rawBody,
		"parsed":   parseIntermecHTML(body),
	}, nil
}

func doGet(client *http.Client, url, user, pass string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// parseIntermecHTML extracts key settings from PM43c HTML pages.
// Returns whatever fields it can find; an empty map is valid (raw_body still available).
func parseIntermecHTML(html string) map[string]string {
	result := map[string]string{}

	// Pattern 1: <td>Label</td><td>Value</td> (common in PM43c table layouts)
	tdPairRe := regexp.MustCompile(`(?i)<td[^>]*>\s*([^<]{2,60}?)\s*</td>\s*<td[^>]*>\s*([^<]{1,120}?)\s*</td>`)
	for _, m := range tdPairRe.FindAllStringSubmatch(html, -1) {
		key := cleanText(m[1])
		val := cleanText(m[2])
		if key != "" && val != "" && !looksLikeHTML(val) {
			result[normalizeKey(key)] = val
		}
	}

	// Pattern 2: <label ...>Key</label> ... <span>Value</span>
	labelSpanRe := regexp.MustCompile(`(?i)<label[^>]*>\s*([^<]{2,60}?)\s*</label>[^<]{0,100}<span[^>]*>\s*([^<]{1,120}?)\s*</span>`)
	for _, m := range labelSpanRe.FindAllStringSubmatch(html, -1) {
		key := cleanText(m[1])
		val := cleanText(m[2])
		if key != "" && val != "" && !looksLikeHTML(val) {
			result[normalizeKey(key)] = val
		}
	}

	// Pattern 3: "Key: Value" text lines (sometimes present in PM43c info pages)
	kvLineRe := regexp.MustCompile(`(?m)^([A-Za-z][A-Za-z0-9 /\-]{2,40}):\s*(.{1,80})\s*$`)
	for _, m := range kvLineRe.FindAllStringSubmatch(stripTags(html), -1) {
		key := cleanText(m[1])
		val := cleanText(m[2])
		if key != "" && val != "" {
			result[normalizeKey(key)] = val
		}
	}

	return result
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	// Collapse whitespace
	wsRe := regexp.MustCompile(`\s+`)
	return wsRe.ReplaceAllString(s, " ")
}

func normalizeKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Replace spaces and slashes with underscores
	return regexp.MustCompile(`[\s/\-]+`).ReplaceAllString(s, "_")
}

func looksLikeHTML(s string) bool {
	return strings.ContainsAny(s, "<>")
}

func stripTags(html string) string {
	tagRe := regexp.MustCompile(`<[^>]+>`)
	return tagRe.ReplaceAllString(html, " ")
}
