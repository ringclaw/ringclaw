# Keller POC · Case Catalog

8 个完整设计的场景，按"RC 能力依赖"分两组：

- **Group A（当前可 demo）**：不依赖 inbound SMS/Fax 监听
- **Group B（需 wire inbound 监听）**：依赖 `monitor.SetMessageStoreHandler()`

---

## Group A — 当前可 demo

### Case 1 · CSR 派单闭环（dispatch-confirm）

**触发**：Sarah 在 #atlanta-orders 里：
```
dispatch A8821 to Mike, tomorrow 10am, 1234 Main St Atlanta,
Engineered Oak 850sqft, customer Jenkins +1 404-555-0199
```

**执行 bot**：sarah-bot（personal · csr SOUL · dispatch-confirm skill）

**流程**：
1. Directory 查 Mike → 解析手机 +1 404-555-0211
2. `CreateTask` — subject="A8821 install", assignee=Mike, due=明天10:00
3. `SendSMS` 到 Mike（标准 crew dispatch 模板，末尾 `Reply CONFIRM`）
4. Schedule at-time cron：30min 后若无 CONFIRM → post #atlanta-orders @tom-bot

**回复给 Sarah**：
```
✅ Task #T992 created · Mike Reyes
✅ SMS +1 404-555-0211 · delivered
⏳ Auto-escalate @tom-bot if no CONFIRM by 10:30
```

**CONFIRM 回路**（Case 1b · 需 inbound SMS）：
Mike 回 SMS "CONFIRM #A8821" → inbound SMS handler 匹配 open dispatch → 取消 cron → chat post "✅ Mike confirmed A8821"

**APIs**：Directory · Task-create · SMS-out · Cron
**CONFIRM 回路 APIs**：**SMS-in（Group B）**
**Demo 状态**：✅ 派单主流程可 demo / ⚠ CONFIRM 检测需 inbound wire

**成功信号**：派单 → 首次 SMS 到达 < 5 秒；整条链路（含 CONFIRM）< 30 秒

---

### Case 2 · CSR 改单通知

**触发**：Sarah：`reschedule A8821 Thursday 2pm same address customer Jenkins`

**执行 bot**：sarah-bot

**流程**：
1. `UpdateTask` A8821（新 due = Thursday 14:00）
2. `SendSMS` 到 Mike（改期通知，不要求 CONFIRM）
3. `SendSMS` 到 Jenkins（客户确认：新时间 + "如有问题请回复"）

**回复给 Sarah**：
```
✅ Task A8821 updated → Thursday 14:00
✅ SMS Mike · SMS Jenkins · both delivered
```

**APIs**：Task-update · SMS-out × 2 · Directory
**Demo 状态**：✅ 完全可 demo，无 inbound 依赖

**成功信号**：两条 SMS < 10 秒，Task 状态同步

---

### Case 3 · Lowe's HQ EOD 批量传真

**触发**：Cron 17:00 weekday（karen-bot）

**执行 bot**：karen-bot（personal · lowes-liaison SOUL · daily-digest skill）

**流程**：
1. 聚合 #lowes-handover 当日 completion-form PDF（由各店 store-mgr bot 上传）
2. 生成 manifest，post 到 chat：

```
[Cron: EOD Batch · 2026-06-03 17:00]
22 stores · 31 files · 117 pages
Recipient: Lowe's HQ Returns +1 919-555-0100
Estimated: 47 min (90s/fax rate limit)

⏳ Awaiting Beth approval — reply `/lowes-batch approve 2026-06-03` in DM
```

3. Beth DM 回 `/lowes-batch approve 2026-06-03` → OOB 审批通过
4. 逐条 `SendFax`（每条 90s 间隔，rate limit）
5. 每条传真确认号 + 页数 → 追加到 #lowes-handover Note（合规台账）
6. 批次完成 → 摘要 post 到 chat（成功数 / 失败数 / 总时长）

**重试逻辑**（karen-bot 硬规则）：失败 → +60s / +120s / +240s，第 3 次失败后 DM Beth

**APIs**：Cron · SendPost · OOB · SendFax × N · Note-append
**Demo 状态**：✅ 完全可 demo，无 inbound 依赖

**成功信号**：Lowe's fax SLA < 2min/单（vs 当前 8min）；Beth 只审批 1 次

---

### Case 4 · 门店每日运营摘要

**触发**：Cron 17:30 weekday（tom-bot）

**执行 bot**：tom-bot（personal · store-mgr SOUL · daily-digest skill）

**流程**：
拉 Task API + Call Log + per-chat memory → daily-digest skill 按 store-mgr 模板组装 → `SendPost` #atlanta-ops

```
[Cron: Atlanta Daily · 2026-06-03 17:30 EDT]
Today: 8 installs completed; 2 delayed.
  - #A8819 — supply issue (rescheduled Thu)
  - #A8820 — customer reschedule request
Tomorrow: 11 booked; 6 confirmed.
Crew gap: Mike's team -2 helpers Wed.
Top stuck task >24h: #T941 (owner: @Tom, 3 days idle).
📎 Archive: ringclaw.local/r/atl-2026-06-03
```

**APIs**：Task-read · CallLog · Memory · Cron · SendPost（纯文本，无 ACTION block）
**Demo 状态**：✅ 完全可 demo

**成功信号**：每日摘要 0 人工；30 min/店/天 → 0

---

### Case 5 · 区域跨店援助协调

**触发**：Atlanta #atlanta-ops 里 tom-bot 发了"班组缺口 Wed -2 helpers" → 区域协调员 bot 看到

**执行 bot**：regional-coord-bot（personal · regional-coordinator SOUL · daily-digest skill）

**流程**：
1. daily-digest skill 每晨 8:00 cross-chat 读各店 #<store>-ops，识别 Atlanta Wed 缺口
2. 检查本区域其他店当日盈余：Dallas +2（愿意出行）
3. 生成跨店 helper 方案（含出行成本估算），post #southeast-coord 等 owner 决策
4. owner 批准 → OOB challenge → Dallas 店长 bot 接到 cross-store 请求

**APIs**：Cross-chat · Cron · OOB · SendPost
**Demo 状态**：✅ 可 demo（cross-chat OOB 已实现）

**成功信号**：缺口到方案 < 24h（vs 当前 45min 首响应、无审计轨迹）

---

### Case 6 · HR 请假申请（角色隔离）

**触发**：员工 DM hr-bot（role bot）：`Need PTO 6/10-6/12, family event`

**执行 bot**：hr-bot（role · hr SOUL · dispatch-confirm skill）

**流程**：
1. 查余额（per-user memory） → 回复员工（warm 语气）：

```
Got it, Marcus. PTO 6/10-6/12 (3 days).
Balance: 4 days → 1 left after this.
Routing approval to Mike. You'll hear back here.
(Reason kept HR-confidential per policy.)
```

2. dispatch-confirm skill → DM Mike（仅日期 + 班组影响，不含"family event"）
3. Mike 批准（在他的 chat 里回复 / DM hr-bot）
4. `UpdateEvent` 日历（Marcus 的）
5. 回复 Marcus：已批准
6. 匿名 crew-gap 广播 → tom-bot：`Crew gap: Mike's team -1 helper 6/10-6/12. (Source: HR-confidential.)`

**关键**：tom-bot、店内 chat、#exec 里没有任何人能看到"family event"或 Marcus 的名字。

**APIs**：Directory · DM · Calendar · Memory
**Demo 状态**：✅ 可 demo

**成功信号**：员工体验到保密；store mgr 收到班组影响信号；零数据泄露

---

## Group B — 需 inbound SMS/Fax wire（同一个代码修复）

> 前置条件：`cmd/start_init.go` 里加 `monitor.SetMessageStoreHandler(fn)`
> + 实现 MessageStoreHandler，这个 fix 解锁以下所有 case

---

### Case 7 · 客户投诉 SMS 处理（complaint-handling）

**触发**：客户外部手机 → 门店号 SMS：
`"Crew didn't show up for #A8810. Worst service ever!!!"`

**执行 bot**：sarah-bot（complaint-handling skill）→ escalation → tom-bot

**流程**：
1. inbound SMS 事件触发 → 检测投诉信号（"didn't show up" + "!!!"）
2. ≤ 60 秒 → `SendSMS` 客户安抚（csr 声音模板）：
```
Hi! I'm so sorry — escalating to our manager right now.
You'll get a call back within 15 min. We take this seriously.
```
3. 同时 post #atlanta-ops（verbatim 引用 + @tom-bot）：
```
🚨 URGENT — customer Jenkins (#A8810) no-show report. Tone: angry.
Verbatim: "Crew didn't show up...Worst service ever!!!"
Auto-reply sent. SLA: 15 min callback.
👉 @tom-bot action required.
```
4. `CreateTask` — assignee=Tom, due=+2h, priority=urgent
5. 自动 pull 派单记录 + call log 验证 → 追加到 escalation 帖（"Mike's crew dispatched 10am per record; ZIP mismatch Atlanta vs Buford GA possible"）
6. SLA cron：2h 后无 close → 升级 regional-coord

**tom-bot 接力**：
1. 读 escalation 帖 + Task
2. 查 A8810 派单记录 → 发现地址 ZIP 可能错误
3. post #atlanta-ops：建议 sarah-bot 核验客户地址
4. sarah-bot → 向客户 SMS 核验，同时准备道歉 + $50 credit 草稿

**APIs**：**SMS-in（需 wire）** · SMS-out · Task · CallLog · cross-chat · Cron
**Demo 状态**：❌ 需 inbound wire（代码已写，只差调用）

**成功信号**：投诉到客户首次 ack < 60 秒；关闭 < 10 min（vs 当前 30-45 min）

---

### Case 8 · Lowe's HQ 质量警告传真 + 双路升级

**触发**：Lowe's HQ Compliance（+1 919-555-0188）传真到 Karen 号码，内容：quality flag for order #A8810

**执行 bot**：karen-bot（inbound fax receiver）→ routing → tom-bot

**流程**：
1. inbound Fax 事件触发（message-store filter，type=Fax）
2. `DownloadAttachment` 下载 PDF
3. agent 解析 PDF 内容（文本提取）→ 提取：受影响 order、SOP 引用、合规截止日
4. post #lowes-handover（合规声音）：
```
[Lowe's HQ Notice · REF-2026-0603-11]
Subject: Quality flag — Order #A8810
SOP reference: §7.3 Re-inspection within 5 business days
Affected order: #A8810 (Atlanta)
Lowe's deadline: 2026-06-10 (5 business days)

📎 Compliance ledger updated.
👉 @tom-bot action required.
```
5. `NoteAppend` #lowes-handover 合规台账（timestamp + ref + store + deadline）
6. dispatch-confirm skill → tom-bot（re-inspection 任务派发）

**双路情况**（#A8810 同时是 Case 7 的投诉订单）：
- karen-bot 检测到 #A8810 在 complaint-handling ledger 里已有 open 投诉
- 额外 DM Beth："#A8810 同时有 Lowe's 质量标记 + 客户投诉，需要联动处理"

**PDF 解析方案**：
- RC AI API 为音频专用，PDF 解析由 agent（Claude）直接读文本层
- 如果是扫描件（图片 PDF），需要额外 OCR 方案（待定）

**APIs**：**Fax-in（需 wire，同 Case 7 fix）** · DownloadAttachment · Note · cross-chat · dispatch-confirm
**Demo 状态**：❌ 需 inbound wire + PDF 解析方案确认

**成功信号**：传真到 Lowe's 内部通知 < 5 min（vs 当前人工处理）；台账自动更新

---

## 汇总

| Case | 所属 Group | 执行 Bot | Skills | 当前状态 |
|------|-----------|---------|--------|---------|
| 1 · CSR 派单 | A | sarah-bot | dispatch-confirm | ✅ 主流程 / ⚠ CONFIRM 回路 |
| 2 · CSR 改单 | A | sarah-bot | dispatch-confirm | ✅ |
| 3 · Lowe's 批量传真 | A | karen-bot | daily-digest | ✅ 最快 demo |
| 4 · 门店日摘要 | A | tom-bot | daily-digest | ✅ |
| 5 · 区域跨店援助 | A | regional-coord-bot | daily-digest | ✅ |
| 6 · HR 请假隔离 | A | hr-bot (role) | dispatch-confirm | ✅ |
| 7 · 客户投诉 SMS | B | sarah-bot + tom-bot | complaint-handling | ❌ 需 wire |
| 8 · Lowe's 传真质量标记 | B | karen-bot + tom-bot | — | ❌ 需 wire + PDF 方案 |

**一个 fix 解锁 Group B 全部**：`monitor.SetMessageStoreHandler()` + inbound handler 实现（估算 ~150 行代码）。

**POC demo 路径建议**：
1. W3：先 demo Case 3 + 4（无 inbound 依赖，完整可跑）
2. W4：Case 1 主流程 + Case 6（充分展示 SOUL 差异化）
3. W5：inbound wire 完成后 demo Case 7（最高视觉冲击力）
4. W6：Case 8（Lowe's 关系加成）
