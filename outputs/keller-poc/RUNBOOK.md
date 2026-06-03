# Keller POC — RUNBOOK

> 适用版本：RingClaw v0.x Keller POC  
> 最后更新：2026-06-03  
> 维护人：Platform Team

---

## §一 系统总览

### 1.1 架构概述

Keller POC 基于 RingClaw 平台，将 RingCentral Team Messaging 与多个业务 AI Bot 连接。各 Bot 通过 SOUL.md 定义人格与触发规则，RingClaw 负责事件路由、ACTION 块执行和 Cron/Heartbeat 调度。

```
RingCentral ──WebSocket──▶ RingClaw ──ACP/HTTP──▶ Bot Agent (SOUL.md)
                                │
                    ┌───────────┴───────────┐
                    │                       │
               Cron Scheduler        Heartbeat Monitor
                    │                       │
            ACTION 执行层（RC API）   ACTION 执行层（RC API）
```

### 1.2 平台 Cron / Heartbeat 能力（新规则，2026-06 生效）

平台已升级 Cron 和 Heartbeat 的默认 ACTION 白名单，取代原"纯文本输出"限制。

#### Cron 默认允许的 ACTION 类型

| ACTION 类型 | 说明 |
|---|---|
| MESSAGE | 向频道/DM/群组发送消息 |
| NOTE | 创建或追加 RingCentral Note |
| TASK | 创建任务（支持 URGENT 优先级） |
| CARD | 推送结构化 Adaptive Card（含按钮） |
| SMS | 发送短信（需对应手机号权限） |
| 跨 chat ACTION | 在非当前 chat 执行上述操作 |

#### Heartbeat 默认允许的 ACTION 类型

| ACTION 类型 | 说明 |
|---|---|
| MESSAGE | 向频道/DM/群组发送消息 |
| NOTE | 创建或追加 RingCentral Note |
| CARD | 推送结构化 Adaptive Card |
| TASK | 创建任务（支持 URGENT 优先级） |

> Heartbeat **不包含** SMS。

#### 需白名单开启的能力

| 能力 | 说明 |
|---|---|
| VIDEO | 创建 Video 会议桥 |
| PHONE_CALL | 发起电话呼叫（FIJI client） |

> **批量 SMS 说明**：karen-bot 批量完工通知通过 ACTION:SMS（批量发送给 Lowe's 各门店协调员）和 ACTION:MESSAGE（发送到 #lowes-handover）实现。Karen 手动点击 Card 中的"发送批量完工通知"按钮或执行 `/lowes-batch` 命令来触发。Cron 可推送包含一键触发按钮的 CARD 以简化操作。

---

## §二 Bot 配置详细说明

### §二.1 orders-bot 配置

**职责**：负责派单管理、确认追踪和队长沟通。

#### SOUL.md 核心配置

```yaml
name: orders-bot
persona: 派单协调员，关注派单确认率与响应速度

cron:
  - id: morning-check
    schedule: "0 8 * * *"     # 每日 08:00
    chat: "#dispatch-ops"
    actions_allowed:
      - TEXT
      - ACTION:TASK
      - ACTION:SMS
    prompt: |
      检查过去18小时内所有未确认派单。
      输出：
        1. TEXT 列表：派单编号、门店、队长姓名、派出时间
        2. ACTION:TASK 为每条未确认派单创建 URGENT 任务，标题"[URGENT] 派单未确认 - {派单编号}"
        3. ACTION:SMS 向对应队长发送提醒短信："您有一个派单待确认，请尽快回复。派单号：{派单编号}"

  - id: 30min-confirm
    schedule: "*/30 * * * *"  # 每30分钟，派单后30分钟首次触发
    chat: "#dispatch-ops"
    trigger: post_dispatch     # 派单事件后触发一次
    actions_allowed:
      - ACTION:TASK
      - ACTION:SMS
    prompt: |
      检查派出后30分钟仍未确认的派单。
      输出：
        1. ACTION:TASK 创建 URGENT 任务，标题"[URGENT] 派单未确认（30分钟）- {派单编号}"
        2. ACTION:SMS 向队长发送第二次确认请求："这是第二次提醒，派单 {派单编号} 仍等待您确认，请立即处理。"
```

#### 关键字段说明

- `morning-check`：每日晨检，自动为所有超18小时未确认派单创建 URGENT 任务并发 SMS，无需人工介入。
- `30min-confirm`：派单后30分钟单次触发，确保时效性跟进。

---

### §二.2 tom-bot 配置

**职责**：Atlanta 区域运营日摘要，汇报 #atlanta-ops 频道。

#### SOUL.md 核心配置

```yaml
name: tom-bot
persona: Atlanta 区域运营助手，关注当日完工率、延迟、班组健康度

heartbeat:
  schedule: "30 17 * * *"    # 每日 17:30
  chat: "#atlanta-ops"
  actions_allowed:
    - TEXT
    - ACTION:CARD
    - ACTION:TASK
  # 注意：Heartbeat 不含 SMS，不能发 SMS
  prompt: |
    生成今日 Atlanta 区域运营日摘要。
    输出：
      1. TEXT 简要摘要（3行以内）
      2. ACTION:CARD 结构化日报 Card，推送至 #atlanta-ops，包含：
           - 今日完成工单数 / 延迟工单数
           - 明日预约数量
           - 班组缺口（人员不足门店）
           - 最久未处理 Task 标题及时长
      3. ACTION:TASK 为所有超期未处理任务升级优先级为 URGENT，
           标题格式："[URGENT升级] {原任务标题}"
           说明：超期任务不等 Tom 手动处理，系统自动升级
```

#### 关键变更说明

- Heartbeat 17:30 **不再只输出纯文字**，Tom 在 #atlanta-ops 看到结构化 Card。
- 超期 Task 自动升级为 URGENT，无需 Tom 手动处理。
- Heartbeat 不支持 SMS，如需短信通知需转移到 Cron。

---

### §二.3 karen-bot 配置

**职责**：Lowe's 完工通知管理、SLA 台账维护和周报生成。

#### SOUL.md 核心配置

```yaml
name: karen-bot
persona: Lowe's 账户运营专员，关注 SLA 合规与 Lowe's 沟通效率

cron:
  - id: batch-sms-notify
    schedule: "0 17 * * *"    # 每日 17:00
    chat: "@karen"            # Karen DM
    actions_allowed:
      - TEXT
      - ACTION:CARD
      - ACTION:NOTE
      - ACTION:SMS
      - ACTION:MESSAGE
    prompt: |
      准备今日 Lowe's 完工通知批次。
      输出：
        1. TEXT 通知清单（工单编号、门店、状态）
        2. ACTION:CARD 通知准备 Card，推送至 Karen DM，包含：
             - 本日待发完工通知列表
             - [发送批量完工通知] 按钮（点击后触发批量 SMS 发送）
             - 预计通知数量和门店分布
        3. ACTION:NOTE 更新 Lowe's SLA 台账，追加当日摘要行：
             日期 | 待发通知数 | 通知状态 | 操作人

  - id: lowes-sla-weekly
    schedule: "0 17 * * 5"   # 每周五 17:00
    chat: "#lowes-account"
    actions_allowed:
      - ACTION:CARD
      - ACTION:NOTE
    prompt: |
      生成 Lowe's 本周 SLA 周报。
      输出：
        1. ACTION:CARD Lowe's SLA 周报 Card，推送至 #lowes-account，包含：
             - 本周完工通知完成率
             - SLA 达标门店 / 未达标门店
             - 逾期风险项
        2. ACTION:NOTE 在 Lowe's SLA 台账追加本周汇总行：
             周次 | 总工单 | 达标率 | 逾期数 | 备注
```

#### 关键变更说明

- `batch-sms-notify` Cron 推送 CARD（而非纯文字），Karen 点"发送批量完工通知"按钮触发批量 ACTION:SMS 给 Lowe's 各门店协调员，同时发 ACTION:MESSAGE 到 #lowes-handover。
- 比输入命令更自然，比全自动更安全（批量通知仍需一次人工确认）。
- `/lowes-batch` 命令保留，作为触发批量 ACTION:SMS 的备用方式。

---

### §二.4 beth-bot 配置

**职责**：Beth（VP）周报生成，汇报关注门店经营状态。

#### SOUL.md 核心配置

```yaml
name: beth-bot
persona: 高管助手，关注门店绩效、异常高亮和战略建议

cron:
  - id: exec-weekly
    schedule: "0 9 * * 1"    # 每周一 09:00
    chat: "@beth"             # Beth DM
    actions_allowed:
      - ACTION:CARD
    prompt: |
      生成本周执行层周报，直接推送结构化 Card 至 Beth DM。
      ACTION:CARD 内容：
        - 33 家门店本周绩效数据（完工率、NPS、逾期率）
        - 关注门店高亮（绩效红色预警门店，列出具体原因）
        - 本周建议询问议题（3条，附数据支撑）
        - 与上周对比趋势
      注意：直接输出 ACTION:CARD，不输出纯文字段落
```

#### 关键变更说明

- 周一 09:00 Cron 推送结构化 Card 到 Beth DM（不再是文字段落）。
- Card 包含 33 店完整数据、关注门店高亮、本周建议询问。

---

### §二.5 finance-bot 配置

**职责**：分包商付款、Lowe's 应收对账、月度结账。

#### SOUL.md 核心配置

```yaml
name: finance-bot
persona: 财务运营助手，关注付款合规、应收回款和月度结账

cron:
  - id: subcontractor-payment
    schedule: "0 15 * * 4"   # 每周四 15:00
    chat: "@alex"
    actions_allowed:
      - ACTION:CARD
    prompt: |
      生成本周分包商付款审批请求。
      ACTION:CARD 付款审批 Card → Alex，包含：
        - 本周待付款分包商列表（姓名、金额、服务门店、工单数）
        - 付款总额汇总
        - [批准付款] 按钮

  - id: lowes-payment-reconciliation
    schedule: "0 0 5 * *"    # 每月5日
    chat: "@alex"
    actions_allowed:
      - ACTION:CARD
      - ACTION:NOTE
    prompt: |
      执行 Lowe's 应收对账。
      输出：
        1. ACTION:CARD 逾期应收明细 Card → Alex，包含：
             - 逾期应收款项列表（工单、金额、逾期天数）
             - 总逾期金额
             - [发催款SMS给 Lowe's 协调员] 按钮
        2. ACTION:NOTE 在对账台账追加本月对账行：
             月份 | 应收总额 | 已收 | 逾期 | 逾期率

  - id: month-end-close
    schedule: "0 0 28 * *"   # 每月28日
    chat: "#finance"
    actions_allowed:
      - ACTION:NOTE
      - ACTION:CARD
    prompt: |
      执行月度结账流程（全部5步自动执行，Alex 只需点按钮批准）：

      Step 1: ACTION:NOTE 在对账台账追加本月汇总行（自动，无需等待）
      Step 2: ACTION:CARD 付款审批 Card → Alex，含 [批准付款] 按钮（推送后继续）
      Step 3: ACTION:NOTE 追加差旅分摊明细至台账（自动）
      Step 4: ACTION:CARD 成本超标预警 Card → Alex，超标门店高亮 + [通知 Karen 启动合同复审] 按钮（推送后继续）
      Step 5: ACTION:CARD 月度管理报告 Card → #exec 频道 + Beth DM（自动）

      Alex 只需在收到 Card 时点按钮批准，不需要主动触发任何步骤。
```

---

### §二.6 hr-bot 配置

**职责**：旺季人员扩编、季度培训追踪。

#### SOUL.md 核心配置

```yaml
name: hr-bot
persona: HR 运营助手，关注人员配置充足性和培训合规率

cron:
  - id: seasonal-crew-scaling
    schedule: "0 0 1 2 *"    # 每年 2月1日
    chat: "#hr-ops"
    actions_allowed:
      - ACTION:MESSAGE
      - ACTION:NOTE
    prompt: |
      启动旺季人员扩编调查。
      输出：
        1. ACTION:MESSAGE 向全国各门店店长发送调查消息，包含：
             - 旺季预计开始日期和持续周期
             - 询问：预计缺口人数、岗位类型、优先级
             - 回复截止日期（3个工作日内）
        2. ACTION:NOTE 初始化全国旺季缺口汇总台账：
             门店 | 区域 | 预计缺口 | 岗位 | 优先级 | 回复状态

  - id: quarterly-training
    schedule: "0 0 1 1,4,7,10 *"  # 每季度1日
    chat: "#hr-ops"
    actions_allowed:
      - ACTION:EVENT
      - ACTION:SMS
    prompt: |
      执行季度培训追踪。
      输出：
        1. ACTION:EVENT 为本季度培训未完成人员创建下次培训场次
        2. ACTION:SMS 向未完成培训人员发送提醒短信：
             "您本季度的培训尚未完成，下次培训场次已为您预约：{日期时间}，请确认参加。"

heartbeat:  # 可选，Linda 配置开启
  schedule: "0 9 1 * *"     # 每月1日 09:00（Linda 可自定义）
  chat: "@linda"
  actions_allowed:
    - ACTION:NOTE
    - ACTION:CARD
  # 注意：Heartbeat 不含 SMS
  prompt: |
    生成月度培训完成率报告。
    输出：
      1. ACTION:NOTE 更新月度培训完成率台账：
           月份 | 应完成人数 | 已完成 | 未完成 | 完成率
      2. ACTION:CARD 未完成人员汇总 Card → Linda DM，包含：
           - 未完成人员名单（姓名、门店、欠缺课程）
           - 逾期风险提示
           - 本月整体完成率
```

---

## §三 触发条件与执行流

### Flow 1：派单执行流

```
[客户下单]
     │
     ▼
orders-bot 接收派单请求
     │
     ├──▶ 派单给队长（ACTION:MESSAGE）
     │
     ▼
[+30分钟]
     │
     ├──▶ 30min-confirm Cron 触发（一次性）
     │         ├── ACTION:TASK（URGENT，标记"派单未确认-30分钟"）
     │         └── ACTION:SMS（向队长发第二次确认请求）
     │
     ▼
[次日 08:00]
     │
     ├──▶ morning-check Cron 触发
     │         ├── TEXT 列出所有超18小时未确认派单
     │         ├── ACTION:TASK（为每条未确认派单创建 URGENT 任务）
     │         └── ACTION:SMS（向对应队长发提醒）
     │
     ▼
[队长确认派单]
     │
     └──▶ 派单状态更新为"已确认"，任务自动关闭
```

### Flow 2：运营日报流（tom-bot）

```
[每日 17:30 Heartbeat 触发]
     │
     ▼
tom-bot 拉取当日 Atlanta 区域运营数据
     │
     ├──▶ TEXT 简要摘要（3行）推送 #atlanta-ops
     │
     ├──▶ ACTION:CARD 结构化日报 Card → #atlanta-ops
     │         ├── 今日完成/延迟工单数
     │         ├── 明日预约数量
     │         ├── 班组缺口
     │         └── 最久未处理 Task
     │
     └──▶ ACTION:TASK 超期任务自动升级为 URGENT
               （不等 Tom 手动处理）

注意：Heartbeat 不发 SMS
```

### Flow 3：Lowe's 完工通知流（karen-bot）

```
[每日 17:00 batch-sms-notify Cron 触发]
     │
     ▼
karen-bot 汇总今日待发完工通知工单
     │
     ├──▶ TEXT 通知清单
     │
     ├──▶ ACTION:CARD 通知准备 Card → Karen DM
     │         ├── 待发完工通知列表
     │         └── [发送批量完工通知] 按钮
     │
     └──▶ ACTION:NOTE 更新 SLA 台账当日摘要行

[Karen 点击 Card 中的"发送批量完工通知"按钮]
     │
     ├──▶ 触发批量 ACTION:SMS 发送给 Lowe's 各门店协调员（人工确认，更安全）
     └──▶ 触发 ACTION:MESSAGE 发送到 #lowes-handover
          （Karen 也可执行 /lowes-batch 命令）
```

### Flow 4：Finance 月度结账流

```
[每月28日 00:00 month-end-close Cron 触发]
     │
     ▼
Step 1: ACTION:NOTE → 对账台账汇总追加（自动）
     │
     ▼
Step 2: ACTION:CARD → Alex（付款审批 Card + [批准付款] 按钮）
     │
     ▼
Step 3: ACTION:NOTE → 差旅分摊明细追加台账（自动）
     │
     ▼
Step 4: ACTION:CARD → Alex（成本超标预警 Card + [通知 Karen 启动合同复审] 按钮）
     │
     ▼
Step 5: ACTION:CARD → #exec 频道 + Beth DM（月度管理报告 Card，自动）

[Alex 早上来到 #finance]
     │
     ├── 发现 2 张 Card 等待批准（付款审批 + 成本超标确认）
     ├── 点按钮批准，整个月结完成
     └── Beth DM 已有管理报告 Card

整个月结：从"3天手工整理" → "Alex 点2次按钮"
```

### Flow 5：HR 旺季扩编流

```
[每年 2月1日 seasonal-crew-scaling Cron 触发]
     │
     ▼
hr-bot 向全国各门店店长发送 ACTION:MESSAGE
     │
     └── 包含旺季缺口调查模板问题

[次日]
     │
     └── Linda 收到各店回复，无需逐个打电话
          ACTION:NOTE 全国缺口汇总台账持续更新
```

---

## §四 角色与权限矩阵

| Bot | 触发类型 | 默认 ACTION 能力 | 白名单能力 | 人工确认点 |
|---|---|---|---|---|
| orders-bot | Cron | TEXT, TASK, SMS | — | 队长确认派单 |
| tom-bot | Heartbeat | TEXT, CARD, TASK | — | 无（全自动） |
| karen-bot | Cron | TEXT, CARD, NOTE, SMS, MESSAGE | — | 批量通知发送（点 Card 按钮） |
| beth-bot | Cron | CARD | — | 无（全自动） |
| finance-bot | Cron | NOTE, CARD | — | Alex 点按钮批准付款/超标 |
| hr-bot | Cron + Heartbeat | MESSAGE, NOTE, TASK, CARD, EVENT, SMS | — | Linda 查看汇总后跟进 |

---

## §五 Cron 配置脚本

以下脚本用于初始化所有 Bot 的 Cron 配置。使用 `ringclaw cron add` 命令。

### 5.1 orders-bot Cron

```bash
# 每日晨检：超18小时未确认派单 → TEXT + TASK(URGENT) + SMS
ringclaw cron add \
  --bot orders-bot \
  --id morning-check \
  --schedule "0 8 * * *" \
  --chat "#dispatch-ops" \
  --actions "TEXT,ACTION:TASK,ACTION:SMS" \
  --prompt-file ~/.ringclaw/prompts/orders-morning-check.md

# 派单后30分钟确认追踪（新增）→ TASK(URGENT) + SMS
ringclaw cron add \
  --bot orders-bot \
  --id 30min-confirm \
  --schedule "*/30 * * * *" \
  --trigger post_dispatch \
  --trigger-once \
  --chat "#dispatch-ops" \
  --actions "ACTION:TASK,ACTION:SMS" \
  --prompt-file ~/.ringclaw/prompts/orders-30min-confirm.md
# ACTION 能力说明：
#   ACTION:TASK - 创建 URGENT 任务（标记未确认30分钟）
#   ACTION:SMS  - 向队长发送第二次确认请求短信
```

### 5.2 tom-bot Heartbeat

```bash
# 每日 17:30 日摘要：TEXT + CARD(结构化日报) + TASK(超期升级)
ringclaw heartbeat set \
  --bot tom-bot \
  --schedule "30 17 * * *" \
  --chat "#atlanta-ops" \
  --actions "TEXT,ACTION:CARD,ACTION:TASK" \
  --prompt-file ~/.ringclaw/prompts/tom-daily-summary.md
# ACTION 能力说明：
#   ACTION:CARD - 推送结构化日报 Card（含完工/延迟/预约/班组缺口/最久Task）
#   ACTION:TASK - 超期任务自动升级为 URGENT
#   注意：Heartbeat 不含 SMS，不可添加 ACTION:SMS
```

### 5.3 karen-bot Cron

```bash
# 每日 17:00 完工通知批次准备：TEXT + CARD(含通知按钮) + NOTE(SLA台账)
ringclaw cron add \
  --bot karen-bot \
  --id batch-sms-notify \
  --schedule "0 17 * * *" \
  --chat "@karen" \
  --actions "TEXT,ACTION:CARD,ACTION:NOTE,ACTION:SMS,ACTION:MESSAGE" \
  --prompt-file ~/.ringclaw/prompts/karen-batch-sms-notify.md
# ACTION 能力说明：
#   ACTION:CARD    - 通知准备 Card，含"发送批量完工通知"按钮（仍需人工点击）
#   ACTION:NOTE    - 更新 SLA 台账当日摘要行
#   ACTION:SMS     - 批量发送完工通知给 Lowe's 各门店协调员（Karen 点按钮触发）
#   ACTION:MESSAGE - 发送完工通知到 #lowes-handover（Karen 点按钮触发）
#   注意：批量通知需 Karen 点 Card 按钮或 /lowes-batch 触发

# 每周五 17:00 Lowe's SLA 周报：CARD + NOTE
ringclaw cron add \
  --bot karen-bot \
  --id lowes-sla-weekly \
  --schedule "0 17 * * 5" \
  --chat "#lowes-account" \
  --actions "ACTION:CARD,ACTION:NOTE" \
  --prompt-file ~/.ringclaw/prompts/karen-sla-weekly.md
# ACTION 能力说明：
#   ACTION:CARD - Lowe's SLA 周报 Card（含达标率、逾期风险）
#   ACTION:NOTE - 台账追加周汇总行
```

### 5.4 beth-bot Cron

```bash
# 每周一 09:00 执行层周报：CARD（直接推 Beth DM）
ringclaw cron add \
  --bot beth-bot \
  --id exec-weekly \
  --schedule "0 9 * * 1" \
  --chat "@beth" \
  --actions "ACTION:CARD" \
  --prompt-file ~/.ringclaw/prompts/beth-exec-weekly.md
# ACTION 能力说明：
#   ACTION:CARD - 结构化周报 Card（33店数据、关注门店高亮、本周建议询问）
#   注意：不输出纯文字段落，直接 CARD
```

### 5.5 finance-bot Cron

```bash
# 每周四 15:00 分包商付款审批：CARD → Alex
ringclaw cron add \
  --bot finance-bot \
  --id subcontractor-payment \
  --schedule "0 15 * * 4" \
  --chat "@alex" \
  --actions "ACTION:CARD" \
  --prompt-file ~/.ringclaw/prompts/finance-subcontractor-payment.md
# ACTION 能力说明：
#   ACTION:CARD - 付款审批 Card（付款清单明细 + [批准付款] 按钮）

# 每月5日 Lowe's 应收对账：CARD + NOTE
ringclaw cron add \
  --bot finance-bot \
  --id lowes-payment-reconciliation \
  --schedule "0 0 5 * *" \
  --chat "@alex" \
  --actions "ACTION:CARD,ACTION:NOTE" \
  --prompt-file ~/.ringclaw/prompts/finance-lowes-reconciliation.md
# ACTION 能力说明：
#   ACTION:CARD - 逾期应收明细 Card（含[发催款SMS给 Lowe's 协调员] 按钮）
#   ACTION:NOTE - 对账台账追加月度行

# 每月28日 月度结账（全自动5步）：NOTE + CARD × 3
ringclaw cron add \
  --bot finance-bot \
  --id month-end-close \
  --schedule "0 0 28 * *" \
  --chat "#finance" \
  --actions "ACTION:NOTE,ACTION:CARD" \
  --prompt-file ~/.ringclaw/prompts/finance-month-end-close.md
# ACTION 能力说明：
#   Step1 ACTION:NOTE  - 对账台账汇总追加（自动）
#   Step2 ACTION:CARD  - 付款审批 Card → Alex（[批准付款]）
#   Step3 ACTION:NOTE  - 差旅分摊明细追加台账（自动）
#   Step4 ACTION:CARD  - 成本超标预警 Card → Alex（[通知 Karen 启动合同复审]）
#   Step5 ACTION:CARD  - 月度管理报告 Card → #exec + Beth DM（自动）
```

### 5.6 hr-bot Cron

```bash
# 每年 2月1日 旺季扩编：MESSAGE(各店) + NOTE(汇总台账)
ringclaw cron add \
  --bot hr-bot \
  --id seasonal-crew-scaling \
  --schedule "0 0 1 2 *" \
  --chat "#hr-ops" \
  --actions "ACTION:MESSAGE,ACTION:NOTE" \
  --prompt-file ~/.ringclaw/prompts/hr-seasonal-scaling.md
# ACTION 能力说明：
#   ACTION:MESSAGE - 向全国各门店店长发送旺季缺口调查（含模板问题）
#   ACTION:NOTE    - 初始化全国缺口汇总台账

# 每季度1日 培训追踪：EVENT(创建培训场次) + SMS(提醒)
ringclaw cron add \
  --bot hr-bot \
  --id quarterly-training \
  --schedule "0 0 1 1,4,7,10 *" \
  --chat "#hr-ops" \
  --actions "ACTION:EVENT,ACTION:SMS" \
  --prompt-file ~/.ringclaw/prompts/hr-quarterly-training.md
# ACTION 能力说明：
#   ACTION:EVENT - 为未完成培训人员创建下次培训场次
#   ACTION:SMS   - 向未完成培训人员发送提醒短信

# hr-bot Heartbeat（可选，Linda 配置）：NOTE + CARD
ringclaw heartbeat set \
  --bot hr-bot \
  --schedule "0 9 1 * *" \
  --chat "@linda" \
  --actions "ACTION:NOTE,ACTION:CARD" \
  --prompt-file ~/.ringclaw/prompts/hr-monthly-training-report.md
# ACTION 能力说明：
#   ACTION:NOTE - 月度培训完成率台账更新
#   ACTION:CARD - 未完成人员汇总 Card → Linda DM
#   注意：Heartbeat 不含 SMS
```

---

## §六 Demo 演示脚本（5分钟版本）

### 分钟 0–1：派单自动追踪（orders-bot）

**场景**：展示派单后的自动确认追踪，强调"系统不等人"。

1. 在 #dispatch-ops 发起一个派单：`/dispatch store=ATL-07 crew=Team-B`
2. 说明：派单已发出，系统记录派出时间。
3. **30分钟后自动触发**（演示时可用 `--dry-run` 模拟）：
   - `30min-confirm` Cron 检测到派单30分钟未确认
   - 展示自动创建的 URGENT 任务："[URGENT] 派单未确认（30分钟）- ORD-2026-0603"
   - 展示自动发出的 SMS 提醒（队长 Team-B 手机）
4. 演示要点："以前这步需要人盯着，现在系统30分钟自动跟进，早高峰派单不再漏确认。"

**指令**（演示环境）：
```bash
ringclaw cron trigger --id 30min-confirm --bot orders-bot --dry-run
```

---

### 分钟 1–2：晨间超期派单批量处理（orders-bot）

**场景**：展示 morning-check Cron 的批量处理能力。

1. 模拟触发 morning-check：
   ```bash
   ringclaw cron trigger --id morning-check --bot orders-bot --dry-run
   ```
2. 展示 #dispatch-ops 输出：
   - TEXT 列表（3条超18小时未确认派单）
   - 自动创建的3个 URGENT 任务
   - 自动发出的3条 SMS（各队长）
3. 演示要点："以前 ops team 每天早上逐一检查，现在全自动，8点整就完成。"

---

### 分钟 2–3：Atlanta 运营日报 Card（tom-bot）

**场景**：展示 Heartbeat 从"文字摘要"升级为"结构化 Card"。

1. 模拟 tom-bot 17:30 Heartbeat：
   ```bash
   ringclaw heartbeat trigger --bot tom-bot --dry-run
   ```
2. 打开 #atlanta-ops，展示结构化 Card：
   - 今日完成：47 单 / 延迟：3 单
   - 明日预约：52 单
   - 班组缺口：ATL-12（缺2人）
   - 最久未处理 Task："电梯维修报告 - 已48小时"
3. 展示同时自动升级为 URGENT 的任务。
4. 演示要点："Tom 不用再写日报，Card 直接给所有人看结构化数据；超期任务也自动升级，不等 Tom 发现。"

---

### 分钟 3–4：Beth 周报 Card（beth-bot）

**场景**：展示高管周报从文字段落升级为结构化 Card，Beth 打开 DM 直接看数据。

1. 模拟 beth-bot 周一 09:00 Cron：
   ```bash
   ringclaw cron trigger --id exec-weekly --bot beth-bot --dry-run
   ```
2. 打开 Beth 的 DM，展示结构化周报 Card（不是文字段落）：
   - 33 家门店本周数据（完工率、NPS 评分、逾期率）
   - 关注门店高亮（红色）：ATL-07（NPS 跌至 3.2）、ATL-22（逾期率 18%）
   - 本周建议询问：
     1. ATL-07 NPS 连续两周下滑，是否需要启动门店审查？
     2. 分包商 Team-C 上周两次未准时，是否考虑替换？
     3. 旺季扩编进展 — 目前缺口 17 人，是否需要加快招聘？
3. 演示要点："以前 Beth 收到的是一段文字，需要自己找关注点。现在打开 DM 直接看高亮，议题也准备好了。"

---

### 分钟 4–5：Finance 月结 Card（finance-bot）

**场景**：展示月结从"3天手工整理"变为"Alex 点2次按钮"。

1. 模拟 month-end-close Cron（月28日 00:00 触发）：
   ```bash
   ringclaw cron trigger --id month-end-close --bot finance-bot --dry-run
   ```
2. 展示自动执行的5个步骤（系统日志）：
   ```
   [00:00:01] Step1 NOTE → Lowe's 对账台账 ✓
   [00:00:03] Step2 CARD → @alex（付款审批）✓
   [00:00:04] Step3 NOTE → 差旅分摊台账 ✓
   [00:00:06] Step4 CARD → @alex（成本超标预警）✓
   [00:00:08] Step5 CARD → #exec + @beth ✓
   ```
3. 打开 Alex 的消息界面，展示2张等待审批的 Card：
   - Card 1：付款审批（本月分包商总额 $184,000）→ [批准付款] 按钮
   - Card 2：成本超标预警（ATL-07 超标 23%）→ [通知 Karen 启动合同复审] 按钮
4. 打开 Beth DM，展示月度管理报告 Card。
5. Alex 点击2次按钮，月结完成。
6. 演示要点："以前月结需要 Alex 花3天手动整理5份报告。现在凌晨系统全跑完，Alex 早上来点2次按钮，Beth 已经有报告了。"

---

## §七 常见问题与排错

### Q1：Cron 中的 ACTION:SMS 没有发出

**可能原因**：
1. Bot 对应的 RingCentral 账号未绑定手机号权限。
2. 目标联系人手机号未在 Directory 中登记。

**排查命令**：
```bash
ringclaw user list --filter has-phone
ringclaw cron logs --id morning-check --bot orders-bot --tail 20
```

### Q2：Karen 点 Card 按钮后 SMS 没有发出

**可能原因**：
1. Lowe's 协调员手机号未在 Directory 中登记。
2. Bot 对应的 RingCentral 账号 SMS 权限不足（需联系 Platform Team）。

**排查命令**：
```bash
ringclaw card logs --bot karen-bot --action-type SMS --tail 10
```

### Q3：tom-bot Heartbeat 没有推送 Card

**可能原因**：
1. Heartbeat 的 `--actions` 中未包含 `ACTION:CARD`。
2. #atlanta-ops 频道 Bot 权限不足。

**排查命令**：
```bash
ringclaw heartbeat status --bot tom-bot
ringclaw heartbeat logs --bot tom-bot --tail 20
```

### Q4：finance-bot month-end-close 只执行了部分步骤

**可能原因**：
Cron prompt 中的多步骤 ACTION 需要按顺序执行，若某步骤 NOTE 写入失败会中断后续。

**排查命令**：
```bash
ringclaw cron logs --id month-end-close --bot finance-bot --verbose
```

---

## §八 版本变更记录

| 版本 | 日期 | 变更摘要 |
|---|---|---|
| v1.0 | 2026-06-03 | 初始版本，基于平台新 Cron/Heartbeat 能力规则（2026-06 生效）创建 |

---

*文档由 Platform Team 维护。如有问题请联系 #platform-ops 频道。*
