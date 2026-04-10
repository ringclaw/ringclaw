---
title: 聊天总结
---

# 聊天总结

总结任意聊天的对话内容：

```
总结我和 John 的聊天                      # 总结今天与 John 的聊天
summarize my chat with Raye from Monday  # 总结从周一开始的聊天
总结 Maxwell 上周的聊天                    # 总结上周的聊天
```

RingClaw 通过名字解析目标聊天，使用 Private App 获取消息，然后通过 AI Agent 生成摘要发送到当前聊天。

## 路由决策

收到消息后，RingClaw 通过两阶段判断是否为总结请求：关键词匹配（快速路径）和 AI 意图分类（回退路径）。

```mermaid
flowchart TD
    Msg[收到消息] --> Trigger{匹配 intent 关键词?}
    Trigger -->|否| Agent[转发给默认 Agent]
    Trigger -->|是| Keyword{以总结关键词开头?}
    Keyword -->|是| Perm[权限检查]
    Keyword -->|否| AI{AI 分类 intent}
    AI -->|summarize| Perm
    AI -->|task/note/event| Agent
    AI -->|chat| Agent
    Perm --> BotGroup{Bot 群聊?}
    BotGroup -->|否, Bot DM| PrivApp{有 Private App?}
    BotGroup -->|是| GroupCfg{配置了 group_id?}
    GroupCfg -->|否| Deny[拒绝]
    GroupCfg -->|是| GroupMatch{当前群 = 配置群?}
    GroupMatch -->|否| Deny
    GroupMatch -->|是| CrossCheck{跨群/跨人检查}
    CrossCheck -->|通过| GroupSum[群内总结]
    CrossCheck -->|拒绝| Deny
    PrivApp -->|否| Deny2[拒绝: 需要 Private App]
    PrivApp -->|是| Resolve[解析目标聊天]
```

## 名字解析

权限检查通过后，RingClaw 通过 mention、Agent 提取、本地缓存、公司目录搜索来解析目标聊天。

```mermaid
flowchart TD
    Start[解析目标] --> Time[解析时间范围]
    Time --> Mention{有 @mention?}
    Mention -->|Team| Done1[返回 Team chatID]
    Mention -->|Person| FindDM[查找 Direct 聊天]
    FindDM --> Done2[返回 DM chatID]
    Mention -->|无| ExtractName{提取人名}
    ExtractName --> AgentExtract[Agent 提取 10s超时]
    AgentExtract -->|成功| Lookup
    AgentExtract -->|失败| FillerStrip[去除填充词]
    FillerStrip --> Lookup[查找缓存]
    Lookup --> CacheExact{精确匹配?}
    CacheExact -->|是| Done3[返回缓存结果]
    CacheExact -->|否| Directory[搜索公司目录]
    Directory -->|找到| CreateConv[创建/获取 Direct 聊天]
    CreateConv --> Cache[写入缓存]
    Cache --> Done4[返回]
    Directory -->|未找到| CacheFuzzy{有模糊匹配?}
    CacheFuzzy -->|是| Done5[返回模糊结果]
    CacheFuzzy -->|否| Error[错误: 找不到匹配]
```

**人名提取**使用两种策略：
1. **Agent 提取**（优先）：让 AI Agent 从消息中提取人名（10秒超时）
2. **填充词去除**（回退）：去除总结关键词、时间词、中日韩/英文填充词，剩余文本即为人名

**缓存**（`~/.ringclaw/chat_cache.json`）将已解析的 名字→chatID 映射和人员信息持久化到磁盘。优先精确匹配，模糊匹配仅作为最后手段。

## 执行流程

目标聊天解析完成后，RingClaw 获取消息、构建 prompt 并发送给 AI Agent。

```mermaid
sequenceDiagram
    participant U as 用户
    participant RC as RingCentral
    participant R as RingClaw
    participant PA as Private App
    participant AI as AI Agent

    U->>RC: "总结我和 John 的聊天"
    RC->>R: WebSocket 事件
    R->>R: 解析时间范围 + 提取人名
    R->>PA: SearchDirectory("John")
    PA-->>R: John Lin (ID: 123)
    R->>PA: CreateConversation([123])
    PA-->>R: chatID: 456
    R->>RC: "Thinking..." 占位消息
    R->>PA: ListPosts(chatID=456, limit=250)
    PA-->>R: 聊天记录
    R->>R: 按时间过滤 + 构建 prompt
    R->>AI: prompt (带 ActionPrompt)
    AI-->>R: 摘要 + ACTION 块
    R->>RC: 更新占位消息为摘要
    R->>RC: 执行 ACTION (如创建笔记)
```

## 时间范围解析

RingClaw 支持 8 种语言的时间范围表达：

| 表达式 | 时间范围 | 支持语言 |
|--------|---------|---------|
| 今天 / today | 今天开始 | 中、英、日、韩 |
| 昨天 / yesterday / 昨日 | 昨天开始 | 中、英、日 |
| 前天 / day before yesterday | 2 天前 | 中、英、日、韩、法、西、德 |
| 本周 / this week / 今週 | 本周一开始 | 中、英、日、韩 |
| 上周 / last week / 先週 | 上周一开始 | 中、英、日、韩 |
| 本月 / this month / 今月 | 本月 1 号 | 中、英、日、韩 |
| 上个月 / last month / 先月 | 上月 1 号 | 中、英、日、韩 |
| 最近 / recently / 近期 | 3 天前 | 中、英、日、韩、法、西、德、俄 |
| 最近N天 / last N days | N 天前 | 中、英 |
| 最近N小时 / last N hours | N 小时前 | 中、英 |

未指定时间范围时，默认为**今天开始**。

## 群内总结

默认情况下，群聊中禁止使用总结功能（摘要会被群内所有人看到）。可以为特定群启用：

```json
{
  "ringcentral": {
    "group_summary_group_id": "1234567",
    "group_summary_message_limit": 200
  }
}
```

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `group_summary_group_id` | — | 只有这个精确的群 ID 允许在群内触发总结 |
| `group_summary_message_limit` | `200` | 开启群内总结后，先拉取最近这么多条消息，再按时间过滤 |

启用群内总结后，RingClaw 会阻止跨目标请求（mention 其他群/其他用户、"chat with" 等短语），防止群内数据泄露。

## 安全限制

::: warning
使用 Bot 时，群聊中禁止使用总结功能（摘要会被群内所有人看到）。请在与 Bot 的私聊中使用。
:::

- **Bot 私聊**：总结正常工作 — Private App 读取目标聊天，Bot 回复摘要
- **群聊**：**禁止**，除非配置了 `group_summary_group_id` 且与当前群一致
- **无 Private App**：总结**完全不可用** — Bot 无法访问其他用户的聊天

## 前提条件

总结功能需要配置 **Private App**。Private App 需要 `ReadAccounts` 权限来：

- 搜索公司目录以解析聊天名称
- 读取 Bot 无法直接访问的其他聊天中的消息
