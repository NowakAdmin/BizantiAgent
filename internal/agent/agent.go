package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"io"
	"net/http"
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
}

func (a *Agent) IsRunning() bool {
	return a.running.Load()
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

		var err error
		websocketURL := strings.TrimSpace(a.cfg.WebSocketURL)

		if websocketURL != "" {
			err = a.runSession(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				a.logger.Printf("Sesja WebSocket zakończona: %v", err)
			}

			if ctx.Err() != nil {
				return
			}

			a.logger.Printf("Przechodzę na fallback HTTP polling.")
			_ = a.runHTTPPolling(ctx, 45*time.Second)
		} else {
			err = a.runHTTPPolling(ctx, 0)
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
				a.logger.Printf("HTTP heartbeat error: %v", err)
			}
		case <-pollTicker.C:
			commands, err := a.pullCommands(ctx)
			if err != nil {
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
		return fmt.Errorf("heartbeat status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

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
		return nil, fmt.Errorf("pull commands status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed pullCommandsResponse
	if err = json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return nil, err
	}

	if !parsed.Success {
		return nil, fmt.Errorf("pull commands returned success=false")
	}

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
	request.Header.Set("X-Agent-ID", a.cfg.AgentID)
	request.Header.Set("X-Agent-Name", a.cfg.DeviceName)

	return request, nil
}

func (a *Agent) runSession(ctx context.Context) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+a.cfg.AgentToken)
	headers.Set("X-Agent-ID", a.cfg.AgentID)
	headers.Set("X-Agent-Name", a.cfg.DeviceName)
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
		_ = conn.Close()
	}()

	a.logger.Printf("Połączono z Bizanti WebSocket: %s", a.cfg.WebSocketURL)

	if err = conn.WriteJSON(OutgoingMessage{
		Type:      "auth",
		AgentID:   a.cfg.AgentID,
		Status:    "online",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]any{
			"device_name": a.cfg.DeviceName,
		},
	}); err != nil {
		return err
	}

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
				AgentID:   a.cfg.AgentID,
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
			AgentID:   a.cfg.AgentID,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			JobID:     message.JobID,
		})
		return

	case messageType == "command":
		result, err := a.executeCommand(commandName, message.Payload)
		out := OutgoingMessage{
			Type:      "command_result",
			AgentID:   a.cfg.AgentID,
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
			value, response, err := devices.ReadWeight(payload.Scale)
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

		weight, response, err := devices.ReadWeight(payload.Scale)
		if err != nil {
			return nil, err
		}

		return map[string]any{
			"weight":       weight,
			"raw_response": response,
		}, nil
	default:
		return nil, fmt.Errorf("nieobsługiwana komenda: %s", command)
	}
}
