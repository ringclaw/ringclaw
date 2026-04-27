---
title: 命令授权
---

# 命令授权（第一层）

第一层决定每个用户在每种聊天形式下可以触发哪些斜杠命令。它在
[发送者白名单](./sender-allowlist) 放行后才会被检查——所以
下表的每一格都默认发送者已经通过第零层。

第一层与其他层的关系见 [权限矩阵](./index#权限矩阵)。

## 命令授权表

列说明：✅ 允许；❌ 拒绝（bot 回复明确拒绝消息或静默丢弃）；
⚠️ 允许但有额外检查。

"Owner" 指配置了 Private App 时的 Private App owner（真正的
机器操作者）。"Bot 私聊（其他 trusted sender）" 列只在
`ringcentral.source_user_ids` 配了多人时有意义——参阅
[安全概览](./index#权限矩阵) 的"私聊是信任边界"提示。

| 命令 / 消息形式 | Bot 私聊 (owner) | Bot 私聊（其他 trusted sender，有 Private App） | Bot 群聊 (owner) | Bot 群聊 (非 owner) | 门控位置 |
|---|---|---|---|---|---|
| 无 `/` 前缀的纯文本（→ 默认 agent） | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/help` | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/info` / `/status` | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/chatinfo [id]` | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/task` / `/note` / `/event` / `/card` | ✅ | ✅ | ✅ | ✅ | `actions_commands.go` |
| `/<agent> <消息>`（发送 / 广播） | ✅ | ✅ | ✅ | ✅ | `handler.go` |
| `/<agent>`（切换默认 agent） | ✅ | ✅ | ✅ | ❌ | `handler.go` |
| `/new` / `/clear` | ✅ | ❌ | ✅ | ❌ | `handler.go` + `handler_commands.go` |
| `/cwd [路径]` | ✅ ⚠️ | ❌ | ✅ ⚠️ | ❌ | `handler_commands.go`（allowlist + denylist） |
| `/cron add\|list\|delete` | ✅ | ❌ | ✅ | ❌ | `handler.go` + `handler_commands.go` |
| `/reload` | ✅ | ❌ | ✅ | ❌ | `handler.go` + `handler_commands.go` |
| 总结（自然语言触发，如"总结"、"summarize"） | ✅（需 Private App） | ❌ | ⚠️ 仅限配置的群组 | ❌ | `handler_summarize.go` + `handler_commands.go` |
| 总结（无 Private App） | ❌ 不可用 | n/a | ❌ 不可用 | ❌ 不可用 | `handler_summarize.go` |
| `/full-access status\|grant\|revoke` | ✅ ⚠️ | ❌（owner-only、DM-only） | ❌（DM-only） | ❌（DM-only） | `handler_fullaccess.go` |
| `/approval <id>` / `/approval deny <id>` | ✅ 已消费；重定向到终端（`ringclaw approval <id>`） | ✅ 已消费；重定向到终端 | ❌ 明确拒绝并给出提示 | ❌ 明确拒绝并给出提示 | `handler.go` + `oob/authorize.go` |
| `/mem add [user\|chat\|global] <文本>` | ✅ | ❌ | ✅ | ❌ | `handler_persona.go` + `handler_commands.go` |
| `/mem del [scope] [confirm]` | ✅ | ❌ | ✅ | ❌ | 同 `/mem add`；需要二次 `confirm` |
| `/mem show [scope]` | ✅ | ✅ | ✅ | ✅ | 只读、非特权 |
| `/persona` | ✅ | ✅ | ✅ | ✅ | 只读、非特权 |

## 额外检查

- **`/cwd`** —— 绝对路径必须在
  `agent_allow_workspace_list ∪ agent_workspace ∪ ~/.ringclaw/workspace`
  范围内，且不含 denylist 目录（`.ssh`、`.gnupg`、`.ringclaw`、
  `.aws`、`.kube`、`.config/gcloud`）。两项检查与 full-access
  状态无关。详见 [工作目录白名单](./workspace-allowlist)。
- **`/full-access grant [时长]`** —— 仅在 owner 在主机执行
  `ringclaw approval <id>` 后才真正激活。challenge TTL 5 分钟，
  默认授权 24 小时，上限 30 天。详见
  [ACP Full-Access](./full-access)。
- **`/approval`** —— Bot 私聊中任何 `/approval ...` 消息都会
  被消费并重定向到终端 CLI（`oob/authorize.go`）。审批需要在
  主机上执行 `ringclaw approval <id>`。在 Bot 私聊之外发送的
  `/approval ...` 格式消息会被明确拒绝，不会转发给默认 agent。
  详见 [审批 CLI](./approval-cli)。
- **群聊总结** —— 只允许 chatID 等于
  `ringcentral.group_summary_group_id` 的群；跨群 / 跨人总结
  会被拒绝（`handler_summarize.go`）。
- **`/mem add` 与 `/mem del`** —— 第一层特权命令（与 `/cron`
  同一道门控）。所有 memory 文件写入严格落在 `persona.memory_dir`
  之内；恶意 chat/user ID 会被 `SanitizeID` 转义成安全文件名，
  无法逃出 memory 树。scope 布局见
  [配置 › persona](../guide/configuration#persona)。
- **`/mem del`** 不带 `confirm` 时不会真正清空——第一次调用
  会打印解析出的文件路径、当前大小以及最后几行预览，方便
  operator 在再次发送 `confirm` 之前确认自己要删的就是这个
  scope。`/mem del confirm` **不会** 重置 agent session：下次
  消息时 banner 会从磁盘重新拼装，但当前正在运行的 session
  依然带着旧 memory 上下文。如果想让在线 agent 也立刻"忘掉"
  旧上下文，清空后再发一条 `/new`。
- **Cron / Heartbeat / HTTP API 不会注入 persona banner。** 这
  些非交互入口没有真实的 chat / user 上下文；banner 仅拼接在
  WebSocket 用户消息之前（`dispatchToAgent` 与
  `broadcastToAgents`）。
