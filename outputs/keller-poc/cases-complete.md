# Keller POC · 完整 Case 场景

## 总览

| # | Case | Bot | 触发方式 | Demo 状态 | 核心 RC 能力 |
|---|------|-----|---------|----------|------------|
| 1a | CSR 派单 | orders-bot（共享） | Sarah @bot | ✅ Group A | Task · SMS |
| 1b | CSR 改单 | orders-bot（共享） | Alex @bot | ✅ Group A | Task-update · SMS×2 |
| 1c | 队长 CONFIRM 检测 | orders-bot | inbound SMS | ⚠ Group B | SMS-in |
| 2 | 店长每日摘要 | tom-bot（personal） | Heartbeat 17:30 | ✅ Group A | Heartbeat · CallLog |
| 3 | 执行层周报 + 问询 | beth-bot（personal） | Cron 周一 9:00 | ✅ Group A | Cron text · cross-chat |
| 4 | Lowe's 批量传真 | karen-bot（personal） | Cron + Karen 手动 | ✅ Group A | SendFax · Note |
| 5 | HR 请假申请 | hr-bot（role bot） | 员工 DM | ✅ Group A | OOB · Event · cross-chat |
| 6 | 未接来电跟进 | beth-bot（personal） | Beth 主动询问 | ✅ Group A | PHONE_CALLLOG · SMS |
| 7 | 客户投诉 SMS | orders-bot（共享） | inbound SMS | ❌ Group B | SMS-in · Task · CallLog |
| 8 | Lowe's 入站传真 | karen-bot（personal） | inbound Fax | ❌ Group B | Fax-in · Note · cross-chat |

---

## Case 1a · CSR 派单闭环

**场景**：Sarah 下达派单指令，orders-bot 完成 Task 创建 + 队长 SMS，全程 < 10 秒
**参与者**：Sarah Cooper（CSR）→ orders-bot（共享）→ Mike Reyes（SMS 接收）→ Tom Rivera（cron owner）
**触发**：Sarah 在 #atlanta-orders @orders-bot 派单指令

```
━━━ Chat：#atlanta-orders ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Sarah Cooper
  @orders-bot dispatch A8821 to Mike, tomorrow 10am,
  1234 Main St Atlanta GA 30309, Engineered Oak 850sqft,
  customer Jenkins +1 404-555-0199

orders-bot  [处理中...]

  收到！核对派单：
  · 工单：#A8821  · 队长：Mike Reyes
  · 时间：06/04 10:00  · 材料：Engineered Oak 850sqft
  · 地址：1234 Main St Atlanta GA 30309 ✅ ZIP 30309 → Atlanta 匹配
  · 客户：Jenkins +1 404-555-0199

  ✅ Task #T992 created（Mike Reyes · due 06/04 10:00）
  ✅ SMS → Mike +1 404-555-0211 · delivered
  ⏳ 30min 无 CONFIRM → Tom 将收到文字提醒

━━━ Mike 手机收到 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Install #A8821 06/04 10am.
  Address: 1234 Main St Atlanta GA 30309
  Material: Engineered Oak 850sqft
  Customer: Jenkins +14045550199
  Reply CONFIRM to acknowledge.
```

**API 调用**：`CreateTask` · `SendSMS`
**chat memory 写入**：`A8821 | Mike Reyes | 06/04 10:00 | pending`

---

### 30 分钟 CONFIRM 提醒（cron TEXT）

Tom 设置过一次性 cron（owner 权限）。30 分钟后若仍无 CONFIRM：

```
━━━ Chat：#atlanta-orders ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

orders-bot  [cron 10:30 触发，TEXT ONLY]

  ⏳ A8821 派出 30min 仍无 CONFIRM
  Mike Reyes · +1 404-555-0211
  建议 Tom 跟进或直接致电
```

> **平台约束**：cron 触发的回复不执行 ACTION 块，只输出文字

---

## Case 1b · CSR 改单通知

**场景**：Alex 接到客户来电要求改期，orders-bot 同时通知队长和客户，无需 CONFIRM
**参与者**：Alex Kim（CSR）→ orders-bot → Mike（SMS）· Martinez（客户 SMS）

```
━━━ Chat：#atlanta-orders ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Alex Kim
  @orders-bot reschedule A8819 Thursday 2pm same address,
  customer Martinez 刚来电确认了

orders-bot

  改期 #A8819：Wed 10am → Thu 06/05 2pm（地址不变）

  ✅ Task #T889 updated（due → 06/05 14:00）
  ✅ SMS → Mike +1 404-555-0211 · delivered（无需 CONFIRM）
  ✅ SMS → Martinez +1 404-555-0177 · delivered

━━━ Mike 手机 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  #A8819 rescheduled.
  New: Thu 06/05 2pm (same address).
  No CONFIRM needed — note for your schedule.

━━━ Martinez 手机 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Hi! Your Keller install is rescheduled to
  Thursday June 5th at 2pm. Same address.
  Reply here with any questions!
```

**API 调用**：`UpdateTask` · `SendSMS` × 2

---

## Case 1c · 队长 CONFIRM 检测（Group B）

**场景**：Mike 回复 SMS "CONFIRM #A8821"，系统自动关闭跟踪
**前提**：`monitor.SetMessageStoreHandler()` 完成 wire

```
━━━ Mike 手机回复 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  CONFIRM #A8821

━━━ [inbound SMS handler 触发] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  → 号码 +14045550211 匹配 chat memory：A8821 | Mike Reyes | pending
  → UpdateTask T992: note="CONFIRM received 10:18"
  → 取消 30min cron

━━━ Chat：#atlanta-orders ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

orders-bot

  ✅ Mike Reyes 已确认 #A8821（10:18）
  · Task #T992 已更新
  · 确认 cron 已取消
```

**API 调用**：`ListMessages`（inbound） · `UpdateTask`

---

## Case 2 · 店长每日运营摘要

**场景**：tom-bot 每天 17:30 自动生成文字摘要，Tom 读后按需行动
**参与者**：tom-bot（heartbeat 触发）→ Tom Rivera（读，手动决策）

```
━━━ Chat：#atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

tom-bot  [Heartbeat 17:30，TEXT ONLY]

  [Atlanta Daily · 2026-06-03 17:30 EDT]
  今日完成：8 单，2 单延迟
    · #A8819 — 材料未到（Carlos 队，改排周四）
    · #A8820 — 客户临时改期（Sarah 已处理）
  明日预约：11 单，已确认 6 单，5 单待确认
  班组缺口：Mike 队周三需 2 名协助（Engineered Oak 经验）
  最久 Task：#T941（3 天无动作，负责人 Tom Rivera）
  Lowe's 待传：2 份（#A8809 · #A8815，Karen EOD 批次包含）
```

> **平台约束**：heartbeat 回复 TEXT ONLY，Tom 读完才决定是否行动

---

### Tom 读后手动行动

```
━━━ Chat：#atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tom Rivera
  @tom-bot 把 T941 更新为进行中，备注"等待供应商确认"

tom-bot
  [ACTION:TASK update T941 status=InProgress note="等待供应商确认 · Tom 06/03"]

  ✅ Task #T941 → 进行中

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tom Rivera
  @tom-bot 帮我起草发给区域协调员的消息：Mike 队周三缺 2 人，
  Engineered Oak，问 Dallas 有没有人

tom-bot
  起草：
  "Atlanta 周三（06/05）班组缺口：Mike 队 -2，需 Engineered Oak 经验。
   问一下 Dallas 是否有空余人手，出行费用 Keller 承担。—— Tom"

  发到 #southeast-coord？

Tom Rivera
  确认

tom-bot
  [ACTION:MESSAGE chatid=#southeast-coord]

━━━ Tom DM（audit notice，自动发送）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

tom-bot → Tom DM：
  [notice] MESSAGE by tom.rivera at 17:36
  origin=#atlanta-ops → target=#southeast-coord

  ✅ 已发（5 秒内确认）

━━━ Chat：#atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

tom-bot
  ✅ 消息已发到 #southeast-coord
```

**API 调用**：`UpdateTask` · `SendPost`（跨 chat，Tom 是 owner → audit notice）

---

## Case 3 · 执行层周报与定向问询

**场景**：beth-bot 每周一 9am 推送全国视图，Beth 读后向门店店长发问询
**参与者**：beth-bot（cron）→ Beth Owens（读）→ Tom Rivera（问询接收方）

```
━━━ Beth DM with beth-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

beth-bot  [Cron 周一 09:00，TEXT ONLY]

  [Weekly Snapshot · W23 · 2026-06-03]

  📊 33 店本周
  安装量：243（↑6% vs W22）
  CSAT：4.4/5（↓0.3 ⚠）
  Lowe's SLA：96%（目标 ≥95% ✅）
  班组缺口事件：7（↑2 vs W22）

  ⚠ 关注：
  · Atlanta：CSAT 4.2（↓0.5，48h 内 "no-show" 投诉 ×3）
  · Phoenix：班组缺口第 3 周连续

  💡 建议询问：
  · Tom（Atlanta）：近期 no-show 投诉原因？
  · Karen：W22 Lowe's 2 份延迟是否已补发？
```

---

### Beth 发问询（owner 跨 chat，audit notice 确认）

```
━━━ Beth DM with beth-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  帮我给 Tom 发个消息，关于 Atlanta CSAT，措辞友好问询不要指责，
  问他有没有注意到，需要什么支持

beth-bot
  起草：
  "Tom，本周数据出来了，Atlanta CSAT 这周是 4.2（↓0.5），
   顺便问一下有没有注意到什么情况？有什么需要我这边支持的随时说。—— Beth"

  发到 Tom DM 还是 #atlanta-ops？

Beth Owens
  Tom DM

beth-bot
  [ACTION:MESSAGE chatid=Tom-DM]

━━━ Beth DM（audit notice）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

beth-bot → Beth DM：
  [notice] MESSAGE by beth.owens at 09:14
  origin=#exec → target=Tom-Rivera-DM

  ✅ 已发（5 秒内确认）

━━━ Tom DM（收到 beth-bot 消息）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  "Tom，本周数据出来了，Atlanta CSAT 这周是 4.2...—— Beth"
```

**API 调用**：`SendPost`（跨 chat，Beth 是 owner → audit notice）

---

## Case 4 · Lowe's EOD 批量传真

**场景**：karen-bot 每天 17:00 准备传真清单，Karen 手动触发批量执行
**参与者**：karen-bot（cron + 命令）→ Karen Yates（确认执行）→ Lowe's HQ（传真接收方）

```
━━━ Chat：#lowes-handover ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

karen-bot  [Cron 17:00，TEXT ONLY]

  [EOD Batch Prep · 2026-06-03 17:00]
  今日待传真：22 店 · 31 份 · 117 页
  收件：Lowe's HQ Returns +1 919-555-0100
  预计：约 47 分钟（每份 90s 限速）

  各店明细：
    Atlanta：3 份（#A8809 · #A8815 · #A8821）
    Dallas：4 份（#D1122 - #D1125）
    Phoenix：2 份（#P0087 · #P0088）
    ... （共 22 店）

  执行：/lowes-batch send 2026-06-03
  推迟：/lowes-batch defer
```

---

### Karen 执行批次

```
━━━ Chat：#lowes-handover ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Karen Yates
  /lowes-batch send 2026-06-03

karen-bot  [/lowes-batch 命令处理，逐条 SendFax]

  [Batch 开始 · 17:02]

  ✅ #A8809 Atlanta → FAX-20260603-001（4页，17:02）
  ✅ #A8815 Atlanta → FAX-20260603-002（3页，17:04）
  ✅ #A8821 Atlanta → FAX-20260603-003（4页，17:05）
  ✅ #D1122 Dallas  → FAX-20260603-004（4页，17:07）
  ... （继续发送）
  ❌ #P0088 Phoenix → 失败，重试 +60s
  ✅ #P0088 Phoenix → FAX-20260603-031（2页，17:49，重试成功）

  [Batch 完成 · 17:51]
  ✅ 31/31 成功（1 次重试）· 耗时 49 分钟

  [Note 追加台账]
  2026-06-03 | 31份 | 全部成功 | FAX-20260603-001 ~ 031
```

**API 调用**：`SendFax` × 31（含 1 次重试）· `NoteAppend`（台账）
**关键**：`/lowes-batch` 是代码层自定义命令，不是 agent ACTION 块

---

### 大批次请 Beth 审批

```
━━━ Chat：#lowes-handover ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Karen Yates
  /lowes-batch request-approval

karen-bot
  [ACTION:MESSAGE chatid=Beth-DM]

━━━ Karen DM（audit notice）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

karen-bot → Karen DM：
  [notice] MESSAGE by karen.yates at 17:03
  origin=#lowes-handover → target=Beth-Owens-DM
  ✅ Karen 确认

━━━ Beth DM（收到请求）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

beth-bot（or直接 karen-bot 消息）：
  Karen 的 EOD 传真批次请求审批：
  22 店 · 83 份 · 312 页 · 预计 2h20min
  收件：Lowe's HQ Returns

  批准请在此 DM：/lowes-batch approve 2026-06-30

Beth Owens
  /lowes-batch approve 2026-06-30

[karen-bot 收到审批确认，执行批次]
```

---

## Case 5 · HR 请假申请（多人角色隔离）

**场景**：员工 Marcus 申请 PTO，信息在每个角色之间严格隔离
**参与者**：Marcus（员工）→ hr-bot → Linda Wu（OOB 审批）→ Mike Reyes（队长审批）→ Tom（班组缺口广播）

---

### 5a 员工首次 DM（Linda 授权）

```
━━━ Marcus DM with hr-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Marcus Johnson
  你好，我想提个请假申请

[触发 allow_group_mention_authorize]

━━━ Linda DM（OOB challenge）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot → Linda DM：
  Pending approval (abc-123).
  授权 Marcus Johnson 使用 hr-bot DM.
  执行：ringclaw approval abc-123

[Linda 在主机]
  $ ringclaw approval abc-123
  ✅ Marcus Johnson 已授权

━━━ Marcus DM ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot
  你好，Marcus！我是 Keller HR 助手，由 Linda 管理。
  请说说你的需求，我来帮你处理。
```

---

### 5b 请假申请主流程

```
━━━ Marcus DM with hr-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Marcus Johnson
  请假申请 6/10-6/12，家庭原因

hr-bot
  收到，Marcus。
  请假：6/10（周二）—6/12（周四），共 3 天。
  当前余额：4 天 → 申请后剩余 1 天。

  正在通知 Mike Reyes 审批。
  理由依 HR 保密政策不会共享。
  审批结果我会在这里告诉你。

[需通知 Mike → 跨 chat → OOB]

━━━ Linda DM（OOB challenge）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot → Linda DM：
  Pending approval (def-456).
  Cross-chat MESSAGE → Mike Reyes DM
  Body: PTO 6/10-6/12 审批请求（3天，班组影响：少1名协助）
  执行：ringclaw approval def-456

[Linda 在主机]
  $ ringclaw approval def-456
  ✅ 消息发到 Mike DM

━━━ Mike DM（收到审批请求）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot：
  请假审批请求
  日期：6/10—6/12（3 天）
  班组影响：这 3 天 -1 名协助
  （理由依 HR 保密政策不会共享）

  批准：DM hr-bot "approve 0610"
  拒绝：DM hr-bot "deny 0610 [原因]"
```

---

### 5c 队长批准与广播

```
━━━ Mike DM with hr-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Mike Reyes
  approve 0610

━━━ Marcus DM ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot
  ✅ Mike Reyes 已批准。
  [ACTION:EVENT title="PTO" start=2026-06-10 end=2026-06-12]
  日历已更新。6/10-6/12 已标记为 PTO。祝你有个好假！

[匿名广播到 #atlanta-ops → 跨 chat → OOB]

━━━ Linda DM（第二次 OOB）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot → Linda DM：
  Pending approval (ghi-789).
  Cross-chat MESSAGE → #atlanta-ops
  Body: 班组缺口：Mike 队 6/10-6/12 -1 名协助（来源：HR 保密）
  执行：ringclaw approval ghi-789

[Linda approve]

━━━ Chat：#atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot：
  班组缺口：Mike 队 6/10（周二）—6/12（周四）-1 名协助。
  （来源：HR 保密。）
```

**信息隔离验证**：

| 角色 | 知道什么 | 不知道什么 |
|------|---------|-----------|
| Marcus | 已批准，日历更新 | 队长看到了什么 |
| Mike | 日期 + 班组影响 | 申请人姓名 · 原因 |
| Tom（#atlanta-ops）| 日期 + 班组影响 | 是谁 · 原因 |
| Linda | 一切 | — |
| Beth / #exec | 不知道这件事 | — |

**API 调用**：`CreateEvent`（日历）· `SendPost`（跨 chat × 2，OOB 审批）

---

## Case 6 · 未接来电自动跟进

**场景**：Beth 一句话，bot 查询今日未接来电并自动发短信跟进，完全用现有实现
**参与者**：Beth Owens → beth-bot
**核心亮点**：零 Group B 依赖，今天可 demo，高使用频率

```
━━━ Beth DM with beth-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  看看今天未接的电话，需要的发个短信跟进

beth-bot
  [ACTION:PHONE_CALLLOG scope=today missing=true next_actions=true limit=20]

  查询中...

  [ListExtensionCallLog：dateFrom=06/03 00:00, dateTo=06/03 23:59]
  [result=Missed, direction=Inbound → 3 条]
  [自动发 follow-up SMS × 3]

  📞 今日未接来电（3 条）

  · 10:23  未知  +1 404-555-0xxx
    → "Hi, this is Beth Owens from Keller Interiors.
       I missed your call. What can I help you with?"
    → SMS delivered ✅

  · 11:45  +1 919-555-0188（Lowe's HQ Compliance）
    → "Hi, this is Beth Owens from Keller Interiors.
       I missed your call. Could you let me know what this is regarding?"
    → SMS delivered ✅
    ⚠️ Lowe's HQ 号段，建议优先回拨确认

  · 14:12  Mark Jenkins  +1 404-555-0199
    → "Hi Mark, this is Beth Owens from Keller Interiors.
       I missed your call. What can I help with?"
    → SMS delivered ✅
    （备注：Jenkins 是 Atlanta 常客，工单 #A8821）

Beth Owens
  Lowe's 这个直接打过去

beth-bot
  好的。需要的话 `/phone ringout +19195550188` 可以直接外呼。
```

**API 调用**：`ListExtensionCallLog`（ReadCallLog scope）· `SendSMS` × 3
**为什么是最快可 demo**：完全在现有 Action 体系内，无新代码依赖

---

## Case 7 · 客户投诉 SMS 处理（Group B）

**场景**：客户发 SMS 投诉，orders-bot 60 秒内自动安抚，并在群里升级
**参与者**：客户（SMS）→ orders-bot（inbound 检测）→ Sarah/Tom（读升级文本）→ tom-bot（辅助分析）
**前提**：`monitor.SetMessageStoreHandler()` 完成 wire

```
━━━ 客户手机发到门店 SMS 号 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  "Crew didn't show up for #A8810. Worst service ever!!!"

━━━ [inbound SMS handler] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  → MessageStoreHandler 触发（type=SMS）
  → 检测投诉信号：
    "didn't show up" ✓   "Worst service ever" ✓   "!!!" ✓
  → 投诉确认

━━━ 客户手机（≤60 秒，orders-bot SendSMS）━━━━━━━━━━━━━━━━━━━━━━━

  "Hi! I'm so sorry about this — I'm escalating to our manager
   right now. You'll get a call back within 15 minutes.
   We take this seriously. — Keller Atlanta"

━━━ Chat：#atlanta-orders ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

orders-bot  [TEXT + ACTION 在 origin chat]

  ⚠️ 投诉升级：客户 Jenkins +1 404-555-0199

  订单：#A8810
  投诉（原文）："Crew didn't show up...Worst service ever!!!"
  情绪：强烈不满（"worst" + "!!!"）
  自动回复：已发 ✅（承诺 15min 回电）

  [ACTION:TASK]
  → CreateTask: "URGENT: A8810 投诉" · 负责人=Tom Rivera · due=+2h · 红色
  → Task #T993 ✅

  自动调查（读 chat memory + call log）：
  · #A8810 记录：Mike Reyes · 06/03 10am · 1234 Main St 30309
  · 今日 Mike 无 30309 区域外呼记录
  · A8810 发送 SMS 记录存在，无 CONFIRM 记录
  建议：联系 Mike 确认是否收到派单 SMS
```

---

### Tom 介入：用 tom-bot 辅助分析

```
━━━ Chat：#atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tom Rivera
  @tom-bot 帮我查 A8810 Mike 今天的外呼记录

tom-bot
  [ACTION:PHONE_CALLLOG scope=today]

  Mike Reyes 今日外呼（截止 14:30）：
  · 08:30 → 供应商 +1 404-555-0233
  · 09:15 → Mike 备用号 +1 404-555-0211
  （无 Jenkins +14045550199 的外呼记录）

  #A8810 派单时间：08:52（Sarah 下单）
  SMS 发出：✅  CONFIRM 回复：❌ 无

  结论：Mike 收到了 SMS 但未 CONFIRM，且未拨打客户。
  建议 Tom 直接致电 Mike。

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tom Rivera
  [直接致电 Mike，确认情况后]
  @orders-bot 给 Jenkins 发短信：队长 20 分钟内到，诚挚道歉

orders-bot
  [ACTION:SMS to=+14045550199]

  "Hi! This is Keller Atlanta — sincerely sorry for the confusion.
   Our crew is on the way and will arrive within 20 minutes.
   Thank you so much for your patience. — Keller Atlanta"

  → SMS delivered ✅
```

**API 调用**：`ListMessages`（inbound）· `SendSMS`（客户 ack）· `CreateTask`（URGENT）· `ListExtensionCallLog`（tom-bot 分析）

---

## Case 8 · Lowe's HQ 入站传真（Group B）

**场景**：Lowe's HQ 传真质量标记，karen-bot 自动解析并记录，Karen 手动路由到相关方
**参与者**：Lowe's HQ（传真）→ karen-bot（inbound 检测）→ Karen（路由）→ Tom + Beth（接收方）
**前提**：inbound fax wire 完成

```
━━━ [Lowe's HQ Compliance +1 919-555-0188 传真进来] ━━━━━━━━━━━━━━

  Quality Inspection Notice
  Order: #A8810, 1234 Main St, Atlanta GA
  Issue: Installation quality flag — carpet alignment deviation
  Action required: Re-inspection within 5 business days
  SOP Reference: §7.3
  Notice date: 2026-06-03

━━━ [inbound fax handler] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  → MessageStoreHandler 触发（type=Fax）
  → DownloadAttachment → PDF 下载
  → agent 读取文本层（text-layer PDF）
  → 解析：订单 #A8810 · 门店 Atlanta · 截止 5 个工作日（= 06/10）· SOP §7.3

━━━ Chat：#lowes-handover ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

karen-bot  [TEXT + Note 在 origin chat]

  [Lowe's HQ Notice · REF-2026-0603-11]
  Subject：Quality flag — Order #A8810，Atlanta
  SOP：§7.3 Re-inspection within 5 business days
  截止：2026-06-10（周二）
  来源：+1 919-555-0188（Compliance 部门）

  [ACTION:NOTE 追加台账]
  → 台账 Note 追加：
    2026-06-03 | REF-2026-0603-11 | A8810 | Atlanta | 复检 | 截止 06/10
  → Note 更新 ✅

  ⚠️ 注意：#A8810 今日同时有客户投诉（来自 orders-bot）
  双路升级建议通知 Beth
```

---

### Karen 手动路由

```
━━━ Chat：#lowes-handover ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Karen Yates
  @karen-bot 把 A8810 质量标记通知发给 Tom，也通知 Beth 这是双路升级

karen-bot
  发到 #atlanta-ops 和 Beth DM 各一条，确认？

Karen Yates
  确认

karen-bot
  [ACTION:MESSAGE chatid=#atlanta-ops]
  [ACTION:MESSAGE chatid=Beth-DM]

━━━ Karen DM（两条 audit notice）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  [notice] MESSAGE origin=#lowes-handover → target=#atlanta-ops ✅
  [notice] MESSAGE origin=#lowes-handover → target=Beth-Owens-DM ✅

━━━ Chat：#atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

karen-bot（via ACTION:MESSAGE）：
  [Lowe's HQ Notice] #A8810 Atlanta 需要复检。
  截止：2026-06-10（周二），SOP §7.3。
  请安排。—— Karen

━━━ Beth DM ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

karen-bot（via ACTION:MESSAGE）：
  #A8810 双路升级：
  · Lowe's 质量标记（复检截止 06/10）
  · 客户当日投诉（orders-bot 升级中）
  Atlanta Tom 已收到通知。—— Karen
```

**API 调用**：`ListMessages`（inbound fax）· `DownloadAttachment` · `NoteAppend`（台账）· `SendPost`（跨 chat × 2）

---

## Bot 协作全景

```
外部输入
  Customer SMS ──────────────────────────┐
  Lowe's HQ Fax ─────────────────────────┤
                                          ↓
                              共享/联络 Bot
                         orders-bot（CSR 团队）
                         karen-bot（Lowe's 联络）
                                          │
                              文本发到共享 Chat
                         #atlanta-orders / #lowes-handover
                                          │
                              人看到文本，决策
                         Sarah / Tom / Karen / Beth
                                          │
                         人 @自己的 Personal Bot 分析
                              tom-bot / beth-bot
                                          │
                         Personal Bot 起草跨 Chat 消息
                                          │
                         Owner 确认 audit notice（5秒）
                                          │
                              消息到达目标 Chat
                         #atlanta-ops / #southeast-coord / DM

关键原则：
  · Bot 与 Bot 不自动通信
  · 跨 Chat 必经 owner 手动确认（audit notice）
  · Cron / Heartbeat 只输出文本，人决定是否行动
  · Linda 是 hr-bot 的 OOB 枢纽，是多人场景里负担最重的角色
```

---

## Group A Demo 路径建议

```
W1  Case 6（未接来电跟进）
    → 零依赖，today 可跑，bethbot + PHONE_CALLLOG + SMS

W2  Case 1a（多 CSR 派单）+ Case 1b（改单）
    → orders-bot 共享模式 + Task + SMS

W3  Case 2（Tom 日摘要）+ Case 3（Beth 周报）
    → Heartbeat / Cron text + 跨 chat audit notice

W4  Case 4（Lowe's 批量传真）
    → 需要 /lowes-batch 命令实现

W5  Case 5（HR 请假）
    → hr-bot role bot + OOB × 3

W5+ Case 7（客户投诉）+ Case 8（Lowe's 传真入站）
    → inbound SMS/Fax wire 完成后
```
