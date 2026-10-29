package devices

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const thermometerTimeout = 3 * time.Second

// ReadTemperaturePayload is the payload for the "read_temperature" command.
// The thermometer is an ESPHome device exposing its sensors through the
// web_server REST API (GET /sensor/<id>).
type ReadTemperaturePayload struct {
	Host     string   `json:"host"`
	Port     int      `json:"port,omitempty"`
	Username string   `json:"username,omitempty"` // ESPHome web_server auth (optional)
	Password string   `json:"password,omitempty"`
	Sensors  []string `json:"sensors"`
}

// TemperatureResult is a single sensor reading; Value is nil when the read failed.
type TemperatureResult struct {
	Value *float64 `json:"value,omitempty"`
	Error string   `json:"error,omitempty"`
}

// ReadTemperatures queries every configured sensor. A failing sensor is
// reported in its own entry so one broken probe never hides the other.
func ReadTemperatures(p ReadTemperaturePayload) (map[string]any, error) {
	host := strings.TrimSpace(p.Host)
	if host == "" {
		return nil, fmt.Errorf("read_temperature: brak pola 'host'")
	}
	if len(p.Sensors) == 0 {
		return nil, fmt.Errorf("read_temperature: brak pola 'sensors'")
	}

	client := &http.Client{Timeout: thermometerTimeout}
	sensors := map[string]TemperatureResult{}

	for _, sensor := range p.Sensors {
		sensors[sensor] = readTemperatureSensor(client, p, espURL(host, p.Port, "/sensor/"+url.PathEscape(sensor)))
	}

	return map[string]any{"sensors": sensors}, nil
}

func espURL(host string, port int, path string) string {
	if port <= 0 {
		port = 80
	}
	return fmt.Sprintf("http://%s:%d%s", strings.TrimSpace(host), port, path)
}

func espGet(client *http.Client, p ReadTemperaturePayload, target string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if p.Username != "" {
		req.SetBasicAuth(p.Username, p.Password)
	}
	return client.Do(req)
}

func readTemperatureSensor(client *http.Client, p ReadTemperaturePayload, sensorURL string) TemperatureResult {
	resp, err := espGet(client, p, sensorURL)
	if err != nil {
		return TemperatureResult{Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return TemperatureResult{Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	// ESPHome reports a disconnected probe as a null/NaN value or "NA" state.
	var body struct {
		Value *float64 `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return TemperatureResult{Error: "nieprawidłowa odpowiedź: " + err.Error()}
	}
	if body.Value == nil || math.IsNaN(*body.Value) {
		return TemperatureResult{Error: "brak odczytu (sonda odłączona?)"}
	}

	return TemperatureResult{Value: body.Value}
}
