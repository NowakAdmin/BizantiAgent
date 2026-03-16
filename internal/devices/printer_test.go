package devices

import (
	"runtime"
	"testing"
)

func TestCanUseWindowsSpoolerFallback(t *testing.T) {
	cfgWithoutPrinter := PrinterConfig{PrinterName: ""}
	if canUseWindowsSpoolerFallback(cfgWithoutPrinter) {
		t.Fatal("expected fallback disabled when printer_name is empty")
	}

	cfgWithPrinter := PrinterConfig{PrinterName: "Dibal W025S"}
	expected := runtime.GOOS == "windows"
	if canUseWindowsSpoolerFallback(cfgWithPrinter) != expected {
		t.Fatalf("unexpected fallback availability for runtime %s", runtime.GOOS)
	}
}
