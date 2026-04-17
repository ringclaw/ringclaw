---
title: 配置
---

# 配置

配置文件：`~/.ringclaw/config.json`

> **仅 config.json**：所有配置均来自 `~/.ringclaw/config.json`。此前支持的环境变量
> （`RC_*`、`RINGCLAW_*`、`OPENCLAW_GATEWAY_*`）已被完全废弃并**静默忽略**。
> 使用 `ringclaw setup` 交互式生成文件，或直接手动编辑。

## 完整配置示例

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

## 顶层字段

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `default_agent` | string | 自动检测 | `agents` 中任意 key；为空时按优先级 `claude, codex, cursor, kimi, gemini, opencode, openclaw, pi, copilot, droid, iflow, kiro, qwen, augment` 自动选择。 |
| `agent_workspace` | string | — | 绝对路径。Agent 启动时的默认工作目录，会被隐式加入 cwd 白名单。 |
| `agent_allow_workspace_list` | string[] | `[]` | 绝对路径数组。`/cwd` 与 `Agent.SetCwd` 仅能切换到此白名单内的目录。`~/.ringclaw/workspace` 与 `agent_workspace` 会自动合并。 |
| `api_addr` | string | `127.0.0.1:18010` | `host:port`。**切勿绑定 `0.0.0.0`**，详见[安全文档](../security/index.md)。 |
| `log_level` | string | `info` | `debug`、`info`、`warn`（别名 `warning`）、`error`。 |
| `log_format` | string | `text` | `text`、`json`、`color`。 |
| `full_access_ack` | bool（可空） | 未设置 | `true` 允许 ACP agent 启用 `full_access: true`；`false` 明确拒绝；未设置按 `false` 处理。 |
| `agents` | object | — | 见 [Agents](#agents)。至少需要 1 个 agent。 |
| `ringcentral` | object | — | 见 [`ringcentral`](#ringcentral)。 |
| `heartbeat` | object | — | 见 [`heartbeat`](#heartbeat)。 |
| `cron` | object | — | 见 [`cron`](#cron)。 |
| `openclaw_gateway` | object | — | 见 [`openclaw_gateway`](#openclaw_gateway)。 |

## `ringcentral`

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `bot_token` | string | — | **必填。** RingCentral Public Bot 的访问 token。 |
| `chat_ids` | string[] | `[]` | **必填。** Bot 允许接收消息的 chat ID 列表。 |
| `source_user_ids` | string[] | `[]` | 受信任的发信人。可填数字 extension ID、邮箱、E.164 电话号码。空且已配置 Private App → 仅机主；空且未配置 Private App → 全部拒绝。 |
| `bot_mention_only` | bool（可空） | `true` | `true` 群聊中必须 `@bot`；`false` 在允许的 chat 中对所有消息响应。 |
| `server_url` | string | SDK 默认（`https://platform.ringcentral.com`） | RingCentral API 地址。 |
| `client_id` | string | — | Private App Client ID。Private App 三个字段需一起填。 |
| `client_secret` | string | — | Private App Client Secret。日志中会脱敏。 |
| `jwt_token` | string | — | Private App JWT token。日志中会脱敏。 |
| `group_summary_group_id` | string | — | 允许使用"当前群总结"功能的群 ID。留空即禁用。 |
| `group_summary_message_limit` | int | `200` | 每次群总结拉取的消息条数。`<= 0` 回退默认值。 |

## `heartbeat`

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `enabled` | bool | `false` | 总开关。 |
| `interval` | string | `30m` | Go duration：`45s`、`30m`、`2h`。必须为正。 |
| `active_hours` | string | — | `HH:MM-HH:MM`（小时 `00`-`23`、分钟 `00`-`59`）。跨零点写 `22:00-06:00`；空为 24 小时。 |
| `timezone` | string | 宿主机本地时区 | IANA 时区，如 `Asia/Shanghai`、`UTC`。 |

## `cron`

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `enabled` | bool | 存在 cron job 时默认 `true` | 调度器总开关。 |

## `openclaw_gateway`

自动发现 `openclaw` agent 时使用的网关连接信息。当 `url` 为空时，RingClaw 会
回退读取 `~/.openclaw/openclaw.json`。

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `url` | string | — | `ws://`、`wss://`、`http://`、`https://` 任一。 |
| `token` | string | — | Bearer token，用于 token 鉴权模式。 |
| `password` | string | — | 密码，用于 password 鉴权模式。 |

## Agents

`agents` 下的 key 是 agent 名称（如 `claude`、`codex`、`gpt`），值是
[`AgentConfig`](#agentconfig-公共字段) 加上各类型专属字段。

### AgentConfig 公共字段

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `type` | string | — | **必填。** `acp` / `cli` / `http`。 |
| `command` | string | — | `acp`/`cli` 必填。绝对路径或 PATH 中的名字。 |
| `args` | string[] | `[]` | 命令的额外参数，如 `["acp"]`、`["--experimental-acp"]`。 |
| `aliases` | string[] | `[]` | 自定义触发词，如 `["gpt", "4o"]`。 |
| `cwd` | string | — | Agent 工作目录。必须满足 `agent_allow_workspace_list`。 |
| `env` | object | `{}` | 传递给 agent 子进程的额外环境变量（不影响 ringclaw 本身）。 |
| `model` | string | 各 agent 自定 | 模型名。默认：claude → `sonnet`；openclaw → `openclaw:main`；HTTP fallback → `gpt-4o-mini`。 |
| `system_prompt` | string | — | 会话启动时注入的 system prompt。 |

### ACP 专属（`type: acp`）

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `allow_write` | bool | `false` | 授予 ACP 文件写入能力（agent 可写文件）。 |
| `full_access` | bool | `false` | 每次新建 session 调用 `session/set_mode "full-access"`。需要顶层 `full_access_ack: true`，否则降级并打印 `WARN`。 |

### HTTP 专属（`type: http`）

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `endpoint` | string | — | 完整的 API URL。 |
| `api_key` | string | — | 作为 `Authorization: Bearer ...` 或 format 指定的 header。 |
| `headers` | object | `{}` | 额外 HTTP header。 |
| `format` | string | `openai` | `openai`、`nanoclaw`、`dify`；未知值退回 `openai`。 |
| `max_history` | int | `20` | 仅 `openai` 格式使用。 |
| `sender` | string | — | 仅 `nanoclaw` 格式使用。 |
| `context_mode` | string | — | 仅 `nanoclaw` 格式使用；原样转发。 |
| `group_jid` | string | — | 仅 `nanoclaw` 格式使用。 |
| `timeout` | int（秒） | `120` | HTTP 客户端超时；`<= 0` 回退默认值。 |

### CLI 专属（`type: cli`）

CLI agent 除公共字段（`command`、`args`、`cwd`、`env`、`model`、`system_prompt`）外无额外字段。
