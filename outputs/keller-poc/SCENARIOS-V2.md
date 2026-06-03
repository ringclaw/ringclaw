# AgentRun · 场景设计 V2

实际可用 ACTION：MESSAGE · TASK · NOTE · EVENT · CARD
外部通信：SMS（SendSMS via cmd/sms_cmd.go）· Fax（SendFax）
无：PHONE_CALL（直接拨号）

---

## 重新定位：没有拨号，场景反而更强

```
之前的设计误区：
  "bot 帮你打电话" → 依赖 FIJI client 在线、用户当前登录
  体验：用户说"打过去" → 等 5 秒 → FIJI 拨出

更好的设计：
  "bot 准备好所有上下文 + 联系方式 → 用户一键行动"
  体验：bot 发来一张 Adaptive Card，显示：
    · 联系人：Lowe's HQ Compliance
    · 号码：+1 919-555-0188
    · 背景：A8810 质量标记，截止 06/10
    · 建议说什么：[3 句话话术]
    [复制号码] [发短信通知]

为什么更好：
  · 不依赖任何客户端状态
  · 适用于手机端、桌面端、任何 RC 客户端
  · 人做决策，bot 做准备 —— 这才是正确的分工
```

---

## 可用能力总览

| 能力 | ACTION / API | 用途 |
|------|-------------|------|
| Team Message | `ACTION:MESSAGE chatid=` | 跨 Chat 通知、Agent 路由、工作上下文分享 |
| Task | `ACTION:TASK` | 派单追踪、投诉 SLA、待办 |
| Note | `ACTION:NOTE` | 台账、会议记录、合规日志 |
| Event | `ACTION:EVENT` | 培训排期、PTO 日历、安装预约 |
| Adaptive Card | `ACTION:CARD` | 结构化报表、仪表盘、一键操作卡片 |
| SMS（外部） | `SendSMS` | 给队长/客户发文本 SMS |
| Fax | `SendFax` | Lowe's HQ 合规传真 |
| Cron/Heartbeat | 定时触发 | 日摘要、周报、月结提醒（TEXT ONLY）|

---

## Case 1 · 多 CSR 共享 Bot 派单（无变化）

**已有的核心能力全部可用**

```
Sarah Cooper
  @orders-bot dispatch A8821 to Mike, tomorrow 10am,
  1234 Main St Atlanta GA 30309, Engineered Oak 850sqft,
  customer Jenkins +1 404-555-0199

orders-bot
  [ACTION:TASK subject="A8821 Install - Mike Reyes" due=06/04T10:00]
  [SendSMS to=+14045550211]
  Install #A8821 06/04 10am. Address: ... Reply CONFIRM.

  ✅ Task #T992 · ✅ SMS Mike · delivered
```

**没有变化**——派单核心用的是 TASK + SMS，从不需要电话。

---

## Case 2 · 店长日摘要 + 跨 Chat 通知（无变化）

**Heartbeat TEXT + ACTION:MESSAGE**

```
tom-bot  [Heartbeat 17:30，TEXT]

  [Atlanta Daily · 17:30]
  今日完成 8 单，2 延迟...

Tom Rivera
  @tom-bot 帮我起草给区域协调员的消息：Mike 队周三缺 2 人

tom-bot
  起草：[...] 发到 #southeast-coord？

Tom Rivera
  确认

tom-bot
  [ACTION:MESSAGE chatid=#southeast-coord]
  → audit notice → Tom 确认 → 发出 ✅
```

**没有变化**——日摘要和跨 Chat 消息都是 MESSAGE，不涉及电话。

---

## Case 3 · 执行层周报 + 未接来电跟进（重新设计）

### 原设计（需要 PHONE_CALL）

```
Beth 说"打过去" → ACTION:PHONE_CALL → FIJI directCall
```

### 新设计：Adaptive Card + SMS 跟进

**beth-bot 的未接来电处理流程**

```
━━━ Beth DM ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Beth Owens
  看看今天未接来电，需要跟进的发短信

beth-bot
  [查询 Call Log → /phone missed 或 ListExtensionCallLog]

  发现 3 条未接来电：

  [ACTION:CARD]
  {
    "type": "AdaptiveCard",
    "version": "1.3",
    "body": [
      {"type": "TextBlock", "text": "今日未接来电 · 3 条", "size": "Large", "weight": "Bolder"},
      {"type": "FactSet", "facts": [
        {"title": "10:23", "value": "+1 404-555-0xxx（未知）"},
        {"title": "11:45", "value": "Lowe's HQ Compliance  +1 919-555-0188  ⚠️ 优先"},
        {"title": "14:12", "value": "Mark Jenkins  +1 404-555-0199（Atlanta 常客）"}
      ]},
      {"type": "TextBlock", "text": "建议：先回 Lowe's HQ", "color": "Warning"}
    ],
    "actions": [
      {"type": "Action.Submit", "title": "发短信给全部 3 人", "data": {"action": "sms_all"}},
      {"type": "Action.Submit", "title": "只回 Lowe's HQ", "data": {"action": "sms_lowe's"}}
    ]
  }
  END_ACTION

Beth Owens
  发短信给全部 3 人

beth-bot
  [SendSMS × 3]

  · +1 404-555-0xxx → "Hi, this is Beth Owens from Keller.
    I missed your call — I'll follow up with you shortly."  ✅
  · +1 919-555-0188 → "Hi, this is Beth Owens from Keller Interiors.
    I missed your call regarding our account.
    I'll call you back within the hour."  ✅
  · +1 404-555-0199 → "Hi Mark! Beth from Keller here.
    Missed your call — what can I help you with?"  ✅

  3/3 SMS delivered
```

**为什么这比直接拨号更好：**
- Adaptive Card 让 Beth 看清楚所有未接，做优先级判断
- SMS 是异步的，对方有记录、有准备，Beth 再打过去效果更好
- 如果 Lowe's 不在办公室，SMS 提前通知降低空打电话概率
- 整个流程在手机端 RC App 就能完成，不需要 FIJI client

---

### Lowe's 双路升级的跟进（重新设计）

**原设计：**
```
beth-bot 说"打过去" → PHONE_CALL
```

**新设计：ACTION:CARD 准备完整上下文，Beth 自己决定何时打**

```
━━━ Beth DM ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

beth-bot  [收到 karen-bot 的双路升级路由]

  ⚠️ A8810 双路升级

  [ACTION:CARD]
  {
    "type": "AdaptiveCard",
    "version": "1.3",
    "body": [
      {"type": "TextBlock", "text": "⚠️ 需要行动：A8810 双路升级", "size": "Large", "weight": "Bolder", "color": "Warning"},
      {"type": "FactSet", "facts": [
        {"title": "Lowe's 质量标记", "value": "REF-2026-0603-11 · SOP §7.3 · 截止 06/10"},
        {"title": "客户投诉", "value": "Jenkins no-show · sarah-bot 已处理中"},
        {"title": "推荐联系", "value": "Lowe's HQ Compliance  +1 919-555-0188"},
        {"title": "建议说法", "value": "\"We're aware of A8810, Tom is handling re-inspection. Can we get a 2-day extension to 06/12?\""}
      ]},
      {"type": "TextBlock", "text": "Tom 已收到复检 Task #T994（due 06/09）", "isSubtle": true}
    ],
    "actions": [
      {"type": "Action.Submit", "title": "通知 Karen 我会处理", "data": {"action": "ack_karen"}},
      {"type": "Action.Submit", "title": "发短信给 Lowe's 安排回电", "data": {"action": "sms_lowe's"}}
    ]
  }
  END_ACTION

Beth Owens
  发短信给 Lowe's 安排回电

beth-bot
  [SendSMS to=+19195550188]
  "Hi, this is Beth Owens, Chief of Staff at Keller Interiors.
   I'm following up on order A8810 (REF-2026-0603-11).
   Could we schedule a brief call today?
   I'm available 2pm-5pm ET. My direct: +1 404-555-0001."
  ✅ SMS delivered

  [ACTION:MESSAGE chatid=Karen-DM]
  "已向 Lowe's 发短信安排回电，等候联系。A8810 情况已了解。—Beth"
  → audit notice → Beth 确认 → 发出 ✅
```

---

## Case 4 · Lowe's 批量传真（无变化）

**用 SendFax，不涉及电话**

```
Karen Yates
  /lowes-batch send 2026-06-03

karen-bot
  [SendFax × 31 → Lowe's HQ +1 919-555-0100]
  ✅ 31/31 · 台账已更新
```

---

## Case 5 · 投诉处理（重新设计"回电承诺"）

**原设计中有"You'll get a call back within 15 minutes"**

这个承诺需要人工打电话，不在 bot 能力范围内。

**新设计：承诺 SMS 跟进，而不是电话回访**

```
━━━ 客户 SMS ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

  "Crew didn't show up for #A8810. Worst service ever!!!"

━━━ sarah-bot → 客户（≤60 秒）━━━━━━━━━━━━━━━━━━━━━━━━━

  [SendSMS to=+14045550199]

  "Hi Mr. Jenkins! We are so sorry for the inconvenience.
   I've escalated this to our manager right now.
   You'll receive an update via text within 15 minutes.
   We take this very seriously. — Keller Atlanta"

  ✅ SMS delivered（51 秒）
```

**为什么"短信更新"比"回电"更好：**
- 客户有书面记录（更有安全感）
- 不需要客户当场接电话
- 如果客户在开会也能收到
- 跟进效果更可测量（发送/阅读状态）

```
━━━ 解决后跟进 SMS（道歉 + $50 credit）━━━━━━━━━━━━━━━━

[Tom 决策后，sarah-bot 执行]

[SendSMS to=+14045550199]
  "Hi Mr. Jenkins! Quick update: Mike's crew is on the way —
   ETA 20 minutes. We're adding a $50 service credit to your account.
   Sorry again for the delay. — Keller Atlanta"
  ✅ SMS delivered
```

---

## Case 6 · 跨部门信息协作（ACTION:MESSAGE 的核心场景）

这是 ACTION:MESSAGE 最能发挥价值的地方：**跨 Chat 传递上下文，让信息找到正确的人**。

### 6a · karen-bot → tom-bot 路由

```
karen-bot 检测到 Lowe's 质量标记（inbound fax，Group B）：

[ACTION:MESSAGE chatid=#atlanta-ops]

  [AGENT_ROUTE:LOWE'S_QUALITY_FLAG]
  订单：#A8810
  问题：安装质量标记，需复检
  SOP §7.3 · 截止：2026-06-10
  台账 REF：REF-2026-0603-11
  @tom-bot 请安排复检 Task

→ audit notice → Karen DM 确认 → 发出 ✅

tom-bot（在 #atlanta-ops 收到，自动处理）：
  [ACTION:TASK subject="A8810 Lowe's 复检" due=2026-06-09]
  ✅ Task #T994 created
```

### 6b · beth-bot → Tom 定向问询（ACTION:MESSAGE + CARD）

```
Beth Owens
  帮我给 Tom 发消息，关于 Atlanta CSAT 下滑，友好问询

beth-bot
  [ACTION:CARD chatid=Tom-DM]
  {
    "type": "AdaptiveCard",
    "body": [
      {"type": "TextBlock", "text": "Atlanta CSAT 情况跟进", "weight": "Bolder"},
      {"type": "FactSet", "facts": [
        {"title": "本周 CSAT", "value": "4.2 / 5（↓0.5）"},
        {"title": "48h 内投诉", "value": "×3（no-show 类型）"},
        {"title": "来自", "value": "Beth Owens"}
      ]},
      {"type": "TextBlock", "text": "Tom，有什么我可以支持的吗？顺便问一下近期有没有注意到这个趋势。",
       "wrap": true}
    ]
  }
  END_ACTION

→ audit notice → Beth 确认 → Card 发到 Tom DM ✅

Tom 在 RC 客户端看到：
  ┌─────────────────────────────────────┐
  │  Atlanta CSAT 情况跟进              │
  │  本周 CSAT:  4.2 / 5（↓0.5）       │
  │  48h 内投诉:  ×3（no-show 类型）    │
  │  来自:       Beth Owens             │
  │                                     │
  │  Tom，有什么我可以支持的吗？...      │
  └─────────────────────────────────────┘
```

---

## Case 7 · HR 请假（无变化）

**全程 ACTION:MESSAGE（DM 通知队长）+ ACTION:EVENT（日历）**

```
Marcus → hr-bot DM
hr-bot → [ACTION:MESSAGE chatid=Mike-DM]（Linda OOB approve）
Mike approve → hr-bot → [ACTION:EVENT PTO 6/10-6/12]
hr-bot → [ACTION:MESSAGE chatid=#atlanta-ops]（匿名广播）
```

电话从来不在 HR 流程里。

---

## Case 8 · Finance 月结报告（ACTION:CARD 最佳场景）

**月度管理报告用 Adaptive Card，不是纯文本**

```
finance-bot  [cron 月末，发到 #exec]

[ACTION:CARD chatid=#exec]
{
  "type": "AdaptiveCard",
  "version": "1.3",
  "body": [
    {"type": "TextBlock", "text": "Keller 月度财务报告 · 2026-05", "size": "ExtraLarge", "weight": "Bolder"},
    {"type": "ColumnSet", "columns": [
      {"type": "Column", "items": [
        {"type": "TextBlock", "text": "Lowe's 收款", "weight": "Bolder"},
        {"type": "TextBlock", "text": "应收：$284,500", "color": "Default"},
        {"type": "TextBlock", "text": "实收：$271,200（95.3%）", "color": "Good"},
        {"type": "TextBlock", "text": "逾期：$13,300 ⚠️", "color": "Warning"}
      ]},
      {"type": "Column", "items": [
        {"type": "TextBlock", "text": "分包商付款", "weight": "Bolder"},
        {"type": "TextBlock", "text": "工程款：$198,400"},
        {"type": "TextBlock", "text": "差旅费：$6,240"},
        {"type": "TextBlock", "text": "利润率：30.1%", "color": "Good"}
      ]}
    ]},
    {"type": "TextBlock", "text": "⚠️ Phoenix 材料成本超标 +12%，Phoenix 店长已知", "color": "Warning"},
    {"type": "TextBlock", "text": "⚠️ LuxCore 连续 2 月成本偏高，Lowe's 合同费率复查信号已发 Karen", "color": "Warning"}
  ],
  "actions": [
    {"type": "Action.Submit", "title": "查看详细明细", "data": {"action": "detail_report"}},
    {"type": "Action.Submit", "title": "导出 PDF", "data": {"action": "export_pdf"}}
  ]
}
END_ACTION
```

---

## 修正后的能力矩阵

| 场景 | 原设计 | 新设计 | 用到的 ACTION |
|------|--------|--------|--------------|
| 派单 | SMS + Task | 同（无变化）| TASK + SMS |
| 日摘要 | Heartbeat TEXT | 同（无变化）| TEXT only |
| 跨 Chat 通知 | ACTION:MESSAGE | 同（无变化）| MESSAGE |
| 投诉安抚 | SMS（承诺回电）| SMS（承诺短信更新）| SMS |
| Beth 未接来电 | PHONE_CALL | **Adaptive Card + SMS 安排** | CARD + SMS |
| Lowe's 联络 | PHONE_CALL | **SMS 安排回电 + Card 上下文** | CARD + SMS |
| 报告展示 | TEXT 摘要 | **Adaptive Card（可交互）**| CARD |
| Lowe's 传真 | SendFax | 同（无变化）| Fax |
| HR 流程 | MESSAGE + EVENT | 同（无变化）| MESSAGE + EVENT |
| Agent 路由 | ACTION:MESSAGE | 同（无变化）| MESSAGE |

---

## 关键设计原则（无 PHONE_CALL 的情况下）

```
原则 1：Bot 不拨号，Bot 准备电话
  → Adaptive Card 展示联系人 + 上下文 + 建议话术
  → 人决定何时拨，有充分准备再打，效率更高

原则 2：SMS 是最好的"拨号替代"
  → 发短信"我稍后联系您"比空打电话更有效
  → 对方有准备，回电成功率更高
  → Bot 能发 SMS，不能拨号——SMS 就是我们的电话接触方式

原则 3：Adaptive Card 让信息可操作
  → 不只是文字，而是"看完即可行动"
  → 按钮直接触发下一步（发短信、通知某人）
  → 这比纯文本摘要的交互转化率高得多
```
