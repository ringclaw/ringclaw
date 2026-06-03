# Keller POC · 从源材料重新分析

---

## 一、原始材料溯源

Keller 分析来自**两份材料的叠加**，不是来自对 Keller 实际运营的直接观察：

| 材料 | 类型 | 提供了什么 | 不提供什么 |
|------|------|-----------|-----------|
| **RC 公开 Case Study（Keller Interiors）** | 网络公开资料 | 33 店 · 15 州 · 100-399 人 · Lowe's 27 年合作 · AI Receptionist 上线后等待时间 12min→90sec · CSAT +3pp · "不用管一个人又省工资" | 内部派工流程 · 是否真用传真 · 是否用 RC Team Messaging · 具体排班痛点 |
| **`docs/architecture/personal-ava-bot-platform.md`** | 产品架构文档 | Bot 类型（Personal/Team/Workflow/Watcher 等）· SOUL 概念 · 权限模型 · RC 能力清单 | 哪些 Bot 类型已实现（大部分是 Phase 4 路线图，未实现）|

**结论：** 现有 POC 的 8 个 case 是把 Keller 业务常识（B 端安装公司的典型痛点）
映射到平台能力上**推理**出来的，不是从 Keller 实际操作中提炼的。
这不是问题——推理是合理的方法——但必须清楚哪些是事实、哪些是假设。

---

## 二、源材料真正告诉我们的

### Keller 的核心业务（事实）

```
Lowe's（家装连锁）→ 向 Keller 发送安装项目订单
Keller（安装商）→ 调度施工队 → 客户家里完成安装
客户满意 → Keller 把完工文件回传 Lowe's
```

- 这是**纯 B2B 外包服务**，Keller 是 Lowe's 的安装服务商
- AI Receptionist 已经解决了**客户来电**这一层（路由到正确门店，等待时间从 12 分钟降到 90 秒）
- 还没解决的是**路由之后的内部协作**：CSR 派单、队长调度、Lowe's 完工文件

### Keller 的已知痛点（事实 vs 假设）

| 痛点 | 来源 | 性质 |
|------|------|------|
| 来电等待时间长 | RC Case Study | ✅ 事实，已被 AIR 解决 |
| 多店协调困难（33 店 15 州）| 规模推理 | 🔶 合理假设 |
| 派单→确认循环低效 | 安装业常识 | 🔶 合理假设 |
| Lowe's 完工表单处理慢 | Lowe's 合作推理 | 🔶 合理假设，不确定是否用传真 |
| 每日摘要手动写 | 规模推理 | 🔶 合理假设 |
| 投诉处理慢 | 服务业常识 | 🔶 合理假设 |

**关键空白：** Keller 的内部沟通工具是什么？已知他们用 RingEX，
但不确定员工是否在用 RC Team Messaging（可能还在用 WhatsApp / SMS / 电话）。

---

## 三、平台能力的正确理解

重新读 `docs/` 之后，发现现有 POC 设计有几个关键错误假设：

### 3.1 Cron 只输出文本（无 ACTION 块）

```
docs/zh/features/cron.md：
"Cron 触发的 Agent 回复不会执行 ACTION: 块。调度器把回复原样发回聊天。"
```

**影响：** 所有"cron 触发 → 创建 Task / 发 SMS / 跨 chat 消息"的设计都是错的。
Cron 的 agent 回复是**纯文本**，人读了才能决定是否手动触发后续行动。

**daily-digest cron 的正确设计：**
```
cron 17:30 → agent 生成摘要文本 → 发到聊天（文本只有人看，不触发任何 API）
Tom 读到文本 → Tom 自己决定是否手动创建 Task 或发消息
```

### 3.2 跨 Chat 有治理开销，不是免费的

```
docs/zh/security/cross-chat-actions.md：
Owner 跨 chat ACTION → 同步 audit notice 到 owner DM（5秒窗口，超时则拒绝）
非 Owner 跨 chat → OOB challenge → 必须在主机执行 ringclaw approval <id>
```

**影响：** "Bot A 自动把消息发到另一个群让 Bot B 处理"不是一个流畅的自动化路径。
每次跨 chat ACTION 都需要 owner 的 DM 参与。

### 3.3 Bot 类型：Team Bot 等是路线图，当前未实现

```
docs/architecture/personal-ava-bot-platform.md Phase 4 路线图：
Team Bot / Project Bot / Workflow Bot / Watcher Bot（未实现）
```

当前可用的只有**一种 Bot 运行模式**：`ringclaw start`，本质是
"一个进程，一个 config.json，监听指定 chat_ids"。

**但这个模式可以支持多人使用同一个 Bot，通过：**
- `source_user_ids`：列出多个用户
- `chat_user_allow`：按群叠加白名单（非 owner 上限模式）
- `allow_group_mention_authorize`：动态添加群成员到白名单

### 3.4 RC 能力不是"全部默认开启"，依赖 Scope

| 能力 | 需要的 JWT App Scope |
|------|---------------------|
| 基础读写（消息/Task/Note/Card/Event）| ReadAccounts（已在 base） |
| SMS 发送 | SMS scope |
| Video | Video scope |
| 外呼（PhoneCall）| 仅 owner；走 FIJI client makeCall |
| Call Log 读取 | ReadCallLog scope |
| 传真发送 | 代码已实现，需确认 Fax scope |
| Presence 查询 | ReadPresence scope |

### 3.5 SOUL + Memory 已实现（Persona 功能）

```json
"persona": {
  "enabled": true,
  "soul_file": "~/.ringclaw/SOUL.md",
  "memory_dir": "~/.ringclaw/memory"
}
```

每个运行中的 ringclaw 进程已经支持 SOUL.md + 三级 memory（global / user / chat）。
这是**当前可用的**，不是路线图。

---

## 四、多人方案的正确设计

当前平台支持三种多人场景，不是"每人一个 Bot 互相通信"：

### 模式 A：多人共用一个 Bot（团队共享模式）

```
Bot 配置：
  chat_ids: ["#atlanta-orders"]
  source_user_ids: ["sarah@keller.com", "alex@keller.com", "maria@keller.com"]
  ringcentral.bot_token: <orders-bot-token>   # 这是 team bot 账号
  Private App owner: tom@keller.com           # Tom 是 owner，有特权命令

效果：
  Sarah @orders-bot → 有回复（非 owner 上限：能发 SMS / Task，不能 /cron /cwd）
  Alex  @orders-bot → 同上
  Tom   @orders-bot → owner，全部特权命令可用
```

**适合场景**：多个 CSR 共用一个派单 Bot，处理同一个 #atlanta-orders 群。

### 模式 B：一人一个专属 Bot（Personal Bot 模式）

```
Bot 配置：
  source_user_ids: ["beth@keller.com"]        # 只有 Beth
  Private App owner: beth@keller.com

效果：
  只有 Beth 驱动，完整 owner 权限
  Beth 的 SOUL 写她自己的名字、偏好、记忆
```

**适合场景**：执行层管理者、Lowe's 联络人等需要个人化 SOUL 的角色。

### 模式 C：服务型 Bot（Role Bot，多员工可 DM）

```
Bot 配置：
  chat_ids: ["#hr-private"]
  source_user_ids: ["linda@keller.com"]       # Linda 是 owner
  allow_group_mention_authorize: true          # 员工可申请授权

效果：
  员工 DM hr-bot → 触发 authorize-mention OOB → Linda 在主机 approve
  批准后该员工加入 chat_user_allow，非 owner 上限下使用 hr-bot
```

**适合场景**：HR 服务、全公司查询类场景。

---

## 五、Keller 的 Bot 架构（重新设计）

### 配置矩阵

| Bot 名 | 模式 | Owner（特权）| 共享用户 | 监听 Chat | SOUL 核心 |
|--------|------|------------|---------|----------|----------|
| `orders-bot` | 团队共享 A | Tom（店长）| 全部 Atlanta CSR | #atlanta-orders | 派单助手 |
| `tom-bot` | Personal B | Tom | — | #atlanta-ops, Tom DM | 店长摘要 |
| `mike-bot` | Personal B | Mike | — | Mike DM | 队长工地助手 |
| `karen-bot` | Personal B | Karen | — | #lowes-handover, Karen DM | Lowe's 联络 |
| `beth-bot` | Personal B | Beth | — | #exec, Beth DM | 高管视图 |
| `hr-bot` | 服务型 C | Linda | 全体员工（DM 逐一授权）| #hr-private | HR 服务 |

### 为什么这样设计

- **orders-bot 是共享的**：3 个 CSR 不需要各自一个 Bot，他们在同一个群聊里，
  共用一个 Bot 效率更高，Bot 的 chat memory 是共享订单上下文。
- **店长/高管是 Personal Bot**：他们的操作权限更高、需要个人化记忆（"Tom 倾向于优先确认 Lowe's 订单"），
  Personal Bot 更合适。
- **hr-bot 是服务型**：HR 信息高度敏感，Linda 一个个授权比 source_user_ids 列全员更安全。

---

## 六、可行案例（重新设计，基于平台约束）

> 分组依据：平台约束，不是工程量

### Group A：今天可以跑（无 inbound SMS/Fax 依赖）

---

**Case 1 · 多 CSR 共享派单 Bot**

```
参与者：Sarah / Alex / Maria（3 位 CSR）+ orders-bot（共享）
触发：Sarah @orders-bot "dispatch A8821 to Mike, tomorrow 10am, 1234 Main St Engineered Oak 850sqft, Jenkins +1404-555-0199"

平台路径：
1. orders-bot 接到 Sarah 的消息（非 owner 上限，但 SMS ACTION 可用）
2. Agent 生成：
   ACTION:TASK subject=A8821 assignee=Mike due=tomorrow-10am
   END_ACTION
   ACTION:SMS to=Mike Reyes
   Install #A8821 tomorrow 10am. 1234 Main St Atlanta. Engineered Oak 850sqft.
   Customer: Jenkins +14045550199. Reply CONFIRM to acknowledge.
   END_ACTION
3. Bot 执行 Task（#atlanta-orders 内，origin chat，✅ 无跨 chat）
4. Bot 执行 SMS（到 Mike 手机，✅ SMS scope）
5. Bot 回复 Sarah："✅ Task #T992 · SMS to Mike +14045550211 · delivered"

30min 后续（cron，TEXT ONLY）：
  /cron add "A8821 followup" at:10:30 "Check #atlanta-orders chat memory: if A8821 still unconfirmed, post a text alert"
  → cron 火了 → agent 输出文字："⏳ A8821 未收到 Mike CONFIRM，请 Tom 跟进"
  → 人看到文字，Tom 手动处理

多人价值：Sarah、Alex、Maria 三人的派单都在同一个 #atlanta-orders 里，
chat memory 里有当天所有 open dispatch。任何一个 CSR 都可以问 bot "今天有几个未确认的？"
```

**平台合规**：✅ SMS Action / Task Action 在 origin chat / 非 owner 上限均支持
**CONFIRM 回路**：⚠ 需要 inbound SMS wire（Group B），否则只有文字提示

---

**Case 2 · 店长 Personal Bot 每日运营文本摘要**

```
参与者：Tom（店长），tom-bot（personal）
触发：Heartbeat 17:30 触发（或 cron，或 Tom 手动问"今天怎么样？"）

平台路径（heartbeat 版本）：
  heartbeat.enabled = true, interval = "24h", active_hours = "17:30-17:31"
  → agent 收到 heartbeat prompt
  → agent 输出纯文本摘要（TEXT ONLY，heartbeat 不执行 ACTION 块）
  → 发到 tom-bot 的 defaultChatID（#atlanta-ops）

输出示例（纯文本）：
  [Atlanta Daily · 2026-06-03 17:30]
  今日：8 单完成，2 单延迟（#A8819 材料未到 · #A8820 客户改期）
  明日：11 单预约，6 单已确认
  班组缺口：Mike 队周三需要 2 名协助
  最久未动 Task：#T941（3 天，负责人 Tom）

Tom 读完，觉得 #T941 需要处理：
  Tom @tom-bot "把 T941 状态更新为 InProgress"
  → 这是 Tom 的 personal bot，Tom 是 owner，ACTION:TASK update 正常执行

正确认知：cron/heartbeat 只是"闹钟 + 报纸"，不是"自动执行者"。
```

**平台合规**：✅ Heartbeat text / Tom 手动触发 Task update 均支持

---

**Case 3 · 执行层高管个人 Bot 周报（Beth）**

```
参与者：Beth（Chief of Staff），beth-bot（personal）
触发：Cron 每周一 9:00

平台路径：
  /cron add "weekly-snapshot" "0 9 * * 1" "Generate this week's Keller ops snapshot using chat memory and available data"
  → cron 触发 → agent 输出纯文本周报 → 发到 #exec

纯文本输出（Beth 主动读）：
  [Cron: Weekly Snapshot · W23]
  安装量：243（↑6% vs 上周）
  CSAT：4.5/5（↓0.2，需关注）
  Lowe's SLA：96%（目标 ≥ 95%）
  ...

Beth 如果想做什么（hand-triggered）：
  Beth @beth-bot "把 Atlanta CSAT 下滑的情况发给 Tom"
  → Beth 是 owner → ACTION:MESSAGE chatid=#atlanta-ops
  → 跨 chat audit notice 发到 Beth DM → 5 秒确认 → 消息发出

正确认知：周报是"Beth 的读物"，不是"自动触发下游行动的机器"。
```

**平台合规**：✅ 完全可行，cron 文本 + owner 跨 chat（有 audit notice）

---

**Case 4 · Lowe's 传真：人工触发 + Bot 协助准备**

```
参与者：Karen（Lowe's 联络），karen-bot（personal）
触发方式 A（Cron 提醒）：
  cron 17:00 → agent 输出文本："今日需传真：22 店 31 份，请输入 /lowes-batch 执行"
  Karen 看到文字提醒

触发方式 B（Karen 手动）：
  Karen @karen-bot "帮我准备今天的传真批次"
  → agent 读取 #lowes-handover chat memory，整理清单，输出文本
  → Karen 确认 → Karen 说"发送批次"

关键约束：传真发送不是 ACTION 块
  → 必须通过代码路径（SendFax API）
  → 对话层面的触发：Karen 说"confirm" → bot 调用代码（非 agent ACTION）
  → 这需要在 messaging 层实现 /lowes-batch 命令（或类似的自定义命令）

多人场景：
  如果 Beth 需要审批批次（高价值场景），flow 是：
  Karen @karen-bot "请 Beth 批准今天的传真批次"
  → ACTION:MESSAGE chatid=Beth-DM（跨 chat，Karen 是 owner → audit notice → 发出）
  → Beth 在自己的 DM 看到请求 → Beth @beth-bot "批准 karen 的传真批次"
  → beth-bot 通知 karen-bot（or Karen 直接收到确认）
  这里的通道是"两个 personal bot 通过 owner 的手动动作串联"，不是自动 bot-to-bot

传真执行：Karen 确认后，代码执行 SendFax（每条 90s rate limit），写确认到 Note
```

**平台合规**：✅ 可行，需要 /lowes-batch 命令实现

---

**Case 5 · 员工请假：hr-bot 服务多人（Role Bot 模式）**

```
参与者：Marcus（员工），hr-bot（服务型），Linda（HR owner）
触发：Marcus 私信 hr-bot："请假申请 6/10-6/12，家庭原因"

平台路径：
1. Marcus 在 hr-bot DM → 触发 allow_group_mention_authorize
   （或者 Marcus 已被 Linda 预先 approve 加入白名单）
2. hr-bot（non-owner 上限）回复 Marcus（在 DM 原 chat 内，✅ 无跨 chat）：
   "收到，Marcus。6/10-6/12（3天）。余额 4→1。
   通知 Mike 队长审批，理由 HR 保密。你会在这里收到结果。"
3. 跨 chat 到 Mike DM（给队长发审批请求）：
   → 这是跨 chat ACTION（从 hr-bot DM → Mike DM）
   → Marcus 是非 owner → OOB challenge 发到 Linda DM → Linda 在主机 approve
   → 审批后 ACTION:MESSAGE 到 Mike DM

4. Mike 回复批准（Mike DM hr-bot）→ hr-bot 回复 Marcus ✅
5. hr-bot 匿名广播到 tom-bot 监听的 #atlanta-ops（又一次跨 chat）：
   "班组缺口: Mike 队 6/10-6/12 少 1 名协助。(来源: HR 保密)"
   → 同上，需要 OOB 或 Linda 手动触发

实际可行路径（简化版）：
  Linda 作为 owner 手动处理跨 chat 部分（she sees audit notices, confirms quickly）
  日常使用 Linda 的 DM 接收 audit notices + 快速确认
  这样整个流程是：员工请假 → Linda（在 DM 里快速确认 3 次） → 完成
  对 Linda 来说，audit notice 是在她的 DM 里，她是 OOB approver，确认成本低

多人价值：所有员工都能用 hr-bot，Linda 统一管控
```

**平台合规**：✅ 可行，Linda 是 owner 处理跨 chat audit notices

---

**Case 6 · 未接来电自动跟进（today's missed calls）**

```
参与者：Beth（或任何有 personal bot 的管理者）
触发：Beth @beth-bot "看看今天的未接电话，需要跟进的发个短信"

平台路径：
  Agent 生成：
  ACTION:PHONE_CALLLOG scope=today missing=true next_actions=true
  END_ACTION

  → RingClaw 查询 Call Log API（ReadCallLog scope）
  → 识别需要跟进的 missed inbound calls
  → 自动发送 ACTION:SMS："Sorry I missed your call. What is this regarding?"
  → 摘要发回 Beth 当前 chat

这个 case 已完整实现，无需任何新代码，仅需 ReadCallLog + SMS scope

对 Keller 的价值：
  AI Receptionist 处理了来电，但错过的 OUTBOUND follow-up call 
  和管理层的 missed inbound 仍然靠人。
  这个 case 用当前平台功能就能直接 demo，无 Group B 依赖。
```

**平台合规**：✅ 完全实现，最快可 demo

---

### Group B：需要 inbound SMS/Fax wire（同一处代码修复）

---

**Case 7 · 客户投诉 SMS → orders-bot 处理**

```
参与者：客户（外部手机） → orders-bot（接收端）
触发：客户发 SMS 到门店号

平台路径（inbound wire 完成后）：
1. message-store 事件触发 MessageStoreHandler
2. orders-bot 检测投诉信号
3. 立即回 SMS 给客户（≤60s）
4. 在 #atlanta-orders 发文本升级通知（在 bot 监听的 chat 内，✅ 无跨 chat）
5. Agent 同时生成 ACTION:TASK (urgent, due=+2h) → 在 #atlanta-orders 执行 ✅

Tom 在 #atlanta-orders 看到升级通知 + Task → Tom 自己处理或 @tom-bot
（这是 Tom 手动调用 tom-bot，不是 bot-to-bot 自动触发）
```

**平台合规**：❌ 需要 inbound SMS wire（代码已写好，缺 cmd 层调用）

---

**Case 8 · Lowe's HQ 传真质量标记 → karen-bot 路由**

```
参与者：Lowe's HQ → karen-bot（接收端）→ Tom（行动）
触发：Lowe's HQ 向 Karen 号码发传真

平台路径（inbound wire 完成后）：
1. message-store 事件触发，type=Fax
2. DownloadAttachment 下载 PDF
3. Agent 解析文本（Claude 可以读 text-layer PDF）
4. 在 #lowes-handover 发通知文本（karen-bot 监听的 chat，✅ 无跨 chat）
5. Note append 台账

跨 chat 到 Tom（需要 Karen 手动确认）：
  Karen 看到通知 → Karen @karen-bot "把 #A8810 的质量标记转给 Tom"
  → ACTION:MESSAGE chatid=#atlanta-ops → audit notice → Karen 确认 → 发出
  这里 Karen 是 owner，audit notice 到她的 DM，她确认，成本低
```

**平台合规**：❌ 需要 inbound Fax wire / ✅ 跨 chat 由 Karen 手动触发

---

## 七、重新理解"多 Person 方案"

多 person 不是"多 Bot 自动通信"，而是三种人机交互模式的组合：

```
模式 1：多人 → 同一 Bot（共享团队 Bot）
         Sarah ──┐
         Alex  ──┤→ orders-bot → 共享 chat memory → 统一派单状态
         Maria ──┘

模式 2：一人 → 专属 Bot（Personal Bot）
         Beth → beth-bot → Beth 的私人记忆和偏好

模式 3：人的手动协调（跨 Bot "通信"的真实路径）
         Sarah 看到 orders-bot 发的升级文本
         Sarah 通知 Tom（RC 消息，不是 bot 自动）
         Tom @tom-bot 了解情况
         Tom 决策后 @tom-bot "告知区域协调员"
         → tom-bot 生成跨 chat ACTION → Tom 确认 audit notice → 发出

结论：Bot 与 Bot 之间的"通信"必须经过人的手，audit notice 是那个确认节点。
      这不是设计缺陷，这是安全架构的正确行为。
      产品设计的关键是：让 audit notice 的确认成本降到最低（DM 里一眼就批）。
```

---

## 八、案例汇总

| Case | Bot 模式 | 多人方式 | 今天可 demo | 关键 RC 能力 |
|------|---------|---------|-----------|------------|
| 1 · 多 CSR 派单 | 共享团队 | 3 CSR 共用 | ✅（CONFIRM 回路 ⚠）| Task · SMS-out |
| 2 · 店长日摘要 | Personal | Tom 独用 | ✅ | Heartbeat · ChatMemory |
| 3 · 高管周报 | Personal | Beth 独用 | ✅ | Cron text · owner 跨 chat |
| 4 · Lowe's 传真 | Personal | Karen + Beth 串联（手动）| ✅（需 /lowes-batch 命令）| SendFax · Note |
| 5 · HR 请假 | 服务型 | 全体员工 DM | ✅（Linda 处理 audit notice）| cross-chat · OOB |
| 6 · 未接来电跟进 | Personal | 任意管理者 | ✅ 最快可 demo | PHONE_CALLLOG · SMS |
| 7 · 客户投诉 SMS | 共享团队 | orders-bot + Tom 手动 | ❌ 需 inbound wire | SMS-in |
| 8 · Lowe's 传真质量标记 | Personal | Karen + Tom 手动串联 | ❌ 需 inbound wire | Fax-in |

---

## 九、给 POC 的调整建议

**保留但修正：**
- dispatch-confirm：保留，但 CONFIRM 回路标注为 Group B
- daily-digest：保留，但明确是"文本报告"，不是"自动执行报告"
- complaint-handling：保留，明确为 inbound SMS wire 之后才完整

**删除或降级：**
- "bot-to-bot 自动传递"：改为"人手动传递，Bot 辅助起草"
- "cron 触发 Task 创建"：改为"cron 生成文本，人触发 Task"
- "跨 chat 无成本"：改为"owner 快速 audit notice 确认"

**新增（目前未设计）：**
- Case 6 未接来电跟进：当前完全实现，高价值低成本，应进 POC W1

**优先 demo 顺序：**
1. Case 6（今天跑，无依赖）→ W1
2. Case 1 主流程（派单 SMS，无 CONFIRM 回路）→ W2
3. Case 2/3（text digest，heartbeat / cron）→ W3
4. Case 4（传真，需 /lowes-batch 命令）→ W4
5. Case 7/8（inbound wire 完成后）→ W5
