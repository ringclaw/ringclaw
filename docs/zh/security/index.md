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

RingClaw 通过三层正交门控处理消息，消息必须逐层通过才能生效。

1. **第一层 — 聊天命令授权**：谁在哪种聊天中可以触发哪些斜杠命令。
2. **第二层 — AI 驱动的 ACTION 派发**：AI 回复中嵌入的 `ACTION:` 块能否执行（尤其是跨聊天场景）。
3. **第三层 — ACP 会话内能力**：ACP agent 在会话内可以读写什么、执行什么。

::: tip Full access 只影响第三层
`/full-access` 授权（以及静态配置 `full_access: true`）只改变 ACP **会话模式**。它不会解锁任何聊天命令——非 owner 在群聊中仍然无法使用 `/cwd`，`/full-access` 本身也仍然只在私聊中可用。它同样不会放宽第二层的跨聊天 fail-closed 通知机制。
:::

**第零层提示** — 每条消息首先经过 Phase 1 trusted-sender allowlist（`messaging/handler.go:444-448`）。不在允许列表中的发送者消息会被直接丢弃，以下三层均不适用。

### 第一层 — 聊天命令授权

列说明：✅ 允许；❌ 拒绝（bot 回复明确拒绝消息或静默丢弃）；⚠️ 允许但有额外检查。

| 命令 / 消息形式 | Bot 私聊 (owner) | Bot 群聊 (owner) | Bot 群聊 (非 owner) | 门控位置 |
|---|---|---|---|---|
| 无 `/` 前缀的纯文本（→ 默认 agent） | ✅ | ✅ | ✅ | `handler.go:528-530` |
| `/help` | ✅ | ✅ | ✅ | `handler.go:491` |
| `/info` / `/status` | ✅ | ✅ | ✅ | `handler.go:475` |
| `/chatinfo [id]` | ✅ | ✅ | ✅ | `handler.go:505` |
| `/task` / `/note` / `/event` / `/card` | ✅ | ✅ | ✅ | `actions_commands.go:30` |
| `/<agent> <消息>`（发送 / 广播） | ✅ | ✅ | ✅ | `handler.go:562` |
| `/<agent>`（切换默认 agent） | ✅ | ✅ | ❌ | `handler.go:537-539` |
| `/new` / `/clear` | ✅ | ✅ | ❌ | `handler.go:462-468` + `handler_commands.go:248` |
| `/cwd [路径]` | ✅ ⚠️ | ✅ ⚠️ | ❌ | `handler_commands.go:19`（allowlist + denylist） |
| `/cron add\|list\|delete` | ✅ | ✅ | ❌ | `handler.go:498` + `handler_commands.go:254` |
| `/reload` | ✅ | ✅ | ❌ | `handler.go:471` + `handler_commands.go:257` |
| 总结（自然语言触发，如"总结"、"summarize"） | ✅（需 Private App） | ⚠️ 仅限配置的群组 | ❌ | `handler_summarize.go:57-82` + `handler_commands.go:245` |
| 总结（无 Private App） | ❌ 不可用 | ❌ 不可用 | ❌ 不可用 | `handler_summarize.go:76` |
| `/full-access status\|grant\|revoke` | ✅ ⚠️ | ❌（仅私聊） | ❌（仅私聊） | `handler_fullaccess.go:62-67` |
| `/approval <id>` / `/approval deny <id>` | ✅ ⚠️（仅发起者本人） | ⚠️ 不被识别，作为普通文本转发给 agent | ⚠️ 不被识别，作为普通文本转发给 agent | `handler.go:576-585` + `oob/authorize.go:126` |

额外检查说明：

- `/cwd` — 绝对路径必须在 `agent_allow_workspace_list ∪ agent_workspace ∪ ~/.ringclaw/workspace` 范围内，且不含 denylist 目录（`.ssh`、`.gnupg`、`.ringclaw`、`.aws`、`.kube`、`.config/gcloud`）。两项检查与 full-access 状态无关。
- `/full-access grant [时长]` — 仅在 owner 回复 `/approval <id>` 后才真正激活。challenge TTL 5 分钟，默认授权 24 小时，上限 30 天。
- `/approval` — 仅原发起者可解析自己的 challenge（`oob/authorize.go:146-154`）。群聊中的 `/approval <id>` 消息不会被拦截，会作为普通文本转发给默认 agent。
- 群聊总结 — 只允许 chatID 等于 `ringcentral.group_summary_group_id` 的群；跨群 / 跨人总结会被拒绝（`handler_summarize.go:84-115`）。

### 第二层 — AI 驱动的 ACTION 派发

`ACTION: NOTE|TASK|EVENT|CARD|MESSAGE ... END_ACTION` 块由 `ParseAgentActions` 解析，由 `ExecuteAgentActions` 对接 RC API 执行（`messaging/actions.go:166`）。**与 full-access 无关**。

**典型触发场景**（用户输入 → agent 可能生成的 ACTION）：

```text
用户："记个笔记：本周优先级 A/B/C"
→ agent 回复中包含：
    ACTION: NOTE title=本周优先级
    A. …
    B. …
    END_ACTION

用户："创建一个任务交给 Alice：跟进 PR #42"
→ ACTION: TASK subject=跟进 PR #42 assignee=Alice END_ACTION

用户："把刚才的会议要点发消息告诉 David"
→ ACTION: MESSAGE chatid=David
    会议要点:
    …
    END_ACTION

用户："这个讨论的摘要发到 #engineering 频道"
→ ACTION: MESSAGE chatid=engineering
    …
    END_ACTION    # ← 触发第二层跨聊天 fail-closed 路径
```

| 场景 | 行为 | 门控位置 |
|---|---|---|
| ACTION 在 origin chat（任意发送者） | ✅ 始终允许 | `actions.go:166-310` |
| `chatid=` 覆盖，发起者**不在** trusted-sender allowlist | ⚠️ `chatid` 被静默忽略，强制回 origin chat（日志：`WARN action: ignoring chatid override from non-owner sender`） | `actions.go:172-184` |
| `chatid=` 覆盖（owner 发起，target = origin） | ✅ 允许（同第 1 行） | — |
| `chatid=` 覆盖（owner 发起，target = owner 自己的私聊） | ✅ 允许，无需 audit notice | `actions.go:195`（guard） |
| `chatid=` 覆盖（owner 发起，target ≠ origin 且 ≠ owner 私聊） | 🔒 **fail-closed**：先同步向 owner 私聊发 `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>`；在 `crossChatNoticeTimeout`（5 秒）内送达则派发 action，否则**拒绝**（`Refused cross-chat <TYPE>: …`） | `actions.go:195-203`；`announceCrossChatOrRefuse` 在 `actions.go:329-357` |
| owner 发起跨聊天 action，但 OOB 未配置（未解析到 owner 私聊） | ❌ 拒绝，返回 `Refused cross-chat <TYPE>: no owner DM audit channel configured` | `actions.go:330-332` |

### 第三层 — ACP 会话内能力（full-access 开关）

**仅适用于 ACP agent**（`agent/acp_agent.go`）。HTTP / CLI agent 没有对应的 `session/set_mode` 机制。

`full_access` 开关控制新建 ACP session 时调用 `session/set_mode "default"` 还是 `session/set_mode "full-access"`（`acp_agent.go:570-620`）。RingClaw 侧的实际门控如下：

| 能力 | Default 模式 | Full-access 模式 | 门控位置 |
|---|---|---|---|
| `session/set_mode` 参数 | `"default"` | `"full-access"` | `acp_agent.go:570-620` |
| `session/request_permission` 回调 | **RingClaw 自动允许**（始终回复第一个 `"allow"` 选项）——RingClaw 本身不做交互式工具调用审批 | agent 在 full-access 模式下通常不再发出 `request_permission` | `acp_rpc.go:230-265` |
| `fs/read_text_file`（ACP 协议） | ✅ 允许；无路径检查，无沙箱 | ✅ 允许（不变） | `acp_terminal.go:420-463` |
| `fs/write_text_file`（ACP 协议） | ✅ 仅当 agent 配置 `allow_write: true`；否则返回 `write permission denied: allowWrite is false` | ✅ 仍需 `allow_write: true`——**full-access 不覆盖 `allow_write`** | `acp_terminal.go:472-475` |
| `terminal/create`（shell 子进程） | ✅ 任意命令、任意 `cwd`，**不检查 `allow_write`，不检查路径 allowlist** | ✅ 相同（不变） | `acp_terminal.go:295-315`，`acp_terminal.go:128-187` |
| Agent 可见的工具清单 | ACP agent 在 `default` 模式下暴露的工具（由 agent 自身策略决定） | ACP agent 在 `full-access` 模式下暴露的工具 | 取决于具体 ACP agent |
| 顶层 `/cwd` allowlist + denylist | 仅约束 `/cwd` 命令和 `Agent.SetCwd` 初始目录 | **不变**——allowlist 不作用于 agent 在工具调用内自行选择的路径 | `handler_commands.go:19-98` |

::: warning 第三层的重要安全边界
- **`allow_write: false` 并非完整沙箱。** 它只拦截 ACP 协议层的 `fs/write_text_file`。它**不能**阻止 agent 通过 `terminal/create` 执行 `echo … > file`、`sed -i`、`git commit` 等 shell 命令来写文件。请将 `allow_write` 视为提示而非沙箱。
- **RingClaw 不做逐次工具调用审批。** `handlePermissionRequest` 自动选择第一个 `allow` 选项。更严格的门控在 ACP agent 自身（例如 Claude 的工具审批逻辑），而不在 RingClaw。从 default → full-access 并不是翻转 RingClaw 侧的某个开关，而是改变 RingClaw 请求 agent 采用的 `session/set_mode` 参数。
- **`/cwd` allowlist ≠ 文件访问沙箱。** allowlist 只约束 `/cwd` 命令可以将 agent 的起始工作目录切换到哪里。ACP agent 仍然可以读写任何它有 OS 权限访问的文件，也可以在任意目录中打开终端。
- **Full-access 有两种激活方式。** 静态方式：`config.json` 中 agent 的 `full_access: true` + 顶层 `full_access_ack: true`（`acp_agent.go:246-252`），重启后仍生效，需修改配置才能撤销。动态方式：在 owner 私聊中执行 `/full-access grant [时长]` → 回复 `/approval <id>` 激活，TTL 到期或 `/full-access revoke` 即失效，全内存状态，重启清空（`handler_fullaccess.go`、`oob/manager.go`）。撤销或 TTL 到期时，`DemoteAllACPFullAccess` 会向每个 live ACP session 发送 `session/set_mode "default"`；降级失败的 session 会从 session map 中移除，下次调用时重建（`acp_agent.go:703-728`）。
:::

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
