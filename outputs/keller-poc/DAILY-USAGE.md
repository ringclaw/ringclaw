# AgentRun · Keller 日常使用指南

每个角色一天的真实使用场景。所有交互都在 RC Team Messaging 里完成。

---

## Sarah · CSR（客服代表）

### 一天的开始（08:00）

**sarah-bot 自动推送晨检** → Sarah DM：

```
sarah-bot

  晨检 · 2026-06-04 08:00

  ⚠️ 未确认派单（过夜）：
  · A8819 · Carlos · 今日 09:30 · 已 18 小时无 CONFIRM
  · A8820 · David  · 今日 10:00 · 已 16 小时无 CONFIRM

  建议：上班后立即跟进，或联系队长确认
```

Sarah 看到，直接致电 Carlos 确认。不需要翻聊天记录找派单信息。

---

### 接到 Lowe's 来的预约（上午，高频）

客户 Williams 预约好了，Sarah 在 #atlanta-orders 直接说：

```
Sarah Cooper
  @sarah-bot dispatch A8831 to Mike, Thursday 2pm,
  421 Northside Dr NW Atlanta GA 30318, Hardwood 400sqft,
  customer Williams +1 404-555-0444

sarah-bot
  [skills index 检测到 dispatch-confirm 激活]

  确认：30318 的 421 Northside Dr NW，
  Westside 住宅区还是 Northside 工业侧？
  （本地知识：两者 GPS 经常混淆）

Sarah Cooper
  Westside

sarah-bot
  核对完成：
  · A8831 · Mike Reyes · 06/05 14:00
  · 421 Northside Dr NW, Atlanta GA 30318 ✅ Westside 确认
  · Hardwood 400sqft · Williams +14045550444

  ✅ Task #T1001 · ✅ SMS Mike +14045550211 · delivered
  ⏳ 30min 无 CONFIRM → 自动提醒
```

**Sarah 的感受**：不需要打开电话簿查 Mike 号码，不需要手动发 SMS，不需要记着跟进。说一句话，3 秒完成。

---

### 中午接到客户投诉（被动触发，Group B 需 wire）

客户短信进来，sarah-bot 自动：
1. 60 秒内回复客户安抚 SMS
2. 在 #atlanta-orders 发升级提醒
3. tom-bot 自动开始调查

**Sarah 什么都不用做**——她会在 #atlanta-orders 看到 bot 已经在处理了。

---

### 下午改单

```
Alex Kim
  @sarah-bot reschedule A8819 Thursday 2pm same address,
  customer Martinez called

sarah-bot
  改期 A8819：Wed 09:30 → Thu 14:00

  ✅ Task 已更新 · ✅ SMS Carlos（改期通知）· ✅ SMS Martinez（客户确认）
```

---

### 17:30 日结自动推送 → #atlanta-orders

```
sarah-bot  [cron 17:30，TEXT ONLY]

  [Atlanta CSR 日结 · 2026-06-03]
  今日派单：11 张
  已确认：9 张 · 未确认：2 张（A8829, A8830）
  明日预约：8 张
  客户投诉：1 起（A8810，已处理 ✅）
```

**Sarah 的日常**：她只需要说"dispatch to Mike"这一句话。Bot 处理一切执行细节。查状态问 bot，改单问 bot，客户投诉 bot 先响应。

---

## Tom · 门店经理

### 08:30 晨预警 → Tom DM

```
tom-bot  [cron 08:30]

  Atlanta 晨预警 · 2026-06-04

  ⚠️ 今日需关注：
  · A8819 Carlos 队 09:30 — 昨日改期，已通知
  · T941 供应商确认待回复（3 天）— 建议今日跟进
  · Mike 队周三 -2 人 — 区域协调请求已发出（待 Dallas 确认）

  今日排期：8 单，6 已确认，2 待确认
  Lowe's 待传：2 份（Karen 今日 EOD 批次）
```

---

### 全天随时查询

Tom 在门店巡查间隙，手机上问：

```
Tom Rivera
  @tom-bot A8810 今天最新情况

tom-bot
  A8810 · Jenkins · 投诉处理中

  · 09:02 客户投诉 SMS 到达
  · 09:03 自动安抚已发 ✅
  · 09:05 调查：Mike 未 CONFIRM，无外呼记录
  · 09:17 Tom 致电 Mike 确认（GPS 问题）
  · 09:19 道歉 + $50 credit SMS 已发 ✅
  · 09:31 Mike 到达现场

  Task #T993 状态：In Progress（待 Mike 完工确认）
```

---

### 下午决策支持

```
Tom Rivera
  @tom-bot 今天的材料成本情况

tom-bot
  [ACTION:PHONE_CALLLOG scope=today] + 读 chat memory

  本周材料消耗（截至今日 14:30）：
  · Engineered Oak：3,420 sqft（5 单）· 成本在预算内
  · Hardwood：810 sqft（2 单）· Carlos 队损耗偏高（+11%）
  · LuxCore：0 sqft（新材料上线前）

  ⚠️ Carlos 队 Hardwood 损耗本周第二次超标
  建议：下次 Hardwood 单前和 Carlos 沟通测量精度

Tom Rivera
  @tom-bot 帮我起草给 Carlos 的消息，提醒他 Hardwood 测量要点，
  措辞友好不要指责

tom-bot
  起草：
  "Carlos，这周两个 Hardwood 单材料用量都偏多一点（+10% 左右）。
   下次施工前可以多留 5% 损耗裕量，但注意精准切割以免浪费太多。
   有什么特殊情况随时告诉我。—— Tom"
  发到 Carlos DM？

Tom Rivera
  确认

tom-bot
  [ACTION:MESSAGE chatid=Carlos-DM → audit notice → Tom 确认 → 发出 ✅]
```

---

### 17:30 日摘要自动推送 → #atlanta-ops

```
tom-bot  [Heartbeat 17:30，TEXT ONLY]

  [Atlanta Daily · 2026-06-03 17:30]
  今日完成：8 单，1 延迟（A8825 供应商迟到，明日补）
  明日：9 预约，7 确认
  班组缺口：Mike 队周三 -2（Dallas 支援确认中）
  最久 Task：T941（4 天，供应商确认）
  Lowe's 待传：2 份（Karen EOD 批次）
```

Tom 读完，如果有需要处理的，直接 @tom-bot 执行。不需要额外整理数据。

---

## Karen · Lowe's 联络

### 每日 17:00 → #lowes-handover

```
karen-bot  [cron 17:00，TEXT ONLY]

  [EOD Batch Prep · 2026-06-03 17:00]
  今日待传：22 店 · 31 份 · 117 页
  预计：47 分钟

  Atlanta：3 份（A8809 · A8815 · A8821）
  Dallas：4 份 ...

  执行：/lowes-batch send 2026-06-03
```

Karen 扫一眼，确认数量对，输入：

```
Karen Yates
  /lowes-batch send 2026-06-03

karen-bot
  [Batch 开始...]
  ✅ A8809 Atlanta → FAX-20260603-001（4页，17:02）
  ✅ ...（31 条，依次发送）

  [Batch 完成 · 17:51]
  ✅ 31/31 · 耗时 49 分钟

  台账已更新 ✅
```

**Karen 的感受**：之前每天手工传真需要 4-5 小时，现在等 49 分钟，自己不用操作任何一步。只需确认数量、输入一行命令。

---

### 收到 Lowe's 入站传真（Group B 后）

```
karen-bot  [inbound fax 自动检测]

  [Lowe's HQ Notice · REF-2026-0603-11]
  订单 A8810 质量标记 · 截止 06/10 · SOP §7.3
  台账已更新 ✅

  ⚠️ 双路升级：A8810 今日同时有客户投诉

Karen Yates
  @karen-bot 通知 Tom 和 Beth

karen-bot
  [audit notice × 2 → Karen 确认 → 发出 ✅]
  · #atlanta-ops：Tom 已通知
  · Beth DM：双路升级已上报
```

---

### 周五 SLA 报告 → #lowes-handover

```
karen-bot  [cron 周五 17:00，TEXT ONLY]

  [Lowe's Weekly SLA · W23]
  发送：156 份
  按时交付：152/97.4%（目标 ≥95% ✅）
  失败重试：4 份（已补发）

  Atlanta：100% ✅  Dallas：98.6% ✅
  Phoenix：91.3% ⚠️（3 份超时，原因：系统故障已恢复）

  月累计 SLA：96.8%（合同要求 ≥95% ✅）
```

---

## Beth · Chief of Staff

### 每周一 09:00 → Beth DM

```
beth-bot  [cron 周一 09:00，TEXT ONLY]

  [Weekly Snapshot · W23 · 2026-06-03]
  安装量：243（↑6%）· CSAT：4.4（↓0.3 ⚠）
  Lowe's SLA：96% ✅ · 班组缺口：7 次（↑2）

  ⚠️ 关注：Atlanta CSAT 4.2（↓0.5，48h no-show ×3）
  💡 建议询问 Tom：no-show 根因？
```

Beth 读完，觉得需要跟进：

```
Beth Owens
  帮我给 Tom 发消息，关于 Atlanta CSAT，友好问询

beth-bot
  起草：[...] 发到 Tom DM？

Beth Owens
  确认

beth-bot  [ACTION:MESSAGE → Tom DM · audit notice · Beth 确认 ✅]
```

---

### 日常随时查询

```
Beth Owens
  看看今天未接来电，需要的发个短信

beth-bot
  [ACTION:PHONE_CALLLOG scope=today missing=true next_actions=true]

  📞 3 条未接来电，3 条 follow-up SMS 已发 ✅
  ⚠️ Lowe's HQ Compliance 11:45 — 建议优先回拨

Beth Owens
  Lowe's 这个打过去

beth-bot
  [ACTION:PHONE_CALL to=+19195550188]
  → FIJI 以 Beth 当前登录身份拨出 ✅
```

---

## 员工 Marcus · 请假申请

### 第一次使用（一次性）

Marcus 打开 RC Team Messaging，找到 hr-bot 私信：

```
Marcus Johnson
  你好

[hr-bot 触发 OOB，Linda 在主机 approve，Marcus 自动加入白名单]

hr-bot
  你好 Marcus！我是 Keller HR 助手，由 Linda 管理。
  可以帮你：请假申请 · 培训查询 · 余额查询。
  有什么可以帮到你？
```

### 申请请假

```
Marcus Johnson
  请假申请 6/10-6/12，家庭原因

hr-bot
  收到，Marcus。
  6/10-6/12（3天）。余额 4 天 → 申请后 1 天。
  通知 Mike 审批，理由保密。结果在这里。

[Mike 审批通过]

hr-bot
  ✅ Mike 已批准，日历已更新。祝你有个好假！
```

整个过程在私信里完成，不需要填表，不需要找 HR 的 email。**Mike 只知道日期，不知道原因。**

### 查询余额

```
Marcus Johnson
  我还有几天假

hr-bot
  Marcus，你当前 PTO 余额：2 天
  （已用：8 天，其中 6/10-6/12 扣减后）

  今年可用假期：10 天（第 1 年）
  下一档升级：入职满 3 年后变为 15 天
```

---

## Alex · Finance Manager

### 每周四 15:00 自动触发

```
finance-bot  [cron 每周四 15:00]

  [本周分包商付款清单 · 2026-06-05]

  Mike Reyes:    $3,675（A8819, A8821）
  Carlos Ruiz:   $2,890（A8823, A8826）
  David Park:    $4,210（A8824, A8825, A8827）

  暂扣（质量标记）：Mike Reyes -$1,890（A8821 待复检）

  本周应付：$8,885
  待确认：$1,890

  [批准] → "approve payments 20260605"

Alex Chen
  approve payments 20260605

finance-bot
  ✅ 付款清单已批准
  ACH 将于明日（周五）发起
  3 条付款通知 SMS 已发给各分包商
```

### 月结（每月 28 日触发）

finance-bot 自动按 5 步走：
1. 检查 Lowe's 逾期收款 → 生成催款请求给 Karen
2. 汇总分包商月度付款 → Alex 一次性审批
3. 汇总跨店差旅费 → 按成本中心分摊
4. 跑材料成本差异 → 超标门店通知店长
5. 生成月度管理报告 → 发 #exec + Beth

**Alex 的感受**：月结从"3 天手工整理 Excel"变成"看 bot 的汇总，审批一次"。

---

## Linda · HR Manager（管理 hr-bot）

### 每天早上

Linda DM 收到 OOB challenge（可能有几条）：

```
hr-bot → Linda DM

  Pending approval (abc-123).
  Marcus Johnson 首次使用 hr-bot DM — 授权？
  执行：ringclaw approval abc-123

  Pending approval (def-456).
  Cross-chat MESSAGE → Mike Reyes DM
  Body: PTO 6/10-6/12 审批请求
  执行：ringclaw approval def-456
```

Linda 在主机（或未来：FIJI 审批 inbox）：

```bash
ringclaw approval abc-123   # 授权 Marcus
ringclaw approval def-456   # 发送给 Mike
```

每天 2-5 条 OOB approve，在 DM 里看到就处理，不需要上下文切换。

### 每周一 09:00 HR 摘要 → #hr-private

```
hr-bot  [cron 周一 09:00，TEXT ONLY]

  [HR 运营周报 · W23]
  请假处理：3 件（2 批准，1 待队长确认 >24h）
  培训完成：LuxCore 本周 +5 人（累计 38/50）
  本周入职：1 名新分包商（Carlos 推荐，入网进行中）
  工伤：无
  ⚠️ PTO 审批待确认 >24h：1 件（Marcus→Mike，建议跟进）
```

---

## 使用规律总结

### 自动发生（无需任何人操作）

```
每天 08:00  → CSR 晨检（未确认派单提醒）→ Sarah DM
每天 08:30  → 店长晨预警（当日关注项）→ Tom DM
每天 17:00  → Lowe's 批次准备文本 → Karen #lowes-handover
每天 17:30  → 门店日摘要 → Tom #atlanta-ops
每周一 09:00 → 高管周报 → Beth DM
每周一 09:00 → HR 运营周报 → Linda #hr-private
每周四 15:00 → 分包商付款清单 → Alex #finance
每月 28 日  → 月结流程自动启动
```

### 用户主动触发（一句话）

```
Sarah："dispatch A8831 to Mike, Thursday 2pm, ..."
Alex:  "reschedule A8819 Thursday 2pm same address"
Tom:   "@tom-bot A8810 今天最新情况"
Tom:   "@tom-bot 帮我起草给 Carlos 的消息"
Karen: "/lowes-batch send 2026-06-03"
Beth:  "看看今天未接来电"
Beth:  "给 Karen 打电话"（→ FIJI 真实拨出）
Marcus: "请假申请 6/10-6/12，家庭原因"
Alex:  "approve payments 20260605"
```

### 跨 Agent 自动发生（用户看到结果，不需要操作）

```
客户投诉 SMS → sarah-bot 检测 → 自动安抚 → 自动路由 tom-bot 调查
Lowe's 入站传真 → karen-bot 解析 → 自动通知 tom-bot + beth-bot
跨店差旅批准 → regional-coord-bot → finance-bot 自动开始追踪
新分包商入网 → hr-bot → finance-bot 自动添加费率记录
```

### 没有 Agent 之前 vs 之后

| 动作 | 之前 | Agent 之后 |
|------|------|-----------|
| 发一张派单 | 查联系人 + 手写 SMS + 手动创建 Task | 说一句话 · 3 秒 |
| 传真 31 份完工单 | 4-5 小时手动操作 | /lowes-batch · 等 49 分钟 |
| 客户投诉首次响应 | 人工看到 → 10-30 分钟才处理 | 60 秒内自动安抚 |
| 写每日运营摘要 | 30 分钟/人/天 × 33 店 | 0 |
| 员工请假申请 | 发邮件等 HR 回复 | DM hr-bot · 当天完成 |
| 分包商月度付款 | 手工整理 Excel 半天 | bot 汇总 · Alex 审批一次 |
| 月度管理报告 | 3 天整理 | bot 自动生成 |
