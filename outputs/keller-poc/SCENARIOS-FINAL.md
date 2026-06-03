# AgentRun · 具体场景设计（含生产缺口标注）

假设条件：
- Frozen Snapshot 已实现（prefix cache 稳定）
- SKILL.md 系统已实现（技能独立，按需加载）
- Agent 学习循环已实现（可自主创建技能）
- PHONE_CALL FIJI bridge 已实现 ✅

---

## 场景 1 · Bot 注册与 SOUL 配置

### 1.1 用户视角：Sarah 第一次创建自己的 Bot

**步骤 1：FIJI 里注册**

```
Sarah 打开 FIJI → 找到「Personal AVA Pro」入口

┌─────────────────────────────────────────┐
│  Personal AVA Pro                        │
│  A personal AI work-agent...            │
│                                          │
│  [Register my AVA Bot]                  │
└─────────────────────────────────────────┘

Sarah 点击 → 展开表单：

  Bot name: [Sarah's AVA CSR      ]
  
  Step 1. Register in Developer Console
  [Auto generate app values]  ← 点击自动生成凭证
  
  RC Bot token:    [●●●●●●●●●●●●●●●●]（自动填入）
  ClientID:        [●●●●●●●●●●●●●●●●]（自动填入）
  ClientSecret:    [●●●●●●●●●●●●●●●●]（自动填入）
  JWTToken:        [●●●●●●●●●●●●●●●●]（自动填入）
  Message space:   [<atlanta-orders-chat-id>]
  
  [Complete registration]
  
30 秒后 → Bot 状态变为 Ready ✅
```

**步骤 2：配置 SOUL（当前的生产缺口）**

```
❌ 当前没有 SOUL 编辑 UI

可用方案 A（in-chat，今天就能做）：

Sarah 打开 RC Team Messaging，找到 sarah-bot DM：
  Sarah: /soul setup csr
  sarah-bot: 已为你加载 CSR SOUL 模板，你现在是 Atlanta 门店派单助手。
            输入 /soul edit 可以修改，/persona 查看当前配置。

  Sarah: /soul edit add-owner-pref "Jenkins 客户总是选 Engineered Oak，周二上午优先"
  sarah-bot: ✅ 已写入 OWNER.md
            下次 session 启动时生效。

可用方案 B（FIJI UI，需要开发）：

┌─────────────────────────────────────────┐
│  Your AVA Bot  · Sarah's AVA  ● Ready   │
│                                          │
│  [Runtime controls] [Soul & Memory]     │  ← 新增 tab
└─────────────────────────────────────────┘

点击 Soul & Memory：
┌─────────────────────────────────────────────────────────────┐
│ Soul Editor                                                  │
│ ┌──────────────────────────────────────────────────────┐   │
│ │ # Sarah's CSR Agent                                  │   │
│ │ 我是 Sarah Cooper 的 Agent...                        │   │
│ │ skills: [dispatch-confirm, complaint-handling]       │   │
│ └──────────────────────────────────────────────────────┘   │
│ [Save Soul]                                                  │
│                                                              │
│ Domain Memory                    Owner Memory               │
│ ┌──────────────────┐            ┌──────────────────┐        │
│ │ § Atlanta ZIP:   │            │ § Jenkins: EO,   │        │
│ │   30301-30350    │            │   周二上午优先   │        │
│ │ § Mike: 0211     │            │ § Sarah 回复 ≤4行│        │
│ │ [+ Add entry]    │            │ [+ Add entry]    │        │
│ └──────────────────┘            └──────────────────┘        │
│                                                              │
│ Skills                                                       │
│ ● dispatch-confirm    v1.2  [View] [Update]                 │
│ ● complaint-handling  v1.0  [View] [Update]                 │
│ ○ daily-digest        v1.0  [Enable]                        │
└─────────────────────────────────────────────────────────────┘
```

**系统状态：**
```
Pod 启动时（CaptureSnapshot 一次性调用）：
  snapshot.soul   = SOUL.md 内容（冻结）
  snapshot.domain = DOMAIN.md 内容（冻结）
  snapshot.owner  = OWNER.md 内容（冻结）
  skills_index    = dispatch-confirm · complaint-handling（冻结）

每条消息：
  stable slots（SOUL+DOMAIN+OWNER+skills）= 不变 → prefix cache 命中
  chat memory = 实时读取（今日 dispatch 列表）
```

---

## 场景 2 · 智能派单（SKILL.md 激活）

### 2.1 标准派单流程

**Sarah 在 #atlanta-orders：**

```
Sarah Cooper
  @sarah-bot dispatch A8821 to Mike, tomorrow 10am,
  1234 Main St Atlanta GA 30309, Engineered Oak 850sqft,
  customer Jenkins +1 404-555-0199
```

**System 内部（Hermes 模式）：**

```
PersonaLoader.Build()：
  stable = [SOUL(80行) + skills_index + DOMAIN.md + OWNER.md]（冻结，prefix cache ✅）
  chat   = "今日 open dispatches: A8819|Carlos|..."（实时读取）
  entity = ""（无 entity_id）

Intent 检测：dispatch → 激活 dispatch-confirm skill
注入：
  <context type="skill" name="dispatch-confirm" state="initial">
    [SKILL.md 完整内容，7 步骤，ZIP 校验规则，SMS 模板]
  </context>

OWNER.md 里有："Jenkins 客户总是选 Engineered Oak，周二上午优先"
→ agent 在校验时读到，在 Task 描述里加注："Jenkins 常客，Engineered Oak 已确认"
```

**Sarah 看到：**

```
sarah-bot

  核对：
  · A8821 · Mike Reyes · 06/04 10:00
  · 1234 Main St Atlanta GA 30309 ✅ ZIP 30309
  · Engineered Oak 850sqft · Jenkins +14045550199
  （备注：Jenkins 常客，材料偏好已确认）

  ✅ Task #T992（Mike Reyes · due 06/04 10:00）
  ✅ SMS +14045550211 · delivered

  Install #A8821 06/04 10am.
  Address: 1234 Main St Atlanta GA 30309
  Material: Engineered Oak 850sqft
  Customer: Jenkins +14045550199
  Reply CONFIRM to acknowledge.
```

### 2.2 ZIP 异常触发学习循环

```
Alex Kim
  @sarah-bot dispatch A8831 to Carlos, Thursday 2pm,
  567 Northside Dr NW Atlanta GA 30318, Hardwood 400sqft,
  customer Park +1 404-555-0333

sarah-bot
  ⚠️ ZIP 校验需要确认：
  
  30318 → 历史上有两个区域映射：
  · Westside Atlanta（常规）
  · Blandtown 工业区（GPS 经常误导）
  
  567 Northside Dr NW 请确认是哪个区域？
  Carlos 去过这个地址吗？

Alex Kim
  是 Westside，Carlos 上周刚去过这栋楼

sarah-bot
  ✅ 确认为 Westside，派单执行...
  [创建 Task + 发 SMS]

  📝 正在记录这个地址规则...
```

**Bot 后台自动创建技能：**

```
~/.ringclaw/skills/atlanta-30318-address-guide/SKILL.md

---
name: atlanta-30318-address-guide
description: 30318 ZIP 区域的地址识别规则（Westside vs Blandtown）
version: 1.0.0
author: sarah-bot
created_at: 2026-06-10
metadata:
  tags: [atlanta, zip, address, local-knowledge]
  created_from: dispatch A8831 · address ambiguity resolved by Alex
---

# Atlanta 30318 ZIP 地址指南

30318 ZIP 覆盖两个区域，GPS 经常混淆：

## Westside Atlanta（住宅/商业）
- Northside Dr NW · Joseph E Lowery Blvd · Donald Lee Hollowell Pkwy
- 识别特征：大楼、住宅小区
- Carlos 队熟悉，无需额外说明

## Blandtown 工业区
- 靠近 I-285 立交桥附近的地址
- 识别特征：仓库、工厂
- 需要提前确认入口（工业区大门通常有门卫）

**遇到 30318 时：** 问 "Westside 住宅区还是 Northside 工业侧？"
```

**第二天，Sarah 遇到同样问题：**

```
Sarah Cooper
  @sarah-bot dispatch A8832 to Mike, Friday 3pm,
  421 Northside Dr NW Atlanta GA 30318, Tile 300sqft,
  customer Williams +1 404-555-0444

sarah-bot
  [skills index 检测到 30318 → 激活 atlanta-30318-address-guide]

  确认一下：30318 的 421 Northside Dr NW，
  是 Westside 住宅区吗（还是 Northside 工业侧）？

Sarah Cooper
  Westside

sarah-bot
  ✅ 确认，派单执行...（无需 Carlos 解释，bot 已知道规则）
```

**体现的价值：** 第一次遇到需要人解释，第二次 bot 自己问对问题，第三次 bot 已能自动处理。

---

## 场景 3 · 店长每日摘要（Frozen Snapshot 效果最明显）

**17:30 Heartbeat 触发 tom-bot：**

```
System：
  [Heartbeat 触发，不是用户消息]
  system prompt = snapshot（冻结，和今天所有消息一样的 prefix）
  + chat memory（今日 #atlanta-orders 状态，实时读）
  → prefix cache 命中率高（因为 Heartbeat 用同一个 stable prefix）

tom-bot → #atlanta-ops（TEXT ONLY）

  [Atlanta Daily · 2026-06-03 17:30]
  今日完成：8 单，2 延迟
    · A8819 — 材料未到（Carlos 队，已改排周四）
    · A8820 — 客户临时改期（Sarah 已处理）
  明日：11 预约，6 确认，5 待确认
  班组缺口：Mike 队周三 -2（Engineered Oak）
  最久 Task：T941（3 天，Tom Rivera）
  Lowe's 待传：2 份（Karen EOD 批次）
  
  [本周新技能：atlanta-30318-address-guide 已由 sarah-bot 创建]
```

**Tom 读完，行动：**

```
Tom Rivera
  @tom-bot T941 更新为进行中，备注"等供应商确认 LuxCore 样品"

tom-bot
  [ACTION:TASK update T941 InProgress note="等供应商确认 LuxCore 样品 · Tom 06/03"]
  ✅ T941 → 进行中

Tom Rivera
  @tom-bot 发给区域协调员：Mike 队周三缺 2 人，Engineered Oak，
  问 Dallas 有没有

tom-bot
  起草：
  "[CREW_GAP_REQUEST] Atlanta 06/05 -2，Engineered Oak 认证。
   Dallas 有空余人手吗？出行 Keller 承担。—— Tom"
  
  发到 #southeast-coord？

Tom Rivera
  确认

tom-bot
  [ACTION:MESSAGE chatid=#southeast-coord]
  → audit notice → Tom DM 5秒确认 ✅
```

---

## 场景 4 · 投诉处理 Multi-Agent 协作

**（Group B，需要 inbound SMS wire，约 150 行代码）**

### 4.1 触发与自动响应

```
[10:02] 客户手机 → 门店 SMS 号：
  "Crew didn't show up for #A8810. Worst service ever!!!"

[MessageStoreHandler 触发]
System：
  complaint-handling skill 激活（检测到投诉信号）
  <context type="skill" name="complaint-handling" state="initial">
  创建 entity：complaint-A8810-20260603
  </context>

[10:03] sarah-bot → 客户手机（ACTION:SMS，≤60 秒）：
  "Hi! I'm so sorry about this — I'm escalating to our manager
   right now. You'll get a call back within 15 minutes.
   We take this seriously. — Keller Atlanta"
  ✅ delivered

[10:03] sarah-bot → #atlanta-orders：
  ⚠️ 投诉升级 · A8810 · Jenkins

  原文："Crew didn't show up...Worst service ever!!!"
  情绪：强烈不满（no-show + !!!）
  安抚 SMS：已发 ✅（15min callback 承诺）

  [ACTION:TASK "URGENT: A8810投诉" assignee=Tom due=+2h color=Red]
  Task #T993 ✅

  写入 entity memory：
  complaint-A8810-20260603.md · status=detected

  [ACTION:MESSAGE → #atlanta-ops，附路由标签]
```

### 4.2 Agent-to-Agent：tom-bot 自动调查

```
[10:03] #atlanta-ops 收到来自 sarah-bot 的消息：
  ⚠️ 投诉升级 · A8810 · Jenkins
  ...（路由标签 embedded）

[tom-bot monitor 检测到 sarah-bot ext ID（在 source_user_ids 中）]

System（tom-bot）：
  加载 entity memory：complaint-A8810-20260603.md
  注入：
    <context type="entity" id="complaint-A8810-20260603">
    [sarah-bot 写入的完整投诉信息]
    </context>
  
  complaint-investigation skill 激活（因路由事件类型）

[10:05] tom-bot 在 #atlanta-ops（无需 Tom 手动触发）：

  [ACTION:PHONE_CALLLOG scope=today]
  
  📋 A8810 调查结论

  派工：Mike Reyes · 06/03 10:00 · 1234 Main St 30309
  SMS 派发：08:52 ✅  CONFIRM：❌ 未回复

  Mike 今日外呼：
  · 08:30 → 供应商（非客户号）
  · 09:15 → 备用号
  无 Jenkins +14045550199 的拨出记录

  结论：Mike 收到 SMS 但未确认，未联系客户
  建议 Tom：直接致电 Mike 确认情况

  更新 entity memory：status=investigating，调查结论已写入
```

### 4.3 Tom 决策，sarah-bot 执行

```
[10:17] Tom Rivera（看到 #atlanta-ops 调查结论，致电 Mike 后）：
  @sarah-bot 给 Jenkins 发短信：队长 20 分钟内到，
  送 $50 服务抵用券，真诚道歉

sarah-bot（加载 entity memory，知道完整上下文）：
  [ACTION:SMS to=+14045550199]
  "Hi Mr. Jenkins! This is Keller Atlanta — we are truly sorry.
   Mike's crew just confirmed your address and will arrive in 20 minutes.
   We're adding a $50 service credit to your account.
   Thank you for your patience."
  ✅ SMS delivered（10:19）

  [ACTION:TASK update T993 note="道歉 SMS 已发 10:19，$50 credit 承诺"]
  
  更新 entity memory：status=resolved
  写入 complaint ledger（per-chat memory）：
  2026-06-03|Jenkins|A8810|no-show|$50 credit|SLA hit 17min

━━━ 时间轴（同一线程）━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  10:02  投诉 SMS 到达
  10:03  安抚 SMS 发出（51秒）✅
  10:03  sarah-bot 路由 tom-bot
  10:05  tom-bot 调查完成（无需 Tom 介入）
  10:17  Tom 致电 Mike 确认
  10:19  道歉 + $50 credit SMS 发出
  总耗时：17 分钟（vs 之前 30-45 分钟）
```

---

## 场景 5 · Beth 外呼（PHONE_CALL，今天已实现）

```
Beth DM with beth-bot：

[Cron 09:00，TEXT ONLY]
  [Weekly Snapshot · W23]
  CSAT：4.4/5（↓0.3 ⚠ Atlanta 48h no-show ×3）
  Lowe's SLA：96% ✅
  ⚠ 关注：Atlanta CSAT，建议询问 Tom

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  看看今天未接来电，需要的发短信

beth-bot
  [ACTION:PHONE_CALLLOG scope=today missing=true next_actions=true]

  📞 今日未接来电（3条）
  · 10:23  +14045550xxx → SMS ✅
  · 11:45  +19195550188（Lowe's HQ Compliance）→ SMS ✅ ⚠️
  · 14:12  Mark Jenkins → SMS ✅

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  Lowe's 这个打过去确认一下

beth-bot
  [ACTION:PHONE_CALL to=+19195550188]

━━━ Control Plane → FIJI ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  action_event: {
    type: "PHONE_CALL",
    status: "client_action_required",
    details: {client_action: "make_call", to_number: "+19195550188"}
  }

  FIJI AvaBotOnboarding 轮询 ≤5 秒
  → executeAvaClientAction()
  → callActionHelper.directCall({toNumber: "+19195550188"})
  → FIJI 以 Beth 当前登录身份拨出 ✅

beth-bot → Beth DM
  📞 已拨出 Lowe's HQ Compliance +19195550188

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  确认延期到 06/12，帮我发消息告诉 Karen

beth-bot
  [ACTION:MESSAGE chatid=Karen-DM → audit notice → Beth 确认 → 发出]
  
  ✅ 已发 Karen DM："A8810 Lowe's 复检截止已确认延至 06/12。—Beth"
```

---

## 场景 6 · HR 服务 Bot（多人隔离，同一 Bot）

```
[同时进行，三个独立线程]

━━━ 线程 1：Marcus DM hr-bot ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Marcus Johnson（10:05）
  请假申请 6/10-6/12，家庭原因

hr-bot（entity: pto-marcus-0610 创建）：
  收到，Marcus。6/10-6/12（3天），余额 4天→1天。
  通知 Mike 审批，理由保密。

[Linda approve → Mike DM 通知 → Mike approve → 日历更新]

hr-bot → Marcus
  ✅ 批准。日历已更新。

#atlanta-ops 匿名广播（Linda approve）：
hr-bot
  班组缺口：Mike 队 6/10-6/12 -1 名协助。（来源：HR 保密。）

━━━ 线程 2：Carlos DM hr-bot（同时）━━━━━━━━━━━━━━━━━━━━━━

Carlos Ruiz（10:07）
  下次 LuxCore 培训什么时候？

hr-bot（读 global memory）：
  LuxCore 安装培训：
  · 06/15（周一）09:00，Atlanta 门店培训室
  · 06/22（周一）同地点（备用场次）
  
  需要报名吗？

━━━ 线程 3：Amy DM hr-bot（同时）━━━━━━━━━━━━━━━━━━━━━━━━

Amy Chen（10:09）
  我想了解绩效评分标准

hr-bot
  Amy，绩效评分属于个人 HR 信息，需要直接联系 Linda Wu。
  有其他我可以帮到你的吗？

━━━ 隔离验证 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Tom（#atlanta-ops）看到：班组缺口日期，不知道是谁
  Mike（审批请求）看到：日期 + 班组影响，不知道原因
  Carlos：只知道自己的培训查询
  Amy：被拒绝，知道找 Linda
  Linda：知道全部（owner，OOB approve）
```

---

## 生产缺口：分级影响

### 🔴 阻断生产的（必须解决）

**1. Inbound SMS/Fax wire（~150 行）**

```
缺了什么：场景 4（投诉处理）完全无法运行
位置：cmd/start_init.go → monitor.SetMessageStoreHandler(fn) 未调用
影响：Case 7、Case B 无法 demo

修复：
  func buildMessageStoreHandler(cfg, handler) ringcentral.MessageStoreHandler {
    return func(ctx, client, evt) {
      for _, change := range evt.Changes {
        switch change.Type {
        case "SMS":  handler.HandleInboundSMS(ctx, client, evt.ExtensionID)
        case "Fax":  handler.HandleInboundFax(ctx, client, evt.ExtensionID)
        }
      }
    }
  }
```

**2. OOB 反向通道（~200 行）**

```
缺了什么：跨 chat 审批需要 terminal，业务用户无法使用
影响：Karen、Beth 的跨 chat 场景在 K8S 部署下需要 kubectl exec

修复方向：
  Control Plane 增加：
    POST /control/v1/bots/{id}/challenges/{id}/approve （FIJI 调用）
    GET  /runtime/v1/pending-approvals （Pod 轮询）
  ringclaw runtime.go 增加 pollPendingApprovals() goroutine
```

### 🟡 影响规模化（POC 后解决）

**3. SOUL 编辑 UI**

```
现有：/persona（只读）
缺少：业务用户自助编辑 SOUL

最快解法（in-chat，~50 行）：
  /soul edit <field> <value>  → 修改 SOUL 指定字段
  /soul show                  → 查看当前 SOUL

完整解法（FIJI UI，需要）：
  FIJI AvaBotOnboarding 新增 "Soul & Memory" tab
  Control Plane 新增 GET/PUT /control/v1/bots/{id}/soul
```

**4. Memory 可视化**

```
现有：/mem add · /mem show · /mem del（in-chat）
缺少：UI 里查看/管理 DOMAIN.md / OWNER.md / entity memory

最快解法：扩展现有 /mem 命令支持 domain/owner scope
完整解法：FIJI Memory Center tab
```

**5. Hermes 三项能力（token 成本 + 学习循环）**

```
优先级：Frozen Snapshot（P0，1天）> Skills（P2，3天）> Learning Loop（P3，3天）
这三项不影响功能正确性，影响成本和长期价值
```

### ✅ 不阻断的

```
· Bot 注册（FIJI UI 已有）
· Bot 生命周期（FIJI UI 已有）
· 所有 ACTION 执行（Task/SMS/Fax/Phone 全部正常）
· PHONE_CALL → FIJI directCall（已实现）
· Agent-to-Agent 信任（加几行 config，今天可做）
· Cron/Heartbeat（已实现）
· Admin 治理（Control Plane 已实现）
```

---

## 最小可 demo 路径（8 周 POC）

```
Week 1：Agent-to-Agent 信任 config（今天）+ Case 5/6 demo
  · orders-bot source_user_ids 加入 karen-bot ext ID
  · beth-bot source_user_ids 加入 karen-bot/tom-bot ext ID
  · Case 6（Beth 外呼）· Case A（上线协调）立刻可跑

Week 2：Inbound SMS wire + Case 4B（投诉处理局部）
  · MessageStoreHandler wire（~150 行）
  · Case 7 核心流程：检测 → 安抚 → 路由

Week 3：OOB 反向通道（~200 行）
  · Karen/Beth 的跨 chat 不再需要 terminal
  · Case C（Lowe's 双路升级）完整可 demo

Week 4：Frozen Snapshot（~1天）
  · Token 成本立降 60%
  · prefix cache 开始起作用

Week 5-8：Skills + Learning Loop + SOUL in-chat edit
  · Bot 开始从 Keller 真实操作中学习
  · Sarah/Tom 可以在 RC chat 里自助配置 bot 行为
```