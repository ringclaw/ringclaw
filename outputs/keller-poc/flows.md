# Keller POC · Case 具体场景流程

每个 case 包含：对话原文 · API 调用 · 多 bot 协作节点 · 平台约束说明

---

## Case 1 · 多 CSR 共享 Bot 派单闭环

**参与者**：Sarah（CSR）→ orders-bot（共享）→ Mike（队长，SMS 接收方）→ Tom（owner，设置 cron）
**核心价值**：3 个 CSR 共用 1 个 Bot，派单到 SMS 完成 < 10 秒，CONFIRM 回路跟踪

---

### 1.1 主流程（Sarah 派单）

```
Chat：#atlanta-orders
---
Sarah Cooper:
  @orders-bot dispatch A8821 to Mike, tomorrow 10am, 1234 Main St Atlanta GA 30309,
  Engineered Oak 850sqft, customer Jenkins +1 404-555-0199

orders-bot [agent 处理中...]

orders-bot:
  收到！核对派单信息：
  · 工单：#A8821
  · 队长：Mike Reyes
  · 时间：明日 06/04 10:00
  · 地址：1234 Main St Atlanta GA 30309 ✅ ZIP 30309 → Atlanta 匹配
  · 材料：Engineered Oak 850sqft
  · 客户：Jenkins +1 404-555-0199

  [RingClaw 执行 ACTION:TASK]
  → CreateTask: subject="A8821 Install - Mike", assignee=Mike Reyes, due=06/04 10:00
  → Task #T992 创建成功

  [RingClaw 执行 ACTION:SMS]
  → SendSMS to +14045550211 (Mike Reyes):
  ┌────────────────────────────────────────
  │ Install #A8821 06/04 10am.
  │ Address: 1234 Main St Atlanta GA 30309
  │ Material: Engineered Oak 850sqft
  │ Customer: Jenkins +14045550199
  │ Reply CONFIRM to acknowledge.
  └────────────────────────────────────────
  → SMS delivered ✅

  ✅ Task #T992 created
  ✅ SMS → Mike +1 404-555-0211 · delivered
  ⏳ 30min 无 CONFIRM → Tom 将收到提醒
```

**API 调用**：`CreateTask` · `SendSMS`
**权限**：Sarah 是 chat_user_allow（非 owner），但 Task 和 SMS ACTION 在 origin chat 内 → ✅ 可执行
**chat memory 写入**：`A8821 | Mike Reyes | 06/04 10:00 | pending`

---

### 1.2 30 分钟提醒（cron TEXT，Tom 设置）

```
Tom（已设置 cron）:
  /cron add "A8821-confirm" at:2026-06-04T10:30:00
    "查看 #atlanta-orders chat memory，检查 A8821 是否已 CONFIRM。
     若未确认，输出文字提醒。"

[10:30 cron 触发，agent 读 chat memory → A8821 状态仍为 pending]

orders-bot 在 #atlanta-orders 发出文本：
  ⏳ A8821 派出 30 分钟后仍无 CONFIRM。
  Mike 手机：+1 404-555-0211
  建议跟进：直接致电或 Tom 在 #atlanta-ops 处理。
```

**平台约束**：cron TEXT ONLY，不触发新的 SMS ACTION
**人工介入**：Tom 或 Sarah 看到文字后手动跟进

---

### 1.3 CONFIRM 回路（Group B，需 inbound SMS wire）

```
[Mike 手机回复 SMS："CONFIRM #A8821"]

[inbound SMS handler 触发]
→ MessageStoreHandler 检测到 inbound SMS from +14045550211
→ 匹配 chat memory 中 A8821 → Mike Reyes → +14045550211

orders-bot 在 #atlanta-orders 发出：
  ✅ Mike Reyes 已确认 #A8821
  · Task #T992 → 状态更新为 Confirmed
  · 30min cron 已取消

[RingClaw 调用 UpdateTask：T992 status=InProgress, note="CONFIRM received 10:18"]
```

---

### 1.4 改单流程（Alex 改期）

```
Chat：#atlanta-orders
---
Alex Kim:
  @orders-bot reschedule A8819 Thursday 2pm same address, customer Martinez 联系了

orders-bot:
  改期 #A8819：
  · 原时间：Wed 10am → 新时间：Thu 2pm
  · 地址不变：987 Peach St Atlanta GA 30308

  [ACTION:TASK update A8819 due=06/05 14:00]
  → Task #T889 updated ✅

  [ACTION:SMS to Mike Reyes +14045550211]
  ┌────────────────────────────────────────
  │ #A8819 rescheduled.
  │ New: Thu 06/05 2pm (same address)
  │ No CONFIRM needed — note for your schedule.
  └────────────────────────────────────────
  → SMS delivered ✅

  [ACTION:SMS to customer Martinez +14045550177]
  ┌────────────────────────────────────────
  │ Hi! Your Keller install is rescheduled to
  │ Thursday June 5th at 2pm. Same address.
  │ Reply here with any questions!
  └────────────────────────────────────────
  → SMS delivered ✅

  ✅ Task updated · SMS to Mike · SMS to Martinez
```

---

## Case 2 · Tom 店长 Bot 每日运营摘要

**参与者**：tom-bot（heartbeat 触发）→ Tom（读，决定是否行动）
**核心价值**：17:30 自动生成摘要，Tom 不用手动整理，阅读后可选择行动

---

### 2.1 Heartbeat 触发（17:30）

```
[tom-bot heartbeat 17:30 触发]
[agent 读取 #atlanta-orders chat memory + call log]

tom-bot 发到 #atlanta-ops（TEXT ONLY）：

[Atlanta Daily · 2026-06-03 17:30 EDT]
今日完成：8 单，2 单延迟
  · #A8819 — 材料未到（Carlos 队，已改排周四）
  · #A8820 — 客户临时改期（Sarah 已处理，改排明日）
明日预约：11 单，已确认 6 单，5 单待确认
班组缺口：Mike 队周三需要 2 名协助（缺 Engineered Oak 经验）
最久 Task：#T941（3 天无动作，负责人 Tom Rivera）
Lowe's 待传：2 份（#A8809 · #A8815，Karen EOD 批次包含）
```

**平台约束**：heartbeat 触发，TEXT ONLY，不执行 ACTION 块

---

### 2.2 Tom 读后手动行动

```
Chat：#atlanta-ops
---
Tom Rivera:
  @tom-bot 把 T941 更新为进行中，备注"今天已查看，等待供应商确认"

tom-bot:
  [ACTION:TASK update T941 status=InProgress note="今天已查看，等待供应商确认 · Tom 06/03"]
  → Task #T941 更新成功 ✅

  T941 已更新为进行中。

---

Tom Rivera:
  @tom-bot 帮我给区域协调员发个消息：Mike 队周三缺 2 个人，
  有 Engineered Oak 经验优先，问问 Dallas 有没有人

tom-bot:
  起草好了：
  "Atlanta 周三（06/05）班组缺口：Mike 队 -2，需 Engineered Oak 经验。
   询问 Dallas 是否有空余人手。出行费用 Keller 承担。"

  发送到 #southeast-coord？

Tom Rivera:
  确认发送

tom-bot:
  [ACTION:MESSAGE chatid=#southeast-coord]
  → 跨 chat：audit notice 发到 Tom DM ──────────────────────────────
  │ [notice] MESSAGE by tom.rivera at 17:34: origin=#atlanta-ops     │
  │ target=#southeast-coord                                           │
  └──────────────────────────────────────────────────────────────────
  → Tom 在 DM 里看到 notice（5 秒内自动确认）→ 消息发出 ✅

  ✅ 消息已发到 #southeast-coord
```

**多 bot 协作**：tom-bot 发消息到 #southeast-coord，如果区域协调员 Bot 在监听这个群，他们的 Bot 可以接到。否则人工接收。

---

## Case 3 · Beth 执行层周报

**参与者**：beth-bot（cron 触发）→ Beth（读）→ Beth 手动行动
**核心价值**：全国 33 店视图，周一 9am 自动推送，Beth 不用 pull 数据

---

### 3.1 周报生成（Monday 9:00 cron）

```
[beth-bot cron 触发，Monday 09:00]
[agent 读取 global memory + per-chat memory]

beth-bot 发到 Beth DM（TEXT ONLY）：

[Weekly Snapshot · W23 · 2026-06-03]
📊 33 店本周
安装量：243（↑6% vs W22）
CSAT：4.4/5（↓0.3 ⚠）
Lowe's SLA：96%（目标 ≥95% ✅）
班组缺口事件：7（↑2 vs W22）

⚠ 关注：
· Atlanta：CSAT 4.2（↓0.5，过去 48h "no-show" 类投诉 ×3）
· Phoenix：班组缺口第 3 周连续

💡 建议询问：
· Tom（Atlanta）：近期 no-show 投诉的原因？
· Karen：W22 Lowe's 2 份延迟是否已补发？
```

---

### 3.2 Beth 读后发问询消息

```
Chat：Beth DM with beth-bot
---
Beth Owens:
  帮我给 Tom 发个关于 Atlanta CSAT 下滑的消息，措辞是友好问询，
  不要指责，问他有没有注意到这个情况，需要什么支持

beth-bot:
  起草：
  "Tom，本周全国 CSAT 数据出来了，Atlanta 这周是 4.2（↓0.5）。
   顺便问一下有没有注意到什么情况？有什么需要支持的随时说。
   —— Beth"

  发到 #atlanta-ops 还是直接 DM Tom？

Beth Owens:
  发到 Tom DM

beth-bot:
  [ACTION:MESSAGE chatid=Tom-DM]
  → 跨 chat：audit notice 到 Beth DM ───────────────────────────────
  │ [notice] MESSAGE by beth.owens at 09:12: origin=#exec           │
  │ target=Tom-Rivera-DM                                            │
  └──────────────────────────────────────────────────────────────────
  → Beth 确认（DM 里看到，5 秒内）→ 消息发到 Tom DM ✅

  ✅ 消息已发到 Tom DM
```

---

## Case 4 · Lowe's EOD 批量传真

**参与者**：karen-bot（cron 提醒）→ Karen（读，触发执行）→ 传真到 Lowe's HQ
**核心价值**：每天 31 份传真从手动 8 分钟/份 → Karen 确认一次自动批量

---

### 4.1 EOD 准备（cron TEXT，17:00）

```
[karen-bot cron 触发，17:00]
[agent 读取 #lowes-handover chat memory]

karen-bot 在 #lowes-handover 发出（TEXT ONLY）：

[Cron: EOD Batch Prep · 2026-06-03 17:00]
今日待传真：22 店 · 31 份 · 117 页
收件：Lowe's HQ Returns +1 919-555-0100
预计时长：约 47 分钟（每份 90s 限速）

各店明细：
  Atlanta：3 份（#A8809 · #A8815 · #A8821）
  Dallas：4 份（#D1122 · #D1123 · #D1124 · #D1125）
  Phoenix：2 份（#P0087 · #P0088）
  ... （22 店，共 31 份）

执行：/lowes-batch send 2026-06-03
推迟：/lowes-batch defer（明日 09:00 再试）
```

---

### 4.2 Karen 执行批次

```
Karen Yates（在 #lowes-handover）：
  /lowes-batch send 2026-06-03

[/lowes-batch 命令由 messaging 层处理（非 agent ACTION）]
[逐条调用 SendFax：]

karen-bot：
  [Batch 开始 · 2026-06-03 17:02]
  #A8809 Atlanta → +1 919-555-0100 ... ✅ Conf#FAX-20260603-001 (4 pages)
  #A8815 Atlanta → +1 919-555-0100 ... ✅ Conf#FAX-20260603-002 (3 pages)
  #A8821 Atlanta → +1 919-555-0100 ... ✅ Conf#FAX-20260603-003 (4 pages)
  #D1122 Dallas  → +1 919-555-0100 ... ✅ Conf#FAX-20260603-004 (4 pages)
  ...
  #P0088 Phoenix → +1 919-555-0100 ... ❌ Failed (attempt 1/3, retry +60s)
  #P0088 Phoenix → +1 919-555-0100 ... ✅ Conf#FAX-20260603-031 (2 pages) retry OK

  [Batch 完成 · 17:51]
  ✅ 31/31 发送成功（1 次重试）
  总耗时：49 分钟

[Note 追加到 #lowes-handover "合规台账"：]
  2026-06-03 | 31 份 | 全部成功 | Conf#FAX-20260603-001 至 031
```

**API 调用**：`SendFax` × 31 · `NoteAppend`（台账）
**关键约束**：`/lowes-batch` 是自定义命令，由代码层执行，不是 agent ACTION 块

---

### 4.3 Beth 审批场景（超大批次）

```
[假设今日是月末，批次 >80 份]

karen-bot cron TEXT 末尾加：
  ⚠ 本次批次超 80 份，建议 Beth 审批后执行。
  输入 /lowes-batch request-approval 发送审批请求。

Karen Yates：
  /lowes-batch request-approval

karen-bot：
  [ACTION:MESSAGE chatid=Beth-DM]
  → 跨 chat audit notice 到 Karen DM → Karen 确认 → 消息发到 Beth DM：

  "Karen 的 EOD 传真批次需要审批（超大批次）：
   22 店 · 83 份 · 312 页
   预计 2h20min
   收件：Lowe's HQ Returns
   执行请 DM karen-bot：/lowes-batch approve 2026-06-30"

Beth Owens（在 karen-bot DM 里）：
  /lowes-batch approve 2026-06-30

[karen-bot 执行批次]
```

---

## Case 5 · HR 服务 Bot：员工请假全流程

**参与者**：Marcus（员工）→ hr-bot → Linda（OOB 审批）→ Mike（队长审批）→ Tom（收班组缺口通知）
**核心价值**：员工体验 HR 保密，店长只看班组影响，全流程无信息泄露

---

### 5.1 员工首次 DM（Linda 授权）

```
Marcus Johnson（DM hr-bot）：
  你好，请假申请

[hr-bot 触发 allow_group_mention_authorize]

Linda Wu（DM 收到 OOB challenge）：
  Pending approval (abc-123).
  授权 Marcus Johnson 使用 hr-bot DM。
  执行：ringclaw approval abc-123

[Linda 在主机执行：ringclaw approval abc-123]
[Marcus 加入 hr-bot chat_user_allow，后续无需再次授权]

hr-bot → Marcus DM：
  你好，Marcus！我是 Keller HR 助手，由 Linda 管理。
  请说说你的需求，我来帮你处理。
```

---

### 5.2 请假申请主流程

```
Marcus Johnson → hr-bot DM：
  请假申请 6/10-6/12，家庭原因

hr-bot → Marcus DM（在 DM 原 chat，无跨 chat）：
  收到，Marcus。
  请假：6/10（周二）— 6/12（周四），共 3 天。
  当前余额：4 天 → 申请后剩余 1 天。

  正在通知 Mike Reyes 审批。
  理由依 HR 保密政策不会共享。
  审批结果我会在这里告诉你。

[hr-bot 需要通知 Mike → 跨 chat → OOB challenge]

Linda Wu DM 收到：
  Pending approval (def-456).
  Cross-chat MESSAGE → Mike Reyes DM
  Body: PTO 6/10-6/12 approval request (3 days, crew impact: -1 helper those days)
  执行：ringclaw approval def-456

[Linda approve → ACTION:MESSAGE 发到 Mike DM]

Mike Reyes DM（收到 hr-bot 消息）：
  请假审批请求：
  申请日期：6/10 — 6/12（3 天）
  班组影响：这 3 天少 1 名协助
  （理由依 HR 保密政策不会共享）

  批准请回复 hr-bot："approve 0610"
  拒绝请回复 hr-bot："deny 0610 [原因]"
```

---

### 5.3 队长批准

```
Mike Reyes（DM hr-bot）：
  approve 0610

[hr-bot 触发 OOB authorize → Mike 也被授权 DM hr-bot]
[hr-bot 处理批准：]

hr-bot → Marcus DM：
  ✅ Mike Reyes 已批准你的请假申请。
  [ACTION:EVENT title="PTO" start=2026-06-10 end=2026-06-12]
  → Calendar 事件已创建 ✅

  日历已更新。6/10-6/12 已标记为 PTO。
  祝你有个好假！

[hr-bot 发匿名班组缺口 → 跨 chat → 再次 OOB]

Linda DM 收到：
  Pending approval (ghi-789).
  Cross-chat MESSAGE → #atlanta-ops
  Body: 班组缺口：Mike 队 6/10-6/12 少 1 名协助（来源：HR 保密）
  执行：ringclaw approval ghi-789

[Linda approve]

#atlanta-ops：
  班组缺口：Mike 队 6/10（周二）—6/12（周四）少 1 名协助。
  （来源：HR 保密。）
```

**信息流验证**：
- Marcus 知道：已批准，日历更新
- Mike 知道：日期 + 班组影响，不知道原因
- Tom 知道（#atlanta-ops）：日期 + 班组影响，不知道是谁、不知道原因
- Linda 知道：一切（她是 owner）
- #exec：不知道这件事（无需知道）

---

## Case 6 · 未接来电自动跟进（今天可 demo）

**参与者**：Beth（或任何 personal bot owner）→ PHONE_CALLLOG → 自动 SMS
**核心价值**：完全用现有功能实现，无 Group B 依赖，高频使用场景

---

```
Chat：Beth DM with beth-bot
---
Beth Owens：
  看看今天的未接来电，需要的发个短信跟进

beth-bot：
  好的，查询今天的通话记录。

  [ACTION:PHONE_CALLLOG scope=today missing=true next_actions=true limit=20]

  [RingClaw 调用 ListExtensionCallLog：]
  → scope=today → dateFrom=2026-06-03T00:00, dateTo=2026-06-03T23:59
  → result=Missed, direction=Inbound
  → 返回 3 条未接来电：
    10:23 +1 404-555-0xxx （未知）
    11:45 +1 919-555-0188 （Lowe's HQ Compliance）
    14:12 +1 404-555-0199 （Mark Jenkins）

  [自动发 follow-up SMS × 3：]
  → SendSMS to +14045550xxx:
    "Hi, this is Beth Owens from Keller Interiors. I missed your call earlier.
     Could you let me know what this is regarding? You can reach me here."
  → SendSMS to +19195550188:
    "Hi, this is Beth Owens from Keller Interiors. I missed your call earlier.
     Could you let me know what this is regarding? Best, Beth"
  → SendSMS to +14045550199:
    "Hi Mark, this is Beth Owens from Keller Interiors. I missed your call.
     What can I help you with? Feel free to reply here."

  📞 今日未接来电（3 条）

  · 10:23 未知号码 +1 404-555-0xxx
    → follow-up SMS 已发 ✅

  · 11:45 Lowe's HQ Compliance +1 919-555-0188
    → follow-up SMS 已发 ✅
    ⚠️ Lowe's 来电，建议优先回拨确认

  · 14:12 Mark Jenkins +1 404-555-0199
    → follow-up SMS 已发 ✅
    （备注：Jenkins 是 Atlanta 常客，工单 #A8821）

  Beth Owens：
    Lowe's 这个我直接打过去

  beth-bot：
    好的。需要的话 `/phone ringout +19195550188` 可以直接外呼。
```

**API 调用**：`ListExtensionCallLog`（ReadCallLog scope）· `SendSMS` × 3
**平台合规**：✅ 完全实现，零 Group B 依赖，beth-bot 是 owner

---

## Case 7 · 客户投诉 SMS → orders-bot 处理（Group B）

**参与者**：客户（外部 SMS）→ orders-bot（inbound 检测）→ Sarah（看到升级文本）→ Tom（手动跟进）
**前提**：`monitor.SetMessageStoreHandler()` 已 wire

---

```
[客户手机 → 门店 SMS 号]
"Crew didn't show up for #A8810. Worst service ever!!!"

[inbound SMS handler 触发：]
→ MessageStoreHandler 检测到 inbound, type=SMS, from=+14045550199
→ agent 读取文本，检测投诉信号：
  "didn't show up" ✓ · "Worst service ever" ✓ · "!!!" ✓
→ 投诉信号确认

[≤60 秒，orders-bot SendSMS to +14045550199（客户）：]
┌────────────────────────────────────────────────────────────
│ Hi! I'm so sorry about this — I'm escalating to our manager
│ right now. You'll get a call back within 15 minutes.
│ We take this seriously. — Keller Atlanta
└────────────────────────────────────────────────────────────

[同时，orders-bot 在 #atlanta-orders 发文本（origin chat，无跨 chat）：]
⚠️ 投诉升级：客户 Jenkins +14045550199
订单：#A8810
投诉内容（原文）："Crew didn't show up...Worst service ever!!!"
情绪信号：强烈不满（3 个感叹号 + "worst"）
自动回复：已发 ✅（15min callback承诺）

[ACTION:TASK 在 #atlanta-orders：]
→ CreateTask: subject="URGENT: A8810 customer complaint", 
  assignee=Tom Rivera, due=+2h, color=Red
→ Task #T993 创建 ✅

[agent 读 chat memory → A8810 派单记录：]
→ A8810 | Mike Reyes | 06/03 10am | 1234 Main St Atlanta GA 30309
→ ZIP 30309 → Atlanta ✅（地址本身正确）
→ Call log 查询：Carlos 队有无在 30309 区域作业记录？

[orders-bot 追加到升级帖：]
初步调查（自动）：
· A8810 记录：Mike Reyes，06/03 10am，1234 Main St 30309
· Call log：今日 Mike 队无 30309 区域外呼记录
· 可能原因：派单未到位 / 时间理解错误
建议 Tom：联系 Mike 确认是否收到派单 SMS

```

**Tom 看到 #atlanta-orders 的升级文本：**
```
Tom Rivera：
  @tom-bot 帮我查 A8810 的派单记录和 Mike 今天的 call log

tom-bot（在 #atlanta-ops）：
  [ACTION:PHONE_CALLLOG scope=today]
  → Mike 今日外呼记录：
    08:30 → +14045550233（供应商）
    09:15 → +14045550211（Mike 自己的备用号）
    ...（无 Jenkins 号码）

  A8810 派单：06/03 08:52 Sarah 下单，SMS 发出记录：✅
  Mike 回复记录：无 CONFIRM（orders-bot chat memory）

  结论：Mike 未回复 CONFIRM，且今日无该地址外呼。
  建议：Tom 直接致电 Mike 确认情况。
```

**多 bot 协作说明**：
- orders-bot 处理 inbound SMS + 升级文本 + Task 创建
- Tom 用 tom-bot 查询额外信息（call log）
- 人工串联：Tom 看到 orders-bot 升级文本 → Tom @tom-bot 查信息 → Tom 决策

---

## Case 8 · Lowe's HQ 质量传真入站（Group B）

**参与者**：Lowe's HQ → karen-bot（inbound fax）→ Karen 手动路由 → Tom
**前提**：inbound fax wire 完成 + /lowes-batch send 命令实现

---

```
[Lowe's HQ Compliance 传真到 Karen 号码]
内容：Quality flag for Order #A8810, Installation at 1234 Main St Atlanta.
      Re-inspection required per §7.3. Deadline: 5 business days from 06/03.

[inbound fax handler 触发：]
→ MessageStoreHandler，type=Fax，from=+19195550188
→ DownloadAttachment → PDF 下载
→ agent 读取 PDF 文本层（Claude 可读 text-layer PDF）

→ 解析出：
  订单：#A8810
  门店：Atlanta
  问题：质量标记，需复检
  截止：5 个工作日（= 06/10）
  SOP：§7.3

[karen-bot 在 #lowes-handover 发通知（TEXT，origin chat）：]
[Lowe's HQ Notice · REF-2026-0603-11]
Subject：Quality flag — Order #A8810，Atlanta
SOP：§7.3 Re-inspection within 5 business days
截止：2026-06-10（周二）
PDF：已存档到台账 Note

[ACTION:NOTE 追加台账（在 #lowes-handover，origin chat，无跨 chat）：]
→ 台账 Note 追加：2026-06-03 | REF-2026-0603-11 | A8810 | Atlanta | 复检 | 截止 06/10

⚠️ 注意：#A8810 今日同时有客户投诉（来自 orders-bot 升级）。
双路升级，建议通知 Beth。

  ✅ 台账已更新（#lowes-handover Note）
```

**Karen 手动路由：**
```
Karen Yates：
  @karen-bot 把 A8810 质量标记通知发给 Tom，同时通知 Beth 是双路升级

karen-bot：
  [ACTION:MESSAGE chatid=#atlanta-ops]
  → 跨 chat audit notice → Karen 确认 → 发出：
  "[Lowe's HQ Notice] #A8810 Atlanta 需要复检，截止 06/10（5 个工作日）。
   SOP §7.3。请安排。Karen。"

  [ACTION:MESSAGE chatid=Beth-DM]
  → 跨 chat audit notice → Karen 确认 → 发出：
  "#A8810 双路升级：Lowe's 质量标记（复检截止 06/10）+ 客户当日投诉。
   Atlanta Tom 已收到通知。Karen。"
```

---

## 多 Bot 协作总结图

```
Customer SMS / Fax（外部触发）
        ↓
   orders-bot / karen-bot（inbound 检测，Group B）
        ↓ 发文本到共享 chat
   人看到升级文本
        ↓ 人手动 @自己的 personal bot
   tom-bot / beth-bot（查询分析）
        ↓ personal bot 起草跨 chat 消息
   Karen/Tom/Beth 确认 audit notice
        ↓
   消息发到目标 chat
        ↓ 目标 chat 里的人看到
   人接收行动

关键路径：
· 共享 Bot（orders-bot / hr-bot）处理团队事务
· Personal Bot（tom/karen/beth）处理个人决策和跨 chat 路由
· 多 Bot"通信"必须经过人的手动确认（audit notice 是那个节点）
· Linda（hr-bot owner）是 OOB 流程中负担最重的角色，需要最顺畅的主机访问
```
