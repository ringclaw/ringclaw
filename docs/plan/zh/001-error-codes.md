# Plan 001: 结构化错误码体系

**日期:** 2026-04-11  
**优先级:** P0  
**状态:** Draft  
**参考:** acpx `ACPX_ERROR_STRATEGY.md`, acpx `src/errors.ts`, acpx `src/acp/error-normalization.ts`

## Problem Statement

RingClaw 全局使用 `fmt.Errorf("...")` 生成错误，没有结构化错误类型和稳定错误码。上层无法程序化区分超时、会话丢失、权限拒绝或运行时崩溃：

1. 用户错误消息不一致（有的 "Error: ..."，有的 "Failed to ..."）
2. API 层（`api/` 包）无法映射到正确的 HTTP 状态码
3. 无法构建重试逻辑（缺少 `retryable` 标记）
4. 日志和监控无法按类型聚合

当前代码示例：
- `agent/acp_agent.go`: `fmt.Errorf("prompt error: %w", done.err)`
- `messaging/handler.go`: `fmt.Sprintf("Error: %v", err)`
- `messaging/cron.go`: 无法区分 agent 超时和 agent 崩溃

## Goals

1. 定义稳定的 `RingClawError` 类型，包含机器可读错误码
2. 定义错误来源（agent、handler、config、ringcentral）
3. 添加归一化层，将原始 error 包装为结构化 `RingClawError`
4. 所有用户错误路径产生一致、可操作的消息
5. 向后兼容——`error` 接口不变

## Non-Goals

- 不修改 ACP JSON-RPC 错误协议（由 ACP spec 定义）
- Phase 1 不对 RingCentral API 客户端错误做深度包装
- 不引入错误上报/遥测
- 不引入 gRPC 风格 status code

## Proposed Solution

### 核心类型

```go
// internal/errors/errors.go

package errors

// Code 是稳定的机器可读错误码
type Code string

const (
    // Session
    CodeNoSession      Code = "NO_SESSION"
    CodeSessionExpired Code = "SESSION_EXPIRED"

    // Agent
    CodeAgentStart    Code = "AGENT_START"
    CodeAgentCrash    Code = "AGENT_CRASH"
    CodeAgentTimeout  Code = "AGENT_TIMEOUT"
    CodeAgentEmpty    Code = "AGENT_EMPTY"
    CodeAgentProtocol Code = "AGENT_PROTOCOL"

    // Permission
    CodePermissionDenied Code = "PERMISSION_DENIED"

    // Config
    CodeConfigLoad    Code = "CONFIG_LOAD"
    CodeConfigMissing Code = "CONFIG_MISSING"

    // Runtime
    CodeRuntime   Code = "RUNTIME"
    CodeNetwork   Code = "NETWORK"
    CodeRateLimit Code = "RATE_LIMITED"
    CodeUsage     Code = "USAGE"

    // Resource
    CodeNotFound Code = "NOT_FOUND"
)

// Origin 标识错误来源子系统
type Origin string

const (
    OriginAgent       Origin = "agent"
    OriginHandler     Origin = "handler"
    OriginConfig      Origin = "config"
    OriginRingCentral Origin = "ringcentral"
    OriginAPI         Origin = "api"
)

// RingClawError 是结构化错误
type RingClawError struct {
    Code      Code   // 稳定错误码
    Message   string // 人类可读摘要（一行）
    Detail    string // 可选详细信息
    Origin    Origin // 子系统来源
    Retryable bool   // 是否可重试
    Cause     error  // 包装的底层错误
}

func (e *RingClawError) Error() string {
    if e.Detail != "" {
        return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Detail)
    }
    return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *RingClawError) Unwrap() error { return e.Cause }

// UserMessage 返回适合 RingCentral 聊天回复的消息
func (e *RingClawError) UserMessage() string {
    msg := e.Message
    if e.Retryable {
        msg += ". 请稍后重试。"
    }
    return msg
}

// 构造函数
func New(code Code, origin Origin, message string) *RingClawError
func Wrap(err error, code Code, origin Origin, message string) *RingClawError
func WrapRetryable(err error, code Code, origin Origin, message string) *RingClawError

// 提取函数（对非 RingClawError 降级处理）
func GetCode(err error) Code       // 返回 RUNTIME 作为默认
func IsRetryable(err error) bool
func UserMessage(err error) string // 通用降级
```

### 归一化层

```go
// internal/errors/normalize.go

// NormalizeAgentError 将原始 agent 错误转换为结构化错误
// 根据错误消息关键词匹配：timeout → AGENT_TIMEOUT, empty → AGENT_EMPTY,
// process exited → AGENT_CRASH, parse session → AGENT_PROTOCOL
func NormalizeAgentError(err error) *RingClawError

// NormalizeRPCError 将 ACP JSON-RPC 错误码映射
func NormalizeRPCError(code int, message string) *RingClawError

// HTTPStatusCode 将 RingClawError 映射到 HTTP 状态码
// NO_SESSION/NOT_FOUND → 404, PERMISSION_DENIED → 403,
// CONFIG_MISSING/USAGE → 400, AGENT_TIMEOUT → 504
func HTTPStatusCode(err error) int
```

### API 层集成

HTTP API 返回结构化 JSON 错误：
```json
{
  "error": {
    "code": "AGENT_TIMEOUT",
    "message": "Agent 响应超时",
    "retryable": true
  }
}
```

## File Layout

```
internal/errors/
  errors.go          # RingClawError 类型、常量、构造函数
  normalize.go       # NormalizeAgentError、NormalizeRPCError、HTTPStatusCode
  errors_test.go     # 构造函数和工具函数测试
  normalize_test.go  # 归一化逻辑测试
```

修改文件：
- `agent/acp_agent.go` — 用 `errors.NormalizeAgentError()` 包装
- `agent/acp_rpc.go` — 用 `errors.NormalizeRPCError()` 处理 JSON-RPC 错误
- `agent/cli_agent.go` — 包装子进程错误
- `agent/http_agent.go` — 包装 HTTP/解析错误
- `messaging/handler.go` — 用 `errors.UserMessage()` 处理回复
- `messaging/cron.go` — 用错误码区分重试策略
- `config/config.go` — 包装 Load/Save 错误
- `api/` — 用 `errors.HTTPStatusCode()` 返回状态码

## Implementation Phases

### Phase 1: Foundation
1. 创建 `internal/errors/errors.go` 和 `normalize.go`
2. 编写完整测试
3. 验证 `go vet ./...` 和 `go test ./internal/errors/ -v`

### Phase 2: Agent 集成
1. 更新 `agent/acp_agent.go` — 替换 `fmt.Errorf`
2. 更新 `agent/acp_rpc.go` — 使用 `NormalizeRPCError`
3. 更新 `agent/cli_agent.go` 和 `agent/http_agent.go`
4. 验证现有测试通过

### Phase 3: Handler 和 messaging 集成
1. 更新 `messaging/handler.go` — 使用 `UserMessage()`
2. 更新 `messaging/cron.go` — 基于错误码做重试策略
3. 更新 `config/config.go`

### Phase 4: API 层集成
1. 更新 `api/` 返回结构化 JSON 错误响应

## Test Plan

- RingClawError 单元测试：Error() 格式、Unwrap() 链、UserMessage()、GetCode() 降级
- 归一化测试：timeout/empty/crash/protocol 关键词匹配、RPC 错误码映射
- 集成测试：现有测试无回归、Handler 返回干净用户消息

## References

- acpx 稳定错误码：NO_SESSION, TIMEOUT, PERMISSION_DENIED, RUNTIME, USAGE
- acpx 错误来源：cli, runtime, queue, acp
- acpx NormalizedOutputError：code, message, detailCode, origin, retryable
- acpx `src/errors.ts`, `src/acp/error-normalization.ts`
