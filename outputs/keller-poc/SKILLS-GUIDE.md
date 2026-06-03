# SKILLS-GUIDE — Keller POC Bot 技能文档

> 版本：v2.0（2026-06-03）
> 本文档描述各 Bot 的 Skill 定义、触发方式与 ACTION 能力范围。

---

## §一 Skill 激活机制

Bot Skill 有三种激活方式：

### 方式1：用户主动 @ 触发（Command）

用户在对话中 @bot 并输入指令，Bot 根据匹配的 Skill 响应，可执行全部允许的 ACTION 类型（MESSAGE / NOTE / TASK / CARD / SMS 等）。

### 方式2：Heartbeat 触发（周期性后台运行）

系统按配置频率自动激活 Bot Skill，无需用户触发。

**Heartbeat 默认允许的 ACTION 类型：**
- MESSAGE — 发送消息到指定 Chat / DM
- NOTE — 写入台账 / 记录
- CARD — 推送结构化卡片
- TASK — 创建或更新任务

**Heartbeat 不含：**
- SMS — Heartbeat 不发短信
- VIDEO / PHONE_CALL — 需白名单开启，不在默认能力中

### 方式3：定时触发（Cron）

系统在指定时间（时刻 / 每日 / 每周 / 每月 / 每年）自动激活 Skill，无需用户触发。

**Cron 默认允许的 ACTION 类型：**
- MESSAGE — 向指定 Chat / 用户发消息
- NOTE — 写入台账 / 日志
- TASK — 创建或更新任务（含设置 URGENT 优先级）
- CARD — 推送结构化操作卡片（可含按钮，供人工一键确认）
- SMS — 向指定手机号发短信
- 跨 Chat ACTION — 可跨频道 / DM 推送内容

**Cron 不含（需特别注意）：**
- VIDEO / PHONE_CALL — 需白名单开启，不在默认能力中

> **旧规则已废除**：此前"Cron / Heartbeat 触发的 Agent 回复 → 纯文本，不执行 ACTION 块"的约束已正式废除。Cron 和 Heartbeat 现在可在各自的默认允许范围内执行 ACTION。

---

## §二 HR Bot Skills（共 7 个）

HR Bot 由 Linda 负责，管理招聘、培训、旺季扩编等 HR 相关流程。

### Skill 1：hire-request（招聘申请处理）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command（店长 @hr-bot） |
| 触发指令 | `/hire [门店] [岗位] [人数]` |
| 主要 ACTION | TASK（创建招聘任务）、NOTE（记录招聘台账）、MESSAGE（通知 Linda） |

**说明：** 店长提交招聘需求后，Bot 自动创建 TASK 并通知 Linda，同时在台账中记录申请详情。

---

### Skill 2：onboarding-checklist（入职清单）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command（HR @hr-bot） |
| 触发指令 | `/onboard [员工姓名] [门店] [开始日期]` |
| 主要 ACTION | TASK（创建入职任务列表）、CARD（入职进度 Card）、NOTE（台账记录） |

**说明：** 为新员工生成标准入职 Checklist，推送结构化 Card 跟踪进度。

---

### Skill 3：offboarding（离职处理）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command（HR @hr-bot） |
| 触发指令 | `/offboard [员工姓名] [门店] [离职日期]` |
| 主要 ACTION | TASK（创建离职任务）、NOTE（台账记录）、MESSAGE（通知相关人员） |

**说明：** 启动离职流程，创建离职清单任务，通知门店和系统管理员。

---

### Skill 4：attendance-summary（出勤汇总）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command 或 Heartbeat |
| 触发指令 | `/attendance [周期]` |
| 主要 ACTION | CARD（出勤汇总 Card）、NOTE（台账更新） |

**说明：** 生成指定周期的出勤数据汇总，推送 Card 展示异常出勤门店。

---

### Skill 5：performance-review（绩效评估）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command（Linda @hr-bot） |
| 触发指令 | `/perf-review [周期] [门店]` |
| 主要 ACTION | CARD（绩效 Card）、NOTE（台账记录）、TASK（跟进任务） |

**说明：** 汇总指定门店绩效数据，生成评估 Card 供 Linda 审阅。

---

### Skill 6：training-scheduler（培训排期）

| 属性 | 值 |
|------|-----|
| 触发方式 | **Cron（每季度第1日自动触发）** |
| Cron 时间 | 每年 1/1、4/1、7/1、10/1 |
| 主要 ACTION | **ACTION:EVENT**（为未完成培训人员创建下次培训场次）、**ACTION:SMS**（向未完成培训人员发送提醒短信） |

**说明（更新后）：**
- Cron 自动检查培训完成情况，对所有**未完成培训人员**自动执行：
  1. `ACTION:EVENT` — 在系统中为该人员创建下一季度培训场次预约
  2. `ACTION:SMS` — 向该人员手机发送培训提醒短信（含培训日期、地点、课程信息）
- Linda 无需手动触发，季度初自动完成全国培训排期与通知
- Bot 同时输出本季度未完成人员汇总文本，便于 Linda 存档

> 旧行为（已废除）：Cron 触发后仅输出纯 TEXT 提醒，需 Linda 手动跟进排期和通知。

---

### Skill 7：seasonal-crew-scaling（旺季人员扩编）

| 属性 | 值 |
|------|-----|
| 触发方式 | **Cron（每年 2月1日自动触发）** |
| Cron 时间 | 每年 02/01 |
| 主要 ACTION | **ACTION:MESSAGE**（向各门店店长发送旺季缺口查询消息）、**ACTION:NOTE**（初始化全国旺季缺口汇总台账） |

**说明（更新后）：**
- Cron 在 2/1 自动向**全国各门店店长**发送 `ACTION:MESSAGE`，内容包含：
  - 旺季用工缺口调查模板问题（岗位需求、时间段、人数估算）
  - 回复截止日期
- 同时执行 `ACTION:NOTE`，在台账中初始化当年旺季扩编汇总表（各门店行，待回填数据）
- Linda 次日收到各店回复汇总，无需逐一致电各门店询问

> 旧行为（已废除）：Cron 触发后仅输出纯 TEXT 通知 Linda，Linda 需手动逐店联系。

---

### HR Bot Heartbeat（可选，Linda 配置）

| 属性 | 值 |
|------|-----|
| 触发方式 | Heartbeat（Linda 自定义配置频率） |
| 主要 ACTION | ACTION:NOTE（月度培训完成率台账更新）、ACTION:CARD（未完成人员汇总 Card → Linda DM） |

**说明：** Linda 可选开启 Heartbeat，定期自动更新培训台账并推送未完成人员汇总 Card 到她的 DM，无需手动查询。

---

## §三 Finance Bot Skills（共 5 个）

Finance Bot 由 Alex 负责，管理分包商付款、Lowe's 对账、月结等财务流程。

### Skill 8：lowe's-payment-reconciliation（Lowe's 付款对账）

| 属性 | 值 |
|------|-----|
| 触发方式 | **Cron（每月5日自动触发）** |
| Cron 时间 | 每月 05 日 |
| 主要 ACTION | **ACTION:CARD**（逾期应收明细 Card → Alex）、**ACTION:NOTE**（对账台账追加月度行） |

**说明（更新后）：**
- Cron 在每月5日自动执行：
  1. `ACTION:CARD` — 向 Alex 推送逾期应收明细 Card，Card 包含：
     - 本月 Lowe's 逾期未付款项明细（项目号、金额、逾期天数）
     - **[发催款 SMS 给协调员]** 操作按钮（点击后触发实际 SMS 发送，仍需人工确认）
  2. `ACTION:NOTE` — 在对账台账中追加本月度对账行（日期、应收、实收、差额）
- Alex 只需在收到 Card 时点按钮确认是否发催款 SMS，无需手动整理数据

> 旧行为（已废除）：Cron 触发后仅输出纯 TEXT 对账报告，Alex 需手动操作后续步骤。

---

### Skill 9：subcontractor-payment（分包商付款审批）

| 属性 | 值 |
|------|-----|
| 触发方式 | **Cron（每周四 15:00 自动触发）** |
| Cron 时间 | 每周四 15:00 |
| 主要 ACTION | **ACTION:CARD**（付款审批 Card → Alex） |

**说明（更新后）：**
- Cron 在每周四 15:00 自动向 Alex 推送付款审批 Card，Card 包含：
  - 本周待支付分包商清单（姓名、工程项目、金额、工时汇总）
  - **[批准付款]** 操作按钮
- Alex 点击 [批准付款] 后系统自动处理付款流程
- 无需 Alex 主动查看列表或手动操作，Card 直接送达 DM 等待审批

> 旧行为（已废除）：Cron 触发后输出纯 TEXT 付款清单，需 Alex 看到后手动操作付款流程。

---

### Skill 10：expense-reconciliation（费用对账）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command（Alex @finance-bot） |
| 触发指令 | `/expense-recon [周期] [门店]` |
| 主要 ACTION | CARD（费用对账 Card）、NOTE（台账记录） |

**说明：** 生成指定周期的差旅与运营费用对账，推送 Card 供 Alex 审核。

---

### Skill 11：invoice-tracking（发票跟踪）

| 属性 | 值 |
|------|-----|
| 触发方式 | Command 或 Heartbeat |
| 触发指令 | `/invoice-track [客户] [状态]` |
| 主要 ACTION | CARD（发票状态 Card）、NOTE（台账更新）、TASK（逾期跟进任务） |

**说明：** 跟踪未结发票状态，对逾期发票自动创建跟进 TASK 并推送汇总 Card。

---

### Skill 12：month-end-close（月结自动化）

| 属性 | 值 |
|------|-----|
| 触发方式 | **Cron（每月28日自动触发，执行完整5步月结流程）** |
| Cron 时间 | 每月 28 日 |
| 主要 ACTION | 见下方5步流程 |

**月结5步执行流程（更新后，全部由 Cron 自动执行）：**

| 步骤 | ACTION 类型 | 内容 | 是否需人工 |
|------|------------|------|-----------|
| Step 1 | **ACTION:NOTE** | 对账台账汇总行追加（本月总收入、总支出、净利润） | 自动，无需等待 |
| Step 2 | **ACTION:CARD** | 付款审批 Card → Alex，含 **[批准付款]** 按钮 | Alex 点按钮批准 |
| Step 3 | **ACTION:NOTE** | 差旅分摊明细追加台账（各门店费用分摊明细行） | 自动 |
| Step 4 | **ACTION:CARD** | 成本超标预警 Card → Alex，超标门店高亮，含 **[通知 Karen 启动合同复审]** 按钮 | Alex 点按钮确认 |
| Step 5 | **ACTION:CARD** | 月度管理报告 Card → #exec 频道 + Beth DM（含关键指标、门店排名、本月亮点） | 自动推送 |

**场景说明（更新后）：**
- Cron 在月28日自动完成全部5步，无需 Alex 手动触发任何步骤
- Alex 早上来时，发现 `#finance` 有 2 张 Card 等待审批（Step 2 付款审批 + Step 4 成本超标确认），Beth DM 有管理报告 Card（Step 5）
- 整个月结从"3天手工整理"变成"Alex 点 2 次按钮"

> 旧行为（已废除）：Cron 触发后输出纯 TEXT 提醒，Alex 需手动执行全部5个步骤，耗时约3天。

---

## §四 Skills 全景图 — 按触发方式分类

### 4.1 Command 触发（用户主动发起）

| Bot | Skill | 指令 | 主要 ACTION |
|-----|-------|------|------------|
| hr-bot | hire-request | `/hire` | TASK、NOTE、MESSAGE |
| hr-bot | onboarding-checklist | `/onboard` | TASK、CARD、NOTE |
| hr-bot | offboarding | `/offboard` | TASK、NOTE、MESSAGE |
| hr-bot | performance-review | `/perf-review` | CARD、NOTE、TASK |
| finance-bot | expense-reconciliation | `/expense-recon` | CARD、NOTE |
| finance-bot | invoice-tracking | `/invoice-track` | CARD、NOTE、TASK |
| orders-bot | dispatch-confirm | `/confirm-dispatch` | TASK、SMS、NOTE |
| karen-bot | batch-sms-notify | `/lowes-batch` | SMS、NOTE、CARD |
| tom-bot | ops-query | `/ops-query` | CARD、NOTE |

### 4.2 Heartbeat 触发（周期性自动运行）

| Bot | Skill | 频率 | 主要 ACTION |
|-----|-------|------|------------|
| tom-bot | daily-summary | 17:30 每日 | **TEXT 摘要 + ACTION:CARD**（结构化日报 Card → #atlanta-ops）+ **ACTION:TASK**（超期任务升级 URGENT） |
| hr-bot | training-completion-tracker | Linda 自定义 | **ACTION:NOTE**（培训完成率台账）+ **ACTION:CARD**（未完成汇总 Card → Linda DM） |
| finance-bot | invoice-tracking | 每日 | CARD（发票状态）、NOTE、TASK |
| hr-bot | attendance-summary | 每周 | CARD（出勤汇总）、NOTE |

> 注意：Heartbeat 不含 SMS、VIDEO、PHONE_CALL。

### 4.3 Cron 触发（指定时间自动执行，含 ACTION）

| Bot | Skill | Cron 时间 | 主要 ACTION（更新后） |
|-----|-------|----------|---------------------|
| orders-bot | morning-check | 每日 08:00 | TEXT 列表 + **ACTION:TASK**（URGENT，未确认派单）+ **ACTION:SMS**（向队长发提醒） |
| orders-bot | 30min-confirm（新增） | 派单后30分钟 | **ACTION:TASK**（URGENT，标记未确认）+ **ACTION:SMS**（第二次确认请求） |
| tom-bot | — | 由 Heartbeat 替代 | 见 Heartbeat 行 |
| karen-bot | batch-sms-notify | 每日 17:00 | TEXT 清单 + **ACTION:CARD**（SMS 通知准备 Card，含"执行批量 SMS 通知"按钮）+ **ACTION:NOTE**（SLA 台账摘要） |
| karen-bot | lowes-sla-weekly | 每周五 17:00 | **ACTION:CARD**（Lowe's SLA 周报 Card）+ **ACTION:NOTE**（台账追加周汇总行） |
| beth-bot | exec-weekly | 每周一 09:00 | **ACTION:CARD**（结构化周报 Card，关注门店高亮 → Beth DM） |
| finance-bot | subcontractor-payment | 每周四 15:00 | **ACTION:CARD**（付款审批 Card → Alex，含 [批准付款] 按钮） |
| finance-bot | lowe's-payment-reconciliation | 每月 05 日 | **ACTION:CARD**（逾期应收明细 Card → Alex，含 [发催款 SMS] 按钮）+ **ACTION:NOTE**（对账台账月度行） |
| finance-bot | month-end-close | 每月 28 日 | 5步自动执行：**NOTE×2 + CARD×3**（付款审批 + 成本预警 + 月报，Alex 仅需点2次按钮） |
| hr-bot | seasonal-crew-scaling | 每年 02/01 | **ACTION:MESSAGE**（向各店发旺季缺口查询）+ **ACTION:NOTE**（缺口汇总台账初始化） |
| hr-bot | training-scheduler | 每季度 1 日 | **ACTION:EVENT**（创建培训场次）+ **ACTION:SMS**（向未完成人员发提醒短信） |

---

## §五 ACTION 类型说明与权限范围

| ACTION 类型 | 说明 | Command | Heartbeat | Cron |
|------------|------|---------|-----------|------|
| MESSAGE | 发送消息到 Chat / DM | ✓ | ✓ | ✓ |
| NOTE | 写入台账 / 日志记录 | ✓ | ✓ | ✓ |
| TASK | 创建/更新任务（含 URGENT 优先级） | ✓ | ✓ | ✓ |
| CARD | 推送结构化操作卡片（可含按钮） | ✓ | ✓ | ✓ |
| SMS | 向手机号发短信 | ✓ | ✗ | ✓ |
| EVENT | 创建日历 / 培训场次 | ✓ | ✓ | ✓ |
| VIDEO | 视频通话 | 白名单 | 白名单 | 白名单 |
| PHONE_CALL | 语音通话 | 白名单 | 白名单 | 白名单 |

---

## §六 一句话总结表格

| Bot | Skill | 触发方式 | 自动化效果（更新后） |
|-----|-------|---------|-------------------|
| hr-bot | training-scheduler | Cron 每季度 | 自动为未完成培训员工创建场次预约 + 发 SMS 提醒，Linda 无需手动排期 |
| hr-bot | seasonal-crew-scaling | Cron 每年 2/1 | 自动向全国各店发 MESSAGE 查询旺季缺口 + 初始化台账，Linda 次日直接看汇总 |
| hr-bot | training-completion-tracker | Heartbeat | 自动更新培训台账 + 推 Card 给 Linda，无需手动查询 |
| finance-bot | lowe's-payment-reconciliation | Cron 每月 5 日 | 自动推逾期应收 Card（含一键催款 SMS 按钮）+ 台账记录，Alex 点按钮即完成 |
| finance-bot | subcontractor-payment | Cron 每周四 | 自动推付款审批 Card（含批准按钮）到 Alex DM，无需 Alex 主动查清单 |
| finance-bot | month-end-close | Cron 每月 28 日 | 5步月结全自动执行，Alex 仅需点 2 次按钮，整个月结从"3天手工"变"2次点击" |
| orders-bot | morning-check | Cron 每日 08:00 | 自动创建超时未确认派单 URGENT 任务 + SMS 通知队长，无需人工筛查 |
| orders-bot | 30min-confirm | Cron 派单后30分钟 | 自动二次确认提醒，创建 URGENT 任务 + 发 SMS，关闭确认盲区 |
| karen-bot | batch-sms-notify | Cron 每日 17:00 | 推 SMS 通知准备 Card（含一键执行按钮）+ 台账更新，比输命令更自然，比全自动更安全 |
| karen-bot | lowes-sla-weekly | Cron 每周五 | 自动推 Lowe's SLA 周报 Card + 台账周汇总，Karen 无需手动整理周报 |
| beth-bot | exec-weekly | Cron 每周一 | 自动推结构化周报 Card 到 Beth DM，关注门店高亮，Beth 一眼掌握全局 |
| tom-bot | daily-summary | Heartbeat 17:30 | 结构化日报 Card 发 #atlanta-ops + 超期任务自动升级 URGENT，Tom 不再依赖文字摘要 |

---

## §七 场景叙事速览

### Case 2：tom-bot 日摘要（Heartbeat 17:30）

**更新前：** Heartbeat 17:30 触发，#atlanta-ops 收到一段文字摘要，Tom 阅读后手动处理超期任务。

**更新后：** Heartbeat 17:30 触发，Tom 在 #atlanta-ops 看到结构化 Card，Card 包含：
- 今日完成派单数 / 延迟派单数
- 明日预约汇总
- 班组缺口提示
- 最久未处理 Task（含负责人）

超期 Task 在 Cron 触发时自动升级为 URGENT，不等 Tom 手动处理。

---

### Case 3：beth-bot 周报（Cron 周一 09:00）

**更新前：** Cron 周一 09:00 触发，Beth DM 收到文字段落格式的周报，需逐段阅读。

**更新后：** Cron 周一 09:00 推送结构化 Card 到 Beth DM，Card 包含：
- 全国33店核心数据（完成率、收入、超期单）
- 关注门店高亮（指标异常门店自动标记）
- 本周建议询问事项

Beth 一眼掌握全国态势，无需逐行阅读文字。

---

### Case 4：karen-bot 批量 SMS 通知（Cron 17:00）

**更新前：** Cron 17:00 触发，输出纯文字清单，Karen 看到后需手动输入 `/lowes-batch` 触发操作。

**更新后：** Cron 17:00 推送 SMS 通知准备 Card（不是文字），Card 包含当日完工通知清单明细 + **[执行批量 SMS 通知]** 按钮。Karen 点按钮触发，比输入命令更自然，比全自动更安全（SMS 仍需一次人工确认）。

> 注意：Cron 推送 CARD 作为中间层，让 Karen 完成人工确认后再发批量 SMS 通知。

---

### Case 5：finance-bot 月结（Cron 每月 28 日）

**更新前：** Cron 月28日触发，输出 TEXT 提醒，Alex 需手动执行5个步骤，耗时约3天。

**更新后：** Cron 月28日自动完成全部5步：
1. 台账汇总自动写入（NOTE）
2. 付款审批 Card 推给 Alex（Card + [批准付款] 按钮）
3. 差旅分摊明细自动写入台账（NOTE）
4. 成本超标预警 Card 推给 Alex（Card + [通知 Karen 启动合同复审] 按钮）
5. 月度管理报告 Card 推给 #exec + Beth DM（CARD）

Alex 早上来发现 `#finance` 有 2 张 Card 等待批准，Beth DM 有管理报告 Card。整个月结从"3天手工整理"变成"Alex 点2次按钮"。

---

### Case 6：hr-bot 旺季扩编（Cron 每年 2/1）

**更新前：** Cron 2/1 触发，输出 TEXT 通知 Linda，Linda 需逐一致电各门店询问旺季用工缺口。

**更新后：** Cron 2/1 自动向全国各门店店长发送 MESSAGE，内含旺季用工调查模板。同时在台账初始化当年旺季扩编汇总表。Linda 次日收到各店回复汇总，无需逐个打电话询问，节省约1天的信息收集工作。
