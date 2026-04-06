---
title: 聊天总结
---

# 聊天总结

总结任意聊天的对话内容：

```
总结我和 John 的聊天               # 总结今天与 John 的聊天
summarize my chat with Raye from Monday  # 总结从周一开始的聊天
```

RingClaw 通过名字解析目标聊天，使用 Private App 获取消息，然后通过 AI Agent 生成摘要发送到当前聊天。

## 工作流程

1. RingClaw 解析消息中的目标聊天名称和可选的时间范围
2. 使用 Private App 搜索公司目录，找到匹配的聊天
3. 从目标聊天获取指定时间范围内的消息
4. 将消息发给默认 AI Agent 生成摘要
5. 将摘要发回当前聊天

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
| `group_summary_message_limit` | `200` | 开启群内总结后，先拉取当前群最近这么多条消息，再按时间范围过滤 |

只要配置了 `group_summary_group_id`，群内总结功能就会自动启用。只有当前群 ID 与该配置完全一致时，才允许在群内触发总结。

## 安全限制

::: warning
使用 Bot 时，群聊中禁止使用总结功能（摘要会被群内所有人看到）。请在与 Bot 的私聊中使用。
:::

- **Bot 私聊**：总结正常工作 — Private App 读取目标聊天，Bot 回复摘要
- **群聊**：**禁止** 以防数据泄露
- **无 Private App**：总结**完全不可用** — Bot 无法访问其他用户的聊天

## 前提条件

总结功能需要配置 **Private App**。Private App 需要 `ReadAccounts` 权限来：

- 搜索公司目录以解析聊天名称
- 读取 Bot 无法直接访问的其他聊天中的消息
