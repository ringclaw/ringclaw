package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ringclaw/ringclaw/agent"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// handleCwd handles the /cwd command. Updates the working directory for all running agents.
//
// Ported from github.com/fastclaw-ai/weclaw commits 2b24d5d + 6df63a9.
func (h *Handler) handleCwd(text string) string {
	arg := strings.TrimSpace(strings.TrimPrefix(text, "/cwd"))
	if arg == "" {
		ag := h.getDefaultAgent()
		if ag == nil {
			return "No agent running."
		}
		info := ag.Info()
		return fmt.Sprintf("cwd: (check agent config)\nagent: %s", info.Name)
	}

	// Expand ~ to home directory (fix: use arg[2:] not arg[1:] for ~/path)
	if arg == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			arg = home
		}
	} else if strings.HasPrefix(arg, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			arg = filepath.Join(home, arg[2:])
		}
	}

	absPath, err := filepath.Abs(arg)
	if err != nil {
		return fmt.Sprintf("Invalid path: %v", err)
	}

	// Allowlist check: must be inside the configured AgentWorkspace.
	// Returns nil when no workspace root is configured (legacy mode).
	if err := agent.EnsurePathInWorkspace(absPath); err != nil {
		slog.Warn("cwd rejected: outside workspace root", "component", "handler", "path", absPath, "error", err)
		return fmt.Sprintf("Denied: %v", err)
	}
	// Denylist check (defense in depth): block sensitive dotfile dirs even
	// if a misconfigured AgentWorkspace would otherwise admit them.
	if err := validateCwdPath(absPath); err != nil {
		return fmt.Sprintf("Denied: %v", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Path not found: %s", absPath)
	}
	if !info.IsDir() {
		return fmt.Sprintf("Not a directory: %s", absPath)
	}

	h.mu.RLock()
	agents := make(map[string]agent.Agent, len(h.agents))
	for name, ag := range h.agents {
		agents[name] = ag
	}
	h.mu.RUnlock()

	for name, ag := range agents {
		ag.SetCwd(absPath)
		slog.Info("updated cwd for agent", "component", "handler", "agent", name, "cwd", absPath)
	}

	return fmt.Sprintf("cwd: %s", absPath)
}

// restrictedDirs are directories that /cwd must never enter.
var restrictedDirs = []string{".ssh", ".gnupg", ".ringclaw", ".aws", ".kube", ".config/gcloud"}

// validateCwdPath rejects paths containing sensitive directories.
func validateCwdPath(absPath string) error {
	// Normalize to forward slashes for consistent matching on Windows
	norm := filepath.ToSlash(absPath)
	for _, dir := range restrictedDirs {
		// Check for /.<dir> or /.<dir>/ anywhere in the path
		pattern := "/" + dir
		if strings.Contains(norm, pattern+"/") || strings.HasSuffix(norm, pattern) {
			return fmt.Errorf("path contains restricted directory: %s", dir)
		}
	}
	return nil
}

func buildHelpText() string {
	return `Available commands:
/agent - Switch default agent
/agent msg - Send to a specific agent
/a /b msg - Broadcast to multiple agents
/new or /clear - Start a new session
/cwd /path - Switch workspace directory
/info - Show current agent info
/help - Show this help message
/full-access status|grant [dur]|revoke - Owner DM only; /approval-gated ACP unlock (default 1d, max 30d)

/task list|create|get|update|delete|complete
/note list|create|get|update|delete|lock|unlock
/event list|create|get|update|delete
/card get|delete
/chatinfo [chatId] - Show chat details
/cron add|list|delete - Scheduled tasks
/reload - Re-detect installed agents

Aliases: /cc(claude) /cx(codex) /cs(cursor) /km(kimi) /gm(gemini) /oc(openclaw) /ocd(opencode) /pi(pi) /cp(copilot) /dr(droid) /if(iflow) /kr(kiro) /qw(qwen) /ag(augment)`
}

func buildHelpCard() json.RawMessage {
	type fact struct {
		Title string `json:"title"`
		Value string `json:"value"`
	}

	mono := func(text string) map[string]any {
		return map[string]any{"type": "TextBlock", "text": text, "fontType": "monospace", "size": "small", "wrap": true}
	}

	generalCmds := []fact{
		{"/agent <msg>", "Send to a specific agent"},
		{"/a /b <msg>", "Broadcast to multiple agents"},
		{"/new", "Start a new session"},
		{"/cwd /path", "Switch workspace directory"},
		{"/info", "Show current agent info"},
		{"/reload", "Re-detect installed agents"},
		{"/full-access", "status | grant [dur] | revoke (owner DM, /approval gated)"},
		{"/help", "Show this help"},
	}

	generalExamples := map[string]any{
		"type":  "Action.ShowCard",
		"title": "Show Examples",
		"card": map[string]any{
			"type": "AdaptiveCard",
			"body": []any{
				mono("/cc help me write a sort function"),
				mono("/cc /cx review this code"),
				mono("/claude  → switch default to claude"),
				mono("/new     → start fresh session"),
				mono("/cwd /home/user/my-project"),
			},
		},
	}

	rcCmds := []fact{
		{"/task", "list | create | get | update | delete | complete"},
		{"/note", "list | create | get | update | delete | lock | unlock"},
		{"/event", "list | create | get | update | delete"},
		{"/card", "get | delete"},
		{"/chatinfo", "Show chat details"},
		{"/cron", "add | list | delete"},
	}

	rcExamples := map[string]any{
		"type":  "Action.ShowCard",
		"title": "Show Examples",
		"card": map[string]any{
			"type": "AdaptiveCard",
			"body": []any{
				mono("/task list"),
				mono("/task create \"Review PR\" assignee=John"),
				mono("/note create \"Meeting Notes\""),
				mono("/event create \"Standup\" 2026-04-10T09:00:00Z 2026-04-10T09:30:00Z"),
				mono("/cron add \"0 9 * * 1-5\" morning standup summary"),
				mono("/chatinfo 158994374662"),
			},
		},
	}

	aliases := "/cc(claude) /cx(codex) /cs(cursor) /km(kimi) /gm(gemini) /oc(openclaw) /ocd(opencode) /pi(pi) /cp(copilot) /dr(droid) /if(iflow) /kr(kiro) /qw(qwen) /ag(augment)"

	body := []any{
		map[string]any{"type": "TextBlock", "text": "RingClaw Commands", "weight": "bolder", "size": "medium"},

		map[string]any{"type": "TextBlock", "text": "General", "weight": "bolder", "spacing": "medium", "separator": true},
		map[string]any{"type": "FactSet", "facts": generalCmds},
		map[string]any{"type": "ActionSet", "actions": []any{generalExamples}},

		map[string]any{"type": "TextBlock", "text": "RingCentral Resources", "weight": "bolder", "spacing": "medium", "separator": true},
		map[string]any{"type": "FactSet", "facts": rcCmds},
		map[string]any{"type": "ActionSet", "actions": []any{rcExamples}},

		map[string]any{"type": "TextBlock", "text": "Agent Aliases", "weight": "bolder", "spacing": "medium", "separator": true},
		map[string]any{"type": "TextBlock", "text": aliases, "wrap": true, "size": "small"},
	}

	card := map[string]any{
		"type":    "AdaptiveCard",
		"version": "1.3",
		"body":    body,
	}

	data, _ := json.Marshal(card)
	return data
}

// handleChatInfo returns information about a chat.
func handleChatInfo(ctx context.Context, client *ringcentral.Client, currentChatID, text string) string {
	arg := strings.TrimSpace(strings.TrimPrefix(text, "/chatinfo"))
	targetID := currentChatID
	if arg != "" {
		targetID = arg
	}

	chat, err := client.GetChat(ctx, targetID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Chat** `%s`\n", chat.ID))
	sb.WriteString(fmt.Sprintf("- Name: %s\n", chat.Name))
	sb.WriteString(fmt.Sprintf("- Type: %s\n", chat.Type))
	if chat.Description != "" {
		sb.WriteString(fmt.Sprintf("- Description: %s\n", chat.Description))
	}
	sb.WriteString(fmt.Sprintf("- Members: %d\n", len(chat.Members)))
	if chat.Status != "" {
		sb.WriteString(fmt.Sprintf("- Status: %s\n", chat.Status))
	}
	sb.WriteString(fmt.Sprintf("- Created: %s\n", chat.CreationTime))
	return sb.String()
}

func wrapAnswer(text string) string {
	return "--------answer--------\n" + text + "\n---------end----------"
}

// isPrivilegedCommand returns true for commands that should be restricted
// to the bot owner in group chats (agent switch, session reset, cwd, summarize).
func isPrivilegedCommand(text string) bool {
	if IsSummarizeCommand(text) {
		return true
	}
	if text == "/new" || text == "/clear" {
		return true
	}
	if strings.HasPrefix(text, "/cwd") {
		return true
	}
	if strings.HasPrefix(text, "/cron") {
		return true
	}
	if text == "/reload" {
		return true
	}
	// /full-access bypasses all ACP guardrails; restrict it to the bot
	// owner even before the handler's stricter "DM-only" check kicks in.
	if IsFullAccessCommand(text) {
		return true
	}
	return false
}
