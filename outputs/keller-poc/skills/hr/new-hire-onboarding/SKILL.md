---
name: new-hire-onboarding
description: 新员工入职全流程——欢迎 SMS · 文件收集 · 培训排期 · 系统配置通知 · 店长就绪报告
version: 1.0.0
metadata:
  tags: [hr, onboarding, new-hire, training, compliance]
  applicable_souls: [hr-service]
  entity_type: onboarding
prerequisites:
  capabilities: [sms]
  memory_keys: [training_calendar, store_directory, system_access_guide]
---

# New Hire Onboarding

Keller 新员工入职涉及多个部门和多个步骤，当前全靠人工串联——
hr-bot 把这条链路自动化，从"录用确认"到"第一天就绪"。

## 触发条件

1. **hiring-jd-generator 触发**（招聘完成后自动衔接）：
   Linda 确认录用 → hiring entity status=hired → 自动触发此 skill

2. **Linda 手动触发**：
   "新员工入职 {姓名} {电话} {邮箱} {角色} {门店} {start_date}"

3. **店长推荐触发**：
   tom-bot 路由 `[AGENT_ROUTE:NEW_HIRE_NOTIFY]` 给 hr-bot

---

## 完整步骤

### Day -7（入职前 7 天）：发起入职流程

1. **创建入职 entity memory**：
   `onboarding-{name}-{start_date}.md`
   status: initiated · role · store · start_date · checklist

2. **欢迎 SMS** → 新员工手机：
   ```
   Hi {first_name}! Welcome to Keller Interiors! 🎉
   We're excited to have you join our {store} team as {role}.
   Your start date: {start_date}.
   
   Over the next few days, I'll send you what you need to get ready.
   Questions? Text back anytime. — Keller HR
   ```

3. **ACTION:EVENT "新员工入职 · {name}"** → Linda 日历（提醒跟进）

4. **通知店长**（ACTION:MESSAGE chatid=#atlanta-ops，Linda OOB approve）：
   ```
   [新员工入职通知]
   姓名：{name}（角色：{role}）
   入职日期：{start_date}
   预计就绪：{start_date + 3 天培训后}
   需要你准备：工牌 · 工具箱 · 停车位说明
   ```

### Day -5（入职前 5 天）：文件收集

5. **文件清单 SMS** → 新员工：
   ```
   Hi {first_name}! Please prepare these documents for Day 1:
   
   Required (all employees):
   · Government-issued photo ID
   · Social Security Card (or equivalent)
   · Completed W-4 (link: {w4_link})
   
   {if installer or W-2}
   · Proof of right to work (I-9 verification)
   
   {if 1099 subcontractor}
   · Proof of liability insurance ($500K min)
   · Completed W-9 (link: {w9_link})
   · Driver's license
   
   Bring originals on Day 1 or reply to confirm you have them.
   Reply READY when you've gathered everything.
   ```

6. **设置文件确认 cron**（3 天后检查）：
   若未收到 "READY" 回复 → 二次提醒 SMS

### Day -3（入职前 3 天）：培训排期

7. **查 training_calendar**，为该角色分配培训场次：

   | 角色 | 必须培训 | 可选 |
   |------|---------|------|
   | CSR | AgentRun 系统使用（1h）· RC Team Messaging 基础（30min）| 客户沟通技巧 |
   | Installer W-2 | OSHA 10 安全（在线）· 材料专项（见专项）| 防滑安全 |
   | Installer 1099 | 材料专项（必须）| OSHA 10（推荐）|
   | Crew Lead | 全部 Installer 培训 + 领导力基础 | 调度管理 |

8. **ACTION:EVENT × N**（创建培训日历事件）：
   - Day 1 09:00：入职文件检查 + 公司介绍（30min）
   - Day 1 10:00：RC Team Messaging 使用（30min，CSR/Crew Lead）
   - Day 1-2：AgentRun 系统使用培训（CSR 专属）
   - Week 1：材料专项培训（Installer，见 training_calendar 最近场次）

9. **培训提醒 SMS** → 新员工：
   ```
   Hi {first_name}! Here's your Week 1 schedule:
   
   📅 {start_date} 9:00am — Welcome & paperwork (30 min)
   📅 {start_date} 10:00am — Team Messaging setup (30 min)
   {if installer}
   📅 {training_date} — {material} installation training ({location})
   
   Location: {store_address}
   Parking: {parking_notes}
   
   See you soon! — Keller HR
   ```

### Day 0（入职当天）：就绪确认

10. **晨间提醒 SMS**（当天 08:00 cron）→ 新员工：
    ```
    Good morning {first_name}! Today's your first day at Keller! 🎉
    Report to: {store_address}, ask for {store_manager_name}.
    Arrival time: 9:00 AM.
    Excited to have you! — Keller HR
    ```

11. **店长就绪确认**（ACTION:MESSAGE → 店长 DM，Linda OOB approve）：
    ```
    [入职提醒] {name}（{role}）今日入职。
    文件状态：{已收到/待确认}
    培训状态：{已排期/待安排}
    请在 EOD 前在 hr-bot DM 发 "Day1 done {name}" 确认完成。
    ```

### Day 1 EOD：完成确认

12. **店长回复 "Day1 done {name}"** → hr-bot 更新 entity：
    `status: day1_complete`
    
    ACTION:MESSAGE → Linda（#hr-private）：
    ```
    ✅ {name} 第一天入职完成（{store} 店长确认）
    ```

### Week 1-2：系统配置与跟踪

13. **系统权限 SMS**（根据角色）：
    ```
    Hi {first_name}! Your next steps:
    
    {if CSR}
    1. RC Team Messaging 已配置，找 {store_manager} 拉入工作群
    2. AgentRun Bot (@orders-bot) 可以在群里直接 @ 使用
    3. 第一周遇到问题随时 DM hr-bot 或找 {store_manager}
    
    {if installer}
    1. 联系 {crew_lead} 确认第一个工单安排
    2. 工具箱和车辆安排联系 {store_manager}
    ```

14. **Week 2 Check-in SMS**：
    ```
    Hi {first_name}! Two weeks in — how's it going?
    Any questions or issues? Just reply here.
    Your HR contact: Linda Wu (linda.wu@keller.com)
    ```

15. **30 天试用期跟进**（30 天后 cron）：
    DM Linda："30天提醒：{name}（{role}，{store}）试用期满。
    需要评估或确认转正吗？"

---

## CSR 专项：AgentRun 系统使用培训

CSR 是使用 orders-bot 的主要用户，入职时专门安排系统培训：

**培训内容（1.5 小时，Day 1 下午）：**

```
模块 1：基础操作（30 min）
  · 如何在 #atlanta-orders 里 @orders-bot
  · 派单指令格式：dispatch {工单} to {人名}, {时间}, {地址}...
  · 查看 Task 状态
  · 改单：reschedule {工单} {新时间}

模块 2：实操练习（30 min）
  · 练习：发 3 张模拟派单（使用测试工单号 TEST001-003）
  · 练习：模拟一次改单
  · 练习：查询今日未确认派单

模块 3：异常处理（30 min）
  · ZIP 不匹配时怎么办
  · 队长长时间未 CONFIRM 怎么办
  · 遇到客户投诉相关词汇时 bot 会做什么
  · 什么时候 @tom-bot vs @orders-bot

培训结束后 SMS 给新 CSR：
  "Training complete! Remember:
   · @orders-bot in #atlanta-orders for dispatches
   · Bot handles Task + SMS automatically
   · Any issues: text back or ask {store_manager}
   Good luck! 🎉"
```

**培训资料**（存 SKILL 的 references 目录）：
- `references/orders-bot-quick-guide.md`：2 页快速参考卡
- `references/dispatch-examples.md`：10 个真实派单例句
- `references/troubleshooting.md`：常见问题 Q&A

---

## Entity Memory 格式

```markdown
<!-- onboarding-{name}-{date}.md -->

# Onboarding: {name} · {role} · {store} · {start_date}

## 基本信息
status: initiated → day1_complete → week2_checkin → complete
role: {CSR / Installer W-2 / Installer 1099 / Crew Lead}
store: {store} · {state}
start_date: {date}
manager: {store_manager}

## 文件收集
- [ ] 政府 ID
- [ ] SSN / 工作授权
- [ ] W-4（W-2）/ W-9（1099）
- [ ] 责任险证明（1099 专用）
reply_received: {date 或 pending}

## 培训安排
- [x] 入职文件 · Day 1 09:00
- [x] RC Team Messaging · Day 1 10:00
- [ ] AgentRun 系统培训 · {date}（CSR 专用）
- [ ] {material} 专项 · {date}

## 时间轴
§ {date} Welcome SMS 已发
§ {date} 文件清单 SMS 已发
§ {date} 培训日历已创建
§ {date} 店长 Day1 确认
```

---

## 失败处理

| 情况 | 行为 |
|------|------|
| 新员工未回复 READY（文件）| Day -2 二次提醒，Day 0 告知店长手动检查 |
| 培训场次已满 | 安排下一场次 + 通知 Linda |
| 店长未发 "Day1 done" | 当天 17:00 提醒 Linda |
| 30 天无法联系新员工 | DM Linda 处理 |
| 1099 保险不符合要求 | 停止培训安排，通知 Linda + 新员工说明要求 |
