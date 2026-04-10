# Repository Guidelines

## Project Overview

RingClaw is a RingCentral Team Messaging bot that bridges chat messages to AI agents (Claude, Codex, Cursor, Gemini, etc.) via the Agent Client Protocol (ACP). Written in Go 1.26.2

## Project Structure

```
main.go                  # Entry point → cmd.Execute()
cmd/                     # CLI commands (start, setup, update, send, etc.)
  start.go               # Bot startup orchestration
  start_init.go          # Agent detection, initialization
  *_cmd.go               # CLI subcommands (task, note, event, chat, etc.)
agent/                   # Agent adapters (ACP, HTTP, CLI)
  acp_agent.go           # ACP protocol: JSON-RPC over stdio
  acp_terminal.go        # Terminal/FS client interfaces for ACP
  http_agent.go          # HTTP/OpenAI-compatible agent
  cli_agent.go           # CLI wrapper agent
messaging/               # Message handling and business logic
  handler.go             # Main message dispatcher
  handler_summarize.go   # Summarize flow
  handler_commands.go    # Slash commands (/help, /cron, etc.)
  actions.go             # ACTION block parsing and execution
  prompts.go             # All prompt templates (centralized)
  intent.go              # AI intent classification
  summarize.go           # Summary building and name resolution
  cron.go                # Scheduled task system
  heartbeat.go           # Health check system
ringcentral/             # RingCentral API client
  client.go              # REST client (bot + private app)
  monitor.go             # WebSocket event listener
config/                  # Configuration loading and agent detection
  config.go              # JSON config (~/.ringclaw/config.json)
  detect.go              # Auto-detect installed agents
```

## Build & Test

```bash
go build ./...           # Build all packages
go vet ./...             # Static analysis
go test ./...            # Run all tests
go test -race ./...      # Run with race detector
go install .             # Install binary to $GOPATH/bin
```

## Coding Conventions

- **Tabs** for indentation (standard Go formatting)
- Run `gofmt` or `goimports` before committing
- Use `slog` for structured logging with `"component"` key: `slog.Info("msg", "component", "handler", ...)`
- Log levels: Info for normal operations, Debug for verbose/noisy output, Warn for recoverable issues
- Minimize comments — only add when behavior is non-obvious
- Prompts are centralized in `messaging/prompts.go`; override via `~/.ringclaw/prompts/<name>.md`

## Testing

- Test files: `*_test.go` alongside source (20 test files across all packages)
- Use standard `testing` package — no external test frameworks
- Test naming: `TestFunctionName_Scenario` (e.g. `TestBuildSummaryPrompt_DefaultMessageLimit`)
- Use `httptest.NewServer` for HTTP mocking, not external mock libraries

## Commit & PR Conventions

Commit messages follow conventional commits:

```
feat: add image analysis support
fix: resolve MCP tool call cancellation
refactor: consolidate prompts into prompts.go
chore: remove temporary debug logging
```

- Prefix: `feat:`, `fix:`, `refactor:`, `chore:`, `docs:`
- PRs require merge queue (no direct push to main)
- PR description should include Problem/Solution/Files sections

## Architecture Notes

- **ACP protocol**: JSON-RPC 2.0 over stdio. `readLoop` dispatches incoming requests (session/update, terminal/*, fs/*) and matches responses by integer ID.
- **Action system**: Agent replies may contain `ACTION:TYPE param=value\nbody\nEND_ACTION` blocks. These are parsed by `ParseAgentActions` and executed against the RingCentral API.
- **Dual client model**: Bot client (webhook-based, sends messages) + Private App client (user-level, accesses directory/calendar). Both are optional but bot is required.
- **`json.RawMessage` in maps**: Never place `json.RawMessage` in `map[string]interface{}` — Go's `json.Marshal` will base64-encode it. Use typed structs instead.
