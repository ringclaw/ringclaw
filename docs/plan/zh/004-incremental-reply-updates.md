# 计划 004: 增量回复更新

**日期:** 2026-04-11
**优先级:** P3
**状态:** Draft
**参考:** acpx `json-patch-plus.md`, acpx `2026-03-31-flow-replay-live-transport.md`

## 问题描述

RingClaw 当前使用两步回复流程：
1. 立即发送 "Thinking..." 占位符（`SendTypingPlaceholder`）
2. agent 完成后替换为完整回复（`UpdatePostText`）

问题：
1. **无增量更新** — 用户可能等待数分钟只看到 "Thinking..."，然后完整回复突然出现
2. **浪费阅读时间** — 长回复时用户无法提前阅读
3. **取消无反馈** — 用户发 `/new` 中断时无法显示部分进度

## 关键发现：分块数据已经到达

ACP agent 在流式过程中已经发送 `agent_message_chunk` 通知。RingClaw 在 `acp_rpc.go:207` 接收到了但**直接抑制**：

```go
case "agent_message_chunk", "agent_thought_chunk":
    // Suppress noisy streaming chunks
```

数据已经到了——只需要累积并定期更新占位符。

## 目标

1. 分块到达时增量更新占位符
2. 限流更新以遵守 RingCentral API 速率限制
3. 支持取消时显示部分结果
4. 向后兼容——可通过配置禁用，非 ACP agent 不受影响

## 非目标

- SSE/WebSocket 传输到 RC 客户端（RC 自行处理推送）
- JSON Patch 协议（RC API 使用 `UpdatePostText` 全文替换，无 patch）
- 逐字符流式（API 调用过多）
- Phase 1 不支持 CLI/HTTP agent 流式

## 方案

### StreamState

```go
// messaging/streaming.go

type StreamingConfig struct {
    Enabled     bool          `json:"streaming_enabled"`
    MinInterval time.Duration `json:"streaming_interval"`    // 默认 2s
    MinNewChars int           `json:"streaming_min_chars"`   // 默认 100
    MaxUpdates  int           `json:"streaming_max_updates"` // 默认 20
}

type StreamState struct {
    mu            sync.Mutex
    chatID        string
    placeholderID string
    client        *ringcentral.Client
    text          strings.Builder
    lastSentLen   int
    lastSentAt    time.Time
    updateCount   int
    done          bool
    config        StreamingConfig
}

func (s *StreamState) Append(ctx context.Context, chunk string) error
func (s *StreamState) Finalize(ctx context.Context, finalText string) error
func (s *StreamState) Cancel(ctx context.Context)
```

### Agent 接口扩展

```go
// agent/agent.go

// Streamer 是可选接口，支持流式响应的 agent 实现
type Streamer interface {
    ChatStream(ctx context.Context, conversationID, message string, onChunk func(chunk string)) (string, error)
}
```

ACP agent 实现 `Streamer`，将 `agent_message_chunk` 通知转发到回调。非 ACP agent 继续使用 `Chat()`。

### RC API 速率限制

- RingCentral Team Messaging `PATCH /chats/{chatId}/posts/{postId}` 速率限制
- 典型限制约 30 req/min — `MinInterval=2s` ≈ 30 updates/min，应该安全
- `MaxUpdates=20` 提供额外安全网

## 文件布局

```
messaging/streaming.go       # StreamState, StreamingConfig
messaging/streaming_test.go  # 限流行为、Finalize、Cancel
```

修改文件：
```
agent/agent.go               # 添加 Streamer 可选接口
agent/acp_agent.go           # 实现 ChatStream，转发分块
agent/acp_rpc.go             # 停止抑制 agent_message_chunk，转发到回调
messaging/handler.go         # 添加 streamingCfg，可用时使用 ChatStream
config/config.go             # 添加 StreamingConfig
```

## 实施阶段

### 阶段 1: StreamState + 限流
1. 创建 `messaging/streaming.go`
2. Append 限流：MinInterval + MinNewChars + MaxUpdates
3. Finalize 和 Cancel
4. 测试限流行为

### 阶段 2: ACP 流式支持
1. 添加 `Streamer` 接口
2. 在 `agent/acp_agent.go` 实现 `ChatStream`
3. 转发 `agent_message_chunk` 通知而非抑制
4. 使用 MockACPAgent 测试（依赖计划 001）

### 阶段 3: Handler 集成
1. 添加 `streamingCfg` 到 Handler
2. 可用时使用 `ChatStream`，否则降级到 `Chat`
3. 使用 MockACPAgent + MockSender 端到端测试

## 参考

- `agent/acp_rpc.go:207`: `agent_message_chunk` 已接收但被抑制
- RC API: `PATCH /team-messaging/v1/chats/{chatId}/posts/{postId}`
- acpx SSE 传输: snapshot + incremental patches（RingClaw 不需要 patch 协议）
