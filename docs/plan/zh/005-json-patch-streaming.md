# Plan 005: JSON Patch+ 流式更新

**日期:** 2026-04-11  
**优先级:** P4  
**状态:** Draft  
**依赖:** Plan 004（Mock Agent，用于测试流式）
**参考:** acpx `json-patch-plus.md`, acpx `2026-03-31-flow-replay-live-transport.md`

## Problem Statement

RingClaw 当前流式实现是两步过程：
1. 立即发送 "Thinking..." 占位符（`SendTypingPlaceholder`）
2. agent 完整响应就绪后更新占位符（`UpdatePostText`）

问题：
1. **无增量更新**：用户可能数分钟只看到 "Thinking..."，然后完整回复突然出现
2. **长响应浪费 token**：agent 写长回复时用户无法提前阅读
3. **取消无反馈**：用户发 `/new` 中断时无法显示部分进度
4. **更新冲突**：快速更新可能因 RC API 限流失败

## Goals

1. 支持增量流式更新：逐步在占位符中构建回复
2. 定义内部 JSON Patch+ 协议用于高效 diff 计算
3. 实现 "append" 操作优化文本流式（避免重发全文）
4. 限流更新以遵守 RingCentral API 限制
5. 支持取消和部分结果展示

## Non-Goals

- 不实现 SSE 传输到 RC 客户端（RC 用 WebSocket）
- 不改变 RC API 客户端——只更频繁调用 UpdatePostText
- 不实现逐字符流式（API 调用过多）
- Phase 1 不对 CLI/HTTP agent 添加流式支持

## Proposed Solution

### StreamState — 限流流式状态

```go
// messaging/streaming.go

// StreamingConfig 流式更新配置
type StreamingConfig struct {
    Enabled     bool          // 是否启用（默认 true）
    MinInterval time.Duration // 最小更新间隔（默认 2s）
    MinNewChars int           // 最小新字符数（默认 100）
    MaxUpdates  int           // 每消息最大更新次数（默认 20）
}

// StreamState 追踪流式响应状态
type StreamState struct {
    mu            sync.Mutex
    chatID        string
    placeholderID string
    client        *ringcentral.Client
    fullText      strings.Builder // 累积文本
    lastSentLen   int             // 上次发送的长度
    lastSentAt    time.Time       // 上次发送时间
    updateCount   int             // 更新计数
    done          bool
    config        StreamingConfig
}

func NewStreamState(chatID, placeholderID string, client *ringcentral.Client, cfg StreamingConfig) *StreamState

// Append 添加新文本，满足条件时发送更新
func (s *StreamState) Append(ctx context.Context, chunk string) error

// Finalize 发送最终文本，标记完成
func (s *StreamState) Finalize(ctx context.Context, finalText string) error

// Cancel 取消流式，显示部分结果
func (s *StreamState) Cancel(ctx context.Context, partialText string)
```

### JSON Patch+ 内部协议

这是内部抽象，不在线上传输（RC API 不支持 patch 操作）：

```go
// messaging/streaming_patch.go

// PatchOp JSON Patch+ 操作
type PatchOp struct {
    Op    string `json:"op"`    // "replace", "append"
    Path  string `json:"path"`  // "/text"
    Value string `json:"value"` // 新值或追加文本
}

// Diff 计算旧文本到新文本的 patch
// 纯追加 → append（只发增量），否则 → replace
func Diff(oldText, newText string) []PatchOp

// Apply 将 patch 应用到基础文本
func Apply(base string, ops []PatchOp) string
```

### Agent 接口扩展

```go
// agent/agent.go

// Streamer 可选接口，支持流式响应的 agent 实现
type Streamer interface {
    ChatStream(ctx context.Context, conversationID string, message string, cb StreamCallback) (string, error)
}

// StreamCallback 接收增量文本分块
type StreamCallback func(chunk string)
```

### 与 Handler 集成

```go
// handler.go — 更新 dispatchToAgent

placeholderID, _ := SendTypingPlaceholder(ctx, client, post.GroupID)

var streamState *StreamState
if h.streamingConfig.Enabled && placeholderID != "" {
    streamState = NewStreamState(post.GroupID, placeholderID, client, h.streamingConfig)
}

// ACP agent: ChatStream 通过回调接收分块
// 非 ACP agent: 仍用 Chat，streamState 为 nil
reply, err := h.chatWithAgentStreaming(ctx, ag, conversationID, message, streamState)

if streamState != nil {
    cleanReply, actions := ParseAgentActions(reply)
    streamState.Finalize(ctx, cleanReply)
}
```

## File Layout

新文件：
```
messaging/streaming.go       # StreamState、StreamingConfig
messaging/streaming_patch.go # Diff、Apply
messaging/streaming_test.go  # 流式和 patch 测试
```

修改文件：
```
agent/acp_agent.go           # 添加 StreamCallback、ChatStream 方法
agent/agent.go               # 添加 Streamer 可选接口
messaging/handler.go         # 添加 streamingConfig、集成 StreamState
cmd/start_init.go            # 配置流式参数
config/config.go             # 添加 StreamingConfig
```

## Implementation Phases

### Phase 1: StreamState 和限流
1. 创建 `messaging/streaming.go`
2. Append 限流：MinInterval + MinNewChars + MaxUpdates
3. Finalize 和 Cancel
4. 测试限流行为

### Phase 2: JSON Patch+ diff
1. 创建 `messaging/streaming_patch.go`
2. replace → append 归一化
3. 测试：空 base → replace、追加 → append、修改 → replace
4. 往返测试：Apply(base, Diff(base, new)) == new

### Phase 3: ACP 流式支持
1. 添加 `StreamCallback` 和 `Streamer` 接口
2. `ACPAgent.ChatStream` — 同 Chat 但分块回调
3. 用 MockACPAgent 测试

### Phase 4: Handler 集成
1. 添加 `streamingConfig` 到 Handler
2. `dispatchToAgent` 创建 StreamState
3. 非 ACP agent 降级到现有逻辑
4. 端到端测试

## Test Plan

- StreamState 测试：Append 累积、限流条件、Finalize、Cancel、并发（-race）
- JSON Patch+ 测试：Diff 场景、Apply 往返
- 集成测试：MockACP + MockSender 端到端、禁用时降级、限流行为

## References

- acpx JSON Patch+：扩展 RFC 6902 添加 "append" 操作
- acpx SSE 传输：snapshot + incremental patches
- acpx replace → append 归一化用于文本流式
- acpx `examples/flows/replay-viewer/src/lib/json-patch-plus.ts`
