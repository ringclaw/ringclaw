package messaging

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ringclaw/ringclaw/messaging/persona"
)

// Persona / Memory slash commands.
//
// These live in their own file so the growing set of management
// commands (/remember, /recall, /forget, /persona) stays out of
// handler.go. Each handler returns a plain-text reply string; the
// caller (HandleMessage) sends it via SendTextReply.
//
// Routing is done in HandleMessage: /remember and /forget are
// privileged (see isPrivilegedCommand); /recall and /persona are
// read-only and available to any trusted sender.

// IsPersonaCommand reports whether text is any of /remember /recall
// /forget /persona (with or without arguments). Used by HandleMessage
// to dispatch, and also as a quick filter before asking the persona
// loader for its state.
func IsPersonaCommand(text string) bool {
	trim := strings.TrimSpace(text)
	for _, prefix := range []string{"/remember", "/recall", "/forget", "/persona"} {
		if trim == prefix || strings.HasPrefix(trim, prefix+" ") {
			return true
		}
	}
	return false
}

// handlePersonaCommand routes a persona/memory slash command to the
// appropriate handler. Returns the reply text to send back.
//
// chatID / userID come from the incoming post; isDM is derived from
// client.IsBotDM so user-memory edits still work from DMs. All four
// sub-handlers tolerate a nil loader by returning a helpful diagnostic
// instead of crashing.
func (h *Handler) handlePersonaCommand(text, chatID, userID string, isDM bool) string {
	loader := h.PersonaLoader()
	if !loader.Enabled() {
		return "Persona & memory feature is disabled (`persona.enabled: false` in `~/.ringclaw/config.json`, or no backing store configured)."
	}

	fields := strings.Fields(strings.TrimSpace(text))
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/remember":
		return handleRemember(loader.Store(), args, chatID, userID)
	case "/recall":
		return handleRecall(loader.Store(), args, chatID, userID, isDM)
	case "/forget":
		return handleForget(loader.Store(), args, chatID, userID)
	case "/persona":
		return handlePersona(loader.Store())
	}
	return "Unknown persona command."
}

// handleRemember appends to the chosen scope's memory. Supported
// forms:
//
//	/remember <text>                 → ScopeChat
//	/remember chat <text>            → ScopeChat (explicit)
//	/remember user <text>            → ScopeUser
//	/remember global <text>          → ScopeGlobal
//
// The timestamp and the "- [...]" bullet are added by the Store so
// this handler only needs to forward the cleaned body.
func handleRemember(st *persona.Store, args []string, chatID, userID string) string {
	if len(args) == 0 {
		return "Usage: `/remember <text>` | `/remember user <text>` | `/remember global <text>`"
	}

	scope := persona.ScopeChat
	body := strings.Join(args, " ")
	if len(args) >= 2 {
		switch strings.ToLower(args[0]) {
		case "chat":
			scope = persona.ScopeChat
			body = strings.Join(args[1:], " ")
		case "user":
			scope = persona.ScopeUser
			body = strings.Join(args[1:], " ")
		case "global":
			scope = persona.ScopeGlobal
			body = strings.Join(args[1:], " ")
		}
	}

	id := memoryIDForScope(scope, chatID, userID)
	if err := st.AppendMemory(scope, id, body); err != nil {
		return fmt.Sprintf("Failed to save memory: %v", err)
	}
	return fmt.Sprintf("Remembered (%s): %s", scope, truncateForReply(body, 120))
}

// handleRecall shows the currently stored memory for a scope. Without
// arguments it shows the current chat's memory (the most common use).
// With an argument it shows the named scope.
func handleRecall(st *persona.Store, args []string, chatID, userID string, isDM bool) string {
	scope := persona.ScopeChat
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "chat":
			scope = persona.ScopeChat
		case "user":
			scope = persona.ScopeUser
		case "global":
			scope = persona.ScopeGlobal
		default:
			return "Usage: `/recall` | `/recall chat` | `/recall user` | `/recall global`"
		}
	}

	id := memoryIDForScope(scope, chatID, userID)
	content, err := st.LoadMemory(scope, id)
	if err != nil {
		return fmt.Sprintf("Failed to load memory: %v", err)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Sprintf("(no %s memory stored yet)", scope)
	}
	label := scopeLabel(scope, isDM)
	return fmt.Sprintf("**%s memory** (%d chars)\n\n%s",
		label, utf8.RuneCountInString(content), content)
}

// handleForget clears a scope's memory. Two-phase confirm: the first
// invocation returns a confirmation prompt; the caller must re-send
// with the explicit "confirm" token to actually clear. This prevents
// accidental clears (especially in group chats where the command
// would land in a shared scrollback).
func handleForget(st *persona.Store, args []string, chatID, userID string) string {
	scope := persona.ScopeChat
	confirmed := false

	for _, a := range args {
		switch strings.ToLower(a) {
		case "chat":
			scope = persona.ScopeChat
		case "user":
			scope = persona.ScopeUser
		case "global":
			scope = persona.ScopeGlobal
		case "confirm":
			confirmed = true
		}
	}

	if !confirmed {
		return fmt.Sprintf(
			"This will erase **%s memory**. Re-send `/forget %s confirm` within this chat to proceed.",
			scope, scope)
	}

	id := memoryIDForScope(scope, chatID, userID)
	if err := st.ClearMemory(scope, id); err != nil {
		return fmt.Sprintf("Failed to forget %s memory: %v", scope, err)
	}
	return fmt.Sprintf("Forgot %s memory.", scope)
}

// handlePersona reports the SOUL.md path + size and where to edit it.
// Read-only; no mutation is possible through this command.
func handlePersona(st *persona.Store) string {
	cfg := st.Config()
	soul, err := st.LoadSoul()
	if err != nil {
		return fmt.Sprintf("Failed to read SOUL.md: %v", err)
	}
	if strings.TrimSpace(soul) == "" {
		return fmt.Sprintf(
			"No SOUL configured yet. Edit `%s` to set the assistant's persona — it is shared across every agent (Claude / Codex / Gemini / …).",
			cfg.SoulFile)
	}
	return fmt.Sprintf(
		"**Persona** — edit `%s` to customize (%d chars)\n\n%s",
		cfg.SoulFile, utf8.RuneCountInString(soul), soul)
}

// memoryIDForScope picks the right ID for the given scope. Global
// scope uses a constant since there is only one global file; the
// other two scopes key by chatID or userID as declared.
func memoryIDForScope(scope persona.Scope, chatID, userID string) string {
	switch scope {
	case persona.ScopeGlobal:
		return ""
	case persona.ScopeUser:
		return userID
	case persona.ScopeChat:
		return chatID
	}
	return ""
}

// scopeLabel returns a user-facing label for the scope. The DM / Group
// hint is added to chat scope so a single /recall reply is
// unambiguous when the same operator runs it from multiple places.
func scopeLabel(scope persona.Scope, isDM bool) string {
	switch scope {
	case persona.ScopeGlobal:
		return "Global"
	case persona.ScopeUser:
		return "User"
	case persona.ScopeChat:
		if isDM {
			return "Chat (DM)"
		}
		return "Chat (Group)"
	}
	return string(scope)
}

func truncateForReply(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	// Truncate by runes, not bytes, so CJK text doesn't mid-codepoint.
	count := 0
	for i := range s {
		count++
		if count > max {
			return s[:i] + "…"
		}
	}
	return s
}
