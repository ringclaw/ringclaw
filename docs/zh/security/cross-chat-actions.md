---
title: 跨聊天 Action
---

# 跨聊天 Action（第二层）

AI 生成的 `ACTION` 块可能携带 `chatid=` 参数，目标聊天与消息
到达的聊天不同。为防止"在 B 群里总结 A 群"这类数据外泄场景，
现在跨聊天派发只允许在结构化 fail-closed 门控下进行。这一页
覆盖 owner 发起的同步 audit notice 路径，以及非 owner 的 OOB
challenge 路径。

第二层与其他层的关系见 [权限矩阵](./index#权限矩阵)；解决跨聊
天 OOB challenge 的终端命令见 [审批 CLI](./approval-cli)。

## 典型触发场景

用户输入 → agent 可能生成的内容：

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
    END_ACTION    # ← 触发跨聊天 fail-closed 路径
```

## 门控表

`ACTION: NOTE|TASK|EVENT|CARD|MESSAGE ... END_ACTION` 块由
`ParseAgentActions` 解析、由 `ExecuteAgentActions` 执行
（`messaging/actions.go`）。**与 full-access 无关**。

| 场景 | 行为 | 门控位置 |
|---|---|---|
| ACTION 在 origin chat（任意发送者） | ✅ 始终允许 | `actions.go` |
| `chatid=` 覆盖，发起者**不在** trusted-sender 白名单 | ⚠️ **OOB 已配置时发起 challenge**：向 owner 私聊发送富信息 challenge 提示；owner 在主机执行 `ringclaw approval <id>` 批准；批准后 action 异步在目标聊天执行。OOB 未配置时回退为静默丢弃（强制回到 origin chat）。 | `actions.go`（`crossChatOOBChallenge`） |
| `chatid=` 覆盖（owner 发起，target = origin） | ✅ 允许（同第 1 行） | — |
| `chatid=` 覆盖（owner 发起，target = owner 自己的私聊） | ✅ 允许，无需 audit notice | `actions.go`（guard） |
| `chatid=` 覆盖（owner 发起，target ≠ origin 且 ≠ owner 私聊） | 🔒 **fail-closed**：先同步向 owner 私聊发 `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>`；在 `crossChatNoticeTimeout`（5 秒）内送达则派发 action，否则**拒绝** | `actions.go`（`announceCrossChatOrRefuse`） |
| owner 发起跨聊天 action，但 OOB 未配置（未解析到 owner 私聊） | ❌ 拒绝，返回 `Refused cross-chat <TYPE>: no owner DM audit channel configured` | `actions.go` |

::: warning Owner 跨聊天行为
Owner 发起的跨聊天 ACTION **不再**无条件执行。如果 audit
channel 不存在或 notice 发送失败，action 会被**拒绝**——什么
都不会落到目标聊天，调用者看到 `Refused cross-chat <TYPE>` 条
目。没有可解析 bot 私聊的运维必须先解决私聊解析，否则只能把
所有 owner-driven action 留在 origin chat 内。
:::

## Owner 发起的跨聊天：同步 audit notice

在跨聊天的 `MESSAGE` / `CARD` / `TASK` / `NOTE` 派发之前，当
target chat 与 origin 不同、且不是 owner 自己的私聊时，bot 会
先向 owner 私聊投递一条仅含元数据的 notice：

```text
[notice] MESSAGE by 12345 at 2026-04-17T10:15:00Z: origin=chat-7 target=chat-42
```

notice 仅携带 `TYPE`、`requesterID`、RFC3339 时间戳、
`originChatID` 与 `targetChatID`——**body、title、content 均
不会泄露到 owner 私聊**。发送有 `crossChatNoticeTimeout`（5
秒）上限，避免被卡住的 RC 接口拖慢 prompt 流水线；上限触发
时跨聊天 action 被拒绝而不是静默派发。

拒绝路径（跨聊天 action 不会落到目标聊天）：

- `OwnerDMChat` 为空（bot 与 owner 的私聊尚未解析、或 OOB 未
  配置）：调用者看到
  `Refused cross-chat <TYPE>: no owner DM audit channel configured`。
- notice 发送返回错误（超时、5xx、传输异常）：
  `Refused cross-chat <TYPE>: audit notice delivery failed: <cause>`。

## 非 owner 跨聊天 OOB challenge

非 owner 发起的跨聊天 ACTION（`chatid=` 指向其他聊天）现在会
进入 OOB 审批流程，而不是直接静默丢弃：

```text
非 owner 用户 @bot → AI 回复包含 ACTION: MESSAGE chatid=<other-chat>
  ↓
Bot 向 owner 私聊发送富信息 challenge 提示：

  Pending approval (challenge `def67890`).
  Action: Cross-chat MESSAGE
  Requester: Alice Cross <alice@example.com> (id=user-7)
  Origin chat: Engineering (id=origin-1)
  Target chat: Customer Support (id=target-9)
  Body: Highlights for the next quarter ...

  Effect: bot will write a MESSAGE into the target chat on the requester's behalf.

  Run on the host:
    ringclaw approval def67890        (approve)
    ringclaw approval deny def67890   (deny)

  Expires in 5m.
  ↓
Owner 在主机执行：ringclaw approval def67890
  ↓
批准 → action 异步在目标聊天执行 → origin 聊天收到通知
拒绝 / 过期 → origin 聊天收到通知
```

prompt 中 body 上限 200 字符（超出截断为 `…`）；
`Title:` / `Subject:` / `Assignee:` 行只在对应 ACTION 参数存
在时才出现。**owner 私聊里只会看到 action 自身将要写入的内
容，没有任何额外信息泄露。**

**回退**：当 OOB 未配置（无 Private App、未解析 owner 私聊或
`OwnerID` 为空），保留旧的静默覆盖行为——`chatid=` 被丢弃，
action 在 origin chat 执行。

**仅终端审批**：challenge ID 经 DM 投递，让 owner 知道有待批
准请求；但批准本身必须在主机执行 `ringclaw approval <id>`。
被盗 RC 账号能看到 challenge ID，却没有主机权限就批不掉。详
见 [审批 CLI](./approval-cli)。

## 审计日志条目

| 事件 | 日志行 | 用途 |
|---|---|---|
| 跨聊天 OOB challenge 发起 | `INFO oob: challenge issued`（`challengeID`、`requesterID`、`intent` 以 `cross-chat …` 开头） | 跟踪每个审批 prompt，包括无人响应而超时的。 |
| 跨聊天 OOB 批准——action 执行 | `INFO action: cross-chat OOB approved - <created note/task/message/…>`（`chatID`、详情） | 确认终端批准后 action 已在目标聊天执行。 |
| 跨聊天 notice 已送达（pre-dispatch） | `INFO action: cross-chat notice sent (pre-dispatch)`（`type`、`from`、`to`、`ownerDMChat`、`requesterID`） | 确认 audit notice 已抵达 owner 私聊；接下来 action 才派发。 |
| 跨聊天 action 被拒 | `WARN action: cross-chat ACTION refused (fail-closed on pre-notice)`（含 `error`） | audit channel 缺失或 notice 发送失败；action 不会被派发。 |
