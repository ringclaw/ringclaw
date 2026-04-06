---
title: 心跳检测
---

# 心跳（Heartbeat）

基于用户编写的检查清单，定时让 Agent 做自主巡检。创建 `~/.ringclaw/HEARTBEAT.md`：

```markdown
# 心跳检查清单
- 检查是否有紧急邮件
- 扫描需要 review 的 PR
- 检查 CI 流水线状态
```

## 工作原理

RingClaw 每次心跳间隔读取此文件，发给默认 Agent：
- Agent 回复 `HEARTBEAT_OK` → 吞掉（一切正常）
- Agent 回复有内容 → 以 `[Heartbeat]` 前缀发送到默认聊天
- 24 小时内重复回复自动去重

## 配置

```json
{
  "heartbeat": {
    "enabled": true,
    "interval": "30m",
    "active_hours": "09:00-18:00",
    "timezone": "Asia/Shanghai"
  }
}
```

## 配置选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `enabled` | `false` | 启用心跳 |
| `interval` | `30m` | 心跳间隔 |
| `active_hours` | — | 仅在此时段运行（如 `09:00-18:00`） |
| `timezone` | 本地 | 活跃时段的时区（IANA 格式） |
