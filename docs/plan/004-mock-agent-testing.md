# Plan 004: Mock Agent 测试框架

**日期:** 2026-04-11  
**优先级:** P3  
**状态:** Draft  
**参考:** acpx `2026-02-19-mock-agent-testing.md`, acpx `test/mock-agent.ts`

## Problem Statement

RingClaw 当前测试使用简单 stub agent：

```go
type testAgent struct { reply string }
func (a *testAgent) Chat(context.Context, string, string) (string, error) {
    return a.reply, nil
}
```

无法模拟：
1. 多轮会话状态
2. 流式分块响应
3. 权限请求
4. 工具调用序列
5. 中途错误（超时、崩溃、协议错误）
6. 图片处理
7. Session 创建/重置生命周期

ACP agent 测试（`agent/acp_agent_test.go`）只测试数据提取辅助函数，不测试实际 JSON-RPC 协议交互。

## Goals

1. 构建完整 ACP 协议 mock，通过 stdin/stdout 上的 JSON-RPC 2.0 通信
2. 支持可配置行为：固定回复、流式分块、错误、延迟
3. 管理 session 状态（创建、跟踪、重置）
4. 支持命令式 prompt 处理（特殊 prompt 触发特殊行为）
5. 支持错误模拟：超时、崩溃、协议错误、权限拒绝
6. 与现有 `httptest.Server` 模式配合使用

## Non-Goals

- 不构建生产 agent 模拟器——仅测试用
- 不支持完整 ACP spec（terminal、fs）——仅 session 和 prompt
- 不替换现有测试——增量增强
- 不测试 CLI/HTTP agent 类型（需单独 mock）

## Proposed Solution

### 核心：MockACPAgent

```go
// agent/mock_acp.go

// MockACPBehavior 配置 mock agent 行为
type MockACPBehavior struct {
    Reply            string                    // 默认回复
    StreamChunks     bool                      // 发送分块通知
    ChunkInterval    time.Duration             // 分块间隔
    ErrorOnPrompt    *int                      // 第 N 次 prompt 时出错（-1 = 每次都错）
    ErrorType        string                    // "timeout", "crash", "protocol"
    DelayBeforeReply time.Duration             // 回复前延迟
    PermissionRequests []MockPermission         // 权限请求模拟
    PromptHandler    func(prompt string) (string, error) // 自定义 prompt 处理
}

// MockACPAgent 是完整的 ACP 协议 mock
type MockACPAgent struct {
    behavior    MockACPBehavior
    mu          sync.Mutex
    sessions    map[string]*mockSession // sessionID -> state
    promptCount int64
}

// 关键方法
func NewMockACPAgent(behavior MockACPBehavior) *MockACPAgent

// Start 通过 pipe 启动 mock，返回一个真实的 ACPAgent
// ACPAgent 以为自己在和真实 agent 子进程通信
func (m *MockACPAgent) Start(ctx context.Context) (*ACPAgent, error)

// 处理的 JSON-RPC 方法：
// - initialize → 返回协议版本和能力
// - session/new → 创建 mock session，返回 sessionID
// - session/prompt → 根据 behavior 返回回复或错误
// - session/set_mode → 空响应

// 查询方法（用于测试断言）
func (m *MockACPAgent) PromptCount() int
func (m *MockACPAgent) SessionCount() int
func (m *MockACPAgent) GetSessionPrompts(sessionID string) []string
```

### 命令式 Prompt 处理

通过 `PromptHandler` 支持特殊命令：

```go
PromptHandler: func(prompt string) (string, error) {
    switch {
    case strings.HasPrefix(prompt, "ECHO:"):
        return prompt[5:], nil
    case strings.HasPrefix(prompt, "ERROR:"):
        return "", fmt.Errorf(prompt[6:])
    case strings.HasPrefix(prompt, "ACTION:"):
        return "Here is the note.\n\nACTION:NOTE title=Test\nBody text\nEND_ACTION", nil
    case strings.HasPrefix(prompt, "DELAY:"):
        secs, _ := strconv.Atoi(prompt[6:])
        time.Sleep(time.Duration(secs) * time.Second)
        return "delayed reply", nil
    default:
        return "default mock reply", nil
    }
}
```

### 与 ACPAgent 集成

```go
// 测试中使用
func TestACPAgentWithMock(t *testing.T) {
    mock := NewMockACPAgent(MockACPBehavior{
        Reply: "Hello from mock",
    })
    
    agent, err := mock.Start(context.Background())
    require.NoError(t, err)
    
    reply, err := agent.Chat(ctx, "conv-1", "Hello")
    assert.NoError(t, err)
    assert.Equal(t, "Hello from mock", reply)
    
    assert.Equal(t, 1, mock.PromptCount())
}
```

## File Layout

新文件：
```
agent/mock_acp.go           # MockACPAgent、MockACPBehavior
agent/mock_acp_test.go      # Mock 自身的单元测试
```

修改文件：
```
messaging/handler_test.go   # 添加使用 MockACPAgent 的集成测试
agent/acp_agent_test.go     # 添加协议级测试
```

## Implementation Phases

### Phase 1: MockACP 核心
1. 创建 `agent/mock_acp.go`
2. 实现 initialize、session/new、session/prompt
3. pipe 连接机制
4. 测试：握手、session 创建、固定回复

### Phase 2: 行为配置
1. 添加 `PromptHandler` 命令处理
2. 添加错误模拟（ErrorOnPrompt、ErrorType）
3. 添加流式分块模拟
4. 测试每种行为模式

### Phase 3: 与 ACPAgent 集成
1. `Start` 方法创建连接到 mock 的真实 ACPAgent
2. 测试完整生命周期：Start → initialize → session/new → prompt → response
3. 测试多轮对话
4. 测试 session 重置

### Phase 4: Handler 集成测试
1. 编写端到端测试：incoming RC post → agent call → reply sent
2. 测试错误场景：agent 超时、空响应、崩溃
3. 测试 ACTION block 完整流程

## Test Plan

- MockACP 单元测试：initialize、session/new、prompt、streaming、error simulation
- ACPAgent + MockACP 集成：完整握手、多轮、session 重置、图片 prompt、并发 session
- Handler + MockACP 集成：HandleMessage → agent → reply、/new 重置 session、ACTION 执行

## References

- acpx 完整 ACP 协议 mock：`test/mock-agent.ts`
- acpx 可配置行为：hangOnNewSession, loadSessionNotFound, setSessionModeFails
- acpx 命令式 prompt 处理：echo、error、delay
- acpx session 状态管理
