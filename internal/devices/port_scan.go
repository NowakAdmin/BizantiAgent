//go:build debugtools

// port_scan is a discovery/diagnostic primitive gated behind the `debugtools`
// build tag: it is compiled only into the internal -debug binary, never into
// the production build. Excluding the port-scanning loop keeps the production
// binary clear of the behavior that antivirus ML heuristics flag as a scanner.
package devices

import (
	"strings"
	"sync"
	"time"
)

const maxConcurrentPortScans = 50

// PortScanPayload is the payload for the "port_scan" command. It probes
// each of Ports on Host and reports per-port reachability/latency — used to
// discover which TCP port a device actually listens on (e.g. a printer's
// serial pass-through port).
type PortScanPayload struct {
	Host      string `json:"host"`
	Ports     []int  `json:"ports"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

// ScanPorts tests TCP connectivity to host on each of the given ports
// concurrently and returns a reachability/latency result per port. It is a
// generic diagnostic primitive used to discover which port a device (e.g. a
// printer's auxiliary serial pass-through) actually listens on.
func ScanPorts(host string, ports []int, timeoutMs int) map[int]PingResult {
	results := make(map[int]PingResult, len(ports))
	if strings.TrimSpace(host) == "" || len(ports) == 0 {
		return results
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeoutMs <= 0 {
		timeout = defaultPingTimeout
	}

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, maxConcurrentPortScans)
	)

	for _, port := range ports {
		port := port
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			result := pingTCPWithTimeout(host, port, timeout)

			mu.Lock()
			results[port] = result
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}
