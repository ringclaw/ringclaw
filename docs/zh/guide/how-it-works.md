---
title: 工作原理
---

# 工作原理

```mermaid
graph LR
    User[用户] -->|发送消息| RC[RingCentral]
    RC -->|WebSocket 事件| RingClaw
    RingClaw -->|路由到| Codex
    RingClaw -->|路由到| Claude[Claude Code]
    RingClaw -->|路由到| OpenClaw
    RingClaw -->|路由到| More[更多 Agent...]
    RingClaw -->|回复| RC
    RC -->|显示回复| User
```

RingClaw 通过 WebSocket 连接 RingCentral Team Messaging 实时接收消息。当消息到达时，路由到配置的 AI Agent 处理，然后将回复发回聊天。Agent 处理期间，会显示 "Thinking..." 占位消息，处理完成后更新为最终回复。

## 消息路由

每条消息经过去重、权限检查和多阶段路由：

```mermaid
flowchart TD
    Msg[收到消息] --> Dedup{去重检查}
    Dedup -->|重复| Drop[丢弃]
    Dedup -->|新消息| TS{可信发送者?<br/>source_user_ids ∪ chat_user_allow}
    TS -->|否| Mention{群聊 @bot &<br/>allow_group_mention_authorize?}
    Mention -->|是| OOBA[向 owner 私聊发<br/>authorize-mention OOB challenge]
    Mention -->|否| Drop2[丢弃]
    TS -->|是| Approval{Bot 私聊里的 /approval 回复?}
    Approval -->|是| OOB[交给 OOB manager 处理]
    Approval -->|否| Strip[去除 @mention + 转发前缀]
    Strip --> Priv{群聊 & 特权命令?}
    Priv -->|是, 非 owner| Block[拒绝]
    Priv -->|是, owner 或 DM| Cmd[执行内置命令]
    Priv -->|否| BuiltIn{内置命令?}
    BuiltIn -->|/help /info /new /cwd /cron /reload /full-access| Cmd
    BuiltIn -->|/task /note /event /card| CRUD[执行 CRUD]
    BuiltIn -->|否| Intent{Intent 触发词?}
    Intent -->|是| Classify[AI 分类 → 路由]
    Intent -->|否| Parse[解析 /agent 前缀]
    Parse --> HasAgent{有 agent 前缀?}
    HasAgent -->|无| Default[发给默认 Agent]
    HasAgent -->|1个| Named[发给指定 Agent]
    HasAgent -->|多个| Broadcast[并行广播]
```

## Agent 接入模式

| 模式 | 工作方式 | 支持的 Agent |
|------|---------|-------------|
| ACP  | 长驻子进程，通过 stdio JSON-RPC 通信。速度最快，复用进程和会话。 | Claude, Codex, Cursor, Kimi, Gemini, OpenCode, OpenClaw, Pi, Copilot, Droid, iFlow, Kiro, Qwen |
| CLI  | 每条消息启动一个新进程，支持通过 `--resume` 恢复会话。 | Claude (`claude -p`)、Codex (`codex exec`) |
| HTTP | OpenAI 兼容的 Chat Completions API。支持 `openai`（默认）、`nanoclaw`、`dify` 三种格式。 | OpenClaw、Dify Chatflow |

::: tip 自动检测
同时存在 ACP 和 CLI 时，自动优先选择 ACP。
:::

## 架构

RingClaw 使用 **Bot App**（必需）进行消息收发，可选的 **Private App** 提供高级功能。

```mermaid
graph LR
    RC[RingCentral] -->|WebSocket| BotApp[Bot App]
    BotApp -->|接收消息| RingClaw
    RingClaw -->|回复| BotApp
    RingClaw -.->|读取其他聊天| PrivateApp[Private App<br/>可选]
```

### 路由规则

当 `chat_ids` 非空时，不在 `chat_ids` 中的消息会被 monitor 直接丢弃；当
`chat_ids` 为空时，monitor 会接收所有 chat 的消息，再继续走后续 sender /
mention / command 权限层。

| 消息来源 | 回复客户端 | 读取/操作客户端 |
|----------|-----------|---------------|
| Bot 私聊（自动发现） | Bot | Private App（如已配置）或 Bot |
| `chat_ids` 中的聊天 | Bot | Private App（如已配置）或 Bot |
| `chat_ids` 为空时的任意聊天 | Bot | Private App（如已配置）或 Bot |

### 群聊行为

- **`group_mention_only: true`**（默认）— Bot 在群聊中只有被 @mention 时才响应（Bot 私聊不受影响）
- **`group_mention_only: false`** — Bot 响应允许群中的所有消息
- **`group_summary_group_id: "..."`** — 只有这个精确的群 ID 允许在群内触发总结
- **`group_summary_message_limit: 200`**（默认）— 开启群内总结后，先拉取当前群最近这么多条消息，再按时间范围过滤

只要配置了 `group_summary_group_id`，群内总结功能就会自动启用。只有当前群 ID 与该配置完全一致时，才允许在群内触发总结。

### 用户白名单

`source_user_ids` 限制 Bot 只响应指定用户的消息。支持数字用户 ID 或邮箱（启动时通过目录 API 自动解析为 ID）。不配置则响应所有用户。

```json
"ringcentral": {
  "source_user_ids": ["alice@example.com", "3061708020"]
}
```

用户数字 ID 可以从 RingClaw 日志中获取：收到消息时会打印 `creatorID=XXXXXXXX`。

`chat_user_allow` 是叠加在 `source_user_ids` 之上的**按群**白名单。可手工预置，也可让 [authorize-mention OOB 流程](../security/sender-allowlist.md#authorize-mention-oob-流程) 在运维批准后自动写入。批准的条目会持久化到 `config.json`（能解析到邮箱时优先存邮箱）。

```json
"ringcentral": {
  "allow_group_mention_authorize": true,
  "chat_user_allow": {
    "chat-engineering-7": ["alice@example.com"],
    "chat-design-9": ["3061708020"]
  }
}
```

`chat_user_allow` **仅放宽该群的发信人白名单**。特权 Layer 1 命令（`/cwd`、`/cron`、`/new`、`/reload`、`/full-access`、总结自然语言触发）仍要求 Private-App-owner 身份——见 [安全 › 权限矩阵](../security/index.md#权限矩阵)。
