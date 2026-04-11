# 计划 002: Session 持久化

**日期:** 2026-04-11
**优先级:** P1
**状态:** Draft
**参考:** acpx `2026-02-17-session-management.md`, acpx `src/session/persistence/`

## 问题描述

ACP agent 的 session 映射（`conversationID → sessionID`）仅存储在内存中（`agent/acp_agent.go:59: sessions map[string]string`）。进程重启（升级、崩溃、`/reload`、机器重启）后所有 session 丢失：

1. 重启后每次对话从头开始，丢失多轮上下文
2. ACP 子进程可能仍在运行并持有有效 session，但 RingClaw 已丢失映射
3. `/new` 命令的状态不会持久化

## 目标

1. 将 `conversationID → sessionID` 映射持久化到磁盘，重启后恢复
2. 包含最少元数据（agent 名称、时间戳）用于诊断
3. 实现过期 session 清理
4. 保持 `Agent` 接口不变

## 非目标

- 持久化完整消息历史（由 ACP agent 自行管理）
- 跨机器 session 复制
- Session 加密
- CLI/HTTP agent session 持久化（Phase 1 仅 ACP）

## 方案

### SessionRecord（最小化）

```go
// agent/session_store.go

type SessionRecord struct {
    ConversationID string    `json:"conversationId"`
    ACPSessionID   string    `json:"acpSessionId"`
    AgentName      string    `json:"agentName"`
    CreatedAt      time.Time `json:"createdAt"`
    LastUsedAt     time.Time `json:"lastUsedAt"`
}
```

### SessionStore

```go
type SessionStore struct {
    mu      sync.RWMutex
    path    string                    // ~/.ringclaw/sessions.json
    records map[string]*SessionRecord // conversationID -> record
}

func NewSessionStore(path string) *SessionStore
func (s *SessionStore) Load() error
func (s *SessionStore) Save() error
func (s *SessionStore) Get(convID string) (string, bool)   // 返回 sessionID
func (s *SessionStore) Put(r *SessionRecord) error
func (s *SessionStore) Touch(convID string)
func (s *SessionStore) Delete(convID string)
func (s *SessionStore) CleanStale(maxAge time.Duration) int
```

### ACPAgent 集成

关键改动 `getOrCreateSession`：
1. 先查 `store.Get(conversationID)` — 有则复用 sessionID
2. 如果 agent 拒绝该 sessionID（session 过期/未知），则创建新 session 并更新 store
3. 成功聊天后调用 `store.Touch(conversationID)`
4. `ResetSession` 时调用 `store.Delete(conversationID)`

### 恢复策略

不需要 PID 存活检查。简单方案：
1. 启动时从磁盘加载 sessions
2. 首次使用恢复的 session 时正常发送 prompt
3. 如果 agent 返回 session 错误，透明地创建新 session
4. 这覆盖所有场景：agent 仍在运行（复用成功）、agent 已重启（降级成功）

> **注意：** 检查 ACP `session/new` 是否支持 `resume` 参数。如果 claude-agent-acp 支持，可以传递旧 sessionID 作为提示。

## 文件布局

```
agent/session_store.go       # SessionRecord, SessionStore
agent/session_store_test.go  # CRUD, Save/Load 往返, CleanStale, 并发
```

修改文件：
```
agent/acp_agent.go           # 添加 store 字段，集成到 getOrCreateSession/chat/Reset
agent/registry.go            # 通过 Create() 传递 SessionStore
cmd/start_init.go            # 初始化 SessionStore，启动时 CleanStale(7天)
```

## 实施阶段

### 阶段 1: SessionStore 实现
1. 创建 `agent/session_store.go`，CRUD + Load/Save
2. 单文件 `~/.ringclaw/sessions.json`
3. 测试：Put/Get/Touch/Delete/CleanStale/并发访问（-race）

### 阶段 2: ACPAgent 集成
1. 添加 `store` 字段
2. `getOrCreateSession` 优先查 store
3. session 错误时透明降级
4. `ResetSession` 从 store 删除

### 阶段 3: 启动集成
1. `cmd/start_init.go` 创建 SessionStore，调用 Load()
2. 启动时 `CleanStale(7 * 24 * time.Hour)`
3. 通过 agent registry 传递 store

## 参考

- acpx `src/session/persistence/repository.ts`: JSON 文件存储
- acpx SessionRecord: acpxRecordId, acpSessionId, agentCommand 等
- acpx loadSession 协议用于恢复
