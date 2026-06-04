---
title: 配置
---

# 配置

配置文件：`~/.ringclaw/config.json`

> **仅 config.json**：所有配置均来自 `~/.ringclaw/config.json`。此前支持的环境变量
> （`RC_*`、`RINGCLAW_*`、`OPENCLAW_GATEWAY_*`）已被完全废弃并**静默忽略**。
> 使用 `ringclaw setup` 交互式生成文件，或直接手动编辑。

RingClaw 启动要求同时配置 `ringcentral.bot_token`，以及 Private JWT App 字段
`ringcentral.client_id`、`ringcentral.client_secret`、`ringcentral.jwt_token`。
JWT App 至少需要 `ReadAccounts`；Video/Phone 能力还需要额外的 `Video`、
`RingOut`、`ReadCallLog` scopes。

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
    "group_mention_only": true,
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

  "persona": {
    "enabled": true,
    "soul_file": "~/.ringclaw/SOUL.md",
    "memory_dir": "~/.ringclaw/memory",
    "max_soul_chars": 2000,
    "max_chat_memory_chars": 4000,
    "max_user_memory_chars": 2000,
    "max_global_memory_chars": 2000
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
| `api_addr` | string | `127.0.0.1:18010` | `host:port`。**切勿绑定 `0.0.0.0`**，详见 [安全 › API 与客户端](../security/api-and-clients.md)。 |
| `log_level` | string | `info` | `debug`、`info`、`warn`（别名 `warning`）、`error`。 |
| `log_format` | string | `text` | `text`、`json`、`color`。 |
| `full_access_ack` | bool（可空） | 未设置 | `true` 允许 ACP agent 启用 `full_access: true`；`false` 明确拒绝；未设置按 `false` 处理。 |
| `agents` | object | — | 见 [Agents](#agents)。至少需要 1 个 agent。 |
| `ringcentral` | object | — | 见 [`ringcentral`](#ringcentral)。 |
| `heartbeat` | object | — | 见 [`heartbeat`](#heartbeat)。 |
| `cron` | object | — | 见 [`cron`](#cron)。 |
| `openclaw_gateway` | object | — | 见 [`openclaw_gateway`](#openclaw_gateway)。 |
| `persona` | object | — | 可选。在每条用户消息前注入 SOUL + 分层 MEMORY banner。见 [`persona`](#persona)。 |

## `ringcentral`

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `bot_token` | string | — | **必填。** RingCentral Public Bot 的访问 token。 |
| `chat_ids` | string[] | `[]` | Bot 允许接收消息的 chat ID 列表。bot 自己的私聊会单独自动放行。留空时，未列出的群聊默认拒绝；只有显式开启 `allow_unlisted_group_chats` 才会放开。 |
| `allow_unlisted_group_chats` | bool（可空） | `false` | 设为 `true` 后，RingClaw 保留 `chat_ids` 对显式列出聊天和 bot 私聊路由的作用，但也会接收 bot 已加入的任意非 bot-DM 聊天消息。`source_user_ids` / `chat_user_allow` 与 `group_mention_only` 仍然照常生效。 |
| `source_user_ids` | string[] | `[]` | 受信任的发信人。可填数字 extension ID、邮箱、E.164 电话号码。空值默认 owner-only，并要求已配置 Private App 凭据。 |
| `capabilities` | string[] | `[]` | `ringclaw onboard --capability ...` 写入的能力选择元数据。支持：`video`、`phone`、`call_log`。Private JWT App 默认必须具备 `ReadAccounts`；选中的能力还需要对应 scope（`Video`、`RingOut`、`ReadCallLog`）。 |
| `allow_group_mention_authorize` | bool（可空） | `false`（v0.4.2 起；v0.4.1 默认开启的改动已撤回） | 控制 OOB 审批流程。**默认关闭**，必须显式设为 `true` 才生效。开启后，非授信用户在允许群聊里 `@bot` 会在 owner 私聊弹出 `/approval`；批准后该用户被加入 `chat_user_allow[<chatID>]`（仅作用于该群）并持久化到 `config.json`。需要 Private App + 可解析的 owner 私聊；缺失时禁用功能并打 ERROR。拒绝 / 过期之后，同一 `(chat, user)` 进入 24 小时冷却期。v0.4.3+ 被批准用户运行在非-owner ceiling 下（不可调 `fs/*` / `terminal/*` / `session/request_permission`；文字回复 + RC ACTION 块仍可用）。详见 [安全 › 群成员 OOB 审批的完整启用步骤](../security/sender-allowlist.md#群成员-oob-审批的完整启用步骤)。 |
| `chat_user_allow` | map[string][]string | `{}` | 在 `source_user_ids` 之上叠加的**按群**白名单：`{"<numericChatID>": ["alice@example.com", "3061708020"]}`。map 的 **key 必须是 RC 数字 chat / group ID**——参考 [安全 › 如何获取 chat ID](../security/sender-allowlist.md#如何获取-chat-id)。条目可为数字 extension ID、邮箱或 E.164 电话号码（启动时通过 Private App directory 解析为数字 ID）。**v0.4.4+：重启后保留。** 被列入的用户按 v0.4.3 非-owner 上限运行（只读 ACP mode + fail-closed 拒绝 `fs/*` / `terminal/*` / `session/request_permission`）；文字回复 + RC ACTION 块仍可用。**仅放宽 Layer 0 发信人白名单**，不会解锁特权 Layer 1 命令（`/cwd`、`/cron`、`/new`、`/reload`、`/full-access`、总结自然语言触发）。 |
| `group_mention_only` | bool（可空） | `true` | `true` 群聊中必须 `@bot`（Bot 私聊不受影响）；`false` 在允许的群聊中对所有消息响应。 |
| `bot_mention_only` | bool（可空） | — | **已废弃**，`group_mention_only` 的旧名。仍会被读取（向后兼容），`Load()` 会把它迁移到新字段并打 `WARN`。未来版本将移除。 |
| `server_url` | string | SDK 默认（`https://platform.ringcentral.com`） | RingCentral API 地址。 |
| `client_id` | string | — | **必填。** Private JWT App Client ID，必须与 `client_secret`、`jwt_token` 一起配置。 |
| `client_secret` | string | — | **必填。** Private JWT App Client Secret。日志中会脱敏。 |
| `jwt_token` | string | — | **必填。** Owner-scoped Private JWT App token。日志中会脱敏。 |
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

## `persona`

可选功能。启用后 RingClaw 会在每条 WebSocket 用户消息之前注入一段
**persona + memory banner**。banner 由本地文件拼装，切换 agent
（`/cc` → `/cx`）或重置 session（`/new`）都不会丢失 operator 精心
维护的上下文。

三层 memory scope（默认都为空，用 `/mem add` 命令写入后才会出现）：

| 范围 | 文件 | 适合放什么 |
|------|------|------------|
| `global` | `<memory_dir>/global.md` | 所有聊天共享的事实（团队约定、代码库规则） |
| `user` | `<memory_dir>/user/<userID>.md` | 跟随某个用户跨聊天的偏好 |
| `chat` | `<memory_dir>/chat/<chatID>.md` | 特定聊天的事实（冲刺目标、项目上下文） |

SOUL.md 在首次启动时如不存在会自动写一份极简中立模板——编辑它来
定义 assistant 的人格。

| 字段 | 类型 | 默认值 | 可选值 / 说明 |
|------|------|--------|---------------|
| `enabled` | bool（可空） | `true` | 设为 `false` 彻底关闭 banner 注入（slash 命令会回"功能已禁用"的提示）。 |
| `soul_file` | string | `~/.ringclaw/SOUL.md` | SOUL 文件路径。支持 `~/` 展开。 |
| `memory_dir` | string | `~/.ringclaw/memory` | memory 树根目录，子项 `global.md`、`user/*.md`、`chat/*.md`。 |
| `max_soul_chars` | int | `2000` | SOUL 截断上限。`≤ 0` 表示不截断。 |
| `max_chat_memory_chars` | int | `4000` | 每个 chat memory 上限。超出保留最新（tail）。 |
| `max_user_memory_chars` | int | `2000` | 每个 user memory 上限。 |
| `max_global_memory_chars` | int | `2000` | global memory 上限。 |

通过聊天命令管理 banner：

```text
/mem add 项目采用 Go 1.25 和 TypeScript strict-mode    # → 当前 chat memory
/mem add user  偏好简洁中文回复                         # → user memory
/mem add global 工程团队一共 8 人                        # → 跨聊天 memory
/mem show                  # 查看当前 chat memory
/mem show user             # 查看 user memory
/mem del                   # 给出文件路径、大小、最后几行预览 + /new 提示
/mem del confirm           # 真正清空（不可撤销；不会自动重置 agent session）
/persona                   # 查看 SOUL.md（此处只读；编辑请直接改文件）
```

`/mem add` 和 `/mem del` 是特权命令（与 `/cron` 同一道 Layer 1 门
控）；`/mem show` 和 `/persona` 只读，任何 trusted sender 都可用。
`/mem del` **不会** 自动重置 agent session——下次消息时 banner 会从
磁盘重新拼装，但当前正在运行的 session 仍然带着旧 memory 上下文。
如果想让在线 agent 也立刻"忘掉"，清空后再发一条 `/new`。
Cron / heartbeat / HTTP API **不会** 被注入 banner——仅限 WebSocket
用户消息路径。详见 [安全 › 权限矩阵](../security/index.md#权限矩阵)。

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
| `restricted_mode_id` | string | 每家 agent 自带默认 | v0.4.3+：应用到非-owner 会话的 ACP modeID（fail-closed 隔离用）。默认：`droid → spec`；`claude / gemini / qwen / cursor-agent → plan`；其余走启发式（在 `availableModes` 中匹配 `plan` / `spec` / `read` / `safe`）。override 必须命中 agent 自己 `availableModes` 中的某一个，否则仍走内置选择。两条路径都无候选时，非-owner 消息直接被拒。详见 [发送者白名单 › 安全公告（v0.4.2 → v0.4.3）](../security/sender-allowlist#authorize-mention-oob-流程)。 |

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
