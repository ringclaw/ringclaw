---
title: AI 驱动操作
---

# AI 驱动的自动操作

AI Agent 在对话中可以自动创建笔记、任务、日历事件和 Adaptive Card。当用户的请求暗示需要创建这些资源时，Agent 会在回复中附加 ACTION 块，RingClaw 通过 RC API 自动执行。

## 工作流程

当 Agent 的回复包含 ACTION 块时，RingClaw 解析它们，先发送文本回复，再逐个执行 ACTION：

```mermaid
sequenceDiagram
    participant AI as AI Agent
    participant R as RingClaw
    participant O as Owner DM
    participant RC as RingCentral

    AI-->>R: 回复 (含 ACTION 块)
    R->>R: ParseAgentActions()
    R->>R: 分离文本回复和 ACTION
    R->>RC: 发送文本回复
    loop 每个 ACTION
        R->>R: 解析类型、参数、chatid
        alt 指定了 chatid 且发送者不在可信列表 且 OOB 已配置
            R->>O: 富信息 challenge 提示<br/>action / requester / origin / target<br/>+ 可选 Title/Subject/Assignee<br/>+ body 预览（200 字符）<br/>+ 主机审批命令
            Note over O,R: Owner 在主机执行 `ringclaw approval id`
            R->>R: 异步等待终端审批
            alt 批准
                R->>RC: 在目标聊天执行 action
                R->>RC: 通知原聊天
            else 拒绝 / 超时
                R->>RC: 通知原聊天
            end
        else 指定了 chatid 且发送者不在可信列表 且 OOB 未配置
            R->>R: 静默丢弃 chatid，强制回到 origin chat (WARN 日志)
        end
        alt owner 跨聊天 (target ≠ origin 且 ≠ owner DM)
            R->>O: 同步 audit notice<br/>[notice] TYPE by requester at ts: origin=... target=...
            O-->>R: ACK (5 秒内) 或超时/错误
            alt 通知发送失败
                R->>R: 拒绝 action (Refused cross-chat TYPE: ...)
            end
        end
        alt NOTE
            R->>RC: CreateNote + PublishNote
        else TASK
            R->>RC: CreateTask (可选: 通过 Private App 解析 assignee)
        else EVENT
            R->>RC: CreateEvent
        else CARD
            R->>RC: CreateAdaptiveCard
        else MESSAGE
            R->>R: 解析 chatid / 人名
            R->>RC: SendPost
        end
    end
    R->>RC: 发送 ACTION 执行结果汇总
```

## ACTION 块格式

```
ACTION:NOTE title=会议纪要
今天站会的关键决定...
END_ACTION

ACTION:TASK subject=更新部署脚本
END_ACTION

ACTION:EVENT title=Sprint 评审 start=2026-04-01T14:00:00Z end=2026-04-01T15:00:00Z
END_ACTION
```

ACTION 可通过 `chatid=<id>` 参数定向到其他聊天。

- **非 owner 发送者**：OOB 已配置时（Private App + owner 私聊已解析），系统向 owner 私聊发送富信息 challenge 提示（action 类型、requester 身份、origin / target 聊天名、可选 `Title:` / `Subject:` / `Assignee:`、≤200 字符的 body 预览、效果说明、主机审批命令）。owner 需在主机上执行 `ringclaw approval <id>` 批准。批准后 action 异步在目标聊天执行。OOB 未配置时回退为静默丢弃（强制回到 origin chat）。owner 私聊收到的提示示例：

  ```text
  Pending approval (challenge `def67890`).
  Action: Cross-chat NOTE
  Requester: Alice Cross <alice@example.com> (id=user-7)
  Origin chat: Engineering (id=origin-1)
  Target chat: Customer Support (id=target-9)
  Title: Quarterly review notes
  Body: Highlights for the next quarter ...

  Effect: bot will write a NOTE into the target chat on the requester's behalf.

  Run on the host:
    ringclaw approval def67890        (approve)
    ringclaw approval deny def67890   (deny)

  Expires in 5m.
  ```

- **owner 发起的跨聊天**：派发前经过**同步 fail-closed audit notice**（通过 owner 私聊确认）——完整门控规则见 [安全 › 第二层](../security/index.md#第二层-ai-驱动的-action-派发)。

无需额外配置 — ACTION 提示词会自动注入。

## 任务命令

```
/task create 修复登录 bug        # 创建任务
/task list                       # 列出当前聊天的任务
/task complete <id>              # 标记任务完成
/task get <id>                   # 获取任务详情
/task update <id> <key=value>    # 更新任务
/task delete <id>                # 删除任务
```

## 笔记命令

```
/note create 会议纪要 | 内容     # 创建笔记（自动发布）
/note list                       # 列出当前聊天的笔记
/note get <id>                   # 获取笔记详情
/note update <id> <key=value>    # 更新笔记
/note lock <id>                  # 锁定编辑
/note unlock <id>                # 解锁
/note delete <id>                # 删除笔记
```

## 日历事件命令

```
/event list                      # 列出日历事件
/event list <chatId>             # 列出指定聊天的事件
/event create <title> <start> <end>  # 创建事件
/event get <id>                  # 获取事件详情
/event update <id> <key=value>   # 更新事件
/event delete <id>               # 删除事件
```

## Adaptive Card（自适应卡片）

AI Agent 可以生成 [Adaptive Card](https://adaptivecards.io/) 用于富文本结构化展示（进度报告、仪表盘、表单等）。当 Agent 在回复中包含 `ACTION:CARD` 块时，RingClaw 会自动将卡片发送到聊天：

```
ACTION:CARD
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Sprint 状态","weight":"bolder"},{"type":"FactSet","facts":[{"title":"已完成","value":"12"},{"title":"剩余","value":"3"}]}]}
END_ACTION
```

通过聊天命令管理卡片：

```
/card get <id>       # 查看卡片详情
/card delete <id>    # 删除卡片
```
