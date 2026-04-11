# 计划 001: Mock Agent 测试框架

**日期:** 2026-04-11
**优先级:** P0
**状态:** Draft
**参考:** acpx `2026-02-19-mock-agent-testing.md`, acpx `test/mock-agent.ts`

## 问题描述

RingClaw 目前没有协议级的 ACP 测试。现有的 `internal/testutil/mock_agent.go` 实现了 `Agent` 接口（Chat/ResetSession/SetCwd/Info），但不模拟 stdio 上的 ACP JSON-RPC 协议。当前 ACP agent 测试（`agent/acp_agent_test.go`）仅覆盖数据提取辅助函数（`extractChunkText`、`extractPromptResultText`）。

这意味着无法测试：
1. Session 生命周期（initialize → session/new → prompt → response）
2. 错误场景（超时、崩溃、协议错误）
3. 流式分块（`agent_message_chunk` 通知）
4. 完整的 Handler → ACPAgent → 回复管道
5. 多轮对话状态

## 目标

1. 构建协议级 ACP mock，通过 pipe 上的 JSON-RPC 2.0 通信
2. 支持可配置行为：固定回复、错误、延迟
3. 管理 session 状态（创建、追踪、重置）
4. 支持端到端测试：消息到达 → agent 调用 → 回复发送
5. 保持简单——测试工具，不是生产 agent 模拟器

## 非目标

- 完整 ACP spec 覆盖（terminal/*、fs/* 能力）——仅 session 和 prompt
- 替换现有 `internal/testutil/MockAgent`——接口级测试继续使用它
- CLI/HTTP agent 类型测试（需要单独 mock）
- 外部测试框架依赖——使用标准 `testing` 包

## 现有基础设施

- `internal/testutil/mock_agent.go` — 实现 `agent.Agent` 接口，记录 Chat 调用，返回可配置响应。**仅接口级别。**
- `internal/testutil/mock_sender.go` — 实现 `ringcentral.MessageSender`，记录所有 API 调用用于断言。
- `agent/acp_agent.go:Start()` — 通过 `cmd.StdinPipe()` / `cmd.StdoutPipe()` 建立 stdin/stdout pipe。mock 需要提供等效的 pipe 端点。

## 方案

### MockACPAgent

```go
// agent/mock_acp_test.go（仅测试用）

type MockACPBehavior struct {
    Reply         string                          // 默认回复文本
    DelayReply    time.Duration                   // 回复前延迟
    ErrorOnPrompt int                             // 第 N 次 prompt 出错（-1 = 每次）
    ErrorMessage  string                          // 错误消息
    PromptHandler func(prompt string) (string, error) // 自定义处理
}

type MockACPAgent struct {
    behavior    MockACPBehavior
    mu          sync.Mutex
    sessions    map[string][]string // sessionID -> 收到的 prompts
    promptCount int
}
```

### Pipe 连接

使用 `os.Pipe()` 创建 stdin/stdout 对，然后构造一个 `ACPAgent`，其子进程 pipe 指向 mock。

### 处理的 JSON-RPC 方法

| 方法 | 响应 |
|------|------|
| `initialize` | `{ protocolVersion: "2025-01-01", capabilities: {} }` |
| `session/new` | `{ id: "mock-session-{n}" }` |
| `session/prompt` | 根据 `MockACPBehavior` 配置 |
| `session/set_mode` | 空成功响应 |

## 文件布局

```
agent/mock_acp_test.go       # MockACPAgent + MockACPBehavior（仅测试）
agent/acp_agent_test.go      # 使用 MockACPAgent 的新协议级测试
```

## 实施阶段

### 阶段 1: 核心 Mock（目标）
1. 创建基于 pipe 的 JSON-RPC mock
2. 处理 `initialize`、`session/new`、`session/prompt`，固定回复
3. 测试：完整生命周期 — Start → initialize → session/new → prompt → response
4. 测试：多轮对话（复用同一 session）

### 阶段 2: 错误模拟
1. 添加 `ErrorOnPrompt`、`ErrorMessage`、`DelayReply`
2. 测试：agent 超时、协议错误、空响应
3. 测试：session 重置（ResetSession 创建新 session）

### 阶段 3: Handler 集成
1. 端到端测试：Handler + MockACPAgent + MockSender
2. 测试：消息到达 → agent 调用 → SendTextReply 记录正确回复
3. 测试：ACTION 块解析和执行

## 参考

- acpx `test/mock-agent.ts`: 完整 ACP 协议 mock，可配置行为
- `agent/acp_agent.go:Start()`: pipe 设置
- `agent/acp_rpc.go`: JSON-RPC 消息解析和分发
