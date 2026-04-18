# RingClaw 改进计划

参考 [acpx](https://github.com/openclaw/acpx) 架构的需求计划，适配 RingClaw 的 Go 代码库和 RingCentral Team Messaging 场景。

## 计划优先级

| 优先级 | 计划 | 状态 | 描述 |
|--------|------|------|------|
| **P0** | [001 — Mock Agent 测试框架](001-mock-agent-testing.md) | Draft | 协议级 ACP mock，用于集成测试 |
| **P1** | [002 — Session 持久化](002-session-persistence.md) | Draft | 跨重启保持 session 映射 |
| **P2** | [003 — 结构化错误码](003-error-codes.md) | Draft | Agent 错误分类，用于重试逻辑和用户消息 |
| **P3** | [004 — 增量回复更新](004-incremental-reply-updates.md) | Draft | 流式显示部分回复，替代 "Thinking..." 占位符 |
| **P4** | [005 — Flow 流程架构](005-flow-architecture.md) | Draft | 声明式多步骤消息处理（未来） |
| **P3** | [006 — Prompt 自进化](006-prompt-self-evolution.md) | Draft | 评估工具 + LLM 驱动的 prompt 优化 |

## 参考

- acpx 文档: https://github.com/openclaw/acpx/tree/main/docs
- 英文版: [../README.md](../README.md)
