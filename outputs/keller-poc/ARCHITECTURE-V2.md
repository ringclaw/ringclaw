# AgentRun · Agent 架构设计 V2
## 参考 Hermes Persona 逻辑的深度整合

---

## 一、Hermes 的核心设计哲学（读代码得出的第一手结论）

读了 `memory_tool.py`、`prompt_builder.py`、`skill_manager_tool.py`，Hermes 有三个设计决策对我们有直接价值：

### 1.1 Frozen Snapshot 模式（最重要）

```python
# memory_tool.py 注释原文：
"""
Both are injected into the system prompt as a frozen snapshot at session start.
Mid-session writes update files on disk immediately (durable) but do NOT change
the system prompt -- this preserves the prefix cache for the entire session.
The snapshot refreshes on the next session start.
"""
```

**含义**：Session 开始时读一次 memory，冻结为 system prompt 的一部分。
Session 中间写入 memory → 持久化到磁盘 → 但本次 session 的 system prompt 不变。
下次 session 才用新的 snapshot。

**为什么重要**：System prompt 稳定 → prefix cache 命中 → 每次 API 调用便宜 ~80%。

### 1.2 SOUL.md + Skills 索引分离

```python
# prompt_builder.py:
# SOUL.md = identity (slot #1 in system prompt)
# build_skills_system_prompt() = compact skill index (slot #2)
# MEMORY.md snapshot (slot #3)
# USER.md snapshot (slot #4)
```

**System prompt 组装顺序**：
```
SOUL.md（我是谁）
↓ 技能索引（我会哪些技能 — 每个只有名字 + 1 行描述）
↓ MEMORY.md snapshot（我知道的领域知识 — 冻结）
↓ USER.md snapshot（我了解的用户 — 冻结）
↓ 工具描述
```

当 agent 需要某个技能时，完整的 SKILL.md 内容才被加载注入。
**技能的详细步骤不占 SOUL 空间，按需加载。**

### 1.3 技能是可学习的程序性记忆

```python
# skill_manager_tool.py docstring:
"""
Skills are the agent's procedural memory: they capture *how to do a specific
type of task* based on proven experience.
General memory (MEMORY.md, USER.md) is broad and declarative.
Skills are narrow and actionable.
"""
```

Agent 可以从成功任务中自主创建新技能，形成**学习闭环**。

---

## 二、我们当前的问题（对照 Hermes 重新审视）

### 问题 1：每条消息都重新构建 system prompt（prefix cache 全失效）

```go
// 当前 persona/loader.go — 每条消息调用一次：
func (l *Loader) Build(_ context.Context, chatID, userID string, isDM bool) string {
    soul, _   := l.store.LoadSoul()                     // 文件读
    global, _ := l.store.LoadMemory(ScopeGlobal, "")    // 文件读
    user, _   := l.store.LoadMemory(ScopeUser, userID)  // 文件读
    chat, _   := l.store.LoadMemory(ScopeChat, chatID)  // 文件读
    // 拼装 XML banner
}
```

每条消息都重新读文件并重新拼装 → system prompt 每次都可能不同 → prefix cache 永远失效。

Keller 场景里 sarah-bot 一天处理 30 张派单，每张派单来回 3-5 条消息，
约 90-150 次 API 调用。当前每次都没有 prefix cache，多付 ~80% token 费用。

### 问题 2：技能步骤挤占 SOUL 空间，超 2000 chars 被截断

```
sarah-bot SOUL.md 当前内容（150 行）：
  · 身份（10 行）
  · dispatch-confirm 7 步骤（40 行）← 详细工作流
  · complaint-handling 检测规则（30 行）← 详细规则
  · 改单工作流（20 行）
  · 升级规则（15 行）
  · Agent 路由格式（15 行）
  · 记忆规则（10 行）
  · SMS 模板（10 行）
  总计 ~5000 chars → 超过 2000 chars 被截断 → 关键步骤消失
```

### 问题 3：Bot 不会从经验中学习，每次 Keller 场景里出现新情况都需要人手动更新 SOUL

---

## 三、改进后的 Persona 架构（Hermes 模式 × 业务 Agent 定制）

### 3.1 System Prompt 组装（新）

```
┌─────────────────────────────────────────────────────────┐
│ Slot 1: SOUL.md（身份，≤80 行，永不截断）               │
│   · 我是谁 + 声音规则                                   │
│   · 技能名称列表（skills: [dispatch-confirm, ...]）     │
│   · 团队访问规则（谁可以 @我，可以问什么）               │
│   · 路由声明（我发出 / 接收哪些 Agent 事件）             │
├─────────────────────────────────────────────────────────┤
│ Slot 2: Skills Index（按需加载，compact）                │
│   dispatch-confirm   · 派单执行 + CONFIRM 跟踪           │
│   complaint-handling · 投诉检测 + 路由                   │
│   （每行：名字 + 1 行描述，不展示步骤）                  │
├─────────────────────────────────────────────────────────┤
│ Slot 3: DOMAIN.md snapshot（冻结，session 开始时读）    │
│   · 业务领域知识：Keller SOPs, Lowe's 要求, 队员目录    │
│   · 对应 Hermes 的 MEMORY.md                           │
│   § 分隔符，char-based 限制（≤2200 chars）              │
├─────────────────────────────────────────────────────────┤
│ Slot 4: OWNER.md snapshot（冻结，session 开始时读）     │
│   · Owner 偏好：Sarah 的模板, Tom 的决策习惯            │
│   · 对应 Hermes 的 USER.md                             │
│   § 分隔符，char-based 限制（≤1375 chars）              │
├─────────────────────────────────────────────────────────┤
│ Slot 5: Chat Memory（每条消息读取，变化频繁）            │
│   · 今日 dispatch 列表                                  │
│   · 仍用当前 per-chat 机制                              │
│   char-based 限制（≤4000 chars）                        │
├─────────────────────────────────────────────────────────┤
│ Slot 6: Entity Memory（按需注入，有 entity_id 才加载）  │
│   · 当前对话涉及的业务实体（complaint-A8810, ...）      │
│   · NEW：跨 Agent 共享上下文                            │
└─────────────────────────────────────────────────────────┘
```

### 3.2 Frozen Snapshot 在 RingClaw 里的实现

```go
// persona/loader.go 修改方案

type PersonaSnapshot struct {
    soul   string    // SOUL.md 内容（一次性读）
    domain string    // DOMAIN.md 冻结快照
    owner  string    // OWNER.md 冻结快照
    // chat memory 不在快照里——它是 per-chat 的，每条消息读
}

type Loader struct {
    store    *Store
    snapshot *PersonaSnapshot    // session 开始时捕获，此后不变
}

// CaptureSnapshot 在 session 开始时调用一次
func (l *Loader) CaptureSnapshot() {
    if l.snapshot != nil {
        return // 已捕获，不重复
    }
    soul, _   := l.store.LoadSoul()
    domain, _ := l.store.LoadMemory(ScopeDomain, "")   // NEW scope
    owner, _  := l.store.LoadMemory(ScopeOwner, "")    // 替代 ScopeUser
    l.snapshot = &PersonaSnapshot{
        soul:   soul,
        domain: domain,
        owner:  owner,
    }
}

// Build 使用冻结快照 + 实时 chat memory
func (l *Loader) Build(_ context.Context, chatID string, entityID string) string {
    if l.snapshot == nil {
        l.CaptureSnapshot()
    }
    chat, _   := l.store.LoadMemory(ScopeChat, chatID)
    entity, _ := l.store.LoadMemory(ScopeEntity, entityID)  // 按需

    return assemble(
        l.snapshot.soul,    // 冻结，每次相同
        l.skillsIndex(),    // 冻结，session 内不变
        l.snapshot.domain,  // 冻结，每次相同
        l.snapshot.owner,   // 冻结，每次相同
        chat,               // 实时
        entity,             // 按需，有 entity_id 才加
    )
}
```

**效果**：
- SOUL + DOMAIN + OWNER + skills index 这 4 个 slot 在同一 session 内完全相同
- Prefix cache 命中率从 ~0% 提升到 ~70%（chat memory 是变化的，但它在末尾）
- Keller 场景节省约 60% token 成本

### 3.3 § 分隔符格式（从 Hermes 直接借鉴）

DOMAIN.md 和 OWNER.md 使用 `§` 分隔独立条目：

```markdown
# sarah-bot DOMAIN.md

Atlanta 门店标准 ZIP：30301-30350。邻近区域 Buford 是 30518。
§
Mike Reyes 手机：+14045550211。专项：Engineered Oak、LVT。
§
Carlos Ruiz 手机：+14045550234。专项：Tile、Hardwood。
§
Jenkins 客户：习惯选 Engineered Oak，通常安排周二上午。
§
Lowe's 完工单截止：签字后 24 小时内传真。
```

```markdown
# sarah-bot OWNER.md

Sarah 偏好回复语言：英文，≤4 行。
§
Sarah 常用改单原因："customer requested" 不需要详细说明。
§
Sarah 总是在 dispatch 时同时发客户确认 SMS，不需要单独提醒。
§
Atlanta Fairlie-Poplar 区域：ZIP 30303，GPS 经常误导到邻近 30308，需手动核实。
```

**§ 分隔的好处**：
- agent 可以精确添加/删除单个条目（不怕破坏 markdown 格式）
- char limit 按总字符数控制，不按行数
- 安全扫描在 entry 粒度执行

---

## 四、Skill 系统（Hermes SKILL.md 格式）

### 4.1 目录结构

```
~/.ringclaw/
├── SOUL.md             ← 身份（≤80 行）
├── memory/
│   ├── DOMAIN.md       ← 业务领域知识（§ 分隔，≤2200 chars）
│   ├── OWNER.md        ← Owner 偏好（§ 分隔，≤1375 chars）
│   ├── chat/
│   │   └── <chatID>.md ← 当前 chat session 状态（实时）
│   └── entities/       ← NEW
│       ├── complaint-A8810-20260603.md
│       └── dispatch-A8821-20260603.md
└── skills/             ← NEW
    ├── dispatch-confirm/
    │   ├── SKILL.md
    │   └── templates/
    │       └── dispatch-sms.md
    ├── complaint-handling/
    │   ├── SKILL.md
    │   └── references/
    │       └── complaint-signals.md
    └── daily-digest/
        └── SKILL.md
```

### 4.2 SKILL.md 格式（Hermes 标准 + 业务 Agent 扩展）

```yaml
---
name: dispatch-confirm
description: 派单执行 + CONFIRM 跟踪闭环
version: 1.2.0
author: sarah-bot            # 可由 agent 自动创建
metadata:
  tags: [dispatch, sms, confirm, task]
  related_skills: [complaint-handling]
  applicable_souls: [csr, store-mgr]   # 哪些 SOUL 可以使用
  entity_type: dispatch                # 激活时创建什么类型的 entity
prerequisites:
  capabilities: [sms]
  memory_keys: [crew_directory]        # DOMAIN.md 里需要有这些
---

# dispatch-confirm · 派单执行 + CONFIRM 跟踪

## 触发条件
用户说以下变体时激活：dispatch / assign / schedule / route + 指定了时间和人员

## 步骤

1. **解析** — 提取：工单号 · 队长 · 日期时间 · 完整地址 · 材料 · 客户信息
   - 缺少任何必填字段 → 停止，追问 owner
   
2. **ZIP 校验** — 核对 Atlanta ZIP（见 DOMAIN.md `ZIP` 条目）
   - 不匹配 → 停止，列两个候选地址
   
3. **创建 Task** → `ACTION:TASK subject="#{order} Install - {assignee}"`

4. **发送 SMS** → `ACTION:SMS to={phone}` 使用 templates/dispatch-sms.md

5. **回报 Owner** → 1 行：`✅ Task #{id} · SMS {phone} · delivered`

6. **写入 Entity Memory** → `memory/entities/dispatch-{order}-{date}.md`

7. **写入 Chat Memory** → 追加到今日 dispatch 列表

## 失败处理

| 情况 | 行为 |
|------|------|
| 队长不在 DOMAIN.md 目录里 | 停止，要求 owner 提供手机号 |
| ZIP 不匹配城市 | 停止，列候选地址 |
| SMS 失败（4xx）| 上报 owner，提供原始 payload |
| SMS 失败（5xx）| 30s 后重试一次，仍失败则上报 |
```

### 4.3 Skills 索引（注入 system prompt 的形态）

```
[Skills Available]
dispatch-confirm   · 派单执行 + CONFIRM 跟踪闭环
complaint-handling · 投诉检测 + 自动安抚 + Agent 路由
daily-digest       · 每日/每周摘要生成（TEXT ONLY）
```

这 3 行就是 system prompt 里的全部技能信息。
当 agent 判断要使用某个技能时，完整 SKILL.md 才被加载注入：

```
<context type="skill" name="dispatch-confirm" state="initial">
{SKILL.md 全文}
</context>
```

### 4.4 Agent 自主创建技能（学习闭环）

这是 Hermes 最有价值的设计：**agent 从成功任务中提炼新技能**。

**Keller 的场景**：

```
第一周：
  sarah-bot 遇到 Atlanta Bankhead 社区地址问题
  → ZIP 30303 但 GPS 指向 30313 区域
  → Sarah 和 bot 解决了：正确方式是先查 Fulton County 地图
  
  [bot 执行 skill_manager 创建新技能]
  技能名：atlanta-address-edge-cases
  内容：Atlanta 特殊区域地址处理规则，包含 Bankhead、Vine City、English Avenue 的 ZIP 异常

第三周：
  Alex（另一位 CSR）遇到相同问题
  → skills index 里已有 atlanta-address-edge-cases
  → bot 自动引用该技能，正确处理

三个月后：
  sarah-bot 已积累 12 个 Keller 特有技能：
  · atlanta-address-edge-cases（地址异常）
  · lowe's-quality-flag-patterns（常见质量标记类型）
  · jenkins-customer-preferences（常客偏好）
  · peak-season-dispatch-protocol（旺季特殊流程）
  · ...
```

**这是 SOUL 模板做不到的**：SOUL 模板是通用的，技能是从真实操作中提炼的。

---

## 五、Entity Memory（Hermes 没有，我们需要）

Hermes 的 MEMORY.md 是 agent 自己的笔记本，没有跨 Agent 共享的概念。
我们需要在此基础上增加一层：**业务实体记忆**，用于 Agent-to-Agent 上下文共享。

### 5.1 Entity Memory 文件格式

```markdown
<!-- memory/entities/complaint-A8810-20260603.md -->
---
entity_id: complaint-A8810-20260603
type: complaint
status: investigating
created_by: sarah-bot
created_at: 2026-06-03T10:02:00Z
last_updated: 2026-06-03T10:05:00Z
related_task: T993
participating_agents: [sarah-bot, tom-bot]
---

# Complaint: A8810 · Jenkins · 2026-06-03

## 投诉原文
"Crew didn't show up for #A8810. Worst service ever!!!"

## 时间轴
§ 10:02 · inbound SMS 到达 · sarah-bot 检测到投诉信号
§ 10:03 · 自动安抚 SMS 已发 · "I'm escalating to our manager..."
§ 10:03 · Task #T993 创建 · URGENT · due +2h · assignee=Tom
§ 10:03 · 路由到 tom-bot · event=complaint.detected
§ 10:05 · tom-bot 开始调查 · CallLog 查询完成
§ 10:05 · 调查结论：Mike 未 CONFIRM，无 30309 外呼记录

## 调查结果（tom-bot 写入）
- 派工记录：Mike Reyes · 06/03 10am · 1234 Main St 30309
- SMS 派发：08:52 ✅
- Mike CONFIRM：❌
- Mike 今日外呼：无 Jenkins 电话

## 待定行动
Tom 已介入，等待决策 → pending_resolution
```

### 5.2 Entity Memory 的注入逻辑

```go
// persona/loader.go

func (l *Loader) Build(chatID, entityID string) string {
    // slots 1-4: 冻结快照（prefix cache 友好）
    stable := l.buildStableSlots()  // SOUL + skills + DOMAIN + OWNER
    
    // slot 5: 实时 chat memory（每条消息读）
    chat := l.store.LoadMemory(ScopeChat, chatID)
    
    // slot 6: entity memory（有 entityID 才加）
    entity := ""
    if entityID != "" {
        entity, _ = l.store.LoadMemory(ScopeEntity, entityID)
    }
    
    return assemble(stable, chat, entity)
}
```

**Entity ID 从哪里来？**

- 人发消息时提到工单号（"A8810"）→ RingClaw 解析 → 匹配 entities/ 目录
- Agent 路由事件携带 entity_id → 接收方 Agent 直接使用
- 如果没有 entity → entity slot 为空 → system prompt 不变（prefix cache 不受影响）

---

## 六、整合后的 System Prompt 示例

以 sarah-bot 收到 tom-bot 路由的投诉（entity_id=complaint-A8810）为例：

```xml
<context type="soul">
# Sarah's CSR Agent — Keller Atlanta

我是 Sarah Cooper 的 Agent，Keller Atlanta 门店派单专家。
...（≤80 行，身份描述）
skills: [dispatch-confirm, complaint-handling]
</context>

<context type="skills">
[Skills Available]
dispatch-confirm   · 派单执行 + CONFIRM 跟踪
complaint-handling · 投诉检测 + 安抚 + Agent 路由
</context>

<context type="skill" name="complaint-handling" state="pending_resolution">
# complaint-handling

[SKILL.md 全文在此展开，因为当前任务匹配这个技能]
当前状态：pending_resolution（调查已完成，等待人工决策）
</context>

<context type="memory" scope="domain">
Atlanta 标准 ZIP：30301-30350。Buford 是 30518。
§
Mike Reyes 手机：+14045550211
§
...（业务领域知识，冻结）
</context>

<context type="memory" scope="owner">
Sarah 偏好回复 ≤4 行
§
...（Owner 偏好，冻结）
</context>

<context type="memory" scope="chat" chat_type="Group">
今日 open dispatch：
A8821|Mike Reyes|06/04 10:00|pending
...（当前 chat 状态，实时）
</context>

<context type="entity" id="complaint-A8810-20260603">
# Complaint: A8810 · Jenkins · 2026-06-03

时间轴：
§ 10:02 inbound SMS 到达
§ 10:03 安抚 SMS 已发
§ 10:05 tom-bot 调查：Mike 未 CONFIRM，建议致电

待定行动：Tom 已介入，等待决策
</context>

用户消息：@sarah-bot 给 Jenkins 发道歉短信，队长 20 分钟内到，$50 credit
```

Agent 拿到这个 prompt：
- 知道自己是谁（SOUL）
- 知道当前在 complaint-handling · pending_resolution 状态
- 知道 A8810 的完整历史（entity memory）
- 执行 ACTION:SMS with 道歉模板
- 更新 entity memory（status=resolved，补充解决方案）

---

## 七、改进前后对比

| 维度 | 当前 | Hermes 模式改进后 |
|------|------|-----------------|
| System prompt 稳定性 | 每条消息重建 | SOUL+DOMAIN+OWNER 冻结，session 内不变 |
| Prefix cache | ~0% 命中 | ~70% 命中（chat+entity 在末尾变化） |
| Token 成本 | 基准 | 节省约 60% |
| SOUL 长度 | 150 行，常被截断 | ≤80 行，永不截断 |
| 技能步骤 | 嵌在 SOUL 里 | 独立 SKILL.md，按需加载 |
| 技能复用 | 复制粘贴 | skills/ 目录共享 |
| 学习能力 | 无 | Agent 从成功任务中创建新技能 |
| 跨 Agent 上下文 | 无（各 chat 独立）| entity memory 跨 Agent 共享 |
| Memory 格式 | 自由 markdown | § 分隔条目，支持精确增删改 |
| 安全扫描 | 无 | 注入 system prompt 的内容扫描注入攻击 |

---

## 八、对 RingClaw 代码的修改清单

### 8.1 Persona Loader（最高优先级，影响所有 Case）

```go
// messaging/persona/loader.go 修改

// 新增：Session 快照（在 WebSocket 连接建立后调用一次）
func (l *Loader) InitSession(ownerUserID string)

// 修改：Build 使用快照 + 实时 chat + 按需 entity
func (l *Loader) Build(ctx context.Context, chatID, entityID string, isDM bool) string

// 新增：Memory scope 扩展
// ScopeGlobal → ScopeDomain（全局业务领域知识）
// ScopeUser → ScopeOwner（Owner 偏好）
// + ScopeEntity（新增，entities/<id>.md）
```

### 8.2 Memory 格式（§ 分隔符）

```go
// messaging/persona/store.go 修改

// 条目写入使用 § 分隔
// 支持精确按条目 add/replace/remove
// char limit（不是 token limit）

type MemoryEntry struct {
    Content  string
    AddedAt  time.Time
    AddedBy  string // "owner", "agent", "entity-sync"
}
```

### 8.3 Skills 系统（新增）

```go
// messaging/skills/ 新建包

// SkillIndex: 扫描 ~/.ringclaw/skills/ 目录，构建 name+description 索引
// SkillLoader: 按名字加载完整 SKILL.md 内容
// SkillMatcher: 根据消息 intent 判断哪个 skill 应激活
// AgentSkillWriter: agent 创建/更新技能（对应 Hermes skill_manager_tool）
```

### 8.4 Entity Memory（新增）

```go
// messaging/persona/store.go 扩展

func (s *Store) LoadEntity(entityID string) (string, error)
func (s *Store) WriteEntity(entityID string, content string) error
func (s *Store) AppendEntityEvent(entityID, event string) error

// entityID 解析：从消息文本中提取工单号，查找匹配的 entity file
func ExtractEntityID(text string, chatMemory string) string
```

### 8.5 Security Scanning（新增）

```go
// messaging/persona/sanitize.go 新建

// 扫描 memory 写入内容是否包含 prompt injection 特征
// 参考 Hermes _MEMORY_THREAT_PATTERNS
func ScanMemoryContent(content string) error
```

---

## 九、实施优先级

| 优先级 | 修改 | 影响 | 估算 |
|--------|------|------|------|
| P0 | Frozen snapshot（loader.go）| prefix cache，所有 case 受益 | 1天 |
| P0 | Domain/Owner scope 分离 | 更清晰的 memory 结构 | 0.5天 |
| P1 | § 分隔符格式 | agent 可精确管理 memory | 1天 |
| P1 | Entity memory（新 scope）| 跨 Agent 上下文共享 | 2天 |
| P2 | Skills 目录 + SKILL.md 格式 | 技能独立，SOUL 精简 | 3天 |
| P2 | Skills index 注入 system prompt | 按需加载技能详情 | 1天 |
| P3 | Agent 自主创建技能（学习闭环）| Keller 场景长期价值 | 3天 |
| P3 | Security scanning | 安全加固 | 1天 |

P0 两天完成，直接降低 ~60% token 成本，今天就能开始做。
