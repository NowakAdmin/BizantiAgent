package agent

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/NowakAdmin/BizantiAgent/internal/config"
	"github.com/NowakAdmin/BizantiAgent/internal/devices"
	"github.com/NowakAdmin/BizantiAgent/internal/version"
)

type IncomingMessage struct {
	Type    string          `json:"type"`
	JobID   string          `json:"job_id,omitempty"`
	Command string          `json:"command,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type OutgoingMessage struct {
	Type      string         `json:"type"`
	AgentID   string         `json:"agent_id,omitempty"`
	JobID     string         `json:"job_id,omitempty"`
	Status    string         `json:"status,omitempty"`
	Timestamp string         `json:"timestamp,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type pullCommandsResponse struct {
	Success bool              `json:"success"`
	Data    []IncomingMessage `json:"data"`
}

type Agent struct {
	cfg    *config.Config
	logger *log.Logger

	running atomic.Bool
	done    chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Retry tracking
	consecutiveFailures int
	pausedUntil         time.Time
	connected           bool
	serverAgentID       string
	mu                  sync.Mutex

	// Persistent Dibal TCP server managers (keyed by "bindHost:rxPort").
	// Dibal scales (with Lantronix ETS-1) hold ONE permanent TCP connection
	// to the PC; a per-job listener would always time out.
	dibalMu       sync.Mutex
	dibalManagers map[string]*devices.DibalManager

	// Serializes Dibal 500-series pushes. Commands run concurrently, but the
	// scale is a single TCP endpoint — parallel connections clash, so PLU
	// programming for a batch of labels must go one at a time.
	dibal500Mu sync.Mutex
}

func New(cfg *config.Config, logger *log.Logger) *Agent {
	return &Agent{
		cfg:    cfg,
		logger: logger,
		done:   make(chan struct{}),
	}
}

func (a *Agent) Start(parent context.Context) error {
	if a.running.Swap(true) {
		return nil
	}

	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel

	// Pre-start persistent Dibal listeners from local config so Lantronix
	// devices can connect immediately after agent startup.
	for _, server := range a.cfg.DibalServers {
		if server.Enabled != nil && !*server.Enabled {
			continue
		}
		mgr := a.getOrCreateDibalManager(server.BindHost, server.RXPort, server.TXPort, server.Addr)
		if mgr != nil {
			name := strings.TrimSpace(server.Name)
			if name == "" {
				name = fmt.Sprintf("%s:%d", server.BindHost, server.RXPort)
			}
			a.logger.Printf("DibalManager prestart: %s", name)
		}
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		defer close(a.done)
		a.loop(ctx)
	}()

	return nil
}

func (a *Agent) Stop() {
	if !a.running.Load() {
		return
	}

	if a.cancel != nil {
		a.cancel()
	}

	a.wg.Wait()
	a.running.Store(false)
	a.setConnected(false)

	// Close all persistent Dibal managers.
	a.dibalMu.Lock()
	for key, mgr := range a.dibalManagers {
		mgr.Close()
		delete(a.dibalManagers, key)
	}
	a.dibalMu.Unlock()
}

func (a *Agent) IsRunning() bool {
	return a.running.Load()
}

// getOrCreateDibalManager returns the DibalManager for the given ports,
// creating and starting one if it does not exist yet.
// The manager runs background goroutines that accept persistent TCP connections
// from the Dibal scale's Lantronix adapter.
func (a *Agent) getOrCreateDibalManager(bindHost string, rxPort, txPort int, addr byte) *devices.DibalManager {
	if bindHost == "" {
		bindHost = "0.0.0.0"
	}
	if rxPort <= 0 {
		rxPort = 3000
	}
	if txPort <= 0 {
		txPort = 3001
	}
	if addr == 0 {
		addr = devices.DibalDefaultAddr
	}

	key := fmt.Sprintf("%s:%d", bindHost, rxPort)

	a.dibalMu.Lock()
	defer a.dibalMu.Unlock()

	if a.dibalManagers == nil {
		a.dibalManagers = make(map[string]*devices.DibalManager)
	}

	if mgr, ok := a.dibalManagers[key]; ok {
		return mgr
	}

	mgr := devices.NewDibalManager(devices.DibalManagerConfig{
		BindHost: bindHost,
		RXPort:   rxPort,
		TXPort:   txPort,
		Addr:     addr,
		Logger:   a.logger,
	})

	a.dibalManagers[key] = mgr
	a.logger.Printf("DibalManager uruchomiony: RX :%d  TX :%d  addr=0x%02X", rxPort, txPort, addr)
	return mgr
}

// recordFailure increments failure counter and sets pause if threshold reached
func (a *Agent) recordFailure() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.consecutiveFailures++
	a.connected = false

	// After 3 failures, pause for 5 minutes
	if a.consecutiveFailures >= 3 {
		a.pausedUntil = time.Now().Add(5 * time.Minute)
		a.logger.Printf("Zbyt wiele błędów (%d). Pauza na 5 minut.", a.consecutiveFailures)
	}
}

// recordSuccess resets failure counter
func (a *Agent) recordSuccess() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.consecutiveFailures = 0
	a.pausedUntil = time.Time{}
}

func (a *Agent) setConnected(connected bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.connected = connected
}

func (a *Agent) setServerAgentID(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.serverAgentID = strings.TrimSpace(id)
}

func (a *Agent) getServerAgentID() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.serverAgentID
}

// isPaused checks if we're currently paused from retrying
func (a *Agent) isPaused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.pausedUntil.IsZero() {
		return false
	}

	if time.Now().After(a.pausedUntil) {
		a.pausedUntil = time.Time{}
		return false
	}

	return true
}

// GetStatus returns human-readable connection status for display
func (a *Agent) GetStatus() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running.Load() {
		if !a.pausedUntil.IsZero() && time.Now().Before(a.pausedUntil) {
			return fmt.Sprintf("Pauza (próba za %d s)", int(a.pausedUntil.Sub(time.Now()).Seconds()))
		}
		if a.connected {
			return "Połączono"
		}
		if a.consecutiveFailures > 0 {
			return fmt.Sprintf("Łączenie... (próba %d)", a.consecutiveFailures+1)
		}
		return "Łączenie..."
	}

	return "Offline"
}

func (a *Agent) loop(ctx context.Context) {
	if strings.TrimSpace(a.cfg.AgentToken) == "" {
		a.logger.Printf("Brak tokena agenta. Użyj: bizanti-agent configure --token=...")
		<-ctx.Done()
		return
	}

	if strings.TrimSpace(a.cfg.ServerURL) == "" && strings.TrimSpace(a.cfg.WebSocketURL) == "" {
		a.logger.Printf("Brak ServerURL i WebSocketURL. Użyj: bizanti-agent configure ...")
		<-ctx.Done()
		return
	}

	backoff := 1 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Jeśli jesteśmy w pauzie, czekaj
		if a.isPaused() {
			a.logger.Printf("Agent w pauzie. Czekam zanim spróbuję ponownie...")
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}

		var err error
		websocketURL := strings.TrimSpace(a.cfg.WebSocketURL)

		if websocketURL != "" {
			err = a.runSession(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				a.logger.Printf("Sesja WebSocket zakończona: %v", err)
				a.recordFailure()
			} else if err == nil {
				a.recordSuccess()
			}

			if ctx.Err() != nil {
				return
			}

			a.logger.Printf("Przechodzę na fallback HTTP polling.")
			_ = a.runHTTPPolling(ctx, 45*time.Second)
		} else {
			err = a.runHTTPPolling(ctx, 0)
			if err != nil && !errors.Is(err, context.Canceled) {
				a.recordFailure()
			} else if err == nil {
				a.recordSuccess()
			}
		}

		if err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Printf("Pętla agenta zakończona błędem: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		if backoff < 20*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) runHTTPPolling(ctx context.Context, maxDuration time.Duration) error {
	if strings.TrimSpace(a.cfg.ServerURL) == "" {
		return fmt.Errorf("brak server_url do fallback HTTP")
	}

	heartbeatEvery := time.Duration(a.cfg.HeartbeatSeconds) * time.Second
	if a.cfg.HeartbeatSeconds <= 0 {
		heartbeatEvery = 30 * time.Second
	}

	pollTicker := time.NewTicker(2 * time.Second)
	heartbeatTicker := time.NewTicker(heartbeatEvery)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()

	if err := a.heartbeat(ctx); err != nil {
		a.setConnected(false)
		a.logger.Printf("HTTP heartbeat error: %v", err)
	}

	var timeout <-chan time.Time
	if maxDuration > 0 {
		timer := time.NewTimer(maxDuration)
		defer timer.Stop()
		timeout = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return context.Canceled
		case <-timeout:
			return nil
		case <-heartbeatTicker.C:
			if err := a.heartbeat(ctx); err != nil {
				a.setConnected(false)
				a.logger.Printf("HTTP heartbeat error: %v", err)
			}
		case <-pollTicker.C:
			commands, err := a.pullCommands(ctx)
			if err != nil {
				a.setConnected(false)
				return err
			}

			// Execute each command in its own goroutine so a slow/hung command
			// can't block the heartbeat or ctx cancellation in this select loop
			// (mirrors the WebSocket path in handleIncoming).
			for _, message := range commands {
				message := message
				go func() {
					commandName := strings.ToLower(strings.TrimSpace(message.Command))
					result, execErr := a.executeCommand(commandName, message.Payload)
					if reportErr := a.reportCommandResult(ctx, message.JobID, result, execErr); reportErr != nil {
						a.logger.Printf("Błąd raportowania wyniku job %s: %v", message.JobID, reportErr)
					}
				}()
			}
		}
	}
}

func (a *Agent) heartbeat(ctx context.Context) error {
	request, err := a.newAPIRequest(ctx, http.MethodPost, "/api/bizanticore/agent/heartbeat", nil)
	if err != nil {
		return err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return fmt.Errorf("heartbeat status %d: %s", response.StatusCode, summarizeResponseBody(body))
	}

	var payload struct {
		AgentID any `json:"agent_id"`
	}

	if err = json.NewDecoder(response.Body).Decode(&payload); err == nil {
		if payload.AgentID != nil {
			a.setServerAgentID(fmt.Sprintf("%v", payload.AgentID))
		}
	}

	a.setConnected(true)

	return nil
}

func (a *Agent) pullCommands(ctx context.Context) ([]IncomingMessage, error) {
	request, err := a.newAPIRequest(ctx, http.MethodGet, "/api/bizanticore/agent/commands/next?limit=5", nil)
	if err != nil {
		return nil, err
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		return nil, fmt.Errorf("pull commands status %d: %s", response.StatusCode, summarizeResponseBody(body))
	}

	var parsed pullCommandsResponse
	if err = json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	if !parsed.Success {
		return nil, fmt.Errorf("pull commands returned success=false")
	}

	a.setConnected(true)

	return parsed.Data, nil
}

func (a *Agent) reportCommandResult(ctx context.Context, jobID string, result map[string]any, execErr error) error {
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("brak job_id")
	}

	payload := map[string]any{}
	if execErr != nil {
		payload["status"] = "failed"
		payload["error"] = execErr.Error()
	} else {
		payload["status"] = "completed"
		payload["result"] = result
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := a.newAPIRequest(ctx, http.MethodPost, "/api/bizanticore/agent/commands/"+jobID+"/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(response.Body)
		return fmt.Errorf("report result status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	if execErr != nil {
		a.logger.Printf("Job %s failed: %v", jobID, execErr)
	} else {
		a.logger.Printf("Job %s completed", jobID)
	}

	return nil
}

func summarizeResponseBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "(pusta odpowiedź)"
	}

	lower := strings.ToLower(text)
	if strings.Contains(lower, "<!doctype") || strings.Contains(lower, "<html") {
		return "(odpowiedź HTML pominięta)"
	}

	const maxLen = 200
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}

	return text
}

func (a *Agent) newAPIRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error) {
	base := strings.TrimRight(strings.TrimSpace(a.cfg.ServerURL), "/")
	if base == "" {
		return nil, fmt.Errorf("server_url is empty")
	}

	pathPart := path
	if !strings.HasPrefix(pathPart, "/") {
		pathPart = "/" + pathPart
	}

	request, err := http.NewRequestWithContext(ctx, method, base+pathPart, body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Authorization", "Bearer "+a.cfg.AgentToken)

	return request, nil
}

func (a *Agent) runSession(ctx context.Context) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+a.cfg.AgentToken)
	if strings.TrimSpace(a.cfg.TenantID) != "" {
		headers.Set("X-Tenant-ID", a.cfg.TenantID)
	}

	conn, response, err := websocket.DefaultDialer.DialContext(ctx, a.cfg.WebSocketURL, headers)
	if err != nil {
		if response != nil {
			return fmt.Errorf("błąd połączenia websocket (http %d): %w", response.StatusCode, err)
		}

		return err
	}
	defer func() {
		a.setConnected(false)
		_ = conn.Close()
	}()

	a.logger.Printf("Połączono z Bizanti WebSocket: %s", a.cfg.WebSocketURL)

	if err = conn.WriteJSON(OutgoingMessage{
		Type:      "auth",
		AgentID:   a.getServerAgentID(),
		Status:    "online",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return err
	}

	a.setConnected(true)

	heartbeatEvery := time.Duration(a.cfg.HeartbeatSeconds) * time.Second
	if a.cfg.HeartbeatSeconds <= 0 {
		heartbeatEvery = 30 * time.Second
	}

	heartbeatTicker := time.NewTicker(heartbeatEvery)
	defer heartbeatTicker.Stop()

	ws := &wsSend{conn: conn}

	readErrors := make(chan error, 1)
	readMessages := make(chan IncomingMessage, 8)

	go func() {
		for {
			var message IncomingMessage
			if readErr := conn.ReadJSON(&message); readErr != nil {
				readErrors <- readErr
				return
			}
			readMessages <- message
		}
	}()

	for {
		select {
		case <-ctx.Done():
			ws.send(OutgoingMessage{Type: "status", Status: "offline"})
			return context.Canceled
		case err = <-readErrors:
			return err
		case message := <-readMessages:
			a.handleIncoming(ws, message)
		case <-heartbeatTicker.C:
			ws.send(OutgoingMessage{
				Type:      "heartbeat",
				AgentID:   a.getServerAgentID(),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Status:    "online",
			})
		}
	}
}

// wsSend is a mutex-protected WebSocket writer shared across goroutines.
type wsSend struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

func (w *wsSend) send(v any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.WriteJSON(v)
}

func (a *Agent) handleIncoming(ws *wsSend, message IncomingMessage) {
	messageType := strings.ToLower(strings.TrimSpace(message.Type))
	commandName := strings.ToLower(strings.TrimSpace(message.Command))

	switch {
	case messageType == "ping" || commandName == "ping":
		ws.send(OutgoingMessage{
			Type:      "pong",
			AgentID:   a.getServerAgentID(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			JobID:     message.JobID,
		})
		return

	case messageType == "command":
		// Execute device commands in a goroutine so the WebSocket read loop
		// can continue receiving messages (pings, other jobs) during execution.
		go func() {
			result, err := a.executeCommand(commandName, message.Payload)
			out := OutgoingMessage{
				Type:      "command_result",
				AgentID:   a.getServerAgentID(),
				JobID:     message.JobID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			if err != nil {
				out.Status = "failed"
				out.Error = err.Error()
				a.logger.Printf("Job %s failed: %v", message.JobID, err)
			} else {
				out.Status = "completed"
				out.Data = result
				a.logger.Printf("Job %s completed", message.JobID)
			}
			ws.send(out)
		}()
		return
	}
}

func (a *Agent) executeCommand(command string, rawPayload json.RawMessage) (map[string]any, error) {
	// Discovery/diagnostic commands (ssh_exec, port_scan) are compiled in only
	// under the `debugtools` build tag. In the production build the stub reports
	// them as unhandled, so they fall through to the "unknown command" default.
	if result, handled, err := a.executeDebugCommand(command, rawPayload); handled {
		return result, err
	}

	switch command {
	case "weigh_and_print":
		var payload devices.WeighAndPrintPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		weight := payload.WeightKg
		var rawResponse string
		if weight == nil {
			value, response, err := a.readWeightWithIntermecFallback(payload.Scale, payload.Printer)
			if err != nil {
				return nil, err
			}
			weight = &value
			rawResponse = response
		}

		replace := map[string]string{}
		for key, value := range payload.Context {
			replace[key] = value
		}
		replace["weight"] = fmt.Sprintf("%.3f", *weight)
		replace["weight_kg"] = fmt.Sprintf("%.3f kg", *weight)

		rendered := devices.RenderTemplate(payload.Template, replace)
		if err := devices.SendToPrinter(payload.Printer, rendered); err != nil {
			return nil, err
		}

		return map[string]any{
			"weight":       *weight,
			"raw_response": rawResponse,
			"printer":      payload.Printer.Model,
		}, nil

	case "print_label":
		var payload devices.WeighAndPrintPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		replace := map[string]string{}
		for key, value := range payload.Context {
			replace[key] = value
		}
		if payload.WeightKg != nil {
			replace["weight"] = fmt.Sprintf("%.3f", *payload.WeightKg)
			replace["weight_kg"] = fmt.Sprintf("%.3f kg", *payload.WeightKg)
		}

		rendered := devices.RenderTemplate(payload.Template, replace)

		// For Dibal direct transport, use the persistent DibalManager instead
		// of the per-job listener inside devices.SendToPrinter.
		printerTransport := strings.ToLower(strings.TrimSpace(payload.Printer.Transport))
		if printerTransport == "dibal_direct" || printerTransport == "dibal" ||
			printerTransport == "dibal_tcp_server" || printerTransport == "dibal_server" {
			if strings.Contains(rendered, "^XA") || strings.Contains(rendered, "^XZ") {
				return nil, fmt.Errorf("szablon ZPL nie jest obsługiwany przez Dibal; użyj linii rejestrów Dibal (np. X1;...)")
			}

			rxPort := payload.Printer.DibalRXPort
			if rxPort <= 0 {
				rxPort = 3000
			}
			txPort := 0 // TX port not needed for print-only; manager defaults to 3001
			if payload.Scale.TXPort > 0 {
				txPort = payload.Scale.TXPort
			}
			mgr := a.getOrCreateDibalManager(payload.Printer.DibalBindHost, rxPort, txPort, payload.Printer.DibalAddr)

			writeTimeout := 8 * time.Second
			if payload.Printer.WriteTimeoutS > 0 {
				writeTimeout = time.Duration(payload.Printer.WriteTimeoutS) * time.Second
			}

			if !mgr.WaitForRXConnected(writeTimeout) {
				return nil, fmt.Errorf("waga Dibal nie jest połączona na porcie RX %d — sprawdź konfigurację Lantronix (Remote IP = IP tego komputera)", rxPort)
			}

			if err := devices.SendDibalContentPersistent(mgr, rendered, writeTimeout); err != nil {
				return nil, err
			}
			return map[string]any{"printer": payload.Printer.Model}, nil
		}

		if err := devices.SendToPrinter(payload.Printer, rendered); err != nil {
			return nil, err
		}

		return map[string]any{
			"printer": payload.Printer.Model,
		}, nil

	case "read_weight":
		var payload devices.WeighAndPrintPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		weight, response, err := a.readWeightWithIntermecFallback(payload.Scale, payload.Printer)
		if err != nil {
			return nil, err
		}

		return map[string]any{
			"weight":       weight,
			"raw_response": response,
		}, nil

	case "tcp_probe":
		var payload devices.TcpProbePayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		response, err := devices.ProbeRawTCP(payload.Host, payload.Port, payload.Data, payload.ReadTimeoutMs)
		if err != nil {
			return nil, err
		}

		return map[string]any{"response": response}, nil

	case "program_dibal_plu":
		// Programs a PLU record directly into a Dibal K-series scale via TCP.
		// Does NOT require Windows Spooler or any Windows scale driver.
		// Uses the persistent DibalManager — scale connects once via Lantronix.
		var payload devices.DibalProgramPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		// Dibal 500-series built-in Ethernet (e.g. W-025S): the scale is the
		// TCP server and the PC connects to it. Server-mode (Lantronix) scales
		// use the DibalManager path below instead.
		scaleTransport := strings.ToLower(strings.TrimSpace(payload.Scale.Transport))
		if scaleTransport == "tcp" || scaleTransport == "ethernet" {
			a.logger.Printf("program_dibal_plu (klient TCP %s:%d): PLU=%s '%s'", payload.Scale.TCPHost, payload.Scale.TCPPort, payload.PLU.Code, payload.PLU.Name)

			if err := devices.SendDibalPLUOverTCPClient(payload.Scale, payload.PLU); err != nil {
				return nil, fmt.Errorf("błąd programowania PLU Dibal: %w", err)
			}

			a.logger.Printf("program_dibal_plu: PLU %s zaprogramowany pomyślnie", payload.PLU.Code)

			return map[string]any{
				"plu_code": payload.PLU.Code,
				"plu_name": payload.PLU.Name,
			}, nil
		}

		rxPort := payload.Scale.RXPort
		if rxPort <= 0 {
			rxPort = 3000
		}
		txPort := payload.Scale.TXPort
		if txPort <= 0 {
			txPort = 3001
		}
		timeout := 5 * time.Second
		if payload.Scale.ReadTimeoutMs > 0 {
			timeout = time.Duration(payload.Scale.ReadTimeoutMs) * time.Millisecond
		}

		mgr := a.getOrCreateDibalManager(payload.Scale.BindHost, rxPort, txPort, payload.Scale.DibalAddr)
		if !mgr.WaitForRXConnected(timeout) {
			return nil, fmt.Errorf("waga Dibal nie jest połączona na porcie RX %d — sprawdź konfigurację Lantronix (Remote IP = IP tego komputera)", rxPort)
		}

		a.logger.Printf("program_dibal_plu: PLU=%s '%s'", payload.PLU.Code, payload.PLU.Name)

		if err := devices.SendDibalPLUPersistent(mgr, payload.PLU, timeout); err != nil {
			return nil, fmt.Errorf("błąd programowania PLU Dibal: %w", err)
		}

		a.logger.Printf("program_dibal_plu: PLU %s zaprogramowany pomyślnie", payload.PLU.Code)

		return map[string]any{
			"plu_code": payload.PLU.Code,
			"plu_name": payload.PLU.Name,
		}, nil

	case "program_dibal_plu_500":
		// Dibal 500-series (built-in Ethernet, e.g. W-025S): program a PLU by
		// building the 130-byte L2 register here and handing it to the 32-bit
		// dibalcom-bridge, which reuses Dibal's native commL.dll for transport.
		var payload devices.Dibal500ProgramPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		return a.programDibal500(payload)

	case "program_dibal_format":
		// Dibal 500-series label FORMAT (the physical layout — "4R"/"H6"
		// registers, reverse-engineered from Dibal's own DLD tool): programs
		// where each field prints, replacing DLD for this step. See
		// devices.BuildFormatRegisters for the byte-layout rationale.
		var payload devices.Dibal500FormatProgramPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		return a.programDibal500Format(payload)

	case "ping_device":
		var payload devices.PingDevicePayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		if payload.Printer == nil && payload.Scale == nil {
			return nil, fmt.Errorf("ping_device: wymagane pole 'printer' lub 'scale'")
		}

		result := map[string]any{}

		if payload.Printer != nil {
			result["printer"] = devices.PingPrinter(*payload.Printer)
		}

		if payload.Scale != nil {
			result["scale"] = devices.PingScale(*payload.Scale)
		}

		return result, nil

	case "read_printer_settings":
		var payload devices.ReadPrinterSettingsPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}
		return devices.FetchPrinterWebSettings(payload.Printer)

	case "agent_version":
		return map[string]any{"version": version.Version}, nil

	case "list_serial_ports":
		ports, err := devices.ListSerialPorts()
		if err != nil {
			return nil, fmt.Errorf("błąd listy portów szeregowych: %w", err)
		}
		return map[string]any{"ports": ports}, nil

	case "serial_probe":
		var payload devices.SerialProbeConfig
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		response, err := devices.ProbeSerial(payload)
		if err != nil {
			return nil, err
		}
		return map[string]any{"response": response}, nil

	default:
		return nil, fmt.Errorf("nieobsługiwana komenda: %s", command)
	}
}

// dibalBridgeResult mirrors dibalcom-bridge's JSON output.
type dibalBridgeResult struct {
	OK    bool   `json:"ok"`
	Stage string `json:"stage"`
	Error string `json:"error"`

	Registers []struct {
		Index    int    `json:"index"`
		OK       bool   `json:"ok"`
		Result   int    `json:"result"`
		EchoHex  string `json:"echo_hex,omitempty"`
		EchoLen  int    `json:"echo_len,omitempty"`
		EchoCode int    `json:"echo_code,omitempty"`
	} `json:"registers"`
}

// runDibal500Bridge sends pre-encoded hex registers to the scale via the
// 32-bit dibalcom-bridge, serialized against concurrent pushes to the same
// scale. Shared by programDibal500 and the debug-only raw-register command
// (executeDebugCommand's "dibal500_raw"), which bypasses BuildArticleRegisters
// entirely to test hand-crafted byte layouts.
func (a *Agent) runDibal500Bridge(scaleIP string, scalePort int, pcIP string, timeoutMs int, transform, echoTest bool, hexRegisters []string) (dibalBridgeResult, error) {
	var res dibalBridgeResult

	scaleIP = strings.TrimSpace(scaleIP)
	if scaleIP == "" {
		return res, fmt.Errorf("brak scale_ip dla wagi Dibal 500")
	}
	if scalePort <= 0 {
		scalePort = 3000
	}
	timeout := timeoutMs
	if timeout <= 0 {
		timeout = 3000
	}

	pcIP = strings.TrimSpace(pcIP)
	if pcIP == "" {
		pcIP = detectLocalIP(scaleIP)
	}
	if pcIP == "" {
		return res, fmt.Errorf("nie udało się ustalić IP komputera (podaj pc_ip)")
	}

	bridge, err := dibalBridgePath()
	if err != nil {
		return res, err
	}

	// One scale, one TCP endpoint: serialize concurrent batch pushes.
	a.dibal500Mu.Lock()
	defer a.dibal500Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout+5000)*time.Millisecond)
	defer cancel()

	transformArg := "0"
	if transform {
		transformArg = "1"
	}

	echoArg := "0"
	if echoTest {
		echoArg = "1"
	}

	var stdin bytes.Buffer
	for _, hexReg := range hexRegisters {
		stdin.WriteString(hexReg)
		stdin.WriteByte('\n')
	}

	cmd := exec.CommandContext(ctx, bridge,
		scaleIP, strconv.Itoa(scalePort),
		pcIP, strconv.Itoa(scalePort),
		strconv.Itoa(timeout), transformArg, echoArg,
	)
	cmd.Stdin = &stdin
	out, runErr := cmd.Output()
	_ = json.Unmarshal(bytes.TrimSpace(out), &res)

	if !res.OK {
		detail := res.Error
		if detail == "" {
			detail = strings.TrimSpace(string(out))
		}
		if detail == "" && runErr != nil {
			detail = runErr.Error()
		}
		return res, fmt.Errorf("dibalcom-bridge (stage=%s, wysłano %d/%d rejestrów): %s", res.Stage, len(res.Registers), len(hexRegisters), detail)
	}

	return res, nil
}

// dibalBridgeReadFormatResult mirrors dibalcom-bridge's -read-format JSON output.
type dibalBridgeReadFormatResult struct {
	OK           bool     `json:"ok"`
	Stage        string   `json:"stage"`
	Error        string   `json:"error"`
	TerminatedBy string   `json:"terminated_by"`
	Registers    []string `json:"registers"`
}

// runDibal500BridgeReadFormat asks the scale for a label format's 4R/H6
// registers (dibalcom-bridge's "-read-format" mode) and returns the raw
// registers it collected, hex-encoded, for the caller to decode with
// devices.ParseFormatRegisters. Serialized against runDibal500Bridge via the
// same mutex — one TCP conversation with the scale at a time.
//
// The bridge itself bounds its read loop to a fixed 20s wall-clock budget
// regardless of timeoutMs (see runReadFormat in cmd/dibalcom-bridge), so the
// process-level context deadline here only needs to cover that plus a
// margin — it is not the primary safety net.
func (a *Agent) runDibal500BridgeReadFormat(scaleIP string, scalePort int, pcIP string, pcPort int, timeoutMs int, formatNum int) (dibalBridgeReadFormatResult, error) {
	var res dibalBridgeReadFormatResult

	scaleIP = strings.TrimSpace(scaleIP)
	if scaleIP == "" {
		return res, fmt.Errorf("brak scale_ip dla wagi Dibal 500")
	}
	if scalePort <= 0 {
		scalePort = 3000
	}
	timeout := timeoutMs
	if timeout <= 0 {
		timeout = 3000
	}

	pcIP = strings.TrimSpace(pcIP)
	if pcIP == "" {
		pcIP = detectLocalIP(scaleIP)
	}
	if pcIP == "" {
		return res, fmt.Errorf("nie udało się ustalić IP komputera (podaj pc_ip)")
	}
	if pcPort <= 0 {
		pcPort = scalePort
	}

	bridge, err := dibalBridgePath()
	if err != nil {
		return res, err
	}

	a.dibal500Mu.Lock()
	defer a.dibal500Mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bridge,
		"-read-format", strconv.Itoa(formatNum),
		scaleIP, strconv.Itoa(scalePort),
		pcIP, strconv.Itoa(pcPort),
		strconv.Itoa(timeout),
	)
	out, runErr := cmd.Output()
	_ = json.Unmarshal(bytes.TrimSpace(out), &res)

	if !res.OK {
		detail := res.Error
		if detail == "" {
			detail = strings.TrimSpace(string(out))
		}
		if detail == "" && runErr != nil {
			detail = runErr.Error()
		}
		return res, fmt.Errorf("dibalcom-bridge -read-format (stage=%s): %s", res.Stage, detail)
	}

	return res, nil
}

// programDibal500 builds the article registers (L2 core record plus any X4
// composition pages) and hands them to the 32-bit dibalcom-bridge, which
// reuses Dibal's native commL.dll to send them all over one connection.
func (a *Agent) programDibal500(payload devices.Dibal500ProgramPayload) (map[string]any, error) {
	registers, err := devices.BuildArticleRegisters(payload.PLU)
	if err != nil {
		return nil, fmt.Errorf("budowa rejestrów artykułu: %w", err)
	}

	hexRegs := make([]string, len(registers))
	for i, reg := range registers {
		hexRegs[i] = hex.EncodeToString(reg)
	}

	scaleIP := strings.TrimSpace(payload.ScaleIP)
	pcIP := strings.TrimSpace(payload.PCIP)
	if pcIP == "" {
		pcIP = detectLocalIP(scaleIP)
	}

	a.logger.Printf("program_dibal_plu_500: PLU=%s '%s' (%d rejestrów) -> waga %s:%d (PC %s)", payload.PLU.Code, payload.PLU.Name, len(registers), scaleIP, payload.ScalePort, pcIP)

	res, err := a.runDibal500Bridge(payload.ScaleIP, payload.ScalePort, payload.PCIP, payload.TimeoutMs, payload.Transform, payload.EchoTest, hexRegs)
	if err != nil {
		return nil, fmt.Errorf("nie zaprogramowano PLU: %w", err)
	}

	a.logger.Printf("program_dibal_plu_500: PLU %s zaprogramowany pomyślnie (%d rejestrów)", payload.PLU.Code, len(registers))

	result := map[string]any{
		"plu_code":  payload.PLU.Code,
		"plu_name":  payload.PLU.Name,
		"pc_ip":     pcIP,
		"registers": len(registers),
	}
	if payload.EchoTest {
		result["echo"] = res.Registers
	}

	return result, nil
}

// programDibal500Format builds and sends a Dibal 500-series label FORMAT
// (physical layout, "4R"+"H6" registers) — see devices.BuildFormatRegisters.
func (a *Agent) programDibal500Format(payload devices.Dibal500FormatProgramPayload) (map[string]any, error) {
	logicalAddr := strings.TrimSpace(payload.LogicalAddr)
	if logicalAddr == "" {
		logicalAddr = "00"
	}
	group := strings.TrimSpace(payload.Group)
	if group == "" {
		group = "00"
	}

	registers, err := devices.BuildFormatRegisters(logicalAddr, group, payload.FormatNum, payload.Width, payload.Height, payload.Fields)
	if err != nil {
		return nil, fmt.Errorf("budowa rejestrów formatu: %w", err)
	}

	hexRegs := make([]string, len(registers))
	for i, reg := range registers {
		hexRegs[i] = hex.EncodeToString(reg)
	}

	a.logger.Printf("program_dibal_format: format %s (%d pól, %d rejestrów) -> waga %s:%d", payload.FormatNum, len(payload.Fields), len(registers), payload.ScaleIP, payload.ScalePort)

	if _, err := a.runDibal500Bridge(payload.ScaleIP, payload.ScalePort, payload.PCIP, payload.TimeoutMs, payload.Transform, false, hexRegs); err != nil {
		return nil, fmt.Errorf("nie zaprogramowano formatu: %w", err)
	}

	a.logger.Printf("program_dibal_format: format %s zaprogramowany pomyślnie (%d rejestrów)", payload.FormatNum, len(registers))

	return map[string]any{
		"format_num": payload.FormatNum,
		"fields":     len(payload.Fields),
		"registers":  len(registers),
	}, nil
}

// dibalBridgePath returns the path to dibalcom-bridge.exe, expected next to the agent.
func dibalBridgePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("nie można ustalić ścieżki agenta: %w", err)
	}

	bridge := filepath.Join(filepath.Dir(exe), "dibalcom-bridge.exe")
	if _, statErr := os.Stat(bridge); statErr != nil {
		return "", fmt.Errorf("brak dibalcom-bridge.exe obok agenta (%s) — skopiuj most i commL.dll", bridge)
	}

	return bridge, nil
}

// detectLocalIP returns the local LAN IP the OS would use to reach scaleIP.
func detectLocalIP(scaleIP string) string {
	conn, err := net.Dial("udp", net.JoinHostPort(scaleIP, "9"))
	if err != nil {
		return ""
	}
	defer func() {
		_ = conn.Close()
	}()

	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}

	return ""
}

func (a *Agent) readWeightWithIntermecFallback(scale devices.ScaleConfig, printer devices.PrinterConfig) (float64, string, error) {
	transport := strings.ToLower(strings.TrimSpace(scale.Transport))
	if transport == "tcp_server" || transport == "server_tcp" || transport == "dibal_tcp_server" || transport == "dibal_server" {
		bindHost := strings.TrimSpace(scale.BindHost)
		if bindHost == "" {
			bindHost = "0.0.0.0"
		}

		txPort := scale.TXPort
		if txPort <= 0 {
			if scale.TCPPort > 0 {
				txPort = scale.TCPPort
			} else {
				txPort = 3001
			}
		}

		rxPort := scale.RXPort
		if rxPort <= 0 {
			rxPort = 3000
		}

		a.logger.Printf("Tryb Dibal TCP server: nasłuch TX=%s:%d RX=%s:%d request=%t", bindHost, txPort, bindHost, rxPort, strings.TrimSpace(scale.RequestCommand) != "")

		mgr := a.getOrCreateDibalManager(bindHost, rxPort, txPort, scale.DibalAddr)
		timeout := 5 * time.Second
		if scale.ReadTimeoutMs > 0 {
			timeout = time.Duration(scale.ReadTimeoutMs) * time.Millisecond
		}
		if !mgr.WaitForTXConnected(timeout) {
			return 0, "", fmt.Errorf("waga Dibal nie jest połączona na porcie TX %d", txPort)
		}

		weight, response, err := devices.ReadWeightPersistent(mgr, scale)
		if err == nil {
			a.logger.Printf("Dibal TCP server: odebrano odczyt wagi: %s", response)
			return weight, response, nil
		}

		a.logger.Printf("Dibal TCP server: błąd odczytu: %v", err)
		return 0, "", err
	}

	weight, response, err := devices.ReadWeight(scale)
	if err == nil {
		if transport == "tcp_server" || transport == "server_tcp" || transport == "dibal_tcp_server" || transport == "dibal_server" {
			a.logger.Printf("Dibal TCP server: odebrano odczyt wagi: %s", response)
		}

		return weight, response, nil
	}

	if transport == "tcp_server" || transport == "server_tcp" || transport == "dibal_tcp_server" || transport == "dibal_server" {
		a.logger.Printf("Dibal TCP server: błąd odczytu: %v", err)
	}

	if !shouldTryIntermecBridge(scale, printer) {
		return 0, "", err
	}

	fallbackScale := scale
	fallbackScale.Transport = "tcp"
	if strings.TrimSpace(fallbackScale.TCPHost) == "" {
		fallbackScale.TCPHost = strings.TrimSpace(printer.Host)
	}
	if fallbackScale.TCPPort <= 0 {
		if printer.Port > 0 {
			fallbackScale.TCPPort = printer.Port
		} else {
			fallbackScale.TCPPort = 9100
		}
	}

	fallbackWeight, fallbackResponse, fallbackErr := devices.ReadWeight(fallbackScale)
	if fallbackErr != nil {
		return 0, "", fmt.Errorf("%w; fallback przez Intermec PM43 (%s:%d) nie powiódł się: %v", err, fallbackScale.TCPHost, fallbackScale.TCPPort, fallbackErr)
	}

	a.logger.Printf("Odczyt wagi przez fallback Intermec PM43 (%s:%d)", fallbackScale.TCPHost, fallbackScale.TCPPort)

	return fallbackWeight, fallbackResponse, nil
}

func shouldTryIntermecBridge(scale devices.ScaleConfig, printer devices.PrinterConfig) bool {
	transport := strings.ToLower(strings.TrimSpace(scale.Transport))
	if transport != "serial" && transport != "rs232" && transport != "com" {
		return false
	}

	model := strings.ToLower(strings.TrimSpace(printer.Model))
	if model != "" && !strings.Contains(model, "intermec") && !strings.Contains(model, "pm43") {
		return false
	}

	printerTransport := strings.ToLower(strings.TrimSpace(printer.Transport))
	if printerTransport != "" && printerTransport != "raw_tcp" && printerTransport != "tcp" && printerTransport != "network" && printerTransport != "jetdirect" {
		return false
	}

	return strings.TrimSpace(printer.Host) != ""
}
