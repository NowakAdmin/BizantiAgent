//go:build debugtools

package agent

import (
	"encoding/hex"
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

	case "dibal500_send_h3":
		// Builds and sends a single H3 register via devices.BuildH3Register —
		// exploratory tool to test whether FechaCongelacion (H3) is what
		// actually drives the scale's "przechowuj zamrożone" message, since
		// the normal push path only ever sends L2/L3/L4/X4/AS. See the
		// BuildH3Register doc comment for the full byte-layout rationale.
		var payload struct {
			ScaleIP       string              `json:"scale_ip"`
			ScalePort     int                 `json:"scale_port,omitempty"`
			PCIP          string              `json:"pc_ip,omitempty"`
			TimeoutMs     int                 `json:"timeout_ms,omitempty"`
			Transform     bool                `json:"transform,omitempty"`
			ShelfLifeDays *int                `json:"shelf_life_days,omitempty"`
			PLU           devices.Dibal500PLU `json:"plu"`
		}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}
		if strings.TrimSpace(payload.PLU.Code) == "" {
			return nil, true, fmt.Errorf("dibal500_send_h3: brak 'plu.code'")
		}

		reg, err := devices.BuildH3Register(payload.PLU, payload.ShelfLifeDays)
		if err != nil {
			return nil, true, err
		}

		res, err := a.runDibal500Bridge(payload.ScaleIP, payload.ScalePort, payload.PCIP, payload.TimeoutMs, payload.Transform, false, []string{hex.EncodeToString(reg)})
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"registers": res.Registers, "sent_hex": hex.EncodeToString(reg)}, true, nil

	case "dibal500_send_format":
		// Builds and sends a full Dibal 500 label FORMAT (the physical
		// layout — "4R" header + "H6" field-placement registers) via
		// devices.BuildFormatRegisters, bypassing DLD entirely. Exploratory:
		// test on an unused format slot (e.g. 40-45) and confirm on the
		// scale/via DFS-RGI-LBS readback before touching a live format.
		var payload struct {
			ScaleIP     string                        `json:"scale_ip"`
			ScalePort   int                           `json:"scale_port,omitempty"`
			PCIP        string                        `json:"pc_ip,omitempty"`
			TimeoutMs   int                           `json:"timeout_ms,omitempty"`
			Transform   bool                          `json:"transform,omitempty"`
			LogicalAddr string                        `json:"logical_addr,omitempty"`
			Group       string                        `json:"group,omitempty"`
			FormatNum   string                        `json:"format_num"`
			Width       int                           `json:"width"`
			Height      int                           `json:"height"`
			Fields      []devices.Dibal500FormatField `json:"fields"`
		}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}
		if strings.TrimSpace(payload.FormatNum) == "" {
			return nil, true, fmt.Errorf("dibal500_send_format: brak 'format_num'")
		}

		logicalAddr := payload.LogicalAddr
		if strings.TrimSpace(logicalAddr) == "" {
			logicalAddr = "00"
		}
		group := payload.Group
		if strings.TrimSpace(group) == "" {
			group = "00"
		}

		regs, err := devices.BuildFormatRegisters(logicalAddr, group, payload.FormatNum, payload.Width, payload.Height, payload.Fields)
		if err != nil {
			return nil, true, err
		}

		hexRegs := make([]string, len(regs))
		for i, r := range regs {
			hexRegs[i] = hex.EncodeToString(r)
		}

		res, err := a.runDibal500Bridge(payload.ScaleIP, payload.ScalePort, payload.PCIP, payload.TimeoutMs, payload.Transform, false, hexRegs)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"registers": res.Registers, "sent_hex": hexRegs}, true, nil

	case "dibal500_read_format":
		// Research spike: asks the scale for a format's 4R/H6 registers
		// back ("Pedir formato"/"Recibir" in DFS/DLD) via
		// devices.BuildFormatRequestRegister + dibalcom-bridge's
		// -read-format mode, then decodes the result with
		// devices.ParseFormatRegisters. Test on a format we already
		// control (e.g. 40) first and compare against what
		// dibal500_send_format last wrote, before trusting this for
		// unknown/factory formats.
		var payload struct {
			ScaleIP   string `json:"scale_ip"`
			ScalePort int    `json:"scale_port,omitempty"`
			PCIP      string `json:"pc_ip,omitempty"`
			PCPort    int    `json:"pc_port,omitempty"`
			TimeoutMs int    `json:"timeout_ms,omitempty"`
			FormatNum int    `json:"format_num"`
		}
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, true, err
		}

		res, err := a.runDibal500BridgeReadFormat(payload.ScaleIP, payload.ScalePort, payload.PCIP, payload.PCPort, payload.TimeoutMs, payload.FormatNum)
		if err != nil {
			return nil, true, err
		}

		registers := make([][]byte, 0, len(res.Registers))
		for _, h := range res.Registers {
			reg, decErr := hex.DecodeString(h)
			if decErr != nil {
				continue
			}
			registers = append(registers, reg)
		}

		formats, err := devices.ParseFormatRegisters(registers)
		if err != nil {
			return map[string]any{"terminated_by": res.TerminatedBy, "raw_hex": res.Registers, "parse_error": err.Error()}, true, nil
		}

		return map[string]any{"terminated_by": res.TerminatedBy, "raw_hex": res.Registers, "formats": formats}, true, nil

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
