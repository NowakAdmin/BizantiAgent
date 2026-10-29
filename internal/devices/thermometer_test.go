package devices

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestReadTemperatures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sensor/chlodnia":
			w.Write([]byte(`{"id":"sensor-chlodnia","value":2.3125,"state":"2.3 °C"}`))
		case "/sensor/mroznia":
			w.Write([]byte(`{"id":"sensor-mroznia","value":null,"state":"NA"}`))
		case "/sensor/wolny":
			time.Sleep(thermometerTimeout + 500*time.Millisecond)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	host, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	result, err := ReadTemperatures(ReadTemperaturePayload{
		Host:    host,
		Port:    port,
		Sensors: []string{"chlodnia", "mroznia", "brak", "wolny"},
	})
	if err != nil {
		t.Fatal(err)
	}

	sensors := result["sensors"].(map[string]TemperatureResult)

	if v := sensors["chlodnia"].Value; v == nil || *v != 2.3125 {
		t.Fatalf("chlodnia: unexpected %+v", sensors["chlodnia"])
	}
	for _, name := range []string{"mroznia", "brak", "wolny"} {
		if sensors[name].Value != nil || sensors[name].Error == "" {
			t.Fatalf("%s: expected error, got %+v", name, sensors[name])
		}
	}
}

func TestReadTemperaturesRequiresHostAndSensors(t *testing.T) {
	if _, err := ReadTemperatures(ReadTemperaturePayload{Sensors: []string{"a"}}); err == nil {
		t.Fatal("expected error for missing host")
	}
	if _, err := ReadTemperatures(ReadTemperaturePayload{Host: "127.0.0.1"}); err == nil {
		t.Fatal("expected error for missing sensors")
	}
}
