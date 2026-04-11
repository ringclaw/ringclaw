# 计划 003: 结构化错误码

**日期:** 2026-04-11
**优先级:** P2
**状态:** Draft
**参考:** acpx `ACPX_ERROR_STRATEGY.md`, acpx `src/errors.ts`

## 问题描述

RingClaw 的 agent 错误使用原始 `fmt.Errorf`，没有分类。handler 将其包装为 `"Error: %v"` 显示给用户。这导致：

1. cron 重试逻辑无法区分超时 vs 崩溃 vs 空响应
2. 无法给用户显示可操作的消息（"agent 超时，请重试" vs "agent 崩溃，请联系管理员"）
3. 日志无法按错误类型聚合

**注意：** API 层（`api/server.go`）已通过 `jsonError(w, msg, code)` 正确处理 HTTP 状态码——本计划仅关注 **agent 错误分类**。

## 目标

1. 定义少量 agent 错误码，覆盖 3 个关键场景
2. 添加 `AgentError` 类型，带 retryable 标记
3. 在 handler 中用于用户消息，在 cron 中用于重试决策
4. 向后兼容——`error` 接口不变

## 非目标

- 全代码库错误重写（108 个 `fmt.Errorf` 调用目前工作正常）
- 包装 ACP JSON-RPC 错误（spec 定义的 -32600 等已有结构）
- HTTP 状态码映射（`api/server.go` 已正确处理）
- 错误遥测/上报

## 方案

### AgentError 类型

```go
// agent/errors.go

type AgentErrorCode string

const (
    ErrAgentTimeout  AgentErrorCode = "AGENT_TIMEOUT"
    ErrAgentCrash    AgentErrorCode = "AGENT_CRASH"
    ErrAgentEmpty    AgentErrorCode = "AGENT_EMPTY"
)

type AgentError struct {
    Code      AgentErrorCode
    Message   string
    Retryable bool
    Cause     error
}

func (e *AgentError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func (e *AgentError) Unwrap() error { return e.Cause }

// 构造函数
func ErrTimeout(cause error) *AgentError
func ErrCrash(cause error) *AgentError
func ErrEmpty() *AgentError

// 辅助函数
func IsRetryable(err error) bool
func UserMessage(err error) string
```

### 集成点

1. **`agent/acp_agent.go`** — 包装超时/崩溃/空错误
2. **`messaging/handler.go`** — 使用 `agent.UserMessage()` 显示用户消息
3. **`messaging/cron.go`** — 使用 `agent.IsRetryable()` 决定重试策略

## 文件布局

```
agent/errors.go       # AgentError, 错误码, 构造函数, 辅助函数
agent/errors_test.go  # Error(), Unwrap(), IsRetryable(), UserMessage()
```

## 实施阶段

### 阶段 1: 错误类型
1. 创建 `agent/errors.go`，3 个错误码 + 构造函数 + 辅助函数
2. 测试 Error()、Unwrap()、IsRetryable()、UserMessage()

### 阶段 2: Agent 集成
1. 在 `acp_agent.go` 中包装错误（timeout、crash、empty）
2. 在 `cli_agent.go` 中包装子进程错误
3. 验证现有测试通过

### 阶段 3: Handler 集成
1. 更新 `handler.go` 使用 `agent.UserMessage()`
2. 更新 `cron.go` 使用 `agent.IsRetryable()`

## 参考

- acpx 错误码: NO_SESSION, TIMEOUT, PERMISSION_DENIED, RUNTIME, USAGE
- acpx NormalizedOutputError: code, message, origin, retryable
