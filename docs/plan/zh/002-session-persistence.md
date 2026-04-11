# Plan 002: Session 持久化与自动恢复

**日期:** 2026-04-11  
**优先级:** P1  
**状态:** Draft  
**参考:** acpx `2026-02-17-session-management.md`, acpx `2026-02-27-acpx-session-model.md`, acpx `src/session/`

## Problem Statement

RingClaw ACP agent 的 session 映射（`sessions map[string]string`，conversationID → sessionID）完全在内存中。进程重启（升级、崩溃、`/reload`、机器重启）后所有 session 丢失：

1. 每次对话重启后从头开始，丢失多轮上下文
2. ACP 子进程可能仍在运行并持有 session，但 RingClaw 已丢失映射
3. `/new` 命令无法持久化"已关闭"状态

当前状态（`agent/acp_agent.go`）：
- `sessions map[string]string` — 纯内存
- `sessions: make(map[string]string)` — 启动时总是空的
- 无持久化、无恢复

## Goals

1. 持久化 conversationID → sessionID 映射到磁盘，重启后恢复
2. Session 记录包含元数据（创建时间、最后使用、PID、状态）
3. 提供 session 索引支持快速查找
4. 实现过期/废弃 session 清理
5. 保持 `Agent` 接口不变

## Non-Goals

- 不持久化完整消息历史（由 ACP agent 自行管理）
- 不实现跨机器 session 复制
- Phase 1 不改 CLI/HTTP agent session 模型
- 不实现 session 加密

## Proposed Solution

### 核心类型

```go
// agent/session_store.go

// SessionRecord 表示一个持久化的 session 映射
type SessionRecord struct {
    ID             string    `json:"id"`                // 唯一 record ID
    ConversationID string    `json:"conversationId"`     // e.g. "rc:dm:{chatID}:{creatorID}"
    ACPSessionID   string    `json:"acpSessionId"`       // Agent 分配的 session ID
    AgentCommand   string    `json:"agentCommand"`       // Agent 二进制路径
    AgentName      string    `json:"agentName"`          // 配置中的 agent 名
    Cwd            string    `json:"cwd"`                // 创建时的工作目录
    CreatedAt      time.Time `json:"createdAt"`
    LastUsedAt     time.Time `json:"lastUsedAt"`
    PID            int       `json:"pid,omitempty"`      // 子进程 PID
    Closed         bool      `json:"closed,omitempty"`   // 是否已关闭
    MessageCount   int       `json:"messageCount"`       // 消息计数
}

// SessionStore 持久化和检索 session 记录
type SessionStore struct {
    mu      sync.RWMutex
    dir     string                    // ~/.ringclaw/sessions/
    records map[string]*SessionRecord // conversationID -> record
    index   map[string]string         // acpSessionID -> conversationID
}

// 关键方法
func NewSessionStore(dir string) *SessionStore
func DefaultSessionDir() (string, error)  // ~/.ringclaw/sessions/
func (s *SessionStore) Load() error
func (s *SessionStore) Save() error
func (s *SessionStore) Get(conversationID string) (*SessionRecord, bool)
func (s *SessionStore) GetBySessionID(sessionID string) (*SessionRecord, bool)
func (s *SessionStore) Put(r *SessionRecord) error
func (s *SessionStore) Touch(conversationID string) error  // 更新 LastUsedAt
func (s *SessionStore) Close(conversationID string) error   // 标记关闭
func (s *SessionStore) CleanStale(maxAge time.Duration) int
func (s *SessionStore) List() []SessionRecord
```

### ACPAgent 集成

```go
type ACPAgent struct {
    // ... 现有字段 ...
    store *SessionStore // nil = 禁用持久化
}
```

关键改动：
1. **`getOrCreateSession`**：先查 `store.Get(conversationID)`，有未关闭记录则复用 ACP session ID
2. **`chatWithEntries`**：成功后调用 `store.Touch(conversationID)`
3. **`ResetSession`**：调用 `store.Close(conversationID)` 而非 `delete(a.sessions, ...)`
4. **`Stop`**：持久化当前 sessions

### Session 存活检测

重启恢复时，ACP 子进程可能已被杀死。恢复逻辑：
1. 检查 PID 是否存活（`os.FindProcess` + signal 0）
2. 如果存活，发送测试 RPC 验证 session 仍存在
3. 如果死亡或 session 不存在，创建新 session 并更新记录

## File Layout

新文件：
```
agent/session_store.go       # SessionRecord、SessionStore
agent/session_store_test.go  # 存储操作测试
```

修改文件：
```
agent/acp_agent.go           # 添加 store 字段，集成到 getOrCreateSession/chat/Reset/Stop
agent/acp_agent_test.go      # 持久化测试
cmd/start_init.go            # 初始化 SessionStore，传递给 agent 创建
```

## Implementation Phases

### Phase 1: SessionStore 实现
1. 创建 `agent/session_store.go`
2. CRUD、Load/Save、CleanStale
3. 完整测试：Put/Get/Touch/Delete/Close/CleanStale/并发

### Phase 2: ACPAgent 集成
1. 添加 `store` 字段
2. `NewACPAgent` 接受可选 `SessionStore`
3. `getOrCreateSession` 查 store 优先
4. `chatWithEntries` 调用 `Touch`
5. `ResetSession` 调用 `Close`

### Phase 3: 启动集成
1. `cmd/start_init.go` 创建 `SessionStore` 并 `Load()`
2. 通过 registry 传递到 agent 创建
3. 启动时 `CleanStale(7 * 24 * time.Hour)` 清理旧 session

### Phase 4: Session 存活检测
1. PID 存活检查
2. ACP session 验证 RPC
3. 透明降级：session 不存在时自动创建新的

## Test Plan

- SessionStore 单元测试：CRUD、Save+Load 往返、CleanStale、并发（-race）
- ACPAgent 持久化测试：getOrCreateSession 从 store 读取、ResetSession 关闭记录
- 集成测试：create → stop → load → resume 完整周期

## References

- acpx SessionRecord：acpxRecordId, acpSessionId, agentCommand, cwd, createdAt, lastUsedAt, pid, closed
- acpx session 存储：JSON in `~/.acpx/sessions/`
- acpx loadSession 协议用于恢复
- acpx `src/session/persistence/repository.ts`
