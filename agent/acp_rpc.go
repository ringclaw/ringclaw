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
	ID      *rpcID          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcID struct {
	Raw json.RawMessage
	Int *int64
}

func (id *rpcID) UnmarshalJSON(data []byte) error {
	id.Raw = append(id.Raw[:0], data...)

	var n int64
	if err := json.Unmarshal(data, &n); err == nil {
		id.Int = &n
		return nil
	}

	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	var parsed int64
	if err := json.Unmarshal([]byte(s), &parsed); err == nil {
		id.Int = &parsed
	}
	return nil
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
			if msg.ID.Int != nil {
				a.pendingMu.Lock()
				ch, ok := a.pending[*msg.ID.Int]
				a.pendingMu.Unlock()
				if ok {
					ch <- &msg
				}
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
	// permissionRequestParamsWithSession is a local extension that
	// also captures the sessionId so the v0.4.3 Layer-B gate can
	// deny tool approvals that originate from non-owner sessions.
	type permissionRequestParamsWithSession struct {
		SessionID string             `json:"sessionId"`
		ToolCall  json.RawMessage    `json:"toolCall"`
		Options   []permissionOption `json:"options"`
	}
	var req struct {
		ID     json.RawMessage                    `json:"id"`
		Params permissionRequestParamsWithSession `json:"params"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		slog.Error("failed to parse permission request", "component", "acp", "error", err)
		return
	}

	type permissionOutcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId"`
	}
	type permissionResult struct {
		Outcome permissionOutcome `json:"outcome"`
	}

	// Layer-B gate: a non-owner session must never have a permission
	// auto-approved on its behalf, even when the agent itself has
	// already decided to ask. We pick the first deny-kind option (or
	// synthesize a "deny" outcome) so the agent learns the tool call
	// was rejected.
	if v, ok := a.sessionRoles.Load(req.Params.SessionID); ok {
		if origin, _ := v.(Origin); !origin.IsOwner {
			denyOption := ""
			for _, opt := range req.Params.Options {
				if opt.Kind == "deny" || opt.Kind == "reject" {
					denyOption = opt.OptionID
					break
				}
			}
			if denyOption != "" {
				a.sendResponse(req.ID, permissionResult{
					Outcome: permissionOutcome{
						Outcome:  "selected",
						OptionID: denyOption,
					},
				})
			} else {
				// No explicit deny option offered — return a
				// cancelled outcome so the agent treats the
				// request as not-granted.
				a.sendResponse(req.ID, permissionResult{
					Outcome: permissionOutcome{Outcome: "cancelled"},
				})
			}
			a.warnDeniedOnce(req.Params.SessionID, "session/request_permission",
				"reason", "non_owner",
				"deny_option", denyOption,
				"sender_id", origin.SenderID,
				"sender_reason", origin.Reason,
			)
			return
		}
	} else {
		// Unknown session → fail-closed deny, mirroring the
		// gateNonOwnerToolCall fallback.
		a.sendResponse(req.ID, permissionResult{
			Outcome: permissionOutcome{Outcome: "cancelled"},
		})
		a.warnDeniedOnce(req.Params.SessionID, "session/request_permission",
			"reason", "unknown_session",
		)
		return
	}

	var optionID string
	for _, opt := range req.Params.Options {
		if optionID == "" {
			optionID = opt.OptionID
		}
		if opt.Kind == "allow" {
			optionID = opt.OptionID
			break
		}
	}
	if optionID == "" {
		optionID = "allow"
	}

	a.sendResponse(req.ID, permissionResult{
		Outcome: permissionOutcome{
			Outcome:  "selected",
			OptionID: optionID,
		},
	})

	optionNames := make([]string, 0, len(req.Params.Options))
	for _, opt := range req.Params.Options {
		optionNames = append(optionNames, fmt.Sprintf("%s(kind=%s)", opt.OptionID, opt.Kind))
	}
	slog.Debug("auto-allowed permission request", "component", "acp",
		"optionId", optionID, "available", strings.Join(optionNames, ","))
}
