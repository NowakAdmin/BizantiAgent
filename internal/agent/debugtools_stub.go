//go:build !debugtools

package agent

import "encoding/json"

// executeDebugCommand is a no-op in the production build: the discovery/
// diagnostic commands (ssh_exec, port_scan) are not compiled in, so every
// command is reported as unhandled and falls through to the regular switch.
func (a *Agent) executeDebugCommand(command string, rawPayload json.RawMessage) (map[string]any, bool, error) {
	return nil, false, nil
}
