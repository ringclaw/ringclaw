---
title: 安全
---

# 安全

RingClaw 是把 RingCentral Team Messaging 远程驱动到一台运行 AI
agent（具备较广的文件系统与 shell 权限）主机上的桥接器。下面的
威胁模型与防御层次说明项目如何通过配置、命令授权、OOB 审批与
进程内本地状态把这些权能限定在合适的人手里。

## 快速导航

| 你想做什么 | 阅读 |
|---|---|
| 决定谁能驱动 bot | [发送者白名单](./sender-allowlist) |
| 看每个用户能用哪些斜杠命令 | [命令授权](./command-authorization) |
| 理解跨聊天 ACTION 门控 | [跨聊天 Action](./cross-chat-actions) |
| 使用 `full_access` 或 `/full-access grant` | [ACP Full-Access](./full-access) |
| 在主机上批准 OOB challenge | [审批 CLI](./approval-cli) |
| 锁定 agent 工作目录 | [工作目录白名单](./workspace-allowlist) |
| 锁定本地 HTTP API、对比 Bot vs Private App | [API 与客户端](./api-and-clients) |

## 威胁模型

每一层防御共享同一个信任假设：拥有 RingCentral 内部立足点的
攻击者（账号被盗、bot 私聊里的 prompt 注入、模仿 owner 的恶意
同事）**并不**同时拥有运行 RingClaw 主机的 shell。

正是这个假设让 OOB 审批方案站得住脚：

- 所有审批（`/full-access grant`、非 owner 跨聊天 ACTION、
  authorize-mention）都需要在主机上执行
  `ringclaw approval <id>`。
- 被盗 RC 账号能从 owner 私聊看到 challenge ID，但没有 SSH 或
  物理访问就无法执行审批。
- 全部 OOB 状态都是进程内内存，重启即清空——意外崩溃后 bot 自
  然回到锁定状态，运维必须重新显式授权。

如果主机本身在 OS 层被攻陷，RingClaw 帮不上忙——API token、
配置文件以及 agent secrets 已经是攻击者能直接读到的。值得问的
反而是：相比已经攻陷主机的攻击者，RingClaw 是否引入了新的攻击
面？下面的分层模型给出的回答是"否"——所有特权入口要么只能改
配置、要么必须有主机权限。

## 四个入口（不止一个）

RingClaw 可以通过 **四个不同入口** 操作 RingCentral。下一节的
三层权限模型 **只覆盖 WebSocket 消息路径**，其他三条入口各有
自己的门控。这里专门列出来，避免运维把"群聊非 owner 用不了
`/cwd`"等同于"非 owner 无法让 bot 做任何事"：

| 入口 | 第零层（sender） | 第一层（命令） | 第二层（ACTION 派发） | 第三层（ACP 模式） | 实际门控 |
|---|---|---|---|---|---|
| WebSocket 消息 | 是 | 是 | 是 | 是 | Chat allowlist + sender allowlist + handler 检查 |
| HTTP API（`/api/send`、`/api/tasks`、`/api/notes`、`/api/events`、`/api/cards`） | 否 | 否 | 否 | n/a | **仅 API token + loopback Host**（`api/auth.go`）；见 [API 与客户端](./api-and-clients) |
| Cron 定时任务 | 否 | 任务通过 `/cron add`（第一层）创建；执行时无人类 sender | 否 — **ACTION 块不执行**；回复原样发出 | 是 | 任务配置在 `~/.ringclaw/cron/jobs.json` |
| 心跳（Heartbeat） | 否 | n/a（配置驱动） | 否 — **ACTION 块不执行** | 是 | `heartbeat.enabled` + `HEARTBEAT.md` |

::: danger API token = 机器操作者
任何能读到 `~/.ringclaw/api_token` 的人都会 **直接绕过第零至
第二层** —— 可向任意聊天发送文本/媒体，也可通过 `/api/...` 创建
/ 删除任意 task/note/event/card。请像对待 SSH 私钥一样对待 token
文件。默认的 loopback 绑定（`api_addr: 127.0.0.1:18011`）把影响面
限制在本机同机进程。
:::

::: tip Chat allowlist 是最外层护栏
**第 -1 层**：不在 `ringcentral.chat_ids` 中的聊天消息会被
WebSocket monitor 直接丢弃，连第零层都到不了
（`ringcentral/monitor.go`）。如果发现消息被静默吞掉，优先检查
chat allowlist——日志会打印 `ignoring message from non-allowed
chat`。
:::

## 权限矩阵

WebSocket 消息路径通过三层正交门控处理。消息必须逐层通过才能
生效。

| 层 | 回答的问题 | 详细页 |
|---|---|---|
| 0 | 这个发送者能不能驱动 bot？ | [发送者白名单](./sender-allowlist) |
| 1 | 这个发送者能不能在这种场景里运行这条命令？ | [命令授权](./command-authorization) |
| 2 | AI 生成的 ACTION 能不能扩散到其他聊天？ | [跨聊天 Action](./cross-chat-actions) |
| 3 | ACP session 内能读 / 写 / 执行什么？ | [ACP Full-Access](./full-access) |

::: tip Full access 只影响第三层
`/full-access` 授权（以及静态配置 `full_access: true`）只改变
ACP **会话模式**。它不会解锁任何聊天命令——非 owner 在群聊中仍
然无法使用 `/cwd`，`/full-access` 本身也仍然只在私聊中可用。它
同样不会放宽第二层的跨聊天 fail-closed 通知机制。
:::

::: warning 私聊是信任边界，不是"仅 owner"
第一层的"仅 owner"门控（`/cwd`、`/cron`、`/new`、`/reload`、
总结自然语言触发）在 **群聊** 里始终生效。在 **Bot 私聊** 里，
只有配置了 Private App 才会生效——此时特权命令只允许
Private App owner 运行，即便是其他 trusted sender 在自己的私聊
里也不行。没有 Private App 时，RingClaw 无法区分"owner"和
"另一个 trusted sender 的私聊"，所以每个 trusted sender 都拥有
自己私聊中的特权命令权限。如果你在 `ringcentral.source_user_ids`
里列了多人又没配 Private App，就等于给所有人同等信任——包括
`/cron add`，它可以在发送者离开后长期运行任意 prompt。
:::

### 第零层 提示

每条消息首先通过 **两重** trusted-sender allowlist
（[`ringcentral/monitor.go`](https://github.com/ringclaw/ringclaw/blob/main/ringcentral/monitor.go)
在 socket 层、
[`messaging/handler.go`](https://github.com/ringclaw/ringclaw/blob/main/messaging/handler.go)
在 handler 层，属于纵深防御）。不在白名单中的发送者消息会被
直接丢弃，下面的层次都不适用。

第零层的集合是 `ringcentral.source_user_ids`、自动注入的
Private App owner ID，以及目标聊天的
`ringcentral.chat_user_allow[<chatID>]`（若有）**三者的并集**。
authorize-mention 批准的用户写入 `chat_user_allow`——它**只放宽
第零层**，绝不会触及下面任何一层。详见
[发送者白名单](./sender-allowlist)。

## 升级时值得复核的配置项

从旧版本升级的运维在重启前应核对以下配置项：

| 配置项 | 位置 | 需要确认的行为 |
|---|---|---|
| `ringcentral.source_user_ids` | `config.json` | 空列表 + Private App = **仅 owner**（自动注入）。空列表 + 无 Private App = **拒绝所有**，启动报错。详见 [发送者白名单](./sender-allowlist)。 |
| `agent_workspace` | `config.json` | 仍是默认 cwd，并且会被隐式加入 cwd allowlist。详见 [工作目录白名单](./workspace-allowlist)。 |
| `agent_allow_workspace_list` | `config.json` | 显式列出 `/cwd` 与 `Agent.SetCwd` 可以切换到的目录。始终与 `~/.ringclaw/workspace`（以及 `agent_workspace`，若有）合并。 |
| `agents.<name>.full_access` | `config.json`（每个 ACP agent） | `true` 仅当顶层同时有 `full_access_ack: true` 才生效；否则降级并打 `WARN` 日志。详见 [ACP Full-Access](./full-access)。 |
| `full_access_ack` | `config.json`（顶层） | `true` 才会真正接受 `full_access`，`false` 或省略均拒绝。 |
| `ringcentral.allow_group_mention_authorize` | `config.json`（在 `ringcentral` 下） | **v0.4.1 起默认开启**（未设置即开启）。非授信用户在允许群聊里 `@bot` 会触发 OOB 审批 challenge 而非静默丢弃。设为 `false` 保留旧的静默丢弃行为。拒绝 / 过期后，同一 `(chat, user)` 进入 24 小时冷却期。需要 Private App + 可解析的 owner 私聊。 |
| `ringcentral.chat_user_allow` | `config.json`（在 `ringcentral` 下） | 按群叠加的发送者白名单。authorize-mention 批准时自动写入，也可手工预置。与 `allow_group_mention_authorize` 互相独立。 |

历史上支持的 `RC_*` / `RINGCLAW_*` / `OPENCLAW_GATEWAY_*` 环境
变量已**全部移除并被静默忽略**。所有配置都集中在
`~/.ringclaw/config.json`。

::: warning
升级后，曾经依赖 "空 `source_user_ids` = 允许所有人" 旧行为的
运维会发现 bot **丢弃所有** 收到的消息，直到他们 (a) 配置
Private App（owner 自动信任），或 (b) 显式填写
`ringcentral.source_user_ids`。启动日志中的
`sender allowlist is empty: ...` 是这种情况的标准信号。
:::
