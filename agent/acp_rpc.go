package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

// --- JSON-RPC types ---

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// call sends a JSON-RPC request and waits for the response.
func (a *ACPAgent) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := a.nextID.Add(1)

	ch := make(chan *rpcResponse, 1)
	a.pendingMu.Lock()
	a.pending[id] = ch
	a.pendingMu.Unlock()

	defer func() {
		a.pendingMu.Lock()
		delete(a.pending, id)
		a.pendingMu.Unlock()
	}()

	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	a.mu.Lock()
	_, err = fmt.Fprintf(a.stdin, "%s\n", data)
	a.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("agent process exited unexpectedly")
		}
		if resp.Error != nil {
			msg := resp.Error.Message
			if a.stderr != nil && (msg == "" || msg == "Internal error" || msg == "internal error") {
				if detail := a.stderr.LastError(); detail != "" {
					msg = detail
				}
			}
			return nil, fmt.Errorf("agent error (code %d): %s", resp.Error.Code, msg)
		}
		return resp.Result, nil
	}
}

// notify sends a JSON-RPC notification (no id, no response expected).
func (a *ACPAgent) notify(method string, params interface{}) {
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal notification", "component", "acp", "method", method, "error", err)
		return
	}
	a.mu.Lock()
	fmt.Fprintf(a.stdin, "%s\n", data)
	a.mu.Unlock()
}

// readLoop reads NDJSON lines from stdout and dispatches to pending requests or notification channels.
func (a *ACPAgent) readLoop() {
	for a.scanner.Scan() {
		line := a.scanner.Text()
		if line == "" {
			continue
		}

		var msg rpcResponse
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			slog.Error("failed to parse message", "component", "acp", "error", err)
			continue
		}

		// Response to a request we made (has id, no method)
		if msg.ID != nil && msg.Method == "" {
			a.pendingMu.Lock()
			ch, ok := a.pending[*msg.ID]
			a.pendingMu.Unlock()
			if ok {
				ch <- &msg
			}
			continue
		}

		// Request from agent or notification
		switch msg.Method {
		case "session/update":
			a.handleSessionUpdate(msg.Params)

		case "session/request_permission":
			a.handlePermissionRequest(line)

		case "terminal/create":
			a.handleTerminalCreate(line)
		case "terminal/output":
			a.handleTerminalOutput(line)
		case "terminal/wait_for_exit":
			a.handleTerminalWaitForExit(line)
		case "terminal/kill":
			a.handleTerminalKill(line)
		case "terminal/release":
			a.handleTerminalRelease(line)

		case "fs/read_text_file":
			a.handleFSReadTextFile(line)
		case "fs/write_text_file":
			a.handleFSWriteTextFile(line)

		default:
			if msg.Method != "" {
				if _, loaded := a.loggedMethods.LoadOrStore(msg.Method, true); !loaded {
					raw := line
					if len(raw) > 200 {
						raw = raw[:200]
					}
					slog.Debug("unhandled method", "component", "acp", "method", msg.Method, "raw", raw)
				}
			}
		}
	}

	if err := a.scanner.Err(); err != nil {
		slog.Error("read loop error", "component", "acp", "error", err)
	}
	slog.Info("read loop ended", "component", "acp")

	// Close all pending request channels
	a.pendingMu.Lock()
	for id, ch := range a.pending {
		close(ch)
		delete(a.pending, id)
	}
	a.pendingMu.Unlock()

	// Mark as not started so next Chat() call triggers auto-restart
	a.mu.Lock()
	a.started = false
	a.mu.Unlock()
}

func (a *ACPAgent) handleSessionUpdate(params json.RawMessage) {
	var p sessionUpdateParams
	if err := json.Unmarshal(params, &p); err != nil {
		slog.Error("failed to parse session/update", "component", "acp", "error", err, "raw", string(params))
		return
	}

	switch p.Update.SessionUpdate {
	case "tool_call":
		tool, args := extractToolAndArgs(p.Update.RawInput)
		if tool == "" {
			tool = strings.TrimPrefix(p.Update.Title, "Tool: ")
		}
		slog.Info("tool_call", "component", "acp",
			"tool", tool, "status", p.Update.Status, "args", args)
	case "tool_call_update":
		slog.Info("tool_call_update", "component", "acp",
			"status", p.Update.Status,
			"output", extractToolOutput(p.Update.RawOutput, 200))
	case "agent_message_chunk", "agent_thought_chunk":
		// Suppress noisy streaming chunks
	default:
		slog.Debug("session/update", "component", "acp", "session", p.SessionID,
			"type", p.Update.SessionUpdate)
	}

	a.notifyMu.Lock()
	ch, ok := a.notifyCh[p.SessionID]
	a.notifyMu.Unlock()

	if ok {
		select {
		case ch <- &p.Update:
		default:
			dropped := a.droppedUpdates.Add(1)
			if dropped == 1 || dropped%100 == 0 {
				slog.Warn("notification channel full", "component", "acp", "dropped", dropped, "session", p.SessionID)
			}
		}
	}
}

func (a *ACPAgent) handlePermissionRequest(raw string) {
	var req struct {
		ID     json.RawMessage         `json:"id"`
		Params permissionRequestParams `json:"params"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		slog.Error("failed to parse permission request", "component", "acp", "error", err)
		return
	}

	optionID := "allow"
	for _, opt := range req.Params.Options {
		if opt.Kind == "allow" {
			optionID = opt.OptionID
			break
		}
	}

	type permissionOutcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId"`
	}
	type permissionResult struct {
		Outcome permissionOutcome `json:"outcome"`
	}

	a.sendResponse(req.ID, permissionResult{
		Outcome: permissionOutcome{
			Outcome:  "selected",
			OptionID: optionID,
		},
	})

	slog.Debug("auto-allowed permission request", "component", "acp",
		"optionId", optionID)
}
