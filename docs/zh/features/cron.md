---
title: 定时任务
---

# 定时任务（Cron）

在聊天中管理定时或一次性任务，定时发消息给 AI Agent 执行：

```
/cron add "standup" every:24h "总结昨天的工作"
/cron add "check" */30 * * * * "检查新的 PR"
/cron add "reminder" at:2026-04-01T09:00:00 "今天下午 Sprint 评审"
/cron list
/cron enable <id>
/cron disable <id>
/cron delete <id>
```

## 调度格式

| 格式 | 示例 | 说明 |
|------|------|------|
| `every:<时长>` | `every:5m`、`every:24h` | 固定间隔 |
| Cron 表达式 | `*/30 * * * *`、`0 9 * * 1-5` | 标准 5 字段 cron |
| `at:<时间>` | `at:2026-04-01T09:00:00` | 一次性（执行后自动禁用） |

## 命令

| 命令 | 说明 |
|------|------|
| `/cron add "<名称>" <调度> "<消息>"` | 创建定时任务 |
| `/cron list` | 列出所有定时任务 |
| `/cron enable <id>` | 启用已禁用的任务 |
| `/cron disable <id>` | 禁用任务（不删除） |
| `/cron delete <id>` | 永久删除任务 |

## 持久化

任务持久化到 `~/.ringclaw/cron/jobs.json`，重启不丢失。每个任务可选指定目标 Agent 或 Chat。
