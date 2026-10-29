package devices

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// EspFirmwarePayload is the payload for the "update_esp_firmware" command.
// FirmwarePath is a Bizanti API path the agent downloads the .bin from;
// the upload goes to ESPHome's web_server OTA endpoint (POST /update).
type EspFirmwarePayload struct {
	ReadTemperaturePayload
	FirmwarePath string `json:"firmware_path"`
}

var (
	espUploadTimeout   = 120 * time.Second
	espRebootDelay     = 5 * time.Second
	espOnlineTimeout   = 90 * time.Second
	espOnlinePollEvery = 2 * time.Second
)

// UploadEspFirmware pushes firmware to the ESP and waits until it answers again after reboot.
// ESPHome keeps the previous firmware on the other OTA partition, so a failed upload is not fatal.
func UploadEspFirmware(p EspFirmwarePayload, firmware []byte) (map[string]any, error) {
	if strings.TrimSpace(p.Host) == "" {
		return nil, fmt.Errorf("update_esp_firmware: brak pola 'host'")
	}
	if len(firmware) == 0 {
		return nil, fmt.Errorf("update_esp_firmware: pusty plik firmware")
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("update", "firmware.bin")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(firmware); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, espURL(p.Host, p.Port, "/update"), &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	if p.Username != "" {
		req.SetBasicAuth(p.Username, p.Password)
	}

	resp, err := (&http.Client{Timeout: espUploadTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("wysyłanie firmware nieudane: %w", err)
	}
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK || strings.Contains(strings.ToLower(string(reply)), "fail") {
		return nil, fmt.Errorf("ESP odrzucił firmware (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(reply)))
	}

	// Give the ESP time to reboot so the old firmware's still-running server is not mistaken for success.
	started := time.Now()
	time.Sleep(espRebootDelay)

	probePath := "/"
	if len(p.Sensors) > 0 {
		probePath = "/sensor/" + url.PathEscape(p.Sensors[0])
	}
	client := &http.Client{Timeout: thermometerTimeout}

	for time.Since(started) < espOnlineTimeout {
		if probe, err := espGet(client, p.ReadTemperaturePayload, espURL(p.Host, p.Port, probePath)); err == nil {
			probe.Body.Close()
			if probe.StatusCode == http.StatusOK {
				return map[string]any{
					"uploaded":       true,
					"bytes":          len(firmware),
					"online_after_s": int(time.Since(started).Seconds()),
				}, nil
			}
		}
		time.Sleep(espOnlinePollEvery)
	}

	return nil, fmt.Errorf("firmware wysłany (%d B), ale ESP nie odpowiada po %s — sprawdź WiFi/hotspot awaryjny", len(firmware), espOnlineTimeout)
}
