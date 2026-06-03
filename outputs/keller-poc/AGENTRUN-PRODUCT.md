# AgentRun · Product Design

> 参考：MuleRun Messages 的协作模式 × RC 通信 API 的执行深度

---

## 一、产品定位

### MuleRun Messages 解决了什么

> "人类员工与 AI Agent 在同一个工作空间里像同事一样协作——Agent 可以被@、可以被拉群、可以持续参与工作流程。"

它的核心突破是把 Agent 从"个人工具"变成"团队成员"。
三种协作在同一空间发生：**人↔人、人↔Agent、Agent↔Agent**。

### AgentRun 在这个基础上做了什么

MuleRun 的 Agent 在线程里**讨论**。

AgentRun 的 Agent 在线程里**执行**——

- Agent 说"已发送短信"，是真的发到了客户手机
- Agent 说"已拨出电话"，是真的通过 FIJI 打出去了
- Agent 说"已传真完毕"，是真的传到了 Lowe's HQ
- Agent 说"已升级处理"，是真的有 Task 创建、有 SLA cron 在跑

**Agent 不是在线程里帮你想，而是在线程里帮你做。**

### 一句话

> **AgentRun：让 Agent 成为你 RC 团队的真实成员——在同一个对话里协作，通过 RC 的通信能力真正执行。**

---

## 二、和竞争对手的本质差异

```
MuleRun Messages：
  新建一个 IM 平台 + AI Agent 在里面协作
  Agent 的执行结果 = 生成文字/内容/建议

AgentRun：
  在 RC Team Messaging（用户已有的 IM）里加 Agent
  Agent 的执行结果 = 真实的 SMS 发出去、电话打出去、传真传出去
                    任务创建了、SLA 在追踪、合规台账更新了

核心差别：
  MuleRun：Agent 在线程里产出内容，人去执行
  AgentRun：Agent 在线程里宣布执行，RC API 是执行载体
```

**唯一能做到这点的原因：RC 是通信平台，不是内容平台。**
Copilot、Gemini、MuleRun 都没有 SMS API、Fax API、Phone Call Bridge。

---

## 三、三种协作模式（对应 MuleRun 框架）

### 模式一：人 ↔ 自己的 Agent

每个人有自己的 Agent（SOUL 定义角色和能力）。
Owner 和自己的 Agent 在 DM 或群聊里 1:1 协作。

```
Sarah @sarah-bot "dispatch A8821 to Mike tomorrow 10am"
→ sarah-bot 创建 Task + 真的发 SMS 到 Mike 手机 ✅
```

### 模式二：人 ↔ 别人的 Agent

**这是 AgentRun 区别于"个人助手"的关键。**
Tom 可以直接在群里访问 sarah-bot。
Karen 可以直接在 #lowes-handover 里访问 tom-bot。
Beth 可以直接 @karen-bot 问 Lowe's SLA 情况。

```
Tom @sarah-bot "今天 #atlanta-orders 有几单未确认？"
→ sarah-bot（Sarah 的 Agent）在 #atlanta-ops 里直接回答 Tom
  ——不需要 Sarah 中转，不需要"我先问问我的 AI 再告诉你"
```

### 模式三：Agent ↔ Agent

**Agent 可以 @其他 Agent，在同一线程里分发任务、共享上下文。**

```
sarah-bot 检测到投诉 → 在 #atlanta-ops 发帖 @tom-bot
tom-bot 收到 → 自动查 CallLog → 在同一帖子里回复调查结论
karen-bot 发现 Lowe's 质量标记 → 在 #lowes-handover @tom-bot 路由任务
```

这是 MuleRun 文章里最重要的突破：
**"Agent 甚至也可以 @其他 Agent 共同协作，分发需求"**

---

## 四、Bot 定义重新设计

### 4.1 从「个人助手」到「团队 Agent」

旧模型：
```
一个 Bot = 一个用户的私人工具
Bot 只响应 owner，owner 中转信息给团队
```

新模型（AgentRun）：
```
一个 Bot = 团队的 Agent 成员，有特定专业角色
Bot 响应：
  · owner（完整权限）
  · 团队成员（受 SOUL 定义的访问权限）
  · 其他 Agent（Agent-to-Agent 协作）
```

### 4.2 SOUL 的三层定义

每个 Agent 的 SOUL 包含三个维度：

```markdown
## 我是谁（私人维度）
我是 [owner] 的专属助手。
当 owner 说话，我用 [owner SOUL 的完整能力] 回复。

## 我作为团队成员能做什么（团队维度）
当其他团队成员 @我时，我提供：
  · [可查询的信息类型]
  · [可代理执行的动作]
  · [需要 owner 确认的边界]

## 我和其他 Agent 如何协作（Agent 维度）
当其他 Agent @我时：
  · 我接受来自 [指定 Agent] 的任务路由
  · 我会在同一线程里返回结果，不另起对话
  · 我拒绝 [超出专业边界] 的请求
```

### 4.3 Keller 的 Agent 成员设计

| Agent | Owner | 团队可访问的能力 | 接受哪些 Agent 路由 |
|-------|-------|----------------|------------------|
| sarah-bot | Sarah（CSR） | 查询今日派单状态 · 代发客户 SMS | karen-bot（投诉路由）|
| tom-bot | Tom（店长）| 查询班组状态 · CallLog 分析 | sarah-bot（投诉升级）· karen-bot（Lowe's 路由）|
| karen-bot | Karen（联络）| Lowe's SLA 状态 · 传真台账 | 无（终点节点）|
| beth-bot | Beth（CoS）| 全国指标查询 · 主动外呼 | 所有 Agent（异常上报终点）|
| hr-bot | Linda（HR）| 员工假期余额查询 | 无 |
| mike-bot | Mike（队长）| 今日工单状态 | sarah-bot（派单接收）|

---

## 五、完整 Case 场景（AgentRun 三种协作模式）

---

### Case A · 新功能上线宣传（类 MuleRun 案例 1）

**场景**：Keller 发布"新安装材料系列"促销，跨部门并行推进
**体现模式**：人↔Agent、Agent↔Agent 在同一线程

```
━━━ Thread：Q3新材料系列上线推广 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  @sarah-bot @karen-bot @tom-bot
  Q3 新材料系列本周五上线，需要三条线同步推进：
  1. 客户 SMS 通知（已预约安装的客户）
  2. Lowe's 合规通知（合同变更需要传真）
  3. 队长 briefing（新材料处理要点）

sarah-bot
  收到。Atlanta 门店本周有 34 个预约客户，涉及新材料系列。
  @Beth 确认后我发送告知 SMS。
  草稿：
  "Hi {name}! Keller 即将上线新一代 LuxCore 地板系列，
   本周五正式开始安装。您的工单 #{order} 已确认沿用此系列。
   如有问题请回复。"
  确认发送？

Beth Owens
  确认

sarah-bot
  [ACTION:SMS × 34]
  ✅ 34 条 SMS 已发送，全部 delivered

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

karen-bot
  收到合规通知需求。Lowe's 合同变更需要传真到 Compliance 部门。
  已准备传真模板，需要 Karen 确认。
  [Karen Yates @karen-bot 确认 → /lowes-fax send compliance-notice]
  ✅ 合规传真已发送 → FAX-20260603-007

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

tom-bot
  @mike-bot LuxCore 系列新材料处理要点已整理：
  1. 施工前需要额外 30 分钟温度适应
  2. 工具清洁步骤变更（见附件 LuxCore-installation-guide.pdf）
  请确认收到并转达队员。

mike-bot  [agent-to-agent 接收]
  收到。Mike Reyes 已通知，今日队内 briefing 完成。
  [ACTION:TASK "LuxCore 材料培训确认" assignee=Mike done=true]
  ✅ Task 已标记完成

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[20 分钟内，同一个线程里：]
  ✅ 34 条客户 SMS 已发出
  ✅ Lowe's 合规传真已发送
  ✅ 队长培训确认 Task 完成
```

**对比 MuleRun 案例 1**：
- MuleRun：各渠道 Agent 生成内容，人去发布
- AgentRun：sarah-bot 真的发出了 SMS 到 34 个客户手机，karen-bot 真的传真到了 Lowe's

---

### Case B · 投诉处理多 Agent 闭环（核心 Case）

**场景**：客户投诉到达，跨部门 Agent 协作在同一线程处理
**体现模式**：Agent 检测 → Agent@Agent → 人确认 → Agent 执行

```
━━━ [inbound SMS 触发] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

客户：
  "Crew didn't show up for #A8810. Worst service ever!!!"

━━━ sarah-bot 在 #atlanta-orders 发帖（≤60秒）━━━━━━━━━━━━━━━━

sarah-bot
  ⚠️ 客户投诉（自动检测）
  客户：Jenkins +14045550199
  订单：A8810
  原文："Crew didn't show up...Worst service ever!!!"
  情绪：强烈不满

  已自动回复客户："正在升级处理，15 分钟内回电。"

  @tom-bot A8810 今日派工情况？

━━━ tom-bot 在同一线程回复（无需 Tom 手动介入）━━━━━━━━━━━━━━

tom-bot
  [ACTION:PHONE_CALLLOG scope=today]

  A8810 派工记录：Mike Reyes · 06/03 10am · 1234 Main St 30309
  Mike 今日外呼记录：无 30309 区域拨出，无 Jenkins 联系记录
  派单 SMS 发出时间：08:52 ✅
  Mike CONFIRM 状态：❌ 未回复

  结论：Mike 收到 SMS 但未确认，且未联系客户。

  [ACTION:TASK "URGENT: A8810 投诉" assignee=Tom due=+2h color=Red]
  ✅ 任务已创建 #T993

  @Tom 建议直接致电 Mike 确认情况。

━━━ Tom 在线程里处理 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tom Rivera
  [致电 Mike 后确认：Mike 看到派单但因 GPS 导航去错位置]

Tom Rivera
  @sarah-bot 给 Jenkins 发道歉 SMS：队长 20 分钟内到，
  并送 $50 服务抵用券

sarah-bot
  [ACTION:SMS to=+14045550199]
  "Hi Mr. Jenkins, this is Keller Atlanta — we're so sorry.
   Mike's crew is on the way and will arrive in 20 minutes.
   We're including a $50 service credit on your account.
   Thank you for your patience."
  ✅ SMS delivered

  [ACTION:TASK update T993 note="道歉 SMS 已发，$50 credit 已承诺"]

━━━ 线程最终状态 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

同一个线程里：
  ✅ 投诉自动检测（sarah-bot）
  ✅ 客户安抚 SMS（sarah-bot）
  ✅ 派工历史调查（tom-bot + CallLog）
  ✅ 紧急任务创建（tom-bot）
  ✅ 解决方案 SMS（sarah-bot + Tom 决策）
  全程 < 8 分钟，之前 30-45 分钟
```

---

### Case C · 跨部门资源协调（Agent↔Agent 分发）

**场景**：班组缺口，tom-bot 自动路由到区域协调 Agent，Dallas 接单
**体现模式**：多 Agent 在多个线程里协同，上下文不丢失

```
━━━ tom-bot 每日摘要（Heartbeat 17:30）发现班组缺口 ━━━━━━━━━━━

tom-bot → #atlanta-ops（TEXT）
  班组缺口：Mike 队周三 -2（Engineered Oak 经验）
  [在同一帖子里 @regional-coord-bot]
  @regional-coord-bot Atlanta 周三需要 2 名 Engineered Oak 经验的帮工

━━━ regional-coord-bot 在 #southeast-coord 处理 ━━━━━━━━━━━━━━━

regional-coord-bot
  检查各店本周容量...
  Dallas：Wednesday +2 名可用（有 Engineered Oak 认证）
  出行费用估算：酒店 $180 + 日津贴 $60 = $240

  @David（区域协调员）Atlanta 周三可安排 Dallas 2 名帮工，
  出行成本 $240，是否批准？

David Chen
  批准

regional-coord-bot
  [ACTION:MESSAGE chatid=#atlanta-ops]
  "@tom-bot Dallas 2 名帮工已确认，周三支援 Atlanta。
   出行安排 Linda 协助（HR 通道）。"

  [ACTION:MESSAGE chatid=#dallas-ops]
  "@dallas-store-bot Atlanta 周三需要 2 名 Engineered Oak 帮工，
   出行已安排，请确认人员名单。"

━━━ 所有上下文在各线程里可见 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  #atlanta-ops：tom-bot 宣布需求 → regional-coord-bot 确认支援
  #southeast-coord：David 审批 → regional-coord-bot 执行
  #dallas-ops：dallas-store-bot 确认人员
  全程：David 批准一次，三个线程自动同步
```

---

### Case D · Lowe's 合规 + 外呼（Beth 主动介入）

**场景**：Lowe's HQ 传真 + 客户投诉双路升级，Beth 用 Agent 直接打电话
**体现模式**：人↔Agent（主动外呼），PHONE_CALL 打通真实电话

```
━━━ karen-bot 检测到入站传真 + 投诉双路 ━━━━━━━━━━━━━━━━━━━━━━

karen-bot → #lowes-handover
  ⚠️ 双路升级 · 订单 A8810
  Lowe's：Quality flag，复检截止 06/10（SOP §7.3）
  客户：当日投诉（sarah-bot 已在 #atlanta-orders 处理中）

  @beth-bot 这是双路升级，建议 Beth 直接联系 Lowe's Compliance 确认处理意向

━━━ beth-bot 转发到 Beth DM ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

beth-bot → Beth DM
  ⚠️ A8810 双路升级
  Lowe's 质量标记（截止 06/10）+ 客户当日强烈投诉
  Karen 建议你直接联系 Lowe's Compliance

  需要帮你打电话吗？

Beth Owens
  打过去

beth-bot
  [ACTION:PHONE_CALL to=+19195550188]

━━━ FIJI AvaClientActionBridge ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  Control Plane 记录 action_event:
  {type: PHONE_CALL, status: client_action_required, details: {make_call, to: +19195550188}}

  FIJI 轮询到事件 → executeAvaClientAction
  → callActionHelper.directCall(+19195550188)
  → FIJI 以 Beth 登录身份拨出 ✅

beth-bot → Beth DM
  📞 已通过 FIJI 拨出 Lowe's HQ Compliance +19195550188

━━━ 通话后 ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  和 Lowe's 确认了，他们同意延期到 06/12。
  帮我记录到 Karen 的 #lowes-handover

beth-bot
  [ACTION:MESSAGE chatid=#lowes-handover]
  "A8810 复检截止已与 Lowe's Compliance 确认延至 06/12。Beth 通话确认。"
  → audit notice → Beth DM 确认 → 发出 ✅
```

---

### Case E · HR 线程（多员工并发，同一 hr-bot）

**场景**：同一时间多个员工 DM hr-bot，每个线程独立，信息严格隔离
**体现模式**：一个 Agent 服务多人，MuleRun 的"多线程并行"模式

```
[同时进行的三个独立线程]

━━━ Marcus DM hr-bot ━━━━━     ━━━ Carlos DM hr-bot ━━━━━     ━━━ Amy DM hr-bot ━━━━━
  请假 6/10-6/12             |   查询培训日期              |   入职文件问题
  hr-bot: 已通知 Mike 审批   |   hr-bot: 7/15 LuxCore 培训 |   hr-bot: 重定向 Linda
  ← 完整 PTO 流程 →          |   ← 30 秒完成 →            |   ← 2 句话完成 →

[三个线程完全隔离，hr-bot 同时处理，Linda 只需处理跨 chat 的 OOB approve]

最终 #atlanta-ops 收到：
  hr-bot: 班组缺口：Mike 队 6/10-6/12 -1 名。（来源：HR 保密。）
  [Carlos 的查询、Amy 的问题均不对外泄露]
```

---

## 六、产品价值量化（对 Keller 的 BV）

| 场景 | 当前耗时 | AgentRun 后 | 年节省（33 店）|
|------|---------|------------|--------------|
| CSR 派单（一条）| 5 分钟 | 10 秒 | ~30 staff-hours/天 |
| 投诉处理（升级+安抚）| 30-45 分钟 | < 8 分钟 | 显著降低客户流失风险 |
| Lowe's 传真批次 | 8 分钟/份 × 31 | 49 分钟（全自动）| ~6.5 小时/天 |
| 跨店班组协调 | 45 分钟首响应 | < 5 分钟（Agent 路由）| 减少急单失单 |
| 每日摘要 | 30 分钟/店/天 | 0（Heartbeat 自动）| 16.5 staff-hours/天 |
| Beth 外呼跟进 | 5 分钟找号码拨号 | 3 秒说一句话 | 高管时间价值 |

**总压缩：~53 staff-hours/天（vs 当前 ~65），不改变组织结构，不替换 AIR**

---

## 七、与 MuleRun Messages 的对比定位

```
                MuleRun Messages          AgentRun（我们）
─────────────────────────────────────────────────────────────
平台            新建 IM 平台              RC Team Messaging（已有）
Agent 产出      内容 / 建议 / 草稿        SMS / 电话 / 传真 / Task / 日历
Agent 执行      生成文字，人去执行         真实 API 执行，线程里可见
用户迁移成本    需要迁移到新 IM            零迁移，在用户已有的 RC 里
差异化          AI 参与团队讨论            AI 参与团队执行
护城河          AI 协作体验               RC 通信 API 的深度
```

**我们不是 MuleRun 的竞争者，我们是 RC 生态的 MuleRun。**
MuleRun 的用户如果用 RC，应该用 AgentRun——因为他们的业务执行动作（打电话给客户、发传真给合作方、发 SMS 给施工队）本来就在 RC 上。

---

## 八、技术实现路径（已有 + 待补）

```
已有（全部工作中）：
  ✅ FIJI Onboarding → Control Plane → K8S → Pod（一键注册 Bot）
  ✅ SOUL / Persona 系统（角色定义）
  ✅ RC Actions：Task · Note · SMS · Message · Video · Phone
  ✅ PHONE_CALL → FIJI AvaClientActionBridge → directCall（最新实现）
  ✅ CallLog API · Fax API · Presence API
  ✅ Heartbeat · Cron（定时任务）
  ✅ OOB Approval（跨 chat 治理）
  ✅ Control Plane 审计轨迹（action_events）

待补（代码层，不是架构层）：
  · inbound SMS/Fax 监听（MessageStoreHandler wire，~150 行）→ 解锁 Case B/E
  · OOB Approval 反向通道（CP→Pod，~200 行）→ 提升跨 chat 体验
  · Agent-to-Agent routing（monitor 接受其他 bot extension ID，~50 行配置）

POC 前三周可 demo 的（无需任何代码）：
  Case A（新功能上线协调）· Case C（资源协调 TEXT 版本）·
  Case D（Beth 外呼）· Case E（HR 并行线程）
```

---

## 九、产品命名

**AgentRun**

- **Agent**：团队里的 AI 成员，可被 @、可被拉群、可协作执行
- **Run**：Agent 不只是说，是真正 Run — 跑 SMS、跑电话、跑传真、跑 Task
- 参考 MuleRun：企业集成的"跑通"理念，但从数据集成变成通信执行集成

副标题：*The team layer where humans and agents work together — and actually execute.*
