//go:build debugtools

package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/NowakAdmin/BizantiAgent/internal/devices"
)

// executeDebugCommand handles the discovery/diagnostic commands that are
// compiled in only under the `debugtools` build tag (the internal -debug
// binary). It returns handled=false for any other command so the caller falls
// back to the regular command switch. The production stub in
// debugtools_stub.go always returns handled=false.
func (a *Agent) executeDebugCommand(command string, rawPayload json.RawMessage) (map[string]any, bool, error) {
	switch command {
	case "dibal500_raw":
		// Bypasses BuildArticleRegisters entirely: sends exactly the given
		// hex-encoded 130-byte registers, for testing hand-crafted byte
		// layouts (e.g. reverse-engineering the composition-field encoding)
		// without rebuilding the normal register builders.
		var payload struct {
			ScaleIP   string   `json:"scale_ip"`
			ScalePort int      `json:"scale_port,omitempty"`
			PCIP      string   `json:"pc_ip,omitempty"`
			TimeoutMs int      `json:"timeout_ms,omitempty"`
			Transform bool     `json:"transform,omitempty"`
			Registers []string `json:"registers"`
		}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}
		if len(payload.Registers) == 0 {
			return nil, true, fmt.Errorf("dibal500_raw: brak 'registers'")
		}

		res, err := a.runDibal500Bridge(payload.ScaleIP, payload.ScalePort, payload.PCIP, payload.TimeoutMs, payload.Transform, false, payload.Registers)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"registers": res.Registers}, true, nil

	case "tcp_capture":
		var payload struct {
			BindHost   string `json:"bind_host"`
			Ports      []int  `json:"ports"`
			DurationMs int    `json:"duration_ms"`
		}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}

		results, err := devices.CaptureTCP(payload.BindHost, payload.Ports, payload.DurationMs)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"captures": results}, true, nil

	case "ssh_exec":
		var payload devices.SSHExecConfig
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}

		output, err := devices.SSHExec(payload)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"output": output}, true, nil

	case "port_scan":
		var payload devices.PortScanPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}
		if strings.TrimSpace(payload.Host) == "" || len(payload.Ports) == 0 {
			return nil, true, fmt.Errorf("port_scan: wymagane pola 'host' i 'ports'")
		}

		results := devices.ScanPorts(payload.Host, payload.Ports, payload.TimeoutMs)
		ports := make(map[string]devices.PingResult, len(results))
		for port, result := range results {
			ports[fmt.Sprintf("%d", port)] = result
		}
		return map[string]any{"ports": ports}, true, nil

	default:
		return nil, false, nil
	}
}
