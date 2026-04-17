package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/ringclaw/ringclaw/config"
)

func init() {
	Register("cli", func(_ context.Context, name string, cfg config.AgentConfig, cwd string) (Agent, error) {
		if cwd == "" {
			cwd = defaultWorkspace()
		}
		return NewCLIAgent(CLIAgentConfig{
			Name:         name,
			Command:      cfg.Command,
			Args:         cfg.Args,
			Cwd:          cwd,
			Env:          cfg.Env,
			Model:        cfg.Model,
			SystemPrompt: cfg.SystemPrompt,
		}), nil
	})
}

// CLIAgent invokes a local CLI agent (claude, codex, etc.) via streaming JSON.
type CLIAgent struct {
	name         string
	command      string
	args         []string          // extra args from config
	cwd          string            // working directory
	env          map[string]string // extra environment variables
	model        string
	systemPrompt string
	mu           sync.Mutex
	sessions     map[string]string // conversationID -> session ID for multi-turn
}

// CLIAgentConfig holds configuration for a CLI agent.
type CLIAgentConfig struct {
	Name         string            // agent name for logging, e.g. "claude", "codex"
	Command      string            // path to binary
	Args         []string          // extra args (e.g. ["--dangerously-skip-permissions"])
	Cwd          string            // working directory (workspace)
	Env          map[string]string // extra environment variables
	Model        string
	SystemPrompt string
}

// NewCLIAgent creates a new CLI agent.
func NewCLIAgent(cfg CLIAgentConfig) *CLIAgent {
	cwd := cfg.Cwd
	if cwd == "" {
		cwd = defaultWorkspace()
	}
	return &CLIAgent{
		name:         cfg.Name,
		command:      cfg.Command,
		args:         cfg.Args,
		cwd:          cwd,
		env:          cfg.Env,
		model:        cfg.Model,
		systemPrompt: cfg.SystemPrompt,
		sessions:     make(map[string]string),
	}
}

// streamEvent represents a single event from claude's stream-json output.
type streamEvent struct {
	Type      string         `json:"type"`
	SessionID string         `json:"session_id"`
	Result    string         `json:"result"`
	IsError   bool           `json:"is_error"`
	Message   *streamMessage `json:"message,omitempty"`
}

// streamMessage represents the message field in an assistant event.
type streamMessage struct {
	Content []streamContent `json:"content"`
}

// streamContent represents a content block in an assistant message.
type streamContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Info returns metadata about this agent.
func (a *CLIAgent) Info() AgentInfo {
	return AgentInfo{
		Name:    a.name,
		Type:    "cli",
		Model:   a.model,
		Command: a.command,
	}
}

// SetCwd changes the working directory for subsequent CLI invocations.
// Rejects paths outside the configured workspace root as a defense-in-depth
// against bypasses of the /cwd handler.
func (a *CLIAgent) SetCwd(cwd string) {
	if err := EnsurePathInWorkspace(cwd); err != nil {
		slog.Warn("cli agent cwd rejected", "component", "cli", "agent", a.name, "cwd", cwd, "error", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cwd = cwd
}

// ResetSession clears the existing session for the given conversationID.
// Returns an empty string because the new session ID is only known after the
// next Chat call (claude assigns it during the conversation).
func (a *CLIAgent) ResetSession(_ context.Context, conversationID string) (string, error) {
	a.mu.Lock()
	delete(a.sessions, conversationID)
	a.mu.Unlock()
	slog.Info("session reset", "component", "cli", "command", a.command, "conversation", conversationID)
	return "", nil
}

// Chat sends a message to the CLI agent and returns the response.
func (a *CLIAgent) Chat(ctx context.Context, conversationID string, message string) (string, error) {
	switch a.name {
	case "codex":
		return a.chatCodex(ctx, message)
	default:
		return a.chatClaude(ctx, conversationID, message)
	}
}

// chatClaude uses claude -p with stream-json to get structured output and session persistence.
func (a *CLIAgent) chatClaude(ctx context.Context, conversationID string, message string) (string, error) {
	args := []string{"-p", message, "--output-format", "stream-json", "--verbose"}

	if a.model != "" {
		args = append(args, "--model", a.model)
	}
	if a.systemPrompt != "" {
		args = append(args, "--append-system-prompt", a.systemPrompt)
	}
	args = append(args, a.args...)

	a.mu.Lock()
	sessionID, hasSession := a.sessions[conversationID]
	a.mu.Unlock()

	if hasSession {
		args = append(args, "--resume", sessionID)
		slog.Info("resuming session", "component", "cli", "command", a.command, "session", sessionID, "conversation", conversationID)
	} else {
		slog.Info("starting new conversation", "component", "cli", "command", a.command, "conversation", conversationID)
	}

	cmd := exec.CommandContext(ctx, a.command, args...)
	if a.cwd != "" {
		cmd.Dir = a.cwd
	}
	if len(a.env) > 0 {
		cmdEnv, err := mergeEnv(os.Environ(), a.env)
		if err != nil {
			return "", fmt.Errorf("build %s env: %w", a.name, err)
		}
		cmd.Env = cmdEnv
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", a.name, err)
	}

	slog.Info("spawned process", "component", "cli", "command", a.command, "pid", cmd.Process.Pid, "conversation", conversationID)

	var result string
	var newSessionID string
	var assistantTexts []string

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.SessionID != "" {
			newSessionID = event.SessionID
		}

		switch event.Type {
		case "result":
			if event.IsError {
				return "", Crash(fmt.Errorf("%s returned error: %s", a.name, event.Result))
			}
			result = event.Result
		case "assistant":
			if event.Message != nil {
				for _, c := range event.Message.Content {
					if c.Type == "text" && c.Text != "" {
						assistantTexts = append(assistantTexts, c.Text)
					}
				}
			}
		}
	}

	if result == "" && len(assistantTexts) > 0 {
		result = strings.Join(assistantTexts, "")
	}

	if err := cmd.Wait(); err != nil {
		if result == "" {
			errMsg := strings.TrimSpace(stderr.String())
			if errMsg != "" {
				return "", Crash(fmt.Errorf("%s exited with error: %w, stderr: %s", a.name, err, errMsg))
			}
			return "", Crash(fmt.Errorf("%s exited with error: %w", a.name, err))
		}
	}

	slog.Info("process exited", "component", "cli", "command", a.command, "pid", cmd.Process.Pid)

	if newSessionID != "" {
		a.mu.Lock()
		a.sessions[conversationID] = newSessionID
		a.mu.Unlock()
		slog.Info("saved session", "component", "cli", "session", newSessionID, "conversation", conversationID)
	}

	result = strings.TrimSpace(result)
	if result == "" {
		return "", Empty()
	}

	return result, nil
}

// chatCodex handles codex CLI invocation using "codex exec".
func (a *CLIAgent) chatCodex(ctx context.Context, message string) (string, error) {
	args := []string{"exec", message}
	if a.model != "" {
		args = append(args, "--model", a.model)
	}
	// Append extra args from config (e.g. --skip-git-repo-check)
	args = append(args, a.args...)

	slog.Info("running codex exec", "component", "cli", "command", a.command)
	cmd := exec.CommandContext(ctx, a.command, args...)
	if a.cwd != "" {
		cmd.Dir = a.cwd
	}
	if len(a.env) > 0 {
		cmdEnv, err := mergeEnv(os.Environ(), a.env)
		if err != nil {
			return "", fmt.Errorf("build %s env: %w", a.name, err)
		}
		cmd.Env = cmdEnv
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", Crash(fmt.Errorf("codex error: %w, stderr: %s", err, errMsg))
		}
		return "", Crash(fmt.Errorf("codex error: %w", err))
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", Empty()
	}
	return result, nil
}
