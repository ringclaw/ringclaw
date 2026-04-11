# 计划 005: Flow 流程架构

**日期:** 2026-04-11
**优先级:** P4
**状态:** Draft
**依赖:** 计划 003（错误码，用于节点结果类型）
**参考:** acpx `2026-03-25-acpx-flows-architecture.md`, acpx `src/flows/`

## 问题描述

RingClaw 消息处理是过程式函数链：`HandleMessage` → `dispatchToAgent` → `chatWithAgent` → `sendReplyWithActions`。这**目前足够用**——`dispatchToAgent` 只有约 15 行，工作正常。

但随着功能增长，可能受限于：
1. 多步骤工作流（分类意图 → 获取上下文 → 调用 agent → 后处理 → 回复）
2. 每步超时控制（整个链共享一个 context timeout）
3. 检查点/恢复（RC API 调用失败时 agent 响应会丢失）
4. 步骤级可观测性

> **YAGNI 注意：** 本计划是前瞻性的。目前没有用户请求多步骤工作流。仅在出现具体需求时实现。唯一可立即实施的是检查点功能，在阶段 1 中说明。

## 目标

1. 定义声明式 Flow 模型
2. 支持节点类型：agent 调用、action 执行、计算、检查点
3. 每节点超时控制和结果追踪
4. 与当前 handler 共存——不强制迁移

## 非目标

- 可视化 flow 编辑器或 DSL 解析器
- 替换当前 handler（两套系统共存）
- Flow 持久化或运行时修改
- 阶段 1 实现完整引擎

## 优先实施：仅检查点

本计划中唯一有即时价值的想法是**检查点**：在通过 RC API 发送前将 agent 回复保存到磁盘。防止 RC API 调用失败时丢失数据。

```go
// messaging/checkpoint.go

func SaveCheckpoint(chatID, reply string) error
func LoadCheckpoint(chatID string) (string, bool)
func ClearCheckpoint(chatID string)
```

在 `dispatchToAgent` 中集成：
```go
reply, err := h.chatWithAgent(ctx, ag, conversationID, message)
if err == nil && reply != "" {
    SaveCheckpoint(post.GroupID, reply) // 发送前保存
}
h.sendReplyWithActions(ctx, client, readClient, post, reply, placeholderID)
ClearCheckpoint(post.GroupID) // 成功发送后清除
```

启动时检查孤立检查点并重试发送。

## 未来：完整 Flow 引擎（按需实施）

### 核心类型

```go
// messaging/flow.go（未来）

type FlowOutcome string
const (
    OutcomeOk       FlowOutcome = "ok"
    OutcomeTimedOut FlowOutcome = "timed_out"
    OutcomeFailed   FlowOutcome = "failed"
)

type FlowNode interface {
    Name() string
    Run(ctx *FlowContext) (interface{}, error)
}

type FlowDefinition struct {
    Name    string
    StartAt string
    Nodes   map[string]FlowNode
    Edges   []FlowEdge
}
```

### 内置节点（未来）

- `AgentNode` — 调用 AI agent，带独立超时
- `ParseActionsNode` — 从回复中提取 ACTION 块
- `SendReplyNode` — 发送到 RingCentral
- `CheckpointNode` — 保存中间状态
- `ComputeNode` — 通用计算

## 文件布局

### 阶段 1（仅检查点）
```
messaging/checkpoint.go       # SaveCheckpoint, LoadCheckpoint, ClearCheckpoint
messaging/checkpoint_test.go  # 往返、孤立检测
```

### 未来（完整引擎）
```
messaging/flow.go             # FlowDefinition, FlowEngine, FlowContext
messaging/flow_nodes.go       # 内置节点类型
messaging/flow_test.go        # 引擎测试
```

## 实施阶段

### 阶段 1: 检查点（按需实施）
1. 创建 `messaging/checkpoint.go`
2. 集成到 `dispatchToAgent`
3. 启动时孤立检查点恢复
4. 测试：save/load 往返、清除、孤立检测

### 阶段 2-4: 完整引擎（推迟到有具体需求）
2. Flow 引擎核心：节点执行、边匹配、结果追踪
3. 默认 flows：simple_chat 替代 dispatchToAgent
4. 高级 flows：意图分类、多 agent 扇出

## 参考

- acpx `FlowDefinition`: startAt, nodes, edges
- acpx `FlowNodeResult`: outcome, duration, output
- acpx 节点类型: acp, action, compute, checkpoint
- acpx `src/flows/definition.ts`, `src/flows/runtime.ts`
