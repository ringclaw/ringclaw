---
title: API 与客户端
---

# API 与客户端

这一页覆盖本地 HTTP API 服务（绕过第零至第二层的入口），以及
决定 bot 在 RingCentral 一侧能做什么的双客户端模型。

## 本地 API 认证

HTTP API 服务（默认 `127.0.0.1:18011`）需要 Token 认证。首次
启动时自动生成随机 Token，存储在 `~/.ringclaw/api_token`。

除 `/health` 外，所有 API 请求必须携带 `X-RingClaw-Token` 请
求头：

```bash
curl -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  http://127.0.0.1:18011/api/send -d '{"text":"hello"}'
```

服务还会验证 `Host` 请求头以防止 DNS 重绑定攻击 —— 仅接受
`localhost`、`127.0.0.1` 和 `::1`。

::: danger
不要将 `config.json` 中的 `api_addr` 设为 `0.0.0.0`，这会将未
加密的企业 RingCentral 账号网关暴露在局域网中。默认的
`127.0.0.1` 绑定已满足所有正常使用场景。
:::

::: danger API token = 机器操作者
任何能读到 `~/.ringclaw/api_token` 的人都会 **直接绕过第零至
第二层** —— 可向任意聊天发送文本/媒体，也可通过 `/api/...` 创
建 / 删除任意 task/note/event/card。也可以通过
`/api/oob/approve` 批准任意 pending OOB challenge。请像对待
SSH 私钥一样对待 token 文件。默认的 loopback 绑定
（`api_addr: 127.0.0.1:18011`）把影响面限制在本机同机进程。
:::

## 审批 CLI 调用本地 API

`ringclaw approval <id>` 读取 `~/.ringclaw/api_token`，再调用
本地 API 服务（loopback-only、token 鉴权）：

- `POST /api/oob/approve`
- `POST /api/oob/deny`
- `GET /api/oob/list`

审批要求能访问运行 `ringclaw` 的主机。这正是把审批权与
RingCentral 账号解耦的属性——完整动机见
[审批 CLI](./approval-cli)。

## 客户端职责

RingClaw 可以同时使用 **两类不同的 RingCentral 客户端**：始终
必备的 Bot App 与可选的 Private App。不同职责按各客户端拥有
的 API 权限路由到合适的 client。

| 职责 | 使用的客户端 | 原因 |
|------|------------|------|
| WebSocket 连接 | Bot App | Bot Token 驱动 WS 连接 |
| 发送回复和占位消息 | Bot App | 所有聊天中使用 Bot 身份 |
| 读取其他聊天和总结 | Private App（可选） | Bot 无法访问他人的私聊 |
| `/task`、`/note`、`/event` API | Private App（如有），否则 Bot | Private App 有更广的访问权限 |
| ACTION block 执行 | Private App（如有），否则 Bot | 跨聊天操作需要 Private App |

## Bot App 与 Private App 权限对比

两种客户端拥有不同的 RingCentral API 权限。了解这些差异可以
帮助你决定是否需要配置 Private App。

**Bot App** 自动获得 `TeamMessaging` 权限。**Private App**
（REST API + JWT）可以被授予 `TeamMessaging` + `ReadAccounts`
权限。

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

## 需要 Private App 的功能

| 功能 | 没有 Private App 时的表现 |
|------|------------------------|
| 总结会话 | 禁用 — Bot 无法读取其他用户的聊天 |
| ACTION block 中的姓名解析（`chatid=张三`、`assignee=李四`） | 失败 — 无法按姓名查找用户 |
| 基于邮箱的 `source_user_ids`（`alice@example.com`） | 忽略 — 无法将邮箱解析为用户 ID |
| 基于电话的 `source_user_ids`（`+15551234567`） | 忽略 — 无法将电话解析为用户 ID |
| 跨聊天操作（在其他聊天中创建任务/笔记） | 仅限 Bot 所在的聊天 |
| Authorize-mention OOB 流程（`allow_group_mention_authorize`） | 启动时禁用。显式 `true` 时打 `ERROR ...`，未设置（v0.5.0+ 默认开启）时打 `INFO ...`；两种情况下非授信 `@bot` 都回退为静默丢弃。 |
| Bot 私聊里的"仅 owner"第一层门控 | 退化为"任意 trusted sender"——详见 [命令授权](./command-authorization) |

::: tip
如果只需要基本的消息和 Agent 交互，Bot App 就足够了。当你需
要总结会话、姓名解析、跨聊天功能或任意 OOB 加固入口
（`/full-access`、authorize-mention）时，再添加 Private App。
:::
