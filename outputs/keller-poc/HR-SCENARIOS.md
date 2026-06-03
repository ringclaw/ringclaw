# HR · 招聘与入职场景设计

两个完整场景，覆盖：人→Agent、Agent→Agent、完整消息流。

---

## 场景一：招聘需求 → JD 生成 → 候选人管理

### 背景

Atlanta 门店 Mike 队周三连续缺 2 人（Engineered Oak 专项），
tom-bot 在每日摘要里检测到缺口超过 2 周。
Tom 决定招一个新的 W-2 安装工，让 tom-bot 把需求发给 hr-bot。

---

### 触发：Agent→Agent（tom-bot → hr-bot）

```
━━━ #atlanta-ops ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

tom-bot  [Heartbeat 17:30，日摘要]

  [Atlanta Daily · 2026-06-03]
  班组缺口：Mike 队连续 2 周 -2（Engineered Oak）
  跨店支援成本本月已 $1,240

  建议 Tom：考虑启动招募流程

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tom Rivera
  @tom-bot 发招募需求给 HR，需要 1 个 Engineered Oak 安装工，
  W-2，Atlanta，尽快

tom-bot
  [ACTION:MESSAGE chatid=hr-bot-private → Linda OOB approve]
  → audit notice → Tom DM 确认 → 发出

  [AGENT_ROUTE:HIRING_REQUEST]
  角色：Installer W-2
  专项：Engineered Oak
  门店：Atlanta · GA
  急迫：本月内
  背景：班组缺口连续 2 周，跨店支援成本上升
  来源：Tom Rivera（店长）
```

---

### hr-bot 接收并生成 JD

```
━━━ hr-bot 在 #hr-private ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot  [收到 HIRING_REQUEST，hiring-jd-generator skill 激活]

  [招募请求] Tom Rivera（Atlanta）
  角色：Installer W-2 · Engineered Oak 专项
  急迫：本月内

  正在生成 JD 草稿...

  [ACTION:CARD]（Linda 审核用）
  ┌─────────────────────────────────────────────────────┐
  │  JD 草稿 · Installer (W-2) · Atlanta, GA            │
  │                                                     │
  │  职位：Flooring Installer – Engineered Oak          │
  │  门店：Keller Interiors · Atlanta, GA               │
  │  类型：W-2 全职  薪资：$22-27/小时（视经验）         │
  │                                                     │
  │  【关于 Keller】                                     │
  │  Keller Interiors 是 Lowe's Home Improvement        │
  │  27 年合作安装服务商，覆盖美国 15 州 33 门店。        │
  │                                                     │
  │  【主要职责】                                        │
  │  · 按 Lowe's 客户订单完成地板安装                    │
  │  · Engineered Oak 铺装（含测量、切割、收边）          │
  │  · 完工后拍照存档，提交完工表单                      │
  │  · 与 CSR 和队长通过 RC 消息协作                     │
  │                                                     │
  │  【任职要求】                                        │
  │  · 1 年以上地板安装经验（Engineered Oak 优先）        │
  │  · 有效驾照，可靠代步工具                            │
  │  · 基本手机使用能力（工单 App）                      │
  │                                                     │
  │  【加分项】                                          │
  │  · Keller LuxCore 系列认证                          │
  │  · OSHA 10 安全认证                                 │
  │                                                     │
  │  【Keller 提供】                                     │
  │  · 竞争力薪资 + 医疗保险（工作满 90 天后）           │
  │  · 10 天 PTO（第一年）                              │
  │  · 材料专项培训和认证支持                            │
  │  · 安全工具套装（入职时提供）                        │
  │                                                     │
  │  【申请】                                            │
  │  SMS：+1 404-555-0099（注明 EO-ATL）                │
  │  Email：hr@keller.com（主题：Installer-Atlanta）     │
  │                                                     │
  │  [批准发布]   [修改 JD]   [暂不发布]                 │
  └─────────────────────────────────────────────────────┘
```

---

### Linda 审核与发布

```
━━━ #hr-private ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Linda Wu
  薪资上限改到 $26/小时，其他 OK

hr-bot
  [更新薪资：$22-26/小时]
  已更新。重新生成 Card...
  [ACTION:CARD 更新版]

Linda Wu
  [点击 批准发布]

hr-bot
  ✅ JD 已批准

  [ACTION:NOTE title="JD · Installer W-2 · Atlanta · 2026-06-03"]
  已存 #hr-private · 永久记录 ✅

  发布内部推荐 SMS...
  [SendSMS × 全体 Atlanta 员工]

  ┌────────────────────────────────────────────────────────┐
  │ Keller Atlanta 正在招募 Engineered Oak 安装工！         │
  │ W-2 全职，$22-26/小时，有意向请联系 hr@keller.com      │
  │ 或 SMS：+1 404-555-0099（注明 EO-ATL）                 │
  │ 成功推荐奖励：$200 入职后 90 天内发放                   │
  └────────────────────────────────────────────────────────┘

  [ACTION:TASK subject="招募跟进 · Installer W-2 · Atlanta"
    assignee=Linda due=2026-06-30]

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot  [3 天后，候选人回复]
  （Linda 手动录入）
  "新候选人 Carlos Jr +14045550444 for EO-ATL"

hr-bot
  [创建候选人追踪]
  entity: hiring-installer-atlanta-20260603.md
  候选人：Carlos Jr · +14045550444 · 状态 initial

  [SendSMS to=+14045550444]
  "Hi Carlos! Thanks for your interest in the Keller Installer
   position (Atlanta, Engineered Oak). Could you share:
   1. Years of EO installation experience?
   2. When can you start?
   Looking forward to hearing from you! — Keller HR"

  ✅ SMS sent · 候选人状态更新：contacted
```

---

### 录用确认 → 触发入职流程

```
━━━ #hr-private ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Linda Wu
  Carlos Jr 录用，6/17 入职，Atlanta，Installer W-2

hr-bot
  ✅ 录用确认
  entity hiring-installer-atlanta-20260603.md · status=hired

  自动触发 new-hire-onboarding skill...
  （详见场景二）
```

---

## 场景二：新员工入职全流程（Carlos Jr）

### 背景

Carlos Jr，新录用安装工（W-2，Engineered Oak），6 月 17 日入职 Atlanta 门店。
hr-bot 自动推进从录用到第一天就绪的全部步骤。

---

### Day -7（6/10）：启动入职

```
━━━ hr-bot 自动执行（hiring skill 触发）━━━━━━━━━━━━━━━━━━━━

hr-bot
  [创建 entity: onboarding-carlos-jr-20260617.md]

  [SendSMS to=+14045550444]

  "Hi Carlos! 🎉 Welcome to Keller Interiors!
   We're excited to have you join our Atlanta team as Flooring Installer.
   Your start date: Monday, June 17th.

   Over the next few days, I'll send you everything you need to get ready.
   Questions? Just text back anytime. — Keller HR (Linda)"

━━━ hr-bot → #hr-private（Linda 知情）━━━━━━━━━━━━━━━━━━━━━━

  新员工入职启动：Carlos Jr（Installer W-2）
  入职日：6/17 · Atlanta
  欢迎 SMS：已发 ✅
  接下来：6/12 发文件清单，6/14 排培训，6/17 入职当天晨间提醒
```

---

### Day -5（6/12）：文件收集

```
hr-bot  [SendSMS to=+14045550444]

  "Hi Carlos! Getting ready for Day 1 (June 17)?

  Please prepare these documents:
  ✅ Government-issued photo ID (driver's license is great)
  ✅ Social Security Card
  ✅ Completed W-4 → keller.com/w4-form
  ✅ Proof of right to work (I-9)

  Bring originals on Day 1 — we'll handle copies.
  Reply READY when you've got them!
  
  Any questions? Text back. — Keller HR"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

[3 天后，Carlos 回复]

Carlos Jr
  READY

hr-bot  [inbound SMS 检测 "READY"，Group B]
  entity 更新：documents_ready=true · 6/14
  [或：Linda 手动在 hr-bot DM 输入 "Carlos ready docs"]
```

---

### Day -3（6/14）：培训排期

```
hr-bot  [ACTION:EVENT × 3]（Carlos + 店长 + Linda 日历）

  ✅ Event 1：入职文件检查（6/17 09:00，30min，Atlanta 门店）
  ✅ Event 2：RC Team Messaging 基础（6/17 10:00，30min）
  ✅ Event 3：Engineered Oak 安装培训（6/24，全天，Atlanta 培训室）

  [SendSMS to=+14045550444]

  "Hi Carlos! Your Week 1 schedule is set:

  📅 Monday 6/17 9:00 AM
     Welcome & paperwork — Atlanta store
     Ask for Tom Rivera (store manager)

  📅 Monday 6/17 10:00 AM
     Team messaging setup (30 min)

  📅 Monday 6/24 All day
     Engineered Oak installation training
     Atlanta Training Room, 1234 Store Ave

  Address: {Atlanta store address}
  Parking: {parking notes from DOMAIN.md}

  See you Monday! — Keller HR"

  entity 更新：training_scheduled=true
```

---

### Day 0（6/17 08:00）：入职当天

```
hr-bot  [cron 08:00]

  [SendSMS to=+14045550444]
  "Good morning Carlos! Today's your first day at Keller! 🎉
   Report to: {Atlanta store address}, ask for Tom Rivera.
   Arrival: 9:00 AM. Bring your documents!
   Excited to have you on the team. — Keller HR"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot  [ACTION:MESSAGE → Tom DM，Linda OOB approve]

  [入职提醒] Carlos Jr（Installer W-2）今日入职 9:00。
  文件状态：✅ 已确认齐全
  培训状态：✅ 已排期（6/17 文件 + RC，6/24 EO 培训）
  请在今天 EOD 前回复 hr-bot：
  "Day1 done Carlos" 确认入职完成。
```

---

### Day 0 EOD：店长确认

```
Tom Rivera  [DM hr-bot]
  Day1 done Carlos

hr-bot
  ✅ Carlos Jr 第一天入职完成（Tom Rivera 确认，6/17 17:32）

  entity 更新：status=day1_complete

  [ACTION:MESSAGE → #hr-private，Linda 知情]
  "✅ Carlos Jr 入职完成（Atlanta，6/17）
   文件：已收
   培训：6/24 EO 专项待完成
   系统：RC App 已配置"

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

hr-bot  [Day 1 下午，系统配置短信]

  [SendSMS to=+14045550444]
  "Hi Carlos! A few things to get set up:

  1. RC Team Messaging app (if not installed):
     Download: {RC app store link}
     Your account: carlos.jr@keller.com
     Password: first login link → {link}

  2. Once in, find the #atlanta-orders channel
     Tom will add you to the team

  3. For dispatch work, you'll use @orders-bot
     (Tom or Sarah will show you Thursday)

  Questions? Text here anytime. — Keller HR"
```

---

### Week 2（6/24）：EO 专项培训后确认

```
hr-bot  [cron Day+7，培训日 reminder]

  [SendSMS to=+14045550444]（Day+7，培训前一天）
  "Reminder Carlos: Engineered Oak installation training
   TOMORROW, 6/24, 9:00 AM, Atlanta Training Room.
   Please confirm: reply YES or let us know if any issue."

[Carlos 回复 YES]

[培训结束后，Linda 在 hr-bot DM 输入]
Linda Wu
  mark training complete EO Carlos Jr 20260624

hr-bot
  per-user memory 更新：
  carlos-jr.md:
    training_status.engineered_oak = completed · 2026-06-24
    employment_type = W-2
    store = Atlanta · GA
    start_date = 2026-06-17

  [SendSMS to=+14045550444]
  "Hi Carlos! Training complete — you're officially certified for
   Engineered Oak installs at Keller. 🎉
   Tom will assign your first solo install soon.
   Great work! — Keller HR"

  entity: status=training_complete
```

---

### 30 天跟进（7/17）

```
hr-bot  [cron Day+30，Linda DM]

  30 天提醒：Carlos Jr（Installer W-2，Atlanta）试用期满。
  状态：培训完成 ✅ · 6 单已完成 · 无投诉记录
  需要：转正确认 or 延长试用期？

Linda Wu
  转正确认

hr-bot
  entity: status=permanent_employee
  per-user memory 更新：probation=passed · 2026-07-17

  [SendSMS to=+14045550444]
  "Congratulations Carlos! You've successfully completed your
   probationary period at Keller Interiors.
   You're now a permanent W-2 employee. 🎉
   PTO and full benefits are now active. — Keller HR"
```

---

## 两个场景的多 Bot 协作总览

```
招聘场景：

  Tom → tom-bot（检测缺口，建议招募）
      ↓ [AGENT_ROUTE:HIRING_REQUEST]
  tom-bot → hr-bot（路由需求）
      ↓ hiring-jd-generator skill 激活
  hr-bot → Linda（Adaptive Card JD 草稿）
      ↓ Linda 批准
  hr-bot → 全体员工（内推 SMS）
      ↓ 候选人回复
  hr-bot → Linda（候选人更新）
      ↓ 录用确认
  hr-bot → new-hire-onboarding skill（自动衔接）

入职场景：

  hr-bot → 新员工（Welcome SMS · 文件清单 SMS · 培训提醒）
  hr-bot → Tom（店长通知 · Day1 提醒）[ACTION:MESSAGE]
  hr-bot → Linda（状态更新）[ACTION:MESSAGE to #hr-private]
  hr-bot → 新员工日历（培训 Event × N）[ACTION:EVENT]
  新员工 → hr-bot（READY 确认）[SMS 回复]
  Tom → hr-bot（Day1 done 确认）[DM 回复]
  hr-bot → 所有相关方（自动状态同步）

人工决策点（不可自动化）：
  · Linda 审批 JD（Card 按钮）
  · Linda 确认录用（手动输入）
  · Linda OOB approve 发给 Tom 的消息
  · Tom 发 Day1 done 确认
```

---

## 两个 Skill 对 HR 工作的改变

| 任务 | 之前 | hr-bot + Skill 后 |
|------|------|-------------------|
| 收到招募需求 | 等邮件或电话，手动整理信息 | tom-bot 路由，格式化需求自动到达 |
| 写 JD | 手动写，套模板 1-2 小时 | bot 1 分钟生成草稿，Linda 微调即可 |
| 发布 JD | 手动发邮件、贴公告板 | 一键发全员内推 SMS |
| 新员工入职跟进 | Excel 清单 + 手动发邮件 | 全部自动化，Linda 只处理异常 |
| 培训安排 | 手动查日历 + 逐一通知 | Bot 自动查 training_calendar + 创建 Event |
| 入职合规文件 | 人到了才发现文件不全 | 入职前 5 天 SMS 发清单，提前确认 |
| 30 天跟进 | 经常忘记 | cron 自动提醒 Linda |
