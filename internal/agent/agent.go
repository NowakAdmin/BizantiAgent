package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/NowakAdmin/BizantiAgent/internal/config"
	"github.com/NowakAdmin/BizantiAgent/internal/devices"
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
}

func (a *Agent) IsRunning() bool {
	return a.running.Load()
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

			for _, message := range commands {
				commandName := strings.ToLower(strings.TrimSpace(message.Command))
				result, execErr := a.executeCommand(commandName, message.Payload)
				if reportErr := a.reportCommandResult(ctx, message.JobID, result, execErr); reportErr != nil {
					a.logger.Printf("Błąd raportowania wyniku job %s: %v", message.JobID, reportErr)
				}
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
			_ = conn.WriteJSON(OutgoingMessage{Type: "status", Status: "offline"})
			return context.Canceled
		case err = <-readErrors:
			return err
		case message := <-readMessages:
			a.handleIncoming(conn, message)
		case <-heartbeatTicker.C:
			_ = conn.WriteJSON(OutgoingMessage{
				Type:      "heartbeat",
				AgentID:   a.getServerAgentID(),
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Status:    "online",
			})
		}
	}
}

func (a *Agent) handleIncoming(conn *websocket.Conn, message IncomingMessage) {
	messageType := strings.ToLower(strings.TrimSpace(message.Type))
	commandName := strings.ToLower(strings.TrimSpace(message.Command))

	switch {
	case messageType == "ping" || commandName == "ping":
		_ = conn.WriteJSON(OutgoingMessage{
			Type:      "pong",
			AgentID:   a.getServerAgentID(),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			JobID:     message.JobID,
		})
		return

	case messageType == "command":
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

		_ = conn.WriteJSON(out)
		return
	}
}

func (a *Agent) executeCommand(command string, rawPayload json.RawMessage) (map[string]any, error) {
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

	case "program_dibal_plu":
		// Programs a PLU record directly into a Dibal K-series scale via TCP.
		// Does NOT require Windows Spooler or any Windows scale driver.
		// The scale's Lantronix adapter must be configured to connect to this PC.
		var payload devices.DibalProgramPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return nil, err
		}

		bindHost := strings.TrimSpace(payload.Scale.BindHost)
		if bindHost == "" {
			bindHost = "0.0.0.0"
		}
		rxPort := payload.Scale.RXPort
		if rxPort <= 0 {
			rxPort = 3000
		}
		timeout := 5 * time.Second
		if payload.Scale.ReadTimeoutMs > 0 {
			timeout = time.Duration(payload.Scale.ReadTimeoutMs) * time.Millisecond
		}

		rxAddr := net.JoinHostPort(bindHost, strconv.Itoa(rxPort))
		listener, err := net.Listen("tcp", rxAddr)
		if err != nil {
			return nil, fmt.Errorf("nie można uruchomić nasłuchu Dibal RX na %s: %w", rxAddr, err)
		}
		defer func() {
			_ = listener.Close()
		}()
		if tcpL, ok := listener.(*net.TCPListener); ok {
			_ = tcpL.SetDeadline(time.Now().Add(timeout))
		}

		a.logger.Printf("program_dibal_plu: nasłuch na %s, PLU=%s '%s'", rxAddr, payload.PLU.Code, payload.PLU.Name)

		conn, err := listener.Accept()
		if err != nil {
			return nil, fmt.Errorf("brak połączenia od wagi Dibal (RX %s): %w", rxAddr, err)
		}
		defer func() {
			_ = conn.Close()
		}()

		addr := devices.DibalDefaultAddr
		if payload.Scale.DibalAddr != 0 {
			addr = payload.Scale.DibalAddr
		}

		if err = devices.SendDibalPLU(conn, addr, payload.PLU, timeout); err != nil {
			return nil, fmt.Errorf("błąd programowania PLU Dibal: %w", err)
		}

		a.logger.Printf("program_dibal_plu: PLU %s zaprogramowany pomyślnie", payload.PLU.Code)

		return map[string]any{
			"plu_code": payload.PLU.Code,
			"plu_name": payload.PLU.Name,
		}, nil

	default:
		return nil, fmt.Errorf("nieobsługiwana komenda: %s", command)
	}
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
