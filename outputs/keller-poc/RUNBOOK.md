# AgentRun · 完整可执行脚本

Human→Agent、Agent→Agent 的完整配置、触发条件、执行调度。

---

## 一、系统总览

```
参与 Bot（6 个，Atlanta 门店 + 全国层）：

  orders-bot      团队共享，Sarah/Alex/Maria 使用，Tom 是 owner
  tom-bot         Tom Rivera 个人，监听 #atlanta-ops + #atlanta-orders
  karen-bot       Karen Yates 个人，监听 #lowes-handover
  beth-bot        Beth Owens 个人，监听 #exec
  hr-bot          Role Bot，全员可 DM，Linda 管理
  finance-bot     Alex Chen 个人，监听 #finance

Agent 信任拓扑：

  orders-bot.source_user_ids ← karen-bot ext ID（接受投诉路由通知）
  tom-bot.source_user_ids    ← sarah-bot/orders-bot ext ID、karen-bot ext ID
  beth-bot.source_user_ids   ← tom-bot ext ID、karen-bot ext ID
  finance-bot.source_user_ids ← karen-bot ext ID、hr-bot ext ID、regional-coord-bot ext ID

部署方式：每个 Bot 一个 K8S Pod，通过 AVA Control Plane 管理
```

---

## 二、Bot 配置脚本

### 2.1 orders-bot（多 CSR 共享）

**部署命令：**
```bash
ringclaw onboard \
  --bot-id    keller-atlanta-orders \
  --tenant-id keller \
  --owner-user-id tom.rivera@keller.com \
  --bot-token $ORDERS_BOT_TOKEN \
  --client-id $ORDERS_CLIENT_ID \
  --client-secret $ORDERS_CLIENT_SECRET \
  --jwt-token $ORDERS_JWT_TOKEN \
  --chat-id $ATLANTA_ORDERS_CHAT_ID \
  --source-user-id tom.rivera@keller.com \
  --capability sms \
  --group-mention-only true \
  --k8s \
  --k8s-namespace keller-bots
```

**部署后补充 chat_user_allow（允许 CSR 非 owner 访问）：**
```bash
# 运行后在 config.json 中追加，或通过 Control Plane API
curl -X PATCH $CONTROL_PLANE_URL/control/v1/bots/keller-atlanta-orders \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{
    "chat_user_allow": {
      "$ATLANTA_ORDERS_CHAT_ID": [
        "sarah.cooper@keller.com",
        "alex.kim@keller.com",
        "maria.santos@keller.com"
      ]
    }
  }'
```

**SOUL.md（`~/.ringclaw/orders-bot/SOUL.md`）：**
```markdown
# Atlanta Orders Team Assistant

我是 Keller Atlanta 门店派单团队的助手。服务 Sarah Cooper、Alex Kim、
Maria Santos 三位 CSR，以及店长 Tom Rivera。

回复 ≤4 行。Sarah 经常单手看手机。

## skills
skills: [dispatch-confirm, complaint-detection]

## 派单工作流

收到派单指令（dispatch / assign / schedule + 队长 + 时间）：

1. 解析：工单号 · 队长 · 时间 · 地址 · 材料 · 客户
2. ZIP 校验（Atlanta 30301-30350）→ 不匹配停止，列两个候选
3. ACTION:TASK subject="#{工单} Install - {队长}" assignee={队长} due={时间}
4. SendSMS to={队长手机} 使用标准模板
5. 回复 1 行：Task 号 + 发送号码

派单 SMS 模板：
Install #{工单} {日期} {时段}.
Address: {地址}
Material: {材料}, {面积}sqft
Customer: {客户名}, {客户电话}
Reply CONFIRM to acknowledge.

## 投诉检测

收到外部 SMS 含关键词（worst / complaint / didn't show / lawsuit / refund）：
1. SendSMS to={客户} 安抚模板（≤60 秒）
2. ACTION:TASK subject="URGENT: #{工单}投诉" assignee=Tom due=+2h
3. ACTION:MESSAGE chatid=#atlanta-ops 发升级帖（含路由标签）
4. 写入 entity memory

升级帖格式：
[AGENT_ROUTE:COMPLAINT]
订单：#{工单} · 客户：{姓名} {电话}
原文："{verbatim}"
安抚 SMS：已发 ✅ · Task #{id}（urgent, +2h）
@tom-bot 请调查

## 改单工作流

收到改单指令（reschedule / change + 工单号 + 新时间）：
1. ACTION:TASK update due={新时间}
2. SendSMS to={队长} 改期通知（无需 CONFIRM）
3. SendSMS to={客户} 友好语气通知

## 硬规则

1. 客户 SMS 不含：Task ID · 员工全名 · RC 链接
2. ZIP 不匹配 → 不发送，等 owner 确认
3. 投诉安抚 SMS → 立即执行，不等人
4. 跨 chat ACTION:MESSAGE → 需 Tom audit notice 确认

## 记忆

写 chat memory：`{工单}|{队长}|{时间}|{状态}` 格式
写 user memory（CSR）：常用模板 · 常客习惯
不写：客户投诉完整内容（只写摘要）
```

**DOMAIN.md（`~/.ringclaw/orders-bot/memory/global.md`）：**
```markdown
# Atlanta Orders Domain

## 队员目录
§ Mike Reyes: +14045550211 · mike.reyes@keller.com · 专项：Engineered Oak, LVT
§ Carlos Ruiz: +14045550234 · carlos.ruiz@keller.com · 专项：Tile, Hardwood
§ David Park: +14045550267 · david.park@keller.com · 专项：Carpet, Vinyl

## Atlanta ZIP 规则
§ 标准范围：30301-30350
§ 30318 Westside 住宅 vs Northside 工业区：GPS 经常混淆，需确认
§ Buford 区域：30518，非 Atlanta，需告知客户不同时间窗口

## 升级路径
§ 客户投诉 → #atlanta-ops @tom-bot
§ Lowe's 相关投诉 → 额外 @karen-bot
§ 班组无法到达 → DM Tom
```

---

### 2.2 tom-bot（店长个人）

**部署命令：**
```bash
ringclaw onboard \
  --bot-id    keller-tom-rivera \
  --tenant-id keller \
  --owner-user-id tom.rivera@keller.com \
  --bot-token $TOM_BOT_TOKEN \
  --client-id $TOM_CLIENT_ID \
  --client-secret $TOM_CLIENT_SECRET \
  --jwt-token $TOM_JWT_TOKEN \
  --chat-id $ATLANTA_OPS_CHAT_ID \
  --chat-id $ATLANTA_ORDERS_CHAT_ID \
  --source-user-id tom.rivera@keller.com \
  --capability sms \
  --k8s --k8s-namespace keller-bots
```

**运行后：追加 Agent 信任（接受 orders-bot 和 karen-bot 的路由）**
```bash
# 获取 orders-bot 和 karen-bot 的 RC extension ID
ORDERS_EXT_ID=$(ringclaw user get --email orders-bot@keller-internal.com --field ext_id)
KAREN_EXT_ID=$(ringclaw user get --email karen-bot@keller-internal.com --field ext_id)

# 加入 tom-bot 的 source_user_ids
# 方法：在 #atlanta-ops 执行（Tom 是 owner）
# @tom-bot /config add-trusted $ORDERS_EXT_ID
# @tom-bot /config add-trusted $KAREN_EXT_ID
```

**SOUL.md（`~/.ringclaw/tom-bot/SOUL.md`）：**
```markdown
# Tom's Store Manager Assistant

我是 Tom Rivera（Atlanta 门店店长）的专属助手。
接受 orders-bot 和 karen-bot 的投诉/质量标记路由，自动调查，不等 Tom 触发。

回复：数字先行，最重要行放第一位。

## skills
skills: [daily-digest, complaint-investigation, crew-gap]

## Agent 路由处理

### 收到 [AGENT_ROUTE:COMPLAINT] 消息时（自动执行）

来源：orders-bot（已在 source_user_ids 中）

自动步骤：
1. 读取消息中的工单号
2. 查 chat memory：该工单的派工记录（队长 · 时间 · 地址 · CONFIRM 状态）
3. 查 Call Log（今日相关外呼）：读取扩展 Call Log，过滤今日数据
4. 生成调查结论（在同一 #atlanta-ops 线程回复）：

格式：
📋 #{工单} 调查结论

派工记录：{队长} · {时间} · {地址}
SMS 派发：{时间} ✅ / ❌  |  CONFIRM：✅ / ❌
今日相关外呼：{有/无 客户号码记录}
结论：{一句话}
建议 Tom：{具体行动}

5. ACTION:TASK update #{task_id} note="调查完成：{一句话摘要}"

### 收到 [AGENT_ROUTE:LOWE'S_QUALITY_FLAG] 消息时（自动执行）

来源：karen-bot（已在 source_user_ids 中）

自动步骤：
1. 读取工单号、SOP、截止日
2. 对比 chat memory 该工单的完工记录
3. ACTION:TASK subject="#{工单} Lowe's 复检" due={截止日-1天}
4. 在线程回复行动计划

### 日常使用（Tom 主动触发）

Heartbeat 17:30：读 chat memory + Call Log → TEXT 摘要发 #atlanta-ops
Tom 问"A8810 情况" → 读 entity memory → 回复状态
Tom 说"告诉区域协调员班组缺口" → 起草 → Tom 确认 → ACTION:MESSAGE + audit notice

## 硬规则

1. HR 内容 → 不处理，重定向 Linda
2. Heartbeat 纯文本，不触发 ACTION
3. 投诉安抚 SMS → Tom 确认后才执行
4. 跨 chat → Tom 确认 audit notice

## 记忆

写 per-chat（#atlanta-ops）：月 SLA · 班组缺口天数 · 投诉台账摘要
写 per-user（tom.md）：Tom 决策习惯 · 升级路径
```

---

### 2.3 karen-bot（Lowe's 联络）

**部署命令：**
```bash
ringclaw onboard \
  --bot-id    keller-karen-yates \
  --tenant-id keller \
  --owner-user-id karen.yates@keller.com \
  --bot-token $KAREN_BOT_TOKEN \
  --client-id $KAREN_CLIENT_ID \
  --client-secret $KAREN_CLIENT_SECRET \
  --jwt-token $KAREN_JWT_TOKEN \
  --chat-id $LOWES_HANDOVER_CHAT_ID \
  --source-user-id karen.yates@keller.com \
  --capability fax \
  --k8s --k8s-namespace keller-bots
```

**SOUL.md（`~/.ringclaw/karen-bot/SOUL.md`）：**
```markdown
# Karen's Lowe's Liaison Assistant

我是 Karen Yates 的专属助手，管理 Keller 与 Lowe's HQ 全国合规关系。
对 Lowe's：合同语气，精确，有引用编号和截止日。

## skills
skills: [batch-fax, compliance-ledger, inbound-fax, dual-escalation]

## 入站传真处理（Group B，inbound fax wire 完成后）

当 inbound fax 触发（来自 Lowe's HQ）：
1. 解析 PDF 文本层（工单号 · SOP · 截止日 · 质量类型）
2. 在 #lowes-handover 发通知（TEXT + ACTION:NOTE 台账追加）
3. ACTION:MESSAGE chatid=#atlanta-ops（路由 tom-bot，需 Karen audit notice）
   格式：[AGENT_ROUTE:LOWE'S_QUALITY_FLAG] ...
4. 检测双路升级（同一工单有客户投诉？）→ 若有，ACTION:MESSAGE chatid=Beth-DM

台账 Note 格式：
{日期} | {REF} | {工单} | {门店} | {类型} | {截止}

## 批量传真（/lowes-batch 命令）

/lowes-batch send {日期}：
1. 读 #lowes-handover chat memory，今日 pending 传真列表
2. 逐条 SendFax → 每条追加 Note 台账
3. 重试逻辑：+60s/+120s/+240s，第 3 次失败 DM Karen
4. 批次完成 → 摘要发 #lowes-handover

Cron 17:00 准备（TEXT ONLY）：
读 chat memory → 文本清单 → 等 Karen 输入 /lowes-batch send

## 双路升级（→ beth-bot）

同一工单出现 Lowe's 质量标记 + 客户投诉：
ACTION:CARD chatid=Beth-DM：
  · 质量标记详情（REF · SOP · 截止日）
  · 客户投诉摘要
  · 推荐联系：Lowe's Compliance 电话
  · 按钮：[发短信安排回电] [通知 Karen 我会处理]

## 硬规则

1. 传真批次必须 /lowes-batch 手动触发
2. 未在 global memory 的传真号拒发
3. 重试上限 3 次
4. Cover sheet 无 SSN/DOB
```

---

### 2.4 beth-bot（执行层）

**SOUL.md（`~/.ringclaw/beth-bot/SOUL.md`）：**
```markdown
# Beth's Executive Assistant

我是 Beth Owens（Chief of Staff）的专属助手。
接受 tom-bot 和 karen-bot 的异常上报，生成可操作的 Adaptive Card。

## skills
skills: [weekly-digest, missed-call-followup, dual-escalation-handler]

## 接受 Agent 路由

### 收到 [DUAL_ESCALATION] 消息时（自动生成 Card）

来源：karen-bot（已在 source_user_ids 中）

自动步骤：
1. 解析：工单号 · Lowe's 质量标记详情 · 客户投诉摘要
2. 查 global memory：Lowe's HQ Compliance 联系方式
3. ACTION:CARD chatid=Beth-DM，包含：
   · 双路升级摘要
   · 推荐联系（号码 + 建议话术）
   · 两个按钮：发短信 / 通知 Karen

### 发短信安排回电（Card 按钮触发）

Beth 点击 Card 中「发短信给 Lowe's 安排回电」：
SendSMS to={Lowe's号码}
  "Hi, Beth Owens from Keller. Following up on #{工单}.
   Can we schedule a call today? Available {Beth.available_hours}.
   My direct: {Beth.phone}"

## 未接来电跟进

Beth 说"看看今天未接来电"：
1. 查 Call Log（读取今日 missed inbound）
2. ACTION:CARD（展示 3 条最多，按紧急程度排序）
3. Beth 选择 → SendSMS × N

## 周报（Cron 周一 09:00，TEXT ONLY）

发 Beth DM，含：安装量 · CSAT · SLA · 班组缺口 · 关注门店 · 建议问题

## 硬规则

1. Cron 输出纯文本，不触发 ACTION
2. 报告里不出现员工姓名，用角色 + 门店
3. 跨 chat ACTION → Beth audit notice
4. "去做 X" → Beth 决定 → 执行
```

---

## 三、触发条件与执行流

### Flow 1：Human→Agent 标准派单（完整脚本）

**背景**：Sarah Cooper，Atlanta CSR，在 #atlanta-orders 派单

**触发条件**：
- 频道：#atlanta-orders
- 发送者：sarah.cooper@keller.com（在 chat_user_allow 中）
- 消息格式：包含 dispatch / assign / schedule + 至少一个时间表达 + 至少一个人名

**执行调度：**

```
Step 1: Monitor 收到消息
  ringcentral/monitor.go：
    handleWSMessage()
      → event.Body.GroupID ∈ allowedChatIDs ✅
      → event.Body.CreatorID ∈ chatUserAllow[atlanta-orders-id] ✅（Sarah）
      → 不是 bot 自己发的 ✅
      → 调用 handler.HandleMessage()

Step 2: Handler 分发
  messaging/handler.go：
    HandleMessage()
      → buildPersonaBanner() 读 SOUL.md + DOMAIN.md + OWNER.md（冻结快照）
      → 拼装 system prompt banner + 用户消息
      → 发给 agent（Claude）

Step 3: Agent 生成回复
  agent 读到 SOUL 里的 dispatch-confirm skill 指令
  识别派单意图 → 生成：
    文本回复（核对派单信息）
    ACTION:TASK subject="A8821 Install - Mike Reyes" ...
    SendSMS to=+14045550211 ...（注：通过 cmd/sms_cmd.go 路径）
  
  注意：orders-bot 用的 SMS 是 SendSMS（直接 RC API），
        不是 ACTION:SMS（当前 actions.go 不支持 ACTION:SMS）

Step 4: RingClaw 执行
  messaging/actions.go ExecuteAgentActions()：
    case "TASK" → ringcentral.CreateTask()
    [SMS 通过 SOUL 指令触发，agent 在回复里包含 /sms 命令]
  
  或者：SOUL 中定义的 SMS 通过 agent 输出 "/sms send +14045550211 ..." 格式，
         messaging/handler_commands.go 识别 /sms 命令 → SendSMS

Step 5: 回复发到 #atlanta-orders
  ringcentral.SendPost() → "✅ Task #T992 · SMS Mike +14045550211"
```

**完整消息流（时间轴）：**

```
T+0s    Sarah 发消息：
        "@orders-bot dispatch A8821 to Mike, tomorrow 10am,
         1234 Main St Atlanta GA 30309, Engineered Oak 850sqft,
         customer Jenkins +1 404-555-0199"

T+0.5s  Monitor 接收，检查 allowlist ✅

T+0.8s  PersonaLoader 构建 system prompt：
        [SOUL.md 80行] + [Skills index 3行] + [DOMAIN.md 冻结] + [OWNER.md 冻结]
        + [chat memory: 今日已有派单列表]

T+1s    发送给 Claude agent：
        system: {上述 banner}
        user: "@orders-bot dispatch A8821 to Mike..."

T+2.5s  Claude 回复（含 ACTION 块）：
        "核对派单：
        · A8821 · Mike Reyes · 06/04 10:00
        · 1234 Main St 30309 ✅ ZIP 匹配
        ACTION:TASK subject="A8821 Install - Mike Reyes" due=2026-06-04T10:00 ..."

T+2.6s  执eAgentActions()：
        CreateTask() → Task #T992
        
T+2.8s  SMS 路径（/sms send）：
        SendSMS(+14045550211, "Install #A8821 06/04 10am...Reply CONFIRM")
        
T+3.2s  SendPost(#atlanta-orders, "✅ Task #T992 · ✅ SMS Mike +14045550211 · delivered")

T+3.5s  写 chat memory：
        "A8821|Mike Reyes|2026-06-04T10:00|pending"
```

**期望输出（#atlanta-orders）：**

```
orders-bot：

  核对：
  · A8821 · Mike Reyes · 06/04 10:00
  · 1234 Main St Atlanta GA 30309 ✅ ZIP 30309 → Atlanta 匹配
  · Engineered Oak 850sqft · Jenkins +14045550199

  ✅ Task #T992（Mike Reyes · due 06/04 10:00）
  ✅ SMS → Mike +14045550211 · delivered
  ⏳ 30min 无 CONFIRM → 提醒（cron）
```

---

### Flow 2：Agent→Agent 投诉处理（完整脚本）

**背景**：客户投诉 SMS 到达，orders-bot 检测 → 自动路由 tom-bot

**触发条件**（Group B，需 inbound SMS wire）：
- MessageStoreHandler 收到 type=SMS event
- from：外部客户手机号
- body：包含投诉信号（worst/complaint/didn't show/lawsuit）

**执行调度：**

```
Step 1: Inbound SMS 到达（待 wire）
  ringcentral/monitor.go：
    MessageStoreHandler（需在 cmd/start_init.go 中设置）
      → change.Type == "SMS"
      → fetchNewMessages() → GetMessage() 或 ListMessages(type=SMS)
      → 投诉信号检测

Step 2: orders-bot Agent 处理投诉
  messaging/handler.go → HandleInboundSMS()
    PersonaLoader.Build()：SOUL + complaint-detection skill 激活
    agent 生成：
      SendSMS（安抚，≤60秒）
      ACTION:TASK（URGENT，Tom，+2h）
      ACTION:MESSAGE chatid=#atlanta-ops（路由帖，含标签）

Step 3: SMS 发出（安抚客户）
  T+51s SendSMS(客户手机, "Hi! I'm so sorry...")

Step 4: Task 创建
  CreateTask("URGENT: A8810投诉", assignee=Tom, due=+2h, color=Red)

Step 5: Agent→Agent 路由消息发出
  ACTION:MESSAGE chatid=#atlanta-ops
  → audit notice 发到 Tom DM（Tom 是 orders-bot owner）
  → Tom 5秒内 DM 看到 notice → 确认
  → SendPost(#atlanta-ops, "[AGENT_ROUTE:COMPLAINT] ...")

  *** 关键配置：tom-bot.source_user_ids 包含 orders-bot ext ID ***

Step 6: tom-bot Monitor 在 #atlanta-ops 收到消息
  tom-bot 监听 #atlanta-orders + #atlanta-ops
  source_user_ids 包含 orders-bot ext ID
  → tom-bot.handleWSMessage() 触发
  → 消息包含 [AGENT_ROUTE:COMPLAINT] → 激活 complaint-investigation skill

Step 7: tom-bot Agent 自动调查（不等 Tom 操作）
  PersonaLoader.Build()：
    SOUL（含 complaint-investigation skill 规则）
    entity memory（complaint-A8810-20260603.md，若已创建）
    chat memory（#atlanta-ops 今日状态）
  
  agent 生成：
    /phone calllog today（or 读 Call Log API）
    在 #atlanta-ops 线程发调查结论

Step 8: tom-bot 发调查结论（在 #atlanta-ops 同一线程）
  SendPost(#atlanta-ops, "📋 A8810 调查结论 ...")
  ACTION:TASK update T993 note="调查完成"

Step 9: Tom 读结论，决策
  Tom 在 #atlanta-ops @orders-bot "给 Jenkins 发道歉短信，$50 credit"
  → orders-bot 执行 SendSMS to={客户}

Step 10: Entity Memory 更新
  complaint-A8810-20260603.md：
    status: resolved
    resolution: 道歉 + $50 credit · SMS 10:19
    sla_hit: 17分钟（目标 ≤30分钟）✅
```

**完整消息流（时间轴）：**

```
T+0:00  客户 SMS 到达："Crew didn't show up...Worst service ever!!!"

T+0:51  orders-bot → 客户 SMS：
        "Hi! I'm so sorry — I'm escalating to our manager.
         You'll receive an update via text within 15 minutes. — Keller Atlanta"

T+0:52  orders-bot → 创建 Task #T993（URGENT · Tom · +2h · Red）

T+0:53  orders-bot → ACTION:MESSAGE #atlanta-ops（发路由帖）
        → audit notice 到 Tom DM

T+0:56  Tom 确认 audit notice（5秒）

T+0:57  路由帖发到 #atlanta-ops：
        "[AGENT_ROUTE:COMPLAINT]
         订单 A8810 · Jenkins +14045550199
         原文：'Crew didn't show up...Worst service ever!!!'
         安抚 SMS：已发 ✅ · Task #T993
         @tom-bot 请调查"

T+0:58  tom-bot Monitor 收到（orders-bot ext ID 在 trust list）
        → 激活 complaint-investigation skill

T+1:05  tom-bot 查 Call Log（今日 Mike 外呼记录）

T+1:08  tom-bot 在 #atlanta-ops 发调查结论：
        "📋 A8810 调查结论
         派工：Mike Reyes · 06/03 10:00 · 1234 Main St 30309
         SMS 08:52 ✅  CONFIRM ❌
         Mike 今日无 30309 外呼记录
         结论：未确认，未联系客户
         建议 Tom：直接致电 Mike"

T+8:17  Tom 致电 Mike（确认 GPS 导航错误）

T+8:19  Tom @orders-bot "给 Jenkins 发道歉短信，队长 20 分钟内到，$50 credit"

T+8:20  orders-bot → SendSMS(+14045550199, "...道歉... $50 credit...")
        Task #T993 更新 status=In Progress

T+8:21  Entity memory 更新 status=resolved
```

---

### Flow 3：Human→Agent + Agent→Agent 完整 Lowe's 场景

**背景**：Lowe's 入站传真（Group B）+ Beth 用 Adaptive Card 安排回电

```
T+0:00  Lowe's HQ 传真进来（+1 919-555-0188）
        内容：A8810 质量标记 · SOP §7.3 · 截止 06/10

T+0:02  inbound fax handler：
        DownloadAttachment → PDF 解析
        提取：A8810 · Atlanta · 截止 06/10 · SOP §7.3

T+0:05  karen-bot 在 #lowes-handover 发通知（TEXT）：
        "[Lowe's HQ Notice · REF-2026-0603-11]
         A8810 · Atlanta · 截止 06/10 · SOP §7.3"
        ACTION:NOTE 追加台账

T+0:06  karen-bot 检测：A8810 同时在客户投诉 entity memory 中
        → 双路升级触发

T+0:07  karen-bot → ACTION:MESSAGE chatid=#atlanta-ops（路由质量标记给 tom-bot）
        → audit notice → Karen 确认
        "[AGENT_ROUTE:LOWE'S_QUALITY_FLAG]
         A8810 · SOP §7.3 · 截止 06/10 · REF-2026-0603-11
         @tom-bot 请安排复检 Task"

T+0:10  tom-bot 在 #atlanta-ops 自动处理：
        ACTION:TASK "A8810 Lowe's 复检" due=06/09
        发行动计划文本

T+0:12  karen-bot → ACTION:CARD chatid=Beth-DM（双路升级上报）
        → audit notice → Karen 确认
        Card 内容：
          · 质量标记（REF · SOP · 截止）
          · 客户投诉摘要（Jenkins no-show）
          · 推荐联系：+1 919-555-0188
          · 建议话术："Aware of A8810, Tom handling re-inspection. Extension to 06/12?"
          [发短信给 Lowe's 安排回电]  [通知 Karen 我会处理]

T+0:15  beth-bot 将 Card 推送到 Beth DM

T+5:30  Beth 打开 RC，看到 Card，点击「发短信给 Lowe's 安排回电」

T+5:31  beth-bot → SendSMS(+19195550188):
        "Hi, Beth Owens from Keller Interiors.
         Following up on A8810 (REF-2026-0603-11).
         Can we schedule a call today? Available 2-5pm ET.
         My direct: +14045550001."
        ✅ SMS delivered

T+5:32  beth-bot → ACTION:MESSAGE chatid=Karen-DM:
        "已向 Lowe's 发短信安排回电，等候联系。—Beth"
        → audit notice → Beth 确认 → 发出
```

---

## 四、关键配置项检查清单

### 必须配置（上线前）

```
□  每个 Bot 的 RC extension ID 已查询并记录
   获取方法：Bot 启动后看日志 "bot extension ID resolved: XXXXXXXX"

□  Agent 信任关系已配置（source_user_ids 互相添加）：
   tom-bot.source_user_ids ← orders-bot-ext-id, karen-bot-ext-id
   beth-bot.source_user_ids ← tom-bot-ext-id, karen-bot-ext-id
   finance-bot.source_user_ids ← karen-bot-ext-id, hr-bot-ext-id

□  chat_ids 配置正确：
   orders-bot → #atlanta-orders
   tom-bot → #atlanta-ops, #atlanta-orders
   karen-bot → #lowes-handover

□  chat_user_allow 配置（非 owner CSR）：
   orders-bot: sarah/alex/maria 加入 #atlanta-orders 的 allow list

□  SOUL.md 已部署到 Pod 的 PVC
   路径：~/.ringclaw/SOUL.md（或 bot-specific path）

□  DOMAIN.md 已预置关键业务数据
   队员目录（名字 + 手机）
   ZIP 规则
   Lowe's 联系人（karen-bot）
   差旅政策（finance-bot）

□  OOB 审批配置：
   owner DM 已解析（Bot 启动日志：bot DM chat resolved）
   allow_group_mention_authorize 按需设置
```

### Agent→Agent 路由验证

```bash
# 测试 tom-bot 能否接收 orders-bot 的路由消息
# 在 #atlanta-ops 里直接发：
# [AGENT_ROUTE:COMPLAINT]
# 订单：TEST001 · 客户：Test +10000000000
# 安抚 SMS：已发 ✅ · Task #TEST
# @tom-bot 请调查

# 期望：tom-bot 在 5 秒内自动回复调查结论
# 如果没有响应：检查 tom-bot 的 source_user_ids 是否包含发送者 ext ID
```

---

## 五、Cron 配置脚本（Tom 执行一次）

```
# 在 #atlanta-orders（Tom 是 owner）执行：

/cron add "morning-check" "0 8 * * 1-5"
  "查看 chat memory 中超过 18 小时未确认的派单，输出提醒列表到 Tom DM。"

/cron add "eod-summary" "30 17 * * 1-5"
  "读取今日 #atlanta-orders chat memory，生成日结摘要：
   今日派单数 / 已确认 / 未确认 / 明日预约数。纯文本发到 #atlanta-orders。"

# 在 #lowes-handover（Karen 是 owner）执行：

/cron add "lowe's-batch-prep" "0 17 * * 1-5"
  "读取 #lowes-handover chat memory 中今日新增待传真完工单，
   生成批次清单文本。不执行传真，等 Karen 手动 /lowes-batch send。"

/cron add "lowe's-sla-weekly" "0 17 * * 5"
  "读取 per-chat memory，生成本周 Lowe's SLA 报告文本，发到 #lowes-handover。"

# 在 Beth DM（Beth 是 owner）执行：

/cron add "exec-weekly" "0 9 * * 1"
  "生成 Keller 全国 33 店本周运营快照：安装量 · CSAT · Lowe's SLA · 班组缺口。
   每个指标带 delta。识别连续 3 周以上异常的门店。纯文本到 Beth DM。"
```

---

## 六、Demo 演示脚本（5 分钟版本）

### 分钟 0-1：Human→Agent 派单

```
[演示者控制台：打开 RC Team Messaging，进入 #atlanta-orders]

演示者以 Sarah 身份说：
"接下来我作为 Atlanta 的 CSR Sarah，派一张安装单给施工队长 Mike。"

[输入消息]
@orders-bot dispatch A8821 to Mike, tomorrow 10am,
1234 Main St Atlanta GA 30309, Engineered Oak 850sqft,
customer Jenkins +1 404-555-0199

[3 秒等待，bot 回复]

解说：
"Bot 自动：查了 Mike 的手机号，创建了追踪 Task，
 把标准派单 SMS 发到了 Mike 的真实手机。
 之前这个操作要 5 分钟，现在 3 秒。"
```

### 分钟 1-2：Agent→Agent 自动调查

```
[演示者]
"现在我模拟一个客户投诉 SMS。
 注意：接下来两个 Bot 之间的协作，完全不需要人介入。"

[在另一个终端模拟 inbound SMS，或直接在 #atlanta-orders 发路由消息]
[AGENT_ROUTE:COMPLAINT]
订单：A8810 · 客户：Jenkins +14045550199
原文："Crew didn't show up...Worst service ever!!!"
安抚 SMS：已发 ✅ · Task #T993
@tom-bot 请调查

[等待 10-15 秒]

[#atlanta-ops 出现 tom-bot 的自动调查结论]

解说：
"orders-bot 发了投诉升级消息到 #atlanta-ops。
 tom-bot 在这个频道监听，它的 source_user_ids 里有 orders-bot 的 ID，
 所以它认出这是可信消息，自动查了今天的 Call Log 和派工记录，
 10 秒内发出了调查结论。Tom 什么都没有操作。"
```

### 分钟 2-3：Human→Agent 决策执行

```
[演示者以 Tom 身份]
"Tom 读到调查结论，知道是 GPS 问题。他做决策："

@orders-bot 给 Jenkins 发道歉短信，队长 20 分钟内到，送 $50 credit

[Bot 回复：SMS delivered]

解说：
"Bot 不自主道歉，不自主承诺 credit。
 Tom 决策，Bot 执行。这是正确的人机分工。"
```

### 分钟 3-4：Adaptive Card 跨部门协作

```
[演示者]
"最后展示跨部门协作。karen-bot 发现这是双路升级，
 自动给 Beth 准备了一张可操作的 Card。"

[展示 Beth DM，出现 Adaptive Card]
  ⚠️ A8810 双路升级
  Lowe's 质量标记 · SOP §7.3 · 截止 06/10
  推荐联系：Lowe's Compliance +1 919-555-0188
  [发短信给 Lowe's 安排回电]  [通知 Karen 我会处理]

[Beth 点击按钮]

解说：
"Bot 不打电话，它准备好所有上下文让 Beth 做最优决策。
 点一下，SMS 发给 Lowe's，把球传给 Lowe's 主动回电。
 这比 Bot 直接拨号更符合企业工作流。"
```

### 分钟 4-5：总结

```
"我们展示了三种协作：
  · Human→Agent：Sarah 一句话，3 秒完成派单
  · Agent→Agent：客户投诉，两个 Bot 自动协作，10 秒完成调查
  · Agent 准备 → Human 决策：Card 给 Beth 完整上下文，Beth 点按钮执行

 这套系统用了：ACTION:MESSAGE · ACTION:TASK · ACTION:NOTE · ACTION:CARD · SMS · Fax
 没有用 PHONE_CALL，但通过 SMS 和 Card 做到了更好的联络效果。

 护城河是 RC 的通信 API 深度——Team Message · SMS · Fax 这些
 竞争对手做不到真实执行，我们可以。"
```
