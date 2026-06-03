# AgentRun · Agent 架构设计

---

## 一、从 Keller 场景归纳出的五个核心模式

读完 5 个 Case，每个 Case 背后都在用同一类模式，只是对象不同：

```
模式 1：检测 → 自动处理 → 人决策 → 执行
  Case B（投诉）：inbound SMS 检测 → sarah-bot 安抚+路由 → tom-bot 调查 → Tom 决策 → 执行安抚

模式 2：广播 → 并行执行 → 结果收敛
  Case A（上线）：Beth 一条指令 → 三个 Agent 各自执行 → 结果回到同一线程

模式 3：定时触发 → 文本摘要 → 人触发 → 行动
  Case 2/3（摘要）：Cron → TEXT → Tom/Beth 读 → 手动触发跟进

模式 4：异常升级链
  Case D（双路升级）：karen-bot 检测 → tom-bot 调查 → beth-bot 收口 → Beth 外呼

模式 5：多用户隔离（同一 Agent 服务多人）
  Case E（HR）：同一 hr-bot，多员工独立线程，信息严格隔离
```

**这五个模式对当前架构分别提出了什么挑战，是本次设计的起点。**

---

## 二、当前架构的真实状态

### 2.1 Persona 系统的工作方式

```
每条消息到达时：
  PersonaLoader.Build(chatID, userID, isDM)
        ↓
  读取 SOUL.md（截断到 2000 chars）
  读取 memory/global.md（截断到 2000 chars）
  读取 memory/user/<userID>.md（截断到 2000 chars）
  读取 memory/chat/<chatID>.md（截断到 4000 chars）
        ↓
  拼成 XML banner：
  <context type="persona">SOUL 全文</context>
  <context type="memory" scope="global">全局记忆</context>
  <context type="memory" scope="user">用户记忆</context>
  <context type="memory" scope="chat">当前 Chat 记忆</context>
        ↓
  banner + 用户消息 → Agent（Claude/Codex）
        ↓
  Agent 输出文本 + ACTION 块
        ↓
  RingClaw 执行 ACTION
```

### 2.2 当前架构能很好处理的

| 能力 | 状态 |
|------|------|
| 单 Agent 单对话（Owner 用自己的 Bot）| ✅ 成熟 |
| 三级 memory（global/user/chat）| ✅ 成熟 |
| ACTION 执行（Task/SMS/Fax 等）| ✅ 成熟 |
| Cron / Heartbeat（定时触发）| ✅ 成熟 |
| 非 Owner 访问控制（source_user_ids）| ✅ 成熟 |

### 2.3 当前架构的结构性问题

**问题 1：SOUL 是单体，随场景复杂度线性膨胀**

```
当前 SOUL.md 包含：
  · 身份（我是谁）
  · 所有工作流的步骤（dispatch 怎么做、complaint 怎么处理）
  · 升级规则
  · Agent 路由规则
  · 记忆规则
  · 格式模板

问题：
  · SOUL 越写越长，超过 2000 chars 就被截断
  · 工作流步骤和身份混在一起，难以复用
  · 两个 Bot 用同一工作流（dispatch-confirm），要复制粘贴
  · 更新一个工作流步骤，要在所有相关 Bot 的 SOUL 里都改
```

**问题 2：Agent 无状态，无法追踪跨会话的业务实体**

```
当前：
  complaint 发生 → sarah-bot 在 #atlanta-orders 发帖 → tom-bot 在 #atlanta-ops 回复
  这两件事在两个不同的 chat 里，用两个不同的 chat memory

tom-bot 在处理投诉时，它"知道的"是：
  · memory/chat/<atlanta-ops-id>.md 里的内容
  它"不知道的"是：
  · sarah-bot 在 #atlanta-orders 里的原始投诉记录
  · 自动安抚 SMS 的发送内容
  · Task #T993 的创建细节

结果：tom-bot 调查时要重新推断上下文，容易不一致。
```

**问题 3：Agent-to-Agent 路由基于文本约定，脆弱**

```
当前：
  sarah-bot 在消息里写 "[AGENT_ROUTE:COMPLAINT]"
  tom-bot 的 SOUL 里写"收到包含 [AGENT_ROUTE:COMPLAINT] 的消息时..."

问题：
  · 格式约定在 SOUL 文本里，不是代码
  · 如果 SOUL 更新时格式变了，路由静默失效（没有类型检查）
  · 无法批量查看"系统里有哪些 Agent 路由"
  · 路由参数（entity_id、priority）藏在自由文本里，无法索引
```

**问题 4：Memory 的粒度和 Keller 的业务不匹配**

```
当前 memory 粒度：
  global（全局共享）
  user/<userID>（每个人）
  chat/<chatID>（每个聊天频道）

Keller 需要的粒度：
  entity/<entity-id>（每个业务对象：一张投诉单、一个 Lowe's 案例）
  · 投诉 A8810 的全过程（跨 sarah-bot、tom-bot 两个 Agent、两个 Chat）
  · 需要 tom-bot 在 #atlanta-ops 里能读到 sarah-bot 在 #atlanta-orders 写的上下文
  · 当前机制做不到（不同 chat，不同 chat memory）
```

**问题 5：技能（Skill）和身份（Identity）没有分离**

```
当前：
  dispatch-confirm 的 7 步骤 ∈ SOUL
  complaint-handling 的检测规则 ∈ SOUL
  daily-digest 的模板 ∈ SOUL

这三个是"技能"，不是"我是谁"。
结果：
  · SOUL = 身份 + 技能 + 规则 + 模板，是一个大杂烩
  · 技能无法跨 Bot 复用
  · 技能无法独立更新（要改 dispatch 步骤，每个 Bot 都要改 SOUL）
```

---

## 三、改进后的架构：四层分离

```
┌────────────────────────────────────────────────────────────┐
│  Layer 4: Skill Engine                                      │
│  独立技能模块，可组合，有状态机                              │
│  dispatch-confirm · complaint-handling · daily-digest ···   │
├────────────────────────────────────────────────────────────┤
│  Layer 3: Entity Registry                                   │
│  跨 Agent、跨 Chat 的业务实体状态                            │
│  entity/<id>.md，所有相关 Agent 可读写                      │
├────────────────────────────────────────────────────────────┤
│  Layer 2: Routing Protocol                                  │
│  结构化 Agent 事件路由，不依赖文本约定                        │
│  事件类型 + entity_id + payload                             │
├────────────────────────────────────────────────────────────┤
│  Layer 1: Agent Kernel（当前已有）                           │
│  SOUL（精简为纯身份）+ 三级 memory + ACTION 执行             │
├────────────────────────────────────────────────────────────┤
│  Layer 0: RC Platform + RingClaw Runtime（当前已有）        │
│  SMS / Fax / Phone / Task / Note / WebSocket / OOB         │
└────────────────────────────────────────────────────────────┘
```

---

## 四、Layer 4：Skill Engine

### 4.1 Skill 是什么

Skill = 一个独立的业务工作流模块，包含：
1. **触发条件**（什么情况下激活）
2. **Prompt fragment**（激活时注入 agent 的额外指令）
3. **状态机**（工作流的步骤和转换）
4. **Entity memory schema**（写入实体记忆的格式）

Skill 和 SOUL 的关系：
```
SOUL = 我是谁 + 我拥有哪些技能（技能名称列表）
Skill = 技能如何执行
```

### 4.2 Skill 定义示例：dispatch-confirm

```yaml
# skills/dispatch-confirm.yaml

name: dispatch-confirm
description: 派单执行 + CONFIRM 跟踪闭环

trigger:
  keywords: [dispatch, assign, schedule, send, route]
  requires_fields: [assignee, datetime]
  missing_field_prompt: "缺少 {field}，请补充后重试"

states:
  created:
    description: 派单已下达
    actions:
      - create_task(subject="#{order} Install - {assignee}", due={datetime})
      - send_sms(to={assignee_phone}, template=dispatch_sms)
      - start_timer(30m, escalate_if_unconfirmed)
    transitions:
      - on: CONFIRM_RECEIVED → confirmed
      - on: TIMER_EXPIRED → escalated

  confirmed:
    description: 队长已确认
    actions:
      - update_task(note="CONFIRM received {time}")
      - cancel_timer()
      - notify_origin_chat("✅ {assignee} confirmed #{order}")
    terminal: true

  escalated:
    description: 超时未确认
    actions:
      - post_text(origin_chat, "⏳ {order} 30min 无 CONFIRM，建议跟进")
    terminal: true

prompt_fragment: |
  当 dispatch-confirm 技能激活时：
  1. 先核对 ZIP code 是否与城市匹配
  2. 创建 Task 追踪
  3. 发送派单 SMS（使用标准模板）
  4. 回复 1 行确认

  标准派单 SMS 模板：
  Install #{order} {date} {time}.
  Address: {address}
  Material: {material}, {sqft}sqft
  Customer: {customer}, {phone}
  Reply CONFIRM to acknowledge.

entity_memory_write: |
  dispatch_record: {order}|{assignee}|{datetime}|{status}
```

### 4.3 Skill 定义示例：complaint-handling

```yaml
# skills/complaint-handling.yaml

name: complaint-handling
description: 投诉检测 + 自动安抚 + 调查路由

trigger:
  inbound_sms: true
  keywords: [complaint, worst, didn't show, lawsuit, refund, BBB]
  patterns: [3+ exclamation marks, ALL CAPS 3+ words]

states:
  detected:
    description: 投诉已检测
    actions:
      - send_ack_sms(to={customer_phone}, template=complaint_ack, sla=60s)
      - create_task(subject="URGENT: #{order}投诉", assignee={store_mgr}, due=+2h, color=Red)
      - post_escalation(target_chat={ops_chat}, template=escalation_post)
      - route_to_agent(tom-bot, event=complaint.detected)
    transitions:
      - on: AGENT_INVESTIGATION_STARTED → investigating

  investigating:
    description: 调查中
    actions:
      - run_calllog_query(order={order})
    transitions:
      - on: RESOLUTION_PROPOSED → pending_resolution

  pending_resolution:
    description: 等待人工决策
    actions:
      - post_resolution_draft(target_chat={origin_chat})
    transitions:
      - on: HUMAN_APPROVED → resolved

  resolved:
    description: 已解决
    actions:
      - send_resolution_sms(to={customer_phone})
      - update_task(status=Complete)
      - write_entity_memory(complaint_ledger)
    terminal: true

  escalated_timeout:
    description: SLA 超时自动升级
    trigger: 2h after detected with no HUMAN_APPROVED
    actions:
      - route_to_agent(beth-bot, event=complaint.sla_breach)
    terminal: false

prompt_fragment: |
  当 complaint-handling 技能激活时：
  1. 60 秒内发送安抚短信（使用 csr 语气）
  2. 在 ops chat 发升级帖（verbatim 引用，不要意译）
  3. 等待 tom-bot 调查结论
  4. 将 tom-bot 结论整理成解决方案草稿
  5. 人工确认后发解决短信

entity_memory_write: |
  complaint_ledger: {date}|{customer}|{order}|{verbatim}|{resolution}|{sla_hit}
```

### 4.4 Skill 激活机制

```
消息到达
    ↓
Skill Engine 检查 trigger 条件
    ↓ 匹配到 complaint-handling
从 skills/complaint-handling.yaml 读取 prompt_fragment
    ↓
合并到 agent prompt：
  <context type="persona">SOUL（精简版）</context>
  <context type="skill" name="complaint-handling" state="detected">
  {skill prompt_fragment}
  当前状态：detected
  Entity ID：complaint-A8810-20260603
  Entity 上下文：{entity memory 内容}
  </context>
  <context type="memory" scope="global">...</context>
    ↓
Agent 按 skill 的 prompt_fragment 工作，不需要 SOUL 里有完整步骤
```

### 4.5 为什么 Skill Engine 更好

| | 当前（SOUL 内嵌）| Skill Engine |
|--|---------------|--------------|
| 复用性 | 每个 Bot 复制粘贴 | 技能定义共享，Bot 只需声明使用哪个 |
| 可维护性 | 改一个步骤要改所有 Bot 的 SOUL | 改 skill.yaml，所有使用这个 skill 的 Bot 自动更新 |
| SOUL 长度 | 越加越长，超 2000 chars 被截断 | SOUL 只有身份，100 行以内 |
| 状态追踪 | 无状态，全靠 LLM 记住步骤 | 显式状态机，状态存在 entity memory |
| 可调试性 | 看不出当前在哪个步骤 | 状态机状态可查 |

---

## 五、Layer 3：Entity Registry（实体记忆）

### 5.1 Entity 是什么

Entity = 一个跨越多个 Agent、多个 Chat、多个时间段的业务对象。

Keller 的业务实体：
```
complaint-{order}-{date}     投诉案例
dispatch-{order}-{date}      派单
lowe's-case-{ref}            Lowe's 合规案例
crew-gap-{store}-{date}      班组缺口
pto-{employee}-{date}        请假申请
```

### 5.2 Entity Memory 结构

```
~/.ringclaw/memory/
  global.md                          全局（当前已有）
  user/<userID>.md                   用户级（当前已有）
  chat/<chatID>.md                   聊天级（当前已有）
  entities/
    complaint-A8810-20260603.md      ← NEW：实体级
    dispatch-A8821-20260603.md
    lowe's-case-REF-2026-0603-11.md
```

### 5.3 Entity Memory 内容格式

```markdown
<!-- entities/complaint-A8810-20260603.md -->

# Complaint: Order A8810 · 2026-06-03

## 基本信息
- 订单：A8810
- 客户：Jenkins +14045550199
- 检测时间：10:02
- 检测来源：inbound SMS

## 原文
"Crew didn't show up for #A8810. Worst service ever!!!"

## 当前状态
state: investigating
last_updated: 10:05

## 时间轴
- 10:02  inbound SMS 到达（sarah-bot 检测）
- 10:03  自动安抚 SMS 发出 ✅
- 10:03  sarah-bot → tom-bot 路由
- 10:03  Task #T993 创建（URGENT, due +2h）
- 10:05  tom-bot 调查启动（CallLog 查询中）

## 相关 Agent
- sarah-bot: 检测 + 安抚 + 执行
- tom-bot: 调查

## 关联资源
- Task: T993
- SMS sent: 10:03（Jenkins +14045550199）
```

### 5.4 Entity Memory 如何实现跨 Agent 共享

```
当 sarah-bot 创建投诉实体时：
  写入 memory/entities/complaint-A8810-20260603.md

当 sarah-bot 路由到 tom-bot 时（Agent 事件）：
  事件 payload 包含 entity_id = "complaint-A8810-20260603"

tom-bot 接收到路由事件后：
  从 memory/entities/complaint-A8810-20260603.md 读取实体上下文
  注入到 agent prompt：
  <context type="entity" id="complaint-A8810-20260603">
  {entity memory 内容}
  </context>

结果：
  tom-bot 无需重新推断上下文，直接拿到完整的投诉历史
  tom-bot 调查完成后，把结论写回 entity memory
  sarah-bot 下次处理这个投诉时，也能读到 tom-bot 的调查结论
```

**Entity memory 解决了"跨 Agent 上下文共享"问题——这是当前架构最大的缺失。**

---

## 六、Layer 2：Routing Protocol（结构化路由）

### 6.1 当前路由的问题

```
当前（文本约定）：
  sarah-bot 发送消息："[AGENT_ROUTE:COMPLAINT] 订单 A8810 ..."
  tom-bot 的 SOUL 里写："收到含 [AGENT_ROUTE:COMPLAINT] 标签的消息时..."

问题：
  1. 格式约定在 SOUL 文本里，LLM 生成的消息可能格式略有不同就失效
  2. 路由元数据（entity_id、priority）藏在文本里，无法程序化索引
  3. 无法在系统层面知道"当前有哪些 pending 的 Agent 路由事件"
  4. 难以调试："为什么 tom-bot 没有响应 sarah-bot 的路由？"
```

### 6.2 结构化路由协议

**方案：在 RC 消息里嵌入机器可读的路由 metadata（对人不可见）**

```
RC 消息结构（对人可见部分）：
  ⚠️ 投诉升级 · A8810
  客户 Jenkins 报告 no-show，情绪强烈
  自动安抚 SMS 已发，Task #T993 已创建
  @tom-bot 请调查

RC 消息结构（对机器可读部分，不渲染在 RC UI 里）：
  <!-- agent-event:
    type: complaint.detected
    from: sarah-bot
    to: tom-bot
    entity_id: complaint-A8810-20260603
    priority: urgent
    payload:
      order_id: A8810
      customer_phone: +14045550199
      task_id: T993
      ack_sent: true
  -->
```

### 6.3 路由事件类型定义（可扩展的事件总线）

```yaml
# routing/events.yaml

events:
  complaint.detected:
    from: csr-agent
    to: store-mgr-agent
    entity_type: complaint
    required_fields: [order_id, customer_phone, verbatim]

  complaint.sla_breach:
    from: store-mgr-agent
    to: exec-agent
    entity_type: complaint
    required_fields: [order_id, elapsed_time]

  lowe's.quality_flag:
    from: lowe's-liaison-agent
    to: store-mgr-agent
    entity_type: lowe's-case
    required_fields: [order_id, sop_ref, deadline]

  dual_escalation:
    from: lowe's-liaison-agent
    to: exec-agent
    entity_type: [complaint, lowe's-case]
    required_fields: [order_id, complaint_id, case_id]

  crew_gap.request:
    from: store-mgr-agent
    to: regional-coord-agent
    entity_type: crew-gap
    required_fields: [store, date, count, skill_required]

  dispatch.created:
    from: csr-agent
    to: crew-lead-agent
    entity_type: dispatch
    required_fields: [order_id, assignee, datetime, address]
```

### 6.4 路由的代码层实现思路

```
RingClaw messaging/actions.go 扩展：

1. Agent 输出中包含 ACTION:ROUTE 块：
   ACTION:ROUTE
   event_type=complaint.detected
   to=tom-bot
   entity_id=complaint-A8810-20260603
   order_id=A8810
   customer_phone=+14045550199
   task_id=T993
   END_ACTION

2. RingClaw 解析 ACTION:ROUTE：
   - 在消息里嵌入 HTML 注释形式的 metadata
   - 同时在 entity memory 里记录这条路由事件

3. 接收 Agent（tom-bot）的 monitor 在处理消息时：
   - 解析 HTML 注释里的 agent-event metadata
   - 如果 to == 自己的 bot ID → 触发处理
   - 从 entity registry 加载 entity memory
   - 注入到 agent prompt

优点：
  - 路由是结构化的，有类型检查
  - 路由参数可以被程序索引
  - 人看到的消息仍然干净可读
  - 调试：查 entity memory 的时间轴就知道路由历史
```

---

## 七、重新设计后的 SOUL 结构

有了 Skill Engine 和 Entity Registry 之后，SOUL 可以精简到纯身份层：

### 7.1 新 SOUL 结构（5 个章节，≤80 行）

```markdown
# [Agent Name]

## Identity（身份）
[2-3 句：我是谁，我的声音，我服务谁]
[Owner 姓名]
[回复风格：≤N 行，什么场合什么语气]

## Skills（技能声明）
skills:
  - dispatch-confirm
  - complaint-handling
# Skill Engine 会自动注入技能的 prompt_fragment 和状态机

## Team Access（团队访问规则）
trusted_senders:
  - tom.rivera@keller.com: [query_dispatch, request_sms]
  - <karen-bot-ext-id>: [route_complaint]

access_policy:
  query: any trusted sender
  execute_action: owner only
  agent_route: declared senders only

## Routing（路由声明，声明式不是描述式）
emits:
  - event: complaint.detected → to: tom-bot
  - event: lowe's.quality_flag → to: tom-bot
  - event: dispatch.created → to: mike-bot

receives:
  - event: complaint.sla_breach → from: any → escalate to beth-bot

## Memory（记忆规则）
write:
  - scope: chat, key: dispatch_records, format: "{order}|{assignee}|{time}|{status}"
  - scope: entity, when: skill=dispatch-confirm, key: dispatch_record

never_write:
  - customer complaint verbatim (>200 chars)
  - HR content of any kind
```

### 7.2 新旧 SOUL 对比

```
当前 SOUL（sarah-bot）：~150 行
  · 7 步骤 dispatch-confirm 工作流描述
  · complaint 检测规则（15 行）
  · 改单工作流描述
  · 升级规则（含格式约定）
  · Agent 路由消息格式（含 [AGENT_ROUTE:XXX]）
  · 记忆写入规则
  · SMS 模板（5 个）
  → 超过 2000 chars 会被截断！关键工作流步骤可能丢失

改进后 SOUL（sarah-bot）：~50 行
  · 3 行身份
  · 2 行技能声明（dispatch-confirm, complaint-handling）
  · 6 行团队访问规则
  · 4 行路由声明
  · 3 行记忆规则
  → 永远不会超 2000 chars
  → 工作流步骤在 skill.yaml 里，按需注入，不占 SOUL 空间
```

---

## 八、综合：一个 Case 的完整架构流程

以 **Case B（投诉处理）** 为例，展示四层架构的完整运行：

```
客户发 inbound SMS（Group B，待 wire）
        │
        ▼ Layer 0: RingClaw Runtime
MessageStoreHandler 检测到 inbound SMS
        │
        ▼ Layer 1: Agent Kernel
Personal Loader 构建 banner：
  <context type="persona">sarah 身份（50行）</context>
  <context type="memory" scope="global">...</context>
  <context type="memory" scope="chat">today's dispatch list</context>
        │
        ▼ Layer 4: Skill Engine
检测到投诉触发词 → 激活 complaint-handling skill
注入 skill prompt_fragment：
  <context type="skill" name="complaint-handling" state="initial">
  60秒内发安抚SMS ... 创建URGENT Task ... 路由到 tom-bot
  </context>
        │
        ▼ Agent（Claude）
生成：
  ACTION:SMS to=+14045550199（安抚短信）
  ACTION:TASK subject="URGENT: A8810投诉" assignee=Tom due=+2h
  ACTION:ROUTE event=complaint.detected to=tom-bot entity_id=complaint-A8810-20260603
        │
        ▼ Layer 0: RingClaw 执行 ACTION
SMS 发出 ✅，Task 创建 ✅，route event 嵌入消息
        │
        ▼ Layer 3: Entity Registry
写入 memory/entities/complaint-A8810-20260603.md：
  - 时间轴记录（10:02 检测，10:03 安抚，10:03 路由）
  - Task ID，SMS 状态
        │
        ▼ Layer 2: Routing Protocol
消息发到 #atlanta-ops，包含 agent-event metadata
        │
        ▼ tom-bot monitor（source_user_ids 包含 sarah-bot ext ID）
        │
        ▼ Layer 3: Entity Registry（tom-bot 读取）
加载 memory/entities/complaint-A8810-20260603.md
        │
        ▼ Layer 4: Skill Engine（tom-bot）
路由事件类型=complaint.detected → 激活 investigation skill
注入：
  <context type="entity" id="complaint-A8810-20260603">
  完整投诉历史（sarah-bot 写的所有内容）
  </context>
  <context type="skill" name="complaint-investigation" state="start">
  执行 CallLog 查询 ... 对比派工记录 ... 输出结论格式
  </context>
        │
        ▼ Agent（Claude in tom-bot）
生成：
  ACTION:PHONE_CALLLOG scope=today（查 CallLog）
  [调查结论文本]
        │
        ▼ Layer 3: Entity Registry（tom-bot 写入）
更新 complaint-A8810-20260603.md：
  - 调查结论
  - CallLog 查询结果
  - state: pending_resolution
        │
        ▼ 人（Tom）读到结论，做决策
        │
        ▼ Tom @tom-bot "给 Jenkins 发道歉短信"
        │
        ▼ Layer 4: Skill Engine
state: pending_resolution + 人工 approve → transition to resolved
        │
        ▼ Agent 生成 ACTION:SMS（道歉短信）
        │
        ▼ Layer 3: Entity Registry
更新 state: resolved，写入 complaint_ledger
```

---

## 九、实施路径：从现在到理想架构

### 阶段 0（今天，零代码）：Agent-to-Agent 信任配置
把其他 Bot 的 ext ID 加入 source_user_ids。
**解锁**：Case A/B/C/D 的 Agent 路由基本流程（文本约定版本）。

### 阶段 1（~2 周）：inbound SMS/Fax wire + 基础 Entity Memory
- MessageStoreHandler wire（~150 行）
- 新增 `memory/entities/` scope 到 Persona Loader
- SOUL 增加 entity memory 读写指令

**解锁**：Case B 完整投诉处理，Case D 传真入站路由。
**改善**：跨 Agent 上下文不再重复推断。

### 阶段 2（~3 周）：Skill Engine MVP
- 把 dispatch-confirm、complaint-handling 从 SOUL 提取为独立 yaml 文件
- Persona Loader 根据 intent 注入对应 skill 的 prompt_fragment
- Skill 状态写入 entity memory

**改善**：SOUL 精简 60%+，工作流可独立更新，技能跨 Bot 复用。

### 阶段 3（~4 周）：结构化路由协议
- ACTION:ROUTE 解析（~100 行）
- Agent 事件总线（routing/events.yaml 定义）
- routing metadata 嵌入 RC 消息

**改善**：路由不再依赖文本约定，可调试，可索引，可扩展新 Agent 类型。

### 阶段 4（~6 周）：OOB 反向通道 + ToolPolicy Enforcement
- CP→Pod 的 approval 反向通道（~200 行）
- ToolPolicy 在 messaging/actions.go 里 enforce
- FIJI Approval Inbox（消费 CP 的 approval API）

**改善**：跨 chat OOB 不再需要 terminal CLI，业务用户可直接在 FIJI 里操作。

---

## 十、架构改进后 SOUL 的最终形态

以 sarah-bot 为例，新 SOUL 的长度和内容：

```markdown
# Sarah's CSR Agent

## Identity
我是 Sarah Cooper（Atlanta CSR）的专属 Agent。
每天 20-30 张派单，我帮 Sarah 把口语指令变成 Task + SMS。
回复 ≤4 行，Sarah 单手操作。

## Skills
skills: [dispatch-confirm, complaint-handling]
# 工作流步骤由 Skill Engine 按需注入，不在这里描述

## Team Access
trusted_senders:
  tom.rivera@keller.com: [query_dispatch_status]
  <karen-bot-ext-id>: [route_complaint]

## Routing
emits:
  complaint.detected → to: tom-bot
  dispatch.created → to: mike-bot

## Memory
write:
  - chat: dispatch_records "{order}|{assignee}|{time}|{status}"
  - entity: dispatch/{order}, complaint/{order}
never_write: [customer_verbatim_complaint, hr_content]
```

**50 行。永远不截断。可维护。**

技能的 7 步骤在 `skills/dispatch-confirm.yaml` 里，更新技能不需要动 SOUL。
路由规则是声明式的，不是 LLM 要解析的文本约定。
Entity memory 让跨 Agent 上下文自动共享。

这就是从"能用"到"好用"的架构差距。
