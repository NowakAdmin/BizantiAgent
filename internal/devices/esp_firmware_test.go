package devices

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func espTestPayload(t *testing.T, server *httptest.Server) EspFirmwarePayload {
	host, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return EspFirmwarePayload{ReadTemperaturePayload: ReadTemperaturePayload{
		Host: host, Port: port, Username: "admin", Password: "secret", Sensors: []string{"chlodnia"},
	}}
}

func TestUploadEspFirmware(t *testing.T) {
	espRebootDelay, espOnlinePollEvery = 0, 10*time.Millisecond

	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/update" {
			file, _, err := r.FormFile("update")
			if err != nil {
				t.Errorf("missing update file: %v", err)
				return
			}
			received, _ = io.ReadAll(file)
			w.Write([]byte("Update Successful!"))
			return
		}
		w.Write([]byte(`{"value":1.5}`))
	}))
	defer server.Close()

	result, err := UploadEspFirmware(espTestPayload(t, server), []byte("FIRMWARE"))
	if err != nil {
		t.Fatal(err)
	}
	if string(received) != "FIRMWARE" || result["uploaded"] != true {
		t.Fatalf("unexpected upload: %q %+v", received, result)
	}
}

func TestUploadEspFirmwareReportsRejectedUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Update Failed!"))
	}))
	defer server.Close()

	if _, err := UploadEspFirmware(espTestPayload(t, server), []byte("FIRMWARE")); err == nil {
		t.Fatal("expected error for rejected firmware")
	}
}
