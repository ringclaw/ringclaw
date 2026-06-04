package persona

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// Loader assembles the per-message context banner. It is the only
// type the handler layer talks to — tests can substitute a Store
// backed by a tempdir without spinning up any RingClaw state.
//
// A nil *Loader is a legal value meaning "persona disabled"; callers
// should guard with `l != nil && l.Enabled()` before calling Build.
// This keeps the handler's integration site a single boolean branch.
type Loader struct {
	store *Store
}

// NewLoader wires a Store to a Loader. If store is nil the returned
// loader's Enabled() reports false so callers skip injection
// entirely.
func NewLoader(store *Store) *Loader {
	return &Loader{store: store}
}

// Enabled reports whether the loader has a backing store and the
// resolved config is switched on. A disabled loader's Build always
// returns the empty string.
func (l *Loader) Enabled() bool {
	return l != nil && l.store != nil && l.store.Config().Enabled
}

// Store exposes the backing Store for callers that need to write
// memory (e.g. the /mem add slash command). Never returns nil when
// the loader itself is non-nil; the handler only reaches for this
// after a successful NewLoader.
func (l *Loader) Store() *Store {
	if l == nil {
		return nil
	}
	return l.store
}

// Build returns the context banner to prepend to a user message. The
// banner is composed of XML-tagged sections (persona, skills index,
// global memory, user memory, chat memory). Any section that resolves
// to an empty string is omitted entirely so a fresh install with no
// memory produces no banner at all — agent behaviour is unchanged from
// the pre-persona baseline.
//
// chatID / userID must be the raw IDs observed on the incoming post;
// Build runs them through SanitizeID internally. isDM feeds the
// chat_type attribute ("DM" vs "Group") so downstream prompts can
// distinguish at a glance.
//
// The ctx parameter is plumbed through for future expansion (timeouts
// on memory backends) but is currently unused by the file-backed
// Store.
func (l *Loader) Build(ctx context.Context, chatID, userID string, isDM bool) string {
	return l.buildBanner(ctx, chatID, userID, isDM, "")
}

// BuildWithEntity is like Build but also injects entity memory when
// entityID is non-empty. The entity section is inserted after chat
// memory using the scope="entity" attribute so agents can distinguish
// it from per-user or per-chat memory.
func (l *Loader) BuildWithEntity(ctx context.Context, chatID, userID string, isDM bool, entityID string) string {
	return l.buildBanner(ctx, chatID, userID, isDM, entityID)
}

// buildBanner is the shared implementation behind Build and
// BuildWithEntity. An empty entityID skips entity memory injection.
func (l *Loader) buildBanner(_ context.Context, chatID, userID string, isDM bool, entityID string) string {
	if !l.Enabled() {
		return ""
	}

	soul, err := l.store.LoadSoul()
	if err != nil {
		slog.Warn("persona: load soul failed", "component", "persona", "error", err)
	}
	global, err := l.store.LoadMemory(ScopeGlobal, "")
	if err != nil {
		slog.Warn("persona: load global memory failed", "component", "persona", "error", err)
	}
	user, err := l.store.LoadMemory(ScopeUser, userID)
	if err != nil {
		slog.Warn("persona: load user memory failed", "component", "persona", "error", err)
	}
	chat, err := l.store.LoadMemory(ScopeChat, chatID)
	if err != nil {
		slog.Warn("persona: load chat memory failed", "component", "persona", "error", err)
	}

	var entity string
	if entityID != "" {
		entity, err = l.store.LoadEntity(entityID)
		if err != nil {
			slog.Warn("persona: load entity memory failed", "component", "persona", "error", err)
		}
	}

	skillsIndex, err := l.store.LoadSkillsIndex()
	if err != nil {
		slog.Warn("persona: load skills index failed", "component", "persona", "error", err)
	}

	chatType := "Group"
	if isDM {
		chatType = "DM"
	}

	var parts []string
	if s := strings.TrimSpace(soul); s != "" {
		parts = append(parts, wrapSection("persona", "", "", s))
	}
	if len(skillsIndex) > 0 {
		parts = append(parts, buildSkillsIndexSection(skillsIndex))
	}
	if s := strings.TrimSpace(global); s != "" {
		parts = append(parts, wrapSection("memory", "global", "", s))
	}
	if s := strings.TrimSpace(user); s != "" {
		parts = append(parts, wrapSection("memory", "user", "", s))
	}
	if s := strings.TrimSpace(chat); s != "" {
		parts = append(parts, wrapSection("memory", "chat", chatType, s))
	}
	if s := strings.TrimSpace(entity); s != "" {
		parts = append(parts, wrapSection("memory", "entity", "", s))
	}

	if len(parts) == 0 {
		return ""
	}
	// Trailing blank line separates the banner from the real user
	// message so the boundary is obvious to the agent.
	return strings.Join(parts, "\n\n") + "\n\n"
}

// buildSkillsIndexSection formats the skills index as a compact
// <context type="skills"> block with one "name · description" line per
// skill. Only the name is emitted when the description is empty.
func buildSkillsIndexSection(entries []SkillEntry) string {
	var sb strings.Builder
	sb.WriteString(`<context type="skills">`)
	sb.WriteByte('\n')
	for _, e := range entries {
		sb.WriteString(e.Name)
		if e.Description != "" {
			sb.WriteString(" · ")
			sb.WriteString(e.Description)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("</context>")
	return sb.String()
}

// wrapSection produces one `<context type="..." ...>...</context>`
// block. scope and chatType are emitted as attributes only when
// non-empty so the rendered XML stays minimal.
func wrapSection(kind, scope, chatType, body string) string {
	var sb strings.Builder
	sb.WriteString(`<context type="`)
	sb.WriteString(kind)
	sb.WriteByte('"')
	if scope != "" {
		fmt.Fprintf(&sb, ` scope=%q`, scope)
	}
	if chatType != "" {
		fmt.Fprintf(&sb, ` chat_type=%q`, chatType)
	}
	sb.WriteString(">\n")
	sb.WriteString(body)
	sb.WriteString("\n</context>")
	return sb.String()
}
