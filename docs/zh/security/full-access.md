---
title: ACP Full-Access
---

# ACP Full-Access（第三层）

第三层决定 ACP agent 在 session 内可以读什么、写什么、执行什
么。Full-access 模式关闭 RingClaw 的逐次 MCP 工具调用审批，并
让 agent 切到 `session/set_mode "full-access"`。激活方式有两
种：**静态**（配置）与**动态**（运行时
`/full-access grant`）。

本页**仅适用于 ACP agent**（`agent/acp_agent.go`）。HTTP / CLI
agent 没有对应的 `session/set_mode` 机制。

第三层与其他层的关系见 [权限矩阵](./index#权限矩阵)；解决
`/full-access` challenge 的终端命令见 [审批 CLI](./approval-cli)。

## ACP agent 文件权限（默认）

默认情况下，ACP Agent 仅获得**只读**文件访问权限。如需允许
文件写入，请在 Agent 配置中设置 `allow_write: true`：

```json
"claude-acp": {
  "type": "acp",
  "command": "claude-agent-acp",
  "allow_write": true
}
```

`allow_write` 只拦截 ACP `fs/write_text_file` 协议路径。下面
[不变量](#第三层重要安全边界) 一节解释了为什么这并不是完整
的写入沙箱。

## 第三层能力矩阵

`full_access` 开关控制新建 ACP session 时调用
`session/set_mode "default"` 还是
`session/set_mode "full-access"`（`acp_agent.go`）。RingClaw
侧的实际门控如下：

| 能力 | Default 模式 | Full-access 模式 | 门控位置 |
|---|---|---|---|
| `session/set_mode` 参数 | `"default"` | `"full-access"` | `acp_agent.go` |
| `session/request_permission` 回调 | **RingClaw 自动允许**（始终回复第一个 `"allow"` 选项）——RingClaw 本身不做交互式工具调用审批 | agent 在 full-access 模式下通常不再发出 `request_permission` | `acp_rpc.go` |
| `fs/read_text_file`（ACP 协议） | ✅ 允许；无路径检查，无沙箱 | ✅ 允许（不变） | `acp_terminal.go` |
| `fs/write_text_file`（ACP 协议） | ✅ 仅当 agent 配置 `allow_write: true`；否则返回 `write permission denied: allowWrite is false` | ✅ 仍需 `allow_write: true`——**full-access 不覆盖 `allow_write`** | `acp_terminal.go` |
| `terminal/create`（shell 子进程） | ✅ 任意命令、任意 `cwd`，**不检查 `allow_write`，不检查路径 allowlist** | ✅ 相同（不变） | `acp_terminal.go` |
| Agent 可见的工具清单 | ACP agent 在 `default` 模式下暴露的工具（由 agent 自身策略决定） | ACP agent 在 `full-access` 模式下暴露的工具 | 取决于具体 ACP agent |
| 顶层 `/cwd` allowlist + denylist | 仅约束 `/cwd` 命令和 `Agent.SetCwd` 初始目录 | **不变**——allowlist 不作用于 agent 在工具调用内自行选择的路径 | `handler_commands.go` |

### 第三层重要安全边界

::: warning
- **`allow_write: false` 并非完整沙箱。** 它只拦截 ACP 协议层
  的 `fs/write_text_file`。它**不能**阻止 agent 通过
  `terminal/create` 执行 `echo … > file`、`sed -i`、
  `git commit` 等 shell 命令来写文件。请将 `allow_write` 视
  为提示而非沙箱。
- **RingClaw 不做逐次工具调用审批。**
  `handlePermissionRequest` 自动选择第一个 `allow` 选项。更严
  格的门控在 ACP agent 自身（例如 Claude 的工具审批逻辑），而
  不在 RingClaw。从 default → full-access 并不是翻转 RingClaw
  侧的某个开关，而是改变 RingClaw 请求 agent 采用的
  `session/set_mode` 参数。
- **`/cwd` allowlist ≠ 文件访问沙箱。** allowlist 只约束
  `/cwd` 命令可以将 agent 的起始工作目录切换到哪里。ACP agent
  仍然可以读写任何它有 OS 权限访问的文件，也可以在任意目录中
  打开终端。详见 [工作目录白名单](./workspace-allowlist)。
- **Full-access 在两条轴上叠加。** 静态方式：`config.json` 中
  agent 的 `full_access: true` + 顶层 `full_access_ack: true`；
  动态方式：在 owner 私聊中执行 `/full-access grant [时长]` →
  在主机执行 `ringclaw approval <id>` 激活；任一启动后，新建
  session 都会进入 full-access 模式。撤销 / TTL 到期会**降级
  所有 live session**（`DemoteAllACPFullAccess`）；降级失败的
  session 会从 session map 中移除，下次 prompt 时重建为
  default 模式。
:::

## 静态激活：`full_access` + `full_access_ack`

在 ACP agent 上设置 `full_access: true` 会调用
`session/set_mode "full-access"`，并关闭 RingClaw 的逐次 MCP
工具调用审批。这种方式风险大：被注入 prompt 的 agent 可以读
写进程能触及的任意文件。

为了防止配置文件被偷或被复制粘贴静默激活，RingClaw 要求
`config.json` 中显式确认：

```jsonc
{
  // 显式、可纳入版本管理的确认。
  "full_access_ack": true
}
```

裁决：

1. `config.json` 中 `full_access_ack: true` → 接受 `full_access`。
2. 其他情况（省略或 `false`） → 拒绝 `full_access`，并打 WARN。

历史上的 `RINGCLAW_FULL_ACCESS_ACK` 环境变量已被静默忽略——
散落在 shell 中的 export 不能再用来悄悄重新启用 full access。

请求被降级时，session 仍保留默认 guarded 模式。请求被接受时，
每个新建的 ACP session 都会额外打一行
`WARN ACP session granted full-access`（`source` 字段区分静态
`config:full_access` 路径与下面的动态 `oob:/full-access` 路径）。

## 动态激活：`/full-access` 两步审批

叠加在静态开关之上的、按时长限制的动态解锁。已配置的静态值
仍然生效；动态流程是**叠加性**的，运维可以把 `config.json` 里
的 `full_access` 留为 `false`，按需在 bot 私聊中临时解锁：

```text
/full-access status         # 查看当前授权状态
/full-access grant           # 申请默认 24 小时解锁
/full-access grant 30m      # 申请 30 分钟解锁
/full-access revoke         # 立即重新锁定
```

授权流程是需要主机权限的两步确认：

1. Owner 发送 `/full-access grant [时长]`。Bot 立即回复
   `Full-access grant requested. Confirm via terminal.`，并向
   owner 私聊发送一条富信息 prompt：

   ```text
   Pending approval (challenge `abc12345`).
   Action: Grant ACP full-access for 30m
   Requester: Owen Owner <owen@example.com> (id=user-owner)
   Effect: agents with `full_access:true` will run MCP tool calls without per-call approval until the grant expires or `/full-access revoke` is used.

   Run on the host:
     ringclaw approval abc12345        (approve)
     ringclaw approval deny abc12345   (deny)

   Expires in 5m. Grant TTL: 30m.
   ```

   requester 标签是 best-effort 目录解析
   （`<displayName> <email> (id=<numeric>)`）；解析失败时退化
   为纯数字 ID 后仍然发出。

2. Owner 在主机执行 `ringclaw approval <id>`（或
   `ringclaw approval deny <id>` 拒绝）。批准后 bot 回复
   `Full-access granted until <RFC3339 expiry>.`；拒绝或过期
   则授权不会生效。

**禁用聊天侧 `/approval`。** Bot 私聊中任何 `/approval ...`
消息都会被消费并重定向到终端 CLI。这把审批权与 RC 账号解耦：
被盗 RC 账号没有主机访问就批不掉。详见
[审批 CLI](./approval-cli)。

### 约束

- 仅 bot 与 owner 的私聊接受 `/full-access`。群聊调用会被明
  确拒绝并给出说明，整个往返保持在受保护的私聊通道中。
- 默认授权时长 **24 小时**；上限 **30 天**。超出输入会被静
  默裁剪。时长用 `time.ParseDuration` 解析（例如 `30m`、`2h`、
  `168h`）。
- **审批需要主机权限。** `ringclaw approval <id>` 调用本地
  API 服务（`127.0.0.1:18011`、loopback-only、token 鉴权）。
  被盗 RC 账号能看到 DM 中的 challenge ID，但没有 SSH / 物理
  访问就无法批准。
- 授权一旦生效，每个新建的 ACP session 都会切到
  `session/set_mode "full-access"`，直到授权过期或
  `/full-access revoke` 被调用。所有 OOB 状态都是内存的，重
  启即清空——意外崩溃后 bot 自然回到锁定状态，运维必须重新
  显式授权。

### 撤销 / TTL 到期会降级 live session

::: warning
`/full-access revoke`（与 TTL 到期）不仅阻止 **新** session
进入 full-access，还会主动向授权窗口内创建的所有 live ACP
session 发送 `session/set_mode "default"`。降级失败的 session
会从 session map 中移除；下次 prompt 时该会话会以默认模式重
建（少量内存中的会话上下文可能会丢失）。
:::

授权结束（显式 `/full-access revoke` 或 TTL 到期）时，manager
触发的 revoke hook 接到 `agent.DemoteAllACPFullAccess`。该
walker 遍历授权窗口期间创建的所有 live ACP session，向每个发
送 `session/set_mode "default"`。`getOrCreateSession` 中的
double-read 关闭了 grant-and-revoke 同时落在 session 创建过
程中的窄窗口竞争：当 revoke 在初始 `set_mode "full-access"`
调用还在飞行时落地，agent 会立即回补一次
`set_mode "default"`。

## 审计日志条目

| 事件 | 日志行 | 用途 |
|---|---|---|
| 静态 full-access session 启用 | `WARN ACP session granted full-access`（`source: config:full_access` 或 `oob:/full-access` 或 `config:full_access+oob`） | 每个进入 full-access 的 ACP session 一行。 |
| Full-access challenge 发起 | `INFO oob: challenge issued`（`intent: grant ACP full-access for <duration>`） | 跟踪每个 `/full-access grant` 提示。 |
| Full-access 已授权 | `WARN oob: ACP full-access granted`（`ttl`、`expiresAt`） | `ringclaw approval <id>` 解决 challenge 后触发。 |
| Full-access 已撤销 | `WARN oob: ACP full-access revoked` | 由 `/full-access revoke` 或重新授权触发。 |
| Full-access 过期（TTL） | `WARN oob: ACP full-access expired (TTL reached)` | 在授权 `expiresAt` 到达时主动触发。 |
| Live session 已降级 | `INFO acp demote: session returned to default mode`（`session`、`conversation`） | 撤销 / 过期后向 live session 投递 `session/set_mode "default"` 成功。 |
| Live session 降级失败 | `WARN acp demote: set_mode default failed, session dropped from map`（含 `error`） | session 被从 map 中移除；下次 prompt 时新建一个全新的（默认模式）session。 |
