---
title: Agent Configuration
---

# Agent Configuration

RingClaw supports three agent types: **ACP**, **CLI**, and **HTTP**. Each agent is configured in the `agents` section of `config.json`.

## Agent Types

### ACP (Agent Communication Protocol)

Long-running subprocess communicating via JSON-RPC over stdio. Fastest mode — reuses process and sessions.

```json
{
  "agents": {
    "claude": {
      "type": "acp",
      "command": "/usr/local/bin/claude-agent-acp",
      "model": "sonnet",
      "aliases": ["ai"],
      "env": {
        "ANTHROPIC_API_KEY": "sk-xxx"
      }
    }
  }
}
```

### CLI

Spawns a new process per message. Supports session resume via `--resume`.

```json
{
  "agents": {
    "claude": {
      "type": "cli",
      "command": "/usr/local/bin/claude",
      "cwd": "/home/user/my-project",
      "args": ["--dangerously-skip-permissions"]
    }
  }
}
```

### HTTP

OpenAI-compatible chat completions API. Supports `openai` (default), `nanoclaw`, and `dify` formats.

```json
{
  "agents": {
    "openclaw": {
      "type": "http",
      "endpoint": "https://api.example.com/v1/chat/completions",
      "api_key": "sk-xxx",
      "model": "openclaw:main"
    },
    "dify": {
      "type": "http",
      "format": "dify",
      "endpoint": "https://api.dify.ai/v1/chat-messages",
      "api_key": "app-xxx",
      "aliases": ["df"]
    }
  }
}
```

## Custom Aliases

You can define custom trigger aliases per agent in `config.json`:

```json
{
  "claude": {
    "type": "acp",
    "command": "/usr/local/bin/claude-agent-acp",
    "aliases": ["gpt", "ai"]
  }
}
```

Then `/gpt hello` will route to Claude. RingClaw warns on startup if custom aliases conflict with built-in aliases or other agents.

## Permission Bypass

By default, some agents require interactive permission approval which doesn't work in a messaging bot context. Add `args` to your agent config to bypass:

| Agent | Flag | What it does |
|-------|------|-------------|
| Claude (CLI) | `--dangerously-skip-permissions` | Skip all tool permission prompts |
| Codex (CLI) | `--skip-git-repo-check` | Allow running outside git repos |

Example:

```json
{
  "agent_workspace": "/home/user/my-project",
  "claude": {
    "type": "cli",
    "command": "/usr/local/bin/claude",
    "cwd": "/home/user/my-project",
    "args": ["--dangerously-skip-permissions"]
  },
  "codex": {
    "type": "cli",
    "command": "/usr/local/bin/codex",
    "cwd": "/home/user/my-project",
    "args": ["--skip-git-repo-check"]
  }
}
```

::: warning
These flags disable safety checks. Only enable them if you understand the risks. ACP agents handle permissions automatically and don't need these flags.
:::

## Workspace Configuration

- **`agent_workspace`** (top-level): Sets the default working directory for all agent processes. RingClaw will start the agent from that workspace before invoking it.
- **`cwd`** (per-agent): Overrides `agent_workspace` for a specific agent.
- If neither is set, RingClaw defaults to `~/.ringclaw/workspace`.

```json
{
  "agent_workspace": "/home/user/my-project",
  "agents": {
    "claude": {
      "type": "acp",
      "command": "claude-agent-acp",
      "cwd": "/home/user/special-project"
    }
  }
}
```
