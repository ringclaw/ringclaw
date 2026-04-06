---
title: 配置文件
---

# 配置

配置文件路径：`~/.ringclaw/config.json`

## 完整配置示例

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

## 环境变量

| 变量 | 说明 |
|------|------|
| `RC_BOT_TOKEN` | Bot App Token（必需） |
| `RC_CLIENT_ID` | Private App Client ID（可选，启用 Summarize） |
| `RC_CLIENT_SECRET` | Private App Client Secret（可选） |
| `RC_JWT_TOKEN` | Private App JWT 凭据（可选） |
| `RC_SERVER_URL` | RingCentral 服务器 URL（默认：`https://platform.ringcentral.com`） |
| `RINGCLAW_DEFAULT_AGENT` | 覆盖默认 Agent |
| `OPENCLAW_GATEWAY_URL` | OpenClaw HTTP 回退地址 |
| `OPENCLAW_GATEWAY_TOKEN` | OpenClaw API Token |
| `RINGCLAW_API_ADDR` | 更改 HTTP API 监听地址（默认：`127.0.0.1:18011`） |

## 配置字段

### 顶层字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `default_agent` | string | 默认 AI Agent 名称 |
| `agent_workspace` | string | 所有 Agent 的默认工作目录 |

### `ringcentral` 部分

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `bot_token` | string | — | Bot App Token（必需） |
| `chat_ids` | string[] | `[]` | 要监控的 Chat ID 列表 |
| `source_user_ids` | string[] | `[]` | 限制响应的用户（ID 或邮箱） |
| `bot_mention_only` | bool | `true` | 群聊中仅在被 @mention 时响应 |
| `group_summary_group_id` | string | — | 允许群内总结的群 ID |
| `group_summary_message_limit` | int | `200` | 群内总结拉取的消息数 |
| `server_url` | string | `https://platform.ringcentral.com` | RingCentral API 服务器 URL |
| `client_id` | string | — | Private App Client ID |
| `client_secret` | string | — | Private App Client Secret |
| `jwt_token` | string | — | Private App JWT Token |

### Agent 配置

| 字段 | 类型 | 说明 |
|------|------|------|
| `type` | string | `acp`、`cli` 或 `http` |
| `command` | string | Agent 二进制路径（ACP/CLI） |
| `endpoint` | string | API 端点 URL（HTTP） |
| `api_key` | string | API 密钥（HTTP） |
| `model` | string | 模型名称 |
| `format` | string | API 格式：`openai`、`nanoclaw` 或 `dify`（HTTP） |
| `cwd` | string | 该 Agent 的工作目录覆盖 |
| `args` | string[] | 额外的 CLI 参数 |
| `aliases` | string[] | 自定义触发别名 |
| `env` | object | Agent 进程的环境变量 |
| `allow_write` | bool | 允许 ACP Agent 写入文件（默认：false） |

### `heartbeat` 部分

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 启用心跳 |
| `interval` | string | `30m` | 心跳间隔 |
| `active_hours` | string | — | 仅在此时段运行（如 `09:00-18:00`） |
| `timezone` | string | 本地 | 活跃时段的时区（IANA 格式） |
