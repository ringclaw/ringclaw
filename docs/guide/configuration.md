---
title: Configuration
---

# Configuration

Config file: `~/.ringclaw/config.json`

## Full Configuration Example

```json
{
  "default_agent": "claude",
  "agent_workspace": "/home/user/my-project",
  "ringcentral": {
    "bot_token": "your_bot_token",
    "chat_ids": ["chat_id_1", "chat_id_2"],
    "source_user_ids": ["alice@example.com"],
    "bot_mention_only": true,
    "group_summary_group_id": "1234567",
    "group_summary_message_limit": 200,
    "server_url": "https://platform.ringcentral.com",
    "client_id": "",
    "client_secret": "",
    "jwt_token": ""
  },
  "agents": {
    "claude": {
      "type": "acp",
      "command": "/usr/local/bin/claude-agent-acp",
      "model": "sonnet",
      "aliases": ["ai"],
      "env": {
        "ANTHROPIC_API_KEY": "sk-xxx"
      }
    },
    "codex": {
      "type": "acp",
      "command": "/usr/local/bin/codex-acp"
    },
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
  },
  "heartbeat": {
    "enabled": true,
    "interval": "30m",
    "active_hours": "09:00-18:00",
    "timezone": "Asia/Shanghai"
  }
}
```

## Environment Variables

| Variable | Description |
|----------|-------------|
| `RC_BOT_TOKEN` | Bot App token (required) |
| `RC_CLIENT_ID` | Private App client ID (optional, enables summarize) |
| `RC_CLIENT_SECRET` | Private App client secret (optional) |
| `RC_JWT_TOKEN` | Private App JWT credential (optional) |
| `RC_SERVER_URL` | RingCentral server URL (default: `https://platform.ringcentral.com`) |
| `RINGCLAW_DEFAULT_AGENT` | Override default agent |
| `OPENCLAW_GATEWAY_URL` | OpenClaw HTTP fallback endpoint |
| `OPENCLAW_GATEWAY_TOKEN` | OpenClaw API token |
| `RINGCLAW_API_ADDR` | Change the HTTP API listen address (default: `127.0.0.1:18011`) |

## Config Fields

### Top-level

| Field | Type | Description |
|-------|------|-------------|
| `default_agent` | string | Name of the default AI agent |
| `agent_workspace` | string | Default working directory for all agents |

### `ringcentral` Section

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `bot_token` | string | — | Bot App token (required) |
| `chat_ids` | string[] | `[]` | Chat IDs to monitor |
| `source_user_ids` | string[] | `[]` | Restrict to these users (IDs or emails) |
| `bot_mention_only` | bool | `true` | Only respond when @mentioned in groups |
| `group_summary_group_id` | string | — | Group ID allowed for in-group summarize |
| `group_summary_message_limit` | int | `200` | Messages to pull for group summarize |
| `server_url` | string | `https://platform.ringcentral.com` | RingCentral API server URL |
| `client_id` | string | — | Private App client ID |
| `client_secret` | string | — | Private App client secret |
| `jwt_token` | string | — | Private App JWT token |

### Agent Config

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | `acp`, `cli`, or `http` |
| `command` | string | Path to agent binary (ACP/CLI) |
| `endpoint` | string | API endpoint URL (HTTP) |
| `api_key` | string | API key (HTTP) |
| `model` | string | Model name |
| `format` | string | API format: `openai`, `nanoclaw`, or `dify` (HTTP) |
| `cwd` | string | Working directory override for this agent |
| `args` | string[] | Additional CLI arguments |
| `aliases` | string[] | Custom trigger aliases |
| `env` | object | Environment variables for the agent process |
| `allow_write` | bool | Allow ACP agent to write files (default: false) |

### `heartbeat` Section

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable heartbeat runner |
| `interval` | string | `30m` | Time between heartbeat checks |
| `active_hours` | string | — | Only run during these hours (e.g. `09:00-18:00`) |
| `timezone` | string | local | IANA timezone for active hours |
