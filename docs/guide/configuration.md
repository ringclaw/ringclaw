---
title: Configuration
---

# Configuration

Config file: `~/.ringclaw/config.json`

> **Config-only**: All configuration is sourced exclusively from
> `~/.ringclaw/config.json`. Previously supported environment variables
> (`RC_*`, `RINGCLAW_*`, `OPENCLAW_GATEWAY_*`) are silently ignored. Use
> `ringclaw setup` to generate the file interactively, or edit it by hand.

## Full Configuration Example

```json
{
  "default_agent": "claude",
  "agent_workspace": "/home/user/my-project",
  "agent_allow_workspace_list": ["/home/user/my-project", "/home/user/notes"],
  "api_addr": "127.0.0.1:18010",
  "log_level": "info",
  "log_format": "color",
  "full_access_ack": false,

  "ringcentral": {
    "server_url": "https://platform.ringcentral.com",
    "client_id": "",
    "client_secret": "",
    "jwt_token": "",
    "bot_token": "your_bot_token",
    "bot_mention_only": true,
    "chat_ids": ["chat_id_1", "chat_id_2"],
    "source_user_ids": ["alice@example.com"],
    "group_summary_group_id": "1234567",
    "group_summary_message_limit": 200
  },

  "heartbeat": {
    "enabled": true,
    "interval": "30m",
    "active_hours": "09:00-18:00",
    "timezone": "Asia/Shanghai"
  },

  "cron": { "enabled": true },

  "openclaw_gateway": {
    "url": "wss://openclaw-gateway.example.com",
    "token": "openclaw-token",
    "password": ""
  },

  "agents": {
    "claude": {
      "type": "acp",
      "command": "/usr/local/bin/claude-agent-acp",
      "model": "sonnet",
      "aliases": ["ai"],
      "allow_write": true,
      "env": { "ANTHROPIC_API_KEY": "sk-xxx" }
    },
    "codex": {
      "type": "acp",
      "command": "/usr/local/bin/codex-acp",
      "full_access": true
    },
    "gpt": {
      "type": "http",
      "endpoint": "https://api.openai.com/v1/chat/completions",
      "api_key": "sk-xxx",
      "model": "gpt-4o-mini",
      "format": "openai",
      "max_history": 20,
      "timeout": 120,
      "aliases": ["4o", "openai"]
    }
  }
}
```

## Top-level Fields

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `default_agent` | string | auto-detected | Name of any key in `agents`. When empty or missing, auto-detection picks by priority: `claude, codex, cursor, kimi, gemini, opencode, openclaw, pi, copilot, droid, iflow, kiro, qwen, augment`. |
| `agent_workspace` | string | — | Absolute path. Default cwd for agents, implicitly added to the cwd allowlist. |
| `agent_allow_workspace_list` | string[] | `[]` | Absolute paths. `/cwd` and `Agent.SetCwd` may only target directories inside this list. `~/.ringclaw/workspace` and `agent_workspace` are always implicitly merged in. |
| `api_addr` | string | `127.0.0.1:18010` | `host:port`. Do NOT bind to `0.0.0.0`; see [Security](../security/index.md). |
| `log_level` | string | `info` | One of `debug`, `info`, `warn` (alias `warning`), `error`. |
| `log_format` | string | `text` | One of `text`, `json`, `color`. |
| `full_access_ack` | bool (nullable) | omitted | `true` to allow ACP agents with `full_access: true`; `false` to explicitly refuse even if `full_access: true` is set. When omitted, treated as `false`. |
| `agents` | object | — | See [Agents](#agents). At least one agent is required to start. |
| `ringcentral` | object | — | See [`ringcentral`](#ringcentral). |
| `heartbeat` | object | — | See [`heartbeat`](#heartbeat). |
| `cron` | object | — | See [`cron`](#cron). |
| `openclaw_gateway` | object | — | See [`openclaw_gateway`](#openclaw_gateway). |

## `ringcentral`

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `bot_token` | string | — | **Required.** RingCentral Public Bot access token. |
| `chat_ids` | string[] | `[]` | **Required.** Chat IDs the bot is allowed to receive messages from. |
| `source_user_ids` | string[] | `[]` | Trusted senders. Accepts numeric extension IDs, emails, or E.164 phone numbers. Empty + Private App configured → owner-only. Empty + no Private App → deny all. |
| `bot_mention_only` | bool (nullable) | `true` | `true` requires `@bot` in group chats; `false` answers every message in allowed chats. |
| `server_url` | string | SDK default (`https://platform.ringcentral.com`) | RingCentral API server URL. |
| `client_id` | string | — | Private App Client ID. All three Private App fields must be set together. |
| `client_secret` | string | — | Private App Client Secret. Redacted in logs. |
| `jwt_token` | string | — | Private App JWT. Redacted in logs. |
| `group_summary_group_id` | string | — | Group chat ID allowed to use current-group summarize. Empty disables the feature. |
| `group_summary_message_limit` | int | `200` | Messages fetched per group-summarize call. `<= 0` falls back to default. |

## `heartbeat`

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `enabled` | bool | `false` | Master switch. |
| `interval` | string | `30m` | Go duration: `45s`, `30m`, `2h`. Must be positive. |
| `active_hours` | string | — | `HH:MM-HH:MM` (hours `00`-`23`, minutes `00`-`59`). Use `22:00-06:00` for over-midnight. Empty = 24/7. |
| `timezone` | string | host local | IANA timezone, e.g. `Asia/Shanghai`, `UTC`. |

## `cron`

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `enabled` | bool | `true` when cron jobs exist | Master switch for the scheduler. |

## `openclaw_gateway`

Connection info for the auto-detected external `openclaw` agent. When `url` is
empty, ringclaw falls back to reading `~/.openclaw/openclaw.json`.

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `url` | string | — | `ws://`, `wss://`, `http://`, or `https://` URL. |
| `token` | string | — | Bearer token. Used when the gateway expects token auth. |
| `password` | string | — | Password. Used when the gateway expects password auth. |

## Agents

Keys under `agents` are free-form agent names (e.g. `claude`, `codex`, `gpt`).
Each value is an [`AgentConfig`](#agentconfig-common-fields) plus type-specific
extras below.

### AgentConfig common fields

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `type` | string | — | **Required.** One of `acp`, `cli`, `http`. |
| `command` | string | — | Required for `acp`/`cli`. Absolute path or binary name in `PATH`. |
| `args` | string[] | `[]` | Extra CLI arguments (e.g. `["acp"]`, `["--experimental-acp"]`). |
| `aliases` | string[] | `[]` | Custom trigger words, e.g. `["gpt", "4o"]`. |
| `cwd` | string | — | Agent working directory. Must satisfy `agent_allow_workspace_list`. |
| `env` | object | `{}` | Extra env vars passed to the agent subprocess (not to ringclaw itself). |
| `model` | string | agent-specific | Model name. Defaults: claude → `sonnet`, openclaw → `openclaw:main`, HTTP fallback → `gpt-4o-mini`. |
| `system_prompt` | string | — | System prompt injected at session start. |

### ACP-specific (`type: acp`)

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `allow_write` | bool | `false` | Grant the ACP fs capability (agent may write files). |
| `full_access` | bool | `false` | Call `session/set_mode "full-access"` on every new session. Requires `full_access_ack: true` at the top level, otherwise downgraded with a `WARN`. |

### HTTP-specific (`type: http`)

| Field | Type | Default | Allowed values / Notes |
|-------|------|---------|------------------------|
| `endpoint` | string | — | Full API URL. |
| `api_key` | string | — | Sent as `Authorization: Bearer ...` or the format-specific header. |
| `headers` | object | `{}` | Extra HTTP headers. |
| `format` | string | `openai` | One of `openai`, `nanoclaw`, `dify`. Unknown values fall back to `openai`. |
| `max_history` | int | `20` | Only used by `openai` format. |
| `sender` | string | — | Only used by `nanoclaw` format. |
| `context_mode` | string | — | Only used by `nanoclaw` format; forwarded verbatim to upstream. |
| `group_jid` | string | — | Only used by `nanoclaw` format. |
| `timeout` | int (seconds) | `120` | HTTP client timeout. `<= 0` falls back to default. |

### CLI-specific (`type: cli`)

CLI agents have no extra fields beyond the common ones (`command`, `args`,
`cwd`, `env`, `model`, `system_prompt`).
