package agent

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
)

// truncateRaw truncates a JSON raw message for logging.
func truncateRaw(data json.RawMessage, maxLen int) string {
	if len(data) == 0 {
		return ""
	}
	s := string(data)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// extractToolAndArgs parses rawInput like {"server":"jira","tool":"jira_search","arguments":{...}}
// and returns "jira/jira_search" and a compact args string.
func extractToolAndArgs(data json.RawMessage) (string, string) {
	if len(data) == 0 {
		return "", ""
	}
	var v struct {
		Server string          `json:"server"`
		Tool   string          `json:"tool"`
		Args   json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.Tool == "" {
		return "", truncateRaw(data, 200)
	}
	tool := v.Tool
	if v.Server != "" {
		tool = v.Server + "/" + v.Tool
	}
	args := string(v.Args)
	if len(args) > 200 {
		args = args[:200] + "..."
	}
	return tool, args
}

// extractToolOutput extracts a readable preview from rawOutput.
func extractToolOutput(data json.RawMessage, maxLen int) string {
	if len(data) == 0 {
		return ""
	}
	s := string(data)
	if len(s) > 1 && s[0] == '"' {
		var unquoted string
		if err := json.Unmarshal(data, &unquoted); err == nil {
			s = unquoted
		}
	}
	var resp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal([]byte(s), &resp); err == nil && len(resp.Content) > 0 {
		text := resp.Content[0].Text
		if resp.IsError {
			text = "[error] " + text
		}
		if len(text) > maxLen {
			return text[:maxLen] + "..."
		}
		return text
	}
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// acpStderrWriter forwards the ACP subprocess stderr to the application log
// and captures the last meaningful error line.
type acpStderrWriter struct {
	prefix      string
	mu          sync.Mutex
	last        string
	npxCacheDir string // populated when ENOTEMPTY + _npx/ detected
}

func (w *acpStderrWriter) Write(p []byte) (int, error) {
	lines := strings.Split(strings.TrimRight(string(p), "\n"), "\n")
	w.mu.Lock()
	for _, line := range lines {
		if line == "" {
			continue
		}
		slog.Debug("subprocess stderr", "prefix", w.prefix, "line", line)
		if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "Traceback") || strings.HasPrefix(line, "...") {
			continue
		}
		// Skip pure brace/bracket structural lines from JSON-style multi-line
		// error dumps (e.g. claude-agent-acp prints `} {`, `}` on their own
		// lines). Without this filter LastError() captures the trailing `}`
		// instead of the actual `details: 'Invalid Mode'` line.
		trimmed := strings.TrimSpace(line)
		if isStructuralOnly(trimmed) {
			continue
		}
		w.last = line
		if w.npxCacheDir == "" && strings.Contains(line, "ENOTEMPTY") {
			if dir := extractNpxCacheDir(line); dir != "" {
				w.npxCacheDir = dir
			}
		}
	}
	w.mu.Unlock()
	return len(p), nil
}

// isStructuralOnly returns true for lines that consist solely of structural
// JSON-ish punctuation such as `{`, `}`, `} {`, `},`, `[`, `]`.
func isStructuralOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case '{', '}', '[', ']', ' ', '\t', ',':
			continue
		default:
			return false
		}
	}
	return true
}

// LastError returns the last captured error line and resets it.
func (w *acpStderrWriter) LastError() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.last
	w.last = ""
	return s
}

// NpxCorruptedDir returns the npx cache directory path if an ENOTEMPTY
// error was detected in stderr, indicating a corrupted npx cache.
func (w *acpStderrWriter) NpxCorruptedDir() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.npxCacheDir
}

// extractNpxCacheDir extracts the npx cache directory from an ENOTEMPTY
// error line. It looks for a path containing "_npx/" and returns the
// directory up to and including the hash segment
// (e.g. "/Users/x/.npm/_npx/d820eb7d96bc2600").
func extractNpxCacheDir(line string) string {
	const marker = "_npx/"
	// Scan for any path-like token containing _npx/
	for _, token := range strings.Fields(line) {
		// Strip surrounding quotes
		token = strings.Trim(token, "'\"")
		idx := strings.Index(token, marker)
		if idx < 0 {
			continue
		}
		// Take up to the next path separator after the hash
		rest := token[idx+len(marker):]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return token[:idx+len(marker)+len(rest)]
		}
		return token[:idx+len(marker)+slash]
	}
	return ""
}
