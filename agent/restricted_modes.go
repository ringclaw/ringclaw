package agent

import "strings"

// SessionMode mirrors the ACP `SessionMode` object returned in the
// `availableModes` field of a `session/new` response. We only care
// about the public id + display name when picking a restricted mode.
type SessionMode struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// agentMatcher captures a substring match against the agent command
// line and the modeId we want to switch to for non-owner senders.
type agentMatcher struct {
	pattern string // lowercased substring matched against the cmd line
	modeID  string
}

// defaultRestrictedModes lists the built-in agent → restricted modeId
// mapping. Order matters — the first pattern that appears as a
// substring in the lowercased agent command line wins.
//
// Survey notes (used as the basis for this table):
//
//   - droid (Factory): exposes `normal / spec / auto-low / auto-medium / auto-high`.
//     `spec` is the read-only "build feature specs" mode.
//   - claude-agent-acp / claude: exposes `default / acceptEdits / plan / auto / dontAsk / bypassPermissions`.
//     `plan` is the read-only research mode. (`dontAsk` is also a candidate
//     but rejects rather than read-onlys; we pick `plan` for UX parity with
//     the other agents.)
//   - gemini --experimental-acp: exposes `default / plan` (best-effort;
//     gemini-cli#22191 notes plan mode is buggy via ACP).
//   - qwen --acp: exposes `default / plan / auto-edit / yolo` (qwen-code#1806
//     notes set_mode("plan") is not enforced agent-side).
//   - cursor-agent acp: exposes `plan / agent / ask`.
//
// IMPORTANT: ringclaw treats set_mode as defense-in-depth only; the
// real client-side enforcement happens via the gateNonOwnerToolCall
// hook in acp_terminal.go which denies every fs/* and terminal/*
// request from non-owner sessions regardless of the agent's mode
// behavior.
var defaultRestrictedModes = []agentMatcher{
	{"droid", "spec"},
	{"claude-code-acp", "plan"},
	{"claude-agent-acp", "plan"},
	{"claude", "plan"},
	{"gemini", "plan"},
	{"qwen", "plan"},
	{"cursor-agent", "plan"},
	{"cursor", "plan"},
}

// readOnlyKeywords drives the heuristic fallback when the agent
// command does not match any built-in pattern but DOES advertise
// availableModes. The first mode whose id or name (case-insensitive)
// contains any of these keywords is taken as the restricted mode.
var readOnlyKeywords = []string{"plan", "spec", "read-only", "readonly", "safe"}

// Reason codes returned by ResolveRestrictedModeID, mirrored by
// audit logs so operators can tell which strategy produced the pick.
const (
	ResolveSourceConfig    = "config"
	ResolveSourceBuiltin   = "builtin"
	ResolveSourceHeuristic = "heuristic"
	ResolveSourceMissing   = "unsupported"
)

// ResolveRestrictedModeID picks the modeId to apply to a non-owner
// ACP session.
//
// Priority order:
//
//  1. cfg override (`agent.restricted_mode_id`) — used when non-empty
//     AND present in availableModes.
//  2. Built-in pattern → modeId map (defaultRestrictedModes).
//     Pattern must match the agent command line AND the resulting
//     modeId must be present in availableModes; otherwise we fall
//     through (covers the case where the agent renamed its modes).
//  3. Heuristic: first mode in availableModes whose id or name
//     (case-insensitive) contains a readOnlyKeywords entry.
//  4. None of the above → return ("", ResolveSourceMissing). The
//     caller fail-closes (refuses the non-owner message).
//
// The `cmdLine` parameter is the pre-joined, lowercased command line
// (typically `strings.ToLower(strings.Join(append([]string{cmd}, args...), " "))`).
func ResolveRestrictedModeID(cmdLine string, available []SessionMode, override string) (modeID string, source string) {
	cmdLine = strings.ToLower(cmdLine)

	if override != "" {
		if hasMode(available, override) {
			return override, ResolveSourceConfig
		}
		// Override didn't match anything the agent reports — fall
		// through to built-in / heuristic so we still try to pick
		// SOMETHING read-only rather than fail-closing on a typo.
	}

	for _, m := range defaultRestrictedModes {
		if !strings.Contains(cmdLine, m.pattern) {
			continue
		}
		if hasMode(available, m.modeID) {
			return m.modeID, ResolveSourceBuiltin + ":" + m.pattern
		}
		// Pattern matched but the agent does not (or no longer)
		// advertises that modeId. Continue scanning patterns and
		// then fall through to the heuristic.
	}

	for _, mode := range available {
		idLower := strings.ToLower(mode.ID)
		nameLower := strings.ToLower(mode.Name)
		for _, kw := range readOnlyKeywords {
			if strings.Contains(idLower, kw) || strings.Contains(nameLower, kw) {
				return mode.ID, ResolveSourceHeuristic + ":" + kw
			}
		}
	}

	return "", ResolveSourceMissing
}

func hasMode(modes []SessionMode, id string) bool {
	for _, m := range modes {
		if m.ID == id {
			return true
		}
	}
	return false
}

// AvailableModeIDs is a tiny helper used by audit logs.
func AvailableModeIDs(modes []SessionMode) []string {
	out := make([]string, 0, len(modes))
	for _, m := range modes {
		out = append(out, m.ID)
	}
	return out
}
