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
