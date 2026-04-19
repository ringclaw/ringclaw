package messaging

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ringclaw/ringclaw/messaging/persona"
)

// Memory & persona slash commands.
//
// Two distinct command surfaces share this file because they share
// the same backing Store/Loader:
//
//   /mem add | show | del   — CRUD on memory/{global,user,chat}.md
//                              (slash-mutable; add+del are privileged)
//   /persona                — read-only display of SOUL.md
//                              (operator-only on disk; never mutated
//                              by a slash command or agent tool)
//
// Each handler returns a plain-text reply; the caller (HandleMessage)
// sends it via SendTextReply.

// memSubcommand returns the lowercased token following "/mem". For
// "/mem" alone it returns "". Used by both the privileged-command
// gate (handler_commands.go) and the dispatcher below so the two
// stay in sync on what counts as a write.
func memSubcommand(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 || !strings.EqualFold(fields[0], "/mem") {
		return ""
	}
	return strings.ToLower(fields[1])
}

// IsMemCommand reports whether text is "/mem" or "/mem ...".
// Used by HandleMessage to dispatch.
func IsMemCommand(text string) bool {
	trim := strings.TrimSpace(text)
	if strings.EqualFold(trim, "/mem") {
		return true
	}
	if len(trim) >= len("/mem ") && strings.EqualFold(trim[:len("/mem ")], "/mem ") {
		return true
	}
	return false
}

// IsPersonaCommand reports whether text is "/persona" (with or
// without trailing whitespace/args). /persona is read-only; the
// handler ignores any arguments.
func IsPersonaCommand(text string) bool {
	trim := strings.TrimSpace(text)
	if strings.EqualFold(trim, "/persona") {
		return true
	}
	if len(trim) >= len("/persona ") && strings.EqualFold(trim[:len("/persona ")], "/persona ") {
		return true
	}
	return false
}

// memArgs is the parsed form of a /mem subcommand.
type memArgs struct {
	sub       string // "add" | "show" | "del" | ""
	scope     persona.Scope
	body      string // for "add"
	confirmed bool   // for "del"
	usage     string // non-empty when the input was malformed; reply with this
}

// parseMemCommand owns argument parsing for /mem add | show | del.
// Default scope when omitted = ScopeChat. Returns a usage string in
// memArgs.usage when the input is unusable; callers should surface it
// directly without touching the store.
func parseMemCommand(text string) memArgs {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 1 || !strings.EqualFold(fields[0], "/mem") {
		return memArgs{usage: memUsage()}
	}
	if len(fields) == 1 {
		return memArgs{usage: memUsage()}
	}

	out := memArgs{sub: strings.ToLower(fields[1]), scope: persona.ScopeChat}
	rest := fields[2:]

	switch out.sub {
	case "add":
		if len(rest) == 0 {
			out.usage = "Usage: `/mem add <text>` | `/mem add user <text>` | `/mem add global <text>`"
			return out
		}
		// Optional first token may be a scope name.
		if scope, ok := scopeFromToken(rest[0]); ok && len(rest) >= 2 {
			out.scope = scope
			out.body = strings.Join(rest[1:], " ")
		} else {
			out.body = strings.Join(rest, " ")
		}
		if strings.TrimSpace(out.body) == "" {
			out.usage = "Usage: `/mem add <text>` | `/mem add user <text>` | `/mem add global <text>`"
		}
		return out

	case "show":
		if len(rest) > 0 {
			scope, ok := scopeFromToken(rest[0])
			if !ok {
				out.usage = "Usage: `/mem show` | `/mem show chat` | `/mem show user` | `/mem show global`"
				return out
			}
			out.scope = scope
		}
		return out

	case "del":
		// Tokens may appear in any order: scope and/or "confirm".
		for _, a := range rest {
			if strings.EqualFold(a, "confirm") {
				out.confirmed = true
				continue
			}
			if scope, ok := scopeFromToken(a); ok {
				out.scope = scope
				continue
			}
			out.usage = "Usage: `/mem del [chat|user|global] [confirm]`"
			return out
		}
		return out

	default:
		out.usage = memUsage()
		return out
	}
}

// scopeFromToken maps a CLI scope token to persona.Scope.
func scopeFromToken(s string) (persona.Scope, bool) {
	switch strings.ToLower(s) {
	case "chat":
		return persona.ScopeChat, true
	case "user":
		return persona.ScopeUser, true
	case "global":
		return persona.ScopeGlobal, true
	}
	return "", false
}

func memUsage() string {
	return "Usage: `/mem add [user|chat|global] <text>` | `/mem show [user|chat|global]` | `/mem del [scope] [confirm]`"
}

// handleMemCommand routes /mem subcommands to the per-verb handlers.
// chatID / userID come from the incoming post; isDM is derived from
// client.IsBotDM so user-memory edits still work from DMs.
func (h *Handler) handleMemCommand(text, chatID, userID string, isDM bool) string {
	loader := h.PersonaLoader()
	if !loader.Enabled() {
		return "Persona & memory feature is disabled (`persona.enabled: false` in `~/.ringclaw/config.json`, or no backing store configured)."
	}

	args := parseMemCommand(text)
	if args.usage != "" {
		return args.usage
	}

	st := loader.Store()
	switch args.sub {
	case "add":
		return handleMemAdd(st, args, chatID, userID)
	case "show":
		return handleMemShow(st, args, chatID, userID, isDM)
	case "del":
		return handleMemDel(st, args, chatID, userID)
	}
	return memUsage()
}

// handlePersonaCommand reports the SOUL.md path + size and where to
// edit it. Read-only; no mutation is possible through this command.
func (h *Handler) handlePersonaCommand() string {
	loader := h.PersonaLoader()
	if !loader.Enabled() {
		return "Persona & memory feature is disabled (`persona.enabled: false` in `~/.ringclaw/config.json`, or no backing store configured)."
	}
	st := loader.Store()
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

// handleMemAdd appends to the chosen scope's memory. Default scope
// is ScopeChat (no scope token in the input).
func handleMemAdd(st *persona.Store, args memArgs, chatID, userID string) string {
	id := memoryIDForScope(args.scope, chatID, userID)
	if err := st.AppendMemory(args.scope, id, args.body); err != nil {
		return fmt.Sprintf("Failed to save memory: %v", err)
	}
	return fmt.Sprintf("Remembered (%s): %s", args.scope, truncateForReply(args.body, 120))
}

// handleMemShow shows the currently stored memory for a scope.
func handleMemShow(st *persona.Store, args memArgs, chatID, userID string, isDM bool) string {
	id := memoryIDForScope(args.scope, chatID, userID)
	content, err := st.LoadMemory(args.scope, id)
	if err != nil {
		return fmt.Sprintf("Failed to load memory: %v", err)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Sprintf("(no %s memory stored yet)", args.scope)
	}
	label := scopeLabel(args.scope, isDM)
	return fmt.Sprintf("**%s memory** (%d chars)\n\n%s",
		label, utf8.RuneCountInString(content), content)
}

// handleMemDel clears a scope's memory. Two-phase confirm: the first
// invocation returns a confirmation prompt; the caller must re-send
// with the explicit "confirm" token to actually clear. This prevents
// accidental clears (especially in group chats where the command
// would land in a shared scrollback).
func handleMemDel(st *persona.Store, args memArgs, chatID, userID string) string {
	if !args.confirmed {
		return fmt.Sprintf(
			"This will erase **%s memory**. Re-send `/mem del %s confirm` within this chat to proceed.",
			args.scope, args.scope)
	}
	id := memoryIDForScope(args.scope, chatID, userID)
	if err := st.ClearMemory(args.scope, id); err != nil {
		return fmt.Sprintf("Failed to forget %s memory: %v", args.scope, err)
	}
	return fmt.Sprintf("Forgot %s memory.", args.scope)
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
// hint is added to chat scope so a single /mem show reply is
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
	count := 0
	for i := range s {
		count++
		if count > max {
			return s[:i] + "…"
		}
	}
	return s
}
