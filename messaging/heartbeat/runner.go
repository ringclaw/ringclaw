package heartbeat

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/config"
	"github.com/ringclaw/ringclaw/messaging"
)

const (
	heartbeatOKToken     = "HEARTBEAT_OK"
	heartbeatDedupWindow = 24 * time.Hour
	defaultHeartbeatFile = "HEARTBEAT.md"
)

// SendFunc sends a text message to a chat. Implemented by messaging.SendTextReply.
type SendFunc func(ctx context.Context, chatID, text string) error

// PromptFunc returns the heartbeat prompt template. Implemented by messaging.HeartbeatPrompt.
type PromptFunc func() string

// ExecuteFunc executes a list of agent actions for a given chat. It is called
// after filterHeartbeatActions has stripped disallowed action types.
type ExecuteFunc func(ctx context.Context, chatID string, actions []messaging.AgentAction)

// heartbeatAllowedActions is the set of action types the heartbeat runner is
// allowed to fire. SMS and PHONE_CALLLOG are excluded to prevent unsolicited
// outbound communication from the periodic check.
var heartbeatAllowedActions = map[string]bool{
	"MESSAGE": true,
	"NOTE":    true,
	"CARD":    true,
	"TASK":    true,
}

// HeartbeatRunner periodically reads HEARTBEAT.md and sends it to the default agent.
type HeartbeatRunner struct {
	cfg            config.HeartbeatConfig
	send           SendFunc
	chatID         string
	getAgent       func() agent.Agent
	prompt         PromptFunc
	interval       time.Duration
	location       *time.Location
	activeStart    int // minutes from midnight
	activeEnd      int // minutes from midnight
	mu             sync.Mutex
	recentHash     map[string]time.Time // hash -> last seen
	executeActions ExecuteFunc          // optional; nil = no action execution
}

// NewHeartbeatRunner creates a heartbeat runner. executeActions is optional
// (pass nil to disable ACTION execution from heartbeat replies).
func NewHeartbeatRunner(cfg config.HeartbeatConfig, send SendFunc, chatID string, getAgent func() agent.Agent, prompt PromptFunc, executeActions ...ExecuteFunc) (*HeartbeatRunner, error) {
	interval := 30 * time.Minute
	if cfg.Interval != "" {
		d, err := time.ParseDuration(cfg.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid heartbeat interval %q: %w", cfg.Interval, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("heartbeat interval must be positive, got %v", d)
		}
		interval = d
	}

	loc := time.Local
	if cfg.Timezone != "" {
		l, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
		}
		loc = l
	}

	r := &HeartbeatRunner{
		cfg:        cfg,
		send:       send,
		chatID:     chatID,
		getAgent:   getAgent,
		prompt:     prompt,
		interval:   interval,
		location:   loc,
		recentHash: make(map[string]time.Time),
	}
	if len(executeActions) > 0 {
		r.executeActions = executeActions[0]
	}

	if cfg.ActiveHours != "" {
		start, end, err := parseActiveHours(cfg.ActiveHours)
		if err != nil {
			return nil, err
		}
		r.activeStart = start
		r.activeEnd = end
	}

	return r, nil
}

// Start runs the heartbeat loop until context is cancelled.
func (r *HeartbeatRunner) Start(ctx context.Context) {
	slog.Info("heartbeat runner started", "interval", r.interval, "activeHours", r.cfg.ActiveHours)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("heartbeat runner stopped")
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *HeartbeatRunner) tick(ctx context.Context) {
	if !r.isActiveTime() {
		slog.Debug("heartbeat skipped: outside active hours", "component", "heartbeat")
		return
	}

	content, err := r.readHeartbeatFile()
	if err != nil {
		slog.Debug("heartbeat skipped: no HEARTBEAT.md", "component", "heartbeat")
		return
	}
	if isEffectivelyEmpty(content) {
		slog.Debug("heartbeat skipped: HEARTBEAT.md is empty", "component", "heartbeat")
		return
	}

	ag := r.getAgent()
	if ag == nil {
		slog.Debug("heartbeat skipped: no agent available", "component", "heartbeat")
		return
	}

	p := r.prompt()
	prompt := fmt.Sprintf(p, heartbeatOKToken, content)
	slog.Info("running heartbeat", "component", "heartbeat")

	reply, err := ag.Chat(ctx, "heartbeat", prompt)
	if err != nil {
		slog.Error("heartbeat agent error", "component", "heartbeat", "error", err)
		return
	}

	reply = strings.TrimSpace(reply)
	clean, actions := messaging.ParseAgentActions(reply)

	// Execute allowed actions (heartbeat allowlist: MESSAGE, NOTE, CARD, TASK)
	if r.executeActions != nil && len(actions) > 0 {
		allowed := filterHeartbeatActions(actions)
		if len(allowed) > 0 {
			r.executeActions(ctx, r.chatID, allowed)
		}
	}

	if clean == "" || strings.EqualFold(clean, heartbeatOKToken) || strings.HasPrefix(strings.TrimSpace(strings.ToUpper(clean)), heartbeatOKToken) {
		slog.Info("heartbeat: all clear", "component", "heartbeat")
		return
	}

	if r.isDuplicate(clean) {
		slog.Info("heartbeat: duplicate reply suppressed", "component", "heartbeat")
		return
	}

	if err := r.send(ctx, r.chatID, "**[Heartbeat]** "+clean); err != nil {
		slog.Error("heartbeat: failed to send reply", "component", "heartbeat", "error", err)
	}
}

func (r *HeartbeatRunner) isActiveTime() bool {
	if r.cfg.ActiveHours == "" {
		return true
	}
	now := time.Now().In(r.location)
	mins := now.Hour()*60 + now.Minute()
	if r.activeStart <= r.activeEnd {
		return mins >= r.activeStart && mins < r.activeEnd
	}
	// Wraps midnight (e.g. 22:00-06:00)
	return mins >= r.activeStart || mins < r.activeEnd
}

func (r *HeartbeatRunner) readHeartbeatFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".ringclaw", defaultHeartbeatFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *HeartbeatRunner) isDuplicate(reply string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	h := fmt.Sprintf("%x", sha256.Sum256([]byte(reply)))
	if t, ok := r.recentHash[h]; ok && time.Since(t) < heartbeatDedupWindow {
		return true
	}
	r.recentHash[h] = time.Now()

	// Clean old entries
	cutoff := time.Now().Add(-heartbeatDedupWindow)
	for k, t := range r.recentHash {
		if t.Before(cutoff) {
			delete(r.recentHash, k)
		}
	}
	return false
}

// filterHeartbeatActions returns only the actions whose types are in the
// heartbeat allowlist (MESSAGE, NOTE, CARD, TASK). SMS, PHONE_CALLLOG and
// any other types are stripped to prevent unsolicited outbound communication
// from the periodic health-check.
func filterHeartbeatActions(actions []messaging.AgentAction) []messaging.AgentAction {
	var allowed []messaging.AgentAction
	for _, a := range actions {
		if heartbeatAllowedActions[a.Type] {
			allowed = append(allowed, a)
		} else {
			slog.Info("heartbeat: action type not allowed, stripping", "component", "heartbeat", "type", a.Type)
		}
	}
	return allowed
}

func isEffectivelyEmpty(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		return false
	}
	return true
}

func parseActiveHours(s string) (start, end int, err error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid active_hours format %q, expected HH:MM-HH:MM", s)
	}
	start, err = parseTimeOfDay(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid active_hours start: %w", err)
	}
	end, err = parseTimeOfDay(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid active_hours end: %w", err)
	}
	return start, end, nil
}

func parseTimeOfDay(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, fmt.Errorf("invalid time %q, expected HH:MM", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("time %q out of range", s)
	}
	return h*60 + m, nil
}
