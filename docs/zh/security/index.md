---
title: 安全
---

# 安全

## 本地 API 认证

HTTP API 服务（默认 `127.0.0.1:18011`）需要 Token 认证。首次启动时自动生成随机 Token，存储在 `~/.ringclaw/api_token`。

除 `/health` 外，所有 API 请求必须携带 `X-RingClaw-Token` 请求头：

```bash
curl -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  http://127.0.0.1:18011/api/send -d '{"text":"hello"}'
```

服务还会验证 `Host` 请求头以防止 DNS 重绑定攻击 — 仅接受 `localhost`、`127.0.0.1` 和 `::1`。

::: danger
不要将 `config.json` 中的 `api_addr` 设为 `0.0.0.0`，这会将未加密的企业 RingCentral 账号网关暴露在局域网中。默认的 `127.0.0.1` 绑定已满足所有正常使用场景。
:::

## ACP Agent 文件权限

默认情况下，ACP Agent 仅获得**只读**文件访问权限。如需允许文件写入，请在 Agent 配置中设置 `allow_write: true`：

```json
"claude-acp": {
  "type": "acp",
  "command": "claude-agent-acp",
  "allow_write": true
}
```

## 工作目录路径限制

`/cwd` 命令会阻止进入敏感目录：`.ssh`、`.gnupg`、`.ringclaw`、`.aws`、`.kube`、`.config/gcloud`。

## 权限矩阵

| 操作 | Bot 私聊 | Bot 群聊 (owner) | Bot 群聊 (非 owner) |
|---|---|---|---|
| 与 Agent 聊天 | Bot 回复 | Bot 回复 | Bot 回复 |
| 总结 (Summarize) | Private App 读, Bot 回复 | **禁止** (防泄露) | **禁止** (防泄露) |
| 总结 (无 Private App) | **不可用** | **不可用** | **不可用** |
| `/clear`、`/new` | 允许 | 允许 | **禁止** (仅 owner) |
| `/cwd` | 允许 | 允许 | **禁止** (仅 owner) |
| 切换 Agent (`/cc`) | 允许 | 允许 | **禁止** (仅 owner) |
| `/info`、`/help` | 允许 | 允许 | 允许 |
| `/task`、`/note`、`/event` | Private App 或 Bot | Private App 或 Bot | Private App 或 Bot |
| ACTION blocks | Private App 或 Bot | Private App 或 Bot | Private App 或 Bot |

## 客户端职责

| 职责 | 使用的客户端 | 原因 |
|------|------------|------|
| WebSocket 连接 | Bot App | Bot Token 驱动 WS 连接 |
| 发送回复和占位消息 | Bot App | 所有聊天中使用 Bot 身份 |
| 读取其他聊天和总结 | Private App（可选） | Bot 无法访问他人的私聊 |
| `/task`、`/note`、`/event` API | Private App（如有），否则 Bot | Private App 有更广的访问权限 |
| ACTION block 执行 | Private App（如有），否则 Bot | 跨聊天操作需要 Private App |

## Bot App 与 Private App 权限对比

两种客户端拥有不同的 RingCentral API 权限。了解这些差异可以帮助你决定是否需要配置 Private App。

**Bot App** 自动获得 `TeamMessaging` 权限。**Private App**（REST API + JWT）可以被授予 `TeamMessaging` + `ReadAccounts` 权限。

| 功能 | API 端点 | 所需权限 | Bot App | Private App |
|------|---------|---------|---------|-------------|
| 发送 / 更新 / 删除帖子 | `/team-messaging/v1/chats/{chatId}/posts` | TeamMessaging | 支持 | 支持 |
| 列出 / 管理聊天 | `/team-messaging/v1/chats` | TeamMessaging | 支持 | 支持 |
| 上传文件 | `/team-messaging/v1/files` | TeamMessaging | 支持 | 支持 |
| 任务 CRUD | `/team-messaging/v1/tasks` | TeamMessaging | 支持 | 支持 |
| 笔记 CRUD | `/team-messaging/v1/notes` | TeamMessaging | 支持 | 支持 |
| 日历事件 CRUD | `/team-messaging/v1/events` | TeamMessaging | 支持 | 支持 |
| 自适应卡片 CRUD | `/team-messaging/v1/adaptive-cards` | TeamMessaging | 支持 | 支持 |
| 获取用户信息 | `/team-messaging/v1/persons/{id}` | TeamMessaging | 支持 | 支持 |
| 创建会话（私聊） | `/team-messaging/v1/conversations` | TeamMessaging | 支持 | 支持 |
| 获取自身扩展信息 | `/restapi/v1.0/account/~/extension/~` | （自身信息） | 支持 | 支持 |
| **搜索公司目录** | `/restapi/v1.0/account/~/directory/entries/search` | **ReadAccounts** | **不支持** | 支持 |

### 需要 Private App 的功能

| 功能 | 没有 Private App 时的表现 |
|------|------------------------|
| 总结会话 | 禁用 — Bot 无法读取其他用户的聊天 |
| ACTION block 中的姓名解析（`chatid=张三`、`assignee=李四`） | 失败 — 无法按姓名查找用户 |
| 基于邮箱的 `source_user_ids`（`alice@example.com`） | 忽略 — 无法将邮箱解析为用户 ID |
| 跨聊天操作（在其他聊天中创建任务/笔记） | 仅限 Bot 所在的聊天 |

::: tip
如果只需要基本的消息和 Agent 交互，Bot App 就足够了。当你需要总结会话、姓名解析或跨聊天功能时，再添加 Private App。
:::
