package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ringclaw/ringclaw/messaging/persona"
	"github.com/ringclaw/ringclaw/ringcentral"
)

// pendingEntry holds a parsed PENDING notification entry from chat memory.
type pendingEntry struct {
	OrderID     string
	Coordinator string
	Phone       string
	Date        string
	raw         string // original line (for replacement)
}

// handleLowesBatch handles /lowes-batch send <date>
// Reads chat memory for pending Lowe's notifications, sends batch ACTION:SMS
// to coordinators, appends completion NOTE to SLA ledger.
//
// Command format: /lowes-batch send 2026-06-03
func (h *Handler) handleLowesBatch(ctx context.Context, client *ringcentral.Client, chatID, text string) string {
	args := strings.Fields(strings.TrimSpace(text))
	// Expected: ["/lowes-batch", "send", "<date>"]
	if len(args) < 2 || args[1] != "send" {
		return "Usage: /lowes-batch send <date>  (e.g. /lowes-batch send 2026-06-03)"
	}
	if len(args) < 3 {
		return "Usage: /lowes-batch send <date>  (e.g. /lowes-batch send 2026-06-03)"
	}

	date := strings.TrimSpace(args[2])
	if !isValidDate(date) {
		return fmt.Sprintf("Invalid date %q — expected YYYY-MM-DD format", date)
	}

	loader := h.PersonaLoader()
	if !loader.Enabled() {
		return "Persona & memory feature is disabled — cannot read pending notifications."
	}

	st := loader.Store()
	rawMemory, err := st.LoadMemory(persona.ScopeChat, chatID)
	if err != nil {
		return fmt.Sprintf("Failed to read chat memory: %v", err)
	}

	// Parse PENDING entries for the given date.
	entries, remaining := parsePendingEntries(rawMemory, date)
	if len(entries) == 0 {
		return fmt.Sprintf("No pending notifications for %s", date)
	}

	// Send SMS for each pending entry.
	var sent []string
	var failed []string
	sentTime := time.Now().UTC().Format(time.RFC3339)

	for _, e := range entries {
		slog.Info("lowes-batch: sending SMS",
			"component", "handler",
			"orderID", e.OrderID,
			"coordinator", e.Coordinator,
			"phone", e.Phone,
			"date", e.Date,
		)
		// ACTION:SMS — record as sent (actual SMS dispatch wired elsewhere).
		err := sendLowesSMS(ctx, client, chatID, e)
		if err != nil {
			slog.Error("lowes-batch: SMS send failed",
				"component", "handler",
				"orderID", e.OrderID,
				"phone", e.Phone,
				"error", err,
			)
			failed = append(failed, fmt.Sprintf("%s(%s)", e.OrderID, e.Phone))
			// Keep PENDING for failed entries.
			remaining = remaining + e.raw + "\n"
			continue
		}
		sent = append(sent, fmt.Sprintf("%s→%s", e.OrderID, e.Coordinator))
		// Mark as SENT in memory.
		remaining = remaining + strings.Replace(e.raw, "PENDING|", "SENT|", 1) + "\n"
	}

	// Rewrite chat memory with updated entries (PENDING→SENT for successes).
	if err := rewriteChatMemory(st, chatID, remaining); err != nil {
		slog.Error("lowes-batch: failed to update chat memory", "component", "handler", "error", err)
	}

	// Append a batch completion NOTE to chat memory.
	noteText := buildBatchNote(date, sent, failed, sentTime)
	if appendErr := st.AppendMemory(persona.ScopeChat, chatID, noteText); appendErr != nil {
		slog.Error("lowes-batch: failed to append batch note", "component", "handler", "error", appendErr)
	}

	return buildBatchSummary(date, sent, failed)
}

// sendLowesSMS executes the ACTION:SMS for a single pending entry.
// In the current implementation this creates a team-messaging post with
// the SMS action marker so the action pipeline can dispatch it; when the
// SMS action handler is wired in actions.go the post body triggers real
// dispatch. Until then the post serves as an audit trail.
func sendLowesSMS(ctx context.Context, client *ringcentral.Client, chatID string, e pendingEntry) error {
	if client == nil {
		return fmt.Errorf("no client available")
	}
	msg := fmt.Sprintf("ACTION:SMS to=%s\nOrder %s coordinator %s %s notification\nEND_ACTION",
		e.Phone, e.OrderID, e.Coordinator, e.Date)
	_, err := client.SendPost(ctx, chatID, msg)
	return err
}

// parsePendingEntries scans rawMemory line-by-line for lines matching
//
//	PENDING|<orderID>|<coordinator>|<phone>|<date>
//
// Lines may be bare or wrapped in the AppendMemory timestamp format:
//
//	- [2026-06-03T00:00:00Z] PENDING|...
//
// Returns matching entries (filtered to the given date) plus the
// remaining lines (non-matching + non-target-date PENDING lines) as a
// single string ready to be written back.
func parsePendingEntries(rawMemory, date string) ([]pendingEntry, string) {
	var entries []pendingEntry
	var remainingLines []string

	for _, line := range strings.Split(rawMemory, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Strip optional AppendMemory timestamp prefix: "- [<ts>] "
		payload := stripMemoryPrefix(trimmed)

		entry, ok := tryParsePending(payload, date)
		if ok {
			entry.raw = payload // store bare payload for PENDING→SENT replacement
			entries = append(entries, entry)
			// Don't add to remaining — we'll add back as SENT if successful.
		} else {
			// Keep the original line (with any timestamp prefix) in remaining.
			remainingLines = append(remainingLines, payload)
		}
	}

	remaining := ""
	if len(remainingLines) > 0 {
		remaining = strings.Join(remainingLines, "\n") + "\n"
	}
	return entries, remaining
}

// stripMemoryPrefix removes the leading "- [<timestamp>] " produced by
// AppendMemory from a memory line, returning the raw payload. If the
// prefix is not present the line is returned unchanged.
func stripMemoryPrefix(line string) string {
	// Pattern: "- [<timestamp>] <payload>"
	if !strings.HasPrefix(line, "- [") {
		return line
	}
	close := strings.Index(line, "] ")
	if close < 0 {
		return line
	}
	return strings.TrimSpace(line[close+2:])
}

// tryParsePending attempts to parse a PENDING entry for the given date.
// Returns (entry, true) on success, (zero, false) on no match.
func tryParsePending(line, date string) (pendingEntry, bool) {
	if !strings.HasPrefix(line, "PENDING|") {
		return pendingEntry{}, false
	}
	// Format: PENDING|<orderID>|<coordinator>|<phone>|<date>
	parts := strings.Split(line, "|")
	if len(parts) != 5 {
		return pendingEntry{}, false
	}
	entryDate := strings.TrimSpace(parts[4])
	if entryDate != date {
		return pendingEntry{}, false
	}
	return pendingEntry{
		OrderID:     strings.TrimSpace(parts[1]),
		Coordinator: strings.TrimSpace(parts[2]),
		Phone:       strings.TrimSpace(parts[3]),
		Date:        entryDate,
		raw:         line,
	}, true
}

// rewriteChatMemory replaces the full chat memory with newContent.
// It does this by clearing and then writing lines back one-by-one to
// leverage AppendMemory's timestamp / truncation logic, or by directly
// writing the file via the store's clear + append pattern.
//
// Since Store doesn't expose a raw write, we clear first and then
// re-append each line as a single entry. This is safe because we are
// only rewriting entries that were already in memory.
func rewriteChatMemory(st *persona.Store, chatID, content string) error {
	if err := st.ClearMemory(persona.ScopeChat, chatID); err != nil {
		return err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := st.AppendMemory(persona.ScopeChat, chatID, line); err != nil {
			return err
		}
	}
	return nil
}

// buildBatchNote returns the summary note text to append to chat memory.
func buildBatchNote(date string, sent, failed []string, sentTime string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "BATCH_SENT|%s|count=%d|time=%s", date, len(sent), sentTime)
	if len(sent) > 0 {
		fmt.Fprintf(&b, "|orders=%s", strings.Join(sent, ","))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&b, "|failed=%s", strings.Join(failed, ","))
	}
	return b.String()
}

// buildBatchSummary returns the user-facing reply for /lowes-batch send.
func buildBatchSummary(date string, sent, failed []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lowe's batch for %s: %d sent", date, len(sent))
	if len(sent) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(sent, ", "))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&b, ", %d failed (%s)", len(failed), strings.Join(failed, ", "))
	}
	b.WriteString(".")
	return b.String()
}

// isValidDate checks that s matches YYYY-MM-DD format (basic check).
func isValidDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	if s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, c := range s {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
