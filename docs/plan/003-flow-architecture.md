# Plan 003: Flow 流程引擎

**日期:** 2026-04-11  
**优先级:** P2  
**状态:** Draft  
**依赖:** Plan 001（错误码，用于节点结果）
**参考:** acpx `2026-03-25-acpx-flows-architecture.md`, acpx `src/flows/`

## Problem Statement

RingClaw 消息处理是单一函数链：`HandleMessage` → `dispatchToAgent` → `chatWithAgent` → `sendReplyWithActions`。这种过程式方法限制：

1. 无法组合多步骤工作流（如：分类意图 → 获取上下文 → 调用 agent → 后处理 → 回复）
2. 超时处理是全有或全无（整个链共享 context timeout）
3. 无中间状态检查点（agent 调用成功但 RC API 失败时丢失 agent 响应）
4. 无步骤级可观测性
5. 添加新功能需直接编辑 `handler.go`

## Goals

1. 定义声明式 Flow 模型，消息处理是类型化节点的图
2. 支持节点类型：agent 调用、action 执行、计算、检查点、条件分支
3. 提供每节点超时控制和结果追踪
4. 支持复杂多步骤 flow 组合，无需修改核心 handler
5. 收集每节点计时指标

## Non-Goals

- Phase 1 不构建可视化 flow 编辑器或 DSL 解析器
- 不立即替换现有 handler——两套系统共存
- 不实现 flow 持久化（flow 在代码中定义）
- 不支持运行时动态修改 flow

## Proposed Solution

### 核心类型

```go
// messaging/flow.go

// FlowOutcome 节点执行结果状态
type FlowOutcome string

const (
    OutcomeOk        FlowOutcome = "ok"
    OutcomeTimedOut  FlowOutcome = "timed_out"
    OutcomeFailed    FlowOutcome = "failed"
    OutcomeCancelled FlowOutcome = "cancelled"
    OutcomeSkipped   FlowOutcome = "skipped"
)

// FlowNodeResult 单个节点执行结果
type FlowNodeResult struct {
    NodeName string       `json:"nodeName"`
    Outcome  FlowOutcome  `json:"outcome"`
    Duration time.Duration `json:"duration"`
    Output   interface{}  `json:"output,omitempty"`
    Error    error        `json:"error,omitempty"`
}

// FlowContext 在节点间传递状态
type FlowContext struct {
    Ctx            context.Context
    Cancel         context.CancelFunc
    ConversationID string
    ChatID         string
    Post           PostContext       // 输入消息详情
    AgentName      string
    Message        string           // 原始消息
    Reply          string           // Agent 回复
    PlaceholderID  string          // "Thinking..." post ID
    Actions        []AgentAction    // 解析的 actions
    Results        []FlowNodeResult // 累积结果
    Metadata       map[string]interface{} // 节点间传递数据
}

// FlowNode 单个处理步骤
type FlowNode interface {
    Name() string
    Run(ctx *FlowContext) (interface{}, error)
}

// FlowEdge 基于结果的节点间转换
type FlowEdge struct {
    From    string       // 源节点名
    Outcome FlowOutcome  // 匹配此结果（空 = 任意）
    To      string       // 目标节点名（空 = 结束）
}

// FlowDefinition 声明式处理流程
type FlowDefinition struct {
    Name    string              `json:"name"`
    StartAt string              `json:"startAt"`
    Nodes   map[string]FlowNode `json:"-"`
    Edges   []FlowEdge          `json:"edges"`
}

// FlowEngine 执行 FlowDefinition
type FlowEngine struct {
    flows map[string]*FlowDefinition
}

func NewFlowEngine() *FlowEngine
func (e *FlowEngine) Register(def *FlowDefinition)
func (e *FlowEngine) Run(ctx context.Context, flowName string, fc *FlowContext) *FlowContext
```

### 内置节点类型

```go
// messaging/flow_nodes.go

// AgentNode 调用 AI agent，结果存入 FlowContext.Reply
type AgentNode struct { name string; timeout time.Duration }

// ParseActionsNode 从 FlowContext.Reply 解析 ACTION blocks
type ParseActionsNode struct{}

// SendReplyNode 发送回复到 RingCentral
type SendReplyNode struct{}

// CheckpointNode 保存中间状态用于恢复
type CheckpointNode struct { key string }

// ComputeNode 通用计算节点
type ComputeNode struct {
    name string
    fn   func(fc *FlowContext) (interface{}, error)
}

// ConditionNode 条件分支节点
type ConditionNode struct {
    name string
    pred func(fc *FlowContext) (string, error) // 返回下一节点名
}
```

### 默认 Flow：Simple Chat

```go
// messaging/flow_default.go

func RegisterDefaultFlows(engine *FlowEngine) {
    engine.Register(&FlowDefinition{
        Name:    "simple_chat",
        StartAt: "agent",
        Nodes: map[string]FlowNode{
            "agent":        NewAgentNode("agent", 3*time.Minute),
            "parse_actions": &ParseActionsNode{},
            "checkpoint":    NewCheckpointNode("post_agent"),
            "send_reply":    &SendReplyNode{},
        },
        Edges: []FlowEdge{
            {From: "agent", Outcome: OutcomeOk, To: "parse_actions"},
            {From: "agent", Outcome: OutcomeTimedOut, To: "send_reply"},
            {From: "agent", Outcome: OutcomeFailed, To: "send_reply"},
            {From: "parse_actions", To: "checkpoint"},
            {From: "checkpoint", To: "send_reply"},
        },
    })
}
```

## File Layout

新文件：
```
messaging/flow.go          # FlowDefinition、FlowEngine、FlowContext
messaging/flow_nodes.go    # 内置节点类型
messaging/flow_default.go  # RegisterDefaultFlows
messaging/flow_test.go     # 引擎和节点测试
```

修改文件：
```
messaging/handler.go       # 添加 FlowEngine 字段，注册 flow 时使用
cmd/start_init.go          # 创建并注册 flow engine
```

## Implementation Phases

### Phase 1: Flow 引擎核心
1. 创建 `messaging/flow.go` 和 `messaging/flow_nodes.go`
2. 引擎执行、边匹配、结果追踪
3. 测试：线性/分支/超时/取消

### Phase 2: 默认 Flows
1. 创建 `messaging/flow_default.go`
2. `simple_chat` flow 替代当前 `dispatchToAgent` 逻辑
3. 端到端测试

### Phase 3: Handler 集成
1. 添加 `flowEngine` 到 Handler
2. `dispatchToAgent` 优先使用 flow，无 flow 时降级到现有逻辑
4. 添加 flow 执行日志和计时

### Phase 4: 高级 Flows（未来）
1. 意图分类 flow（classify → branch → summarize or chat）
2. 多 agent 扇出 flow
3. 带检查点恢复的重试 flow

## Test Plan

- 引擎测试：线性、分支、超时传播、取消、缺失节点
- 节点测试：AgentNode + mock、ParseActionsNode、CheckpointNode、ComputeNode
- 集成测试：simple_chat flow 输出与现有 handler 一致

## References

- acpx `FlowDefinition`：startAt、nodes map、edges
- acpx `FlowNodeResult`：outcome (ok/timed_out/failed/cancelled)、duration、output
- acpx 节点类型：acp、action、compute、checkpoint
- acpx `src/flows/definition.ts`, `src/flows/runtime.ts`
