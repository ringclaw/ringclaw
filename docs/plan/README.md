# RingClaw Improvement Plans

Requirement plans inspired by [acpx](https://github.com/openclaw/acpx) architecture, adapted for RingClaw's Go codebase and RingCentral Team Messaging context.

## Plans by Priority

| Priority | Plan | Status | Description |
|----------|------|--------|-------------|
| **P0** | [001 — Mock Agent Testing](001-mock-agent-testing.md) | Draft | Protocol-level ACP mock for integration testing |
| **P1** | [002 — Session Persistence](002-session-persistence.md) | Draft | Persist session mappings across restarts |
| **P2** | [003 — Structured Error Codes](003-error-codes.md) | Draft | Agent error classification for retry logic and user messages |
| **P3** | [004 — Incremental Reply Updates](004-incremental-reply-updates.md) | Draft | Stream partial replies instead of "Thinking..." placeholder |
| **P4** | [005 — Flow Architecture](005-flow-architecture.md) | Draft | Declarative multi-step message processing (future) |
| **P3** | [006 — Prompt Self-Evolution](006-prompt-self-evolution.md) | Draft | Eval harness + LLM-based prompt optimization |

## References

- acpx docs: https://github.com/openclaw/acpx/tree/main/docs
- Chinese versions: [zh/](zh/)
