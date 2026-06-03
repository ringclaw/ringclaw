# Keller POC — 完整文档

---

## 一、背景与目标

本文档记录 Keller 项目 POC 阶段的完整设计与实现规格，涵盖平台约束、各 Bot 的 SOUL 配置、以及关键业务场景的端到端描述。

---

## 二、平台规格

### 2.1 核心概念

- **Bot**：绑定特定频道或 DM，具备 SOUL 配置的自动化代理
- **ACTION**：Bot 可执行的结构化操作，类型包括 MESSAGE / NOTE / TASK / CARD / SMS / EVENT / PHONE_CALL / VIDEO 等
- **Cron**：按计划时间触发的定时任务
- **Heartbeat**：周期性轮询触发的保活任务
- **CARD**：结构化富文本卡片，可包含按钮供人工点击触发后续操作

### 2.2 三个关键平台约束

**约束1（已更新）：Cron / Heartbeat 默认允许的 ACTION 类型**

| 触发类型 | 默认允许的 ACTION |
|---|---|
| **Cron** | MESSAGE / NOTE / TASK / CARD / SMS / 跨chat ACTION |
| **Heartbeat** | MESSAGE / NOTE / CARD / TASK（不含 SMS） |
| **VIDEO / PHONE_CALL** | 需白名单开启，不在默认能力中 |

**约束2：Bot 身份隔离**

每个 Bot 仅能操作其绑定的频道及被授权的跨 chat 目标，不能越权访问其他 Bot 的数据源。

**约束3：人工确认门槛**

涉及资金流转（付款）等高风险操作，平台要求至少一次人工确认（点击 Card 按钮），Bot 不得全自动执行。

---

## 三、Bot 清单

| Bot | 主频道 | 核心职责 |
|---|---|---|
| orders-bot | #orders | 派单跟踪、确认催收 |
| tom-bot | #atlanta-ops | Atlanta 区日常运营摘要 |
| karen-bot | #admin | Lowe's 完工通知管理、SLA 台账、Lowe's 对接 |
| beth-bot | Beth DM | 高管周报 |
| finance-bot | #finance | 付款审批、对账、月结 |
| hr-bot | #hr | 人员扩编、培训跟踪 |

---

## 四、数据模型（简述）

- **Order**：派单记录，含状态（pending / confirmed / completed）、关联队长、时间戳
- **Task**：任务项，含优先级（NORMAL / URGENT）、负责人、截止时间、状态
- **SLA Record**：SLA 台账行，含日期、指标值、汇总标记
- **Training Record**：培训记录，含人员、完成状态、培训日期

---

## 五、各 Bot SOUL 配置

### 5.1 平台 SOUL 模板结构

```yaml
soul:
  identity: <bot名称和角色>
  channel: <主频道>
  triggers:
    - type: <cron|heartbeat|message|command>
      schedule: <cron表达式或间隔>
      actions: [<允许的ACTION类型列表>]
  constraints:
    - <约束描述>
```

---

### 5.2 orders-bot SOUL

**身份**：派单跟踪机器人，负责监控所有待确认派单，确保队长在规定时间内回确。

**触发器配置**：

#### morning-check Cron（每日 08:00）

```yaml
trigger:
  type: cron
  schedule: "0 8 * * *"
  name: morning-check
  actions:
    - TEXT        # 列出超18小时未确认派单清单
    - ACTION:TASK # 为每个未确认派单创建 URGENT 任务
    - ACTION:SMS  # 向对应队长发送提醒短信
```

**行为描述**：
1. 查询所有状态为 `pending` 且创建时间超过 18 小时的派单
2. 在频道发送 TEXT 列表，列出所有超期未确认派单
3. 为每条超期派单执行 `ACTION:TASK`，创建优先级为 URGENT 的跟踪任务，指派给对应队长
4. 执行 `ACTION:SMS`，向对应队长发送催确认短信

#### 30min-confirm Cron（派单后 30 分钟触发一次）

```yaml
trigger:
  type: cron
  schedule: "*/30 * * * *"
  name: 30min-confirm
  actions:
    - ACTION:TASK # 创建/更新 URGENT 任务，标记未确认状态
    - ACTION:SMS  # 向队长发送第二次确认请求
```

**行为描述**：
1. 检查最近 30 分钟内创建但仍未确认的派单
2. 执行 `ACTION:TASK`，创建 URGENT 任务标记未确认状态
3. 执行 `ACTION:SMS`，向队长发送第二次确认请求短信

**约束**：
- 仅处理未确认（`pending`）状态派单
- SMS 发送频率限制：同一队长同一派单每小时最多 2 条

---

### 5.3 tom-bot SOUL

**身份**：Atlanta 区运营摘要机器人，每日下班前汇总当日运营状态并推送至 #atlanta-ops。

**触发器配置**：

#### Heartbeat（每日 17:30）

```yaml
trigger:
  type: heartbeat
  schedule: "30 17 * * *"
  name: daily-summary
  actions:
    - TEXT        # 日摘要文字说明
    - ACTION:CARD # 结构化日报卡片推送至 #atlanta-ops
    - ACTION:TASK # 为超期未处理任务升级为 URGENT
```

**行为描述**：
1. 汇总当日数据：完成派单数、延迟派单数、明日预约数、班组缺口、最久未处理 Task
2. 在 #atlanta-ops 发送 TEXT 摘要说明
3. 执行 `ACTION:CARD`，推送结构化日报 Card 至 #atlanta-ops，Card 内容包含：
   - 今日完成数
   - 今日延迟数
   - 明日预约数
   - 班组缺口统计
   - 最久未处理 Task 高亮
4. 执行 `ACTION:TASK`，将所有超期未处理任务优先级自动升级为 URGENT（无需等待 Tom 手动处理）

**注意**：Heartbeat 不含 SMS 权限，本 Bot 不发送短信。

---

### 5.4 karen-bot SOUL

**身份**：行政对接机器人，负责 Lowe's 完工通知管理（批量 ACTION:SMS + ACTION:MESSAGE）、SLA 台账维护。

**触发器配置**：

#### Cron（每日 17:00，batch-prep）

```yaml
trigger:
  type: cron
  schedule: "0 17 * * *"
  name: batch-prep
  actions:
    - TEXT        # 完工通知清单文字说明
    - ACTION:CARD # 完工通知准备 Card，含"发送批量完工通知"按钮
    - ACTION:NOTE # 更新 SLA 台账当日摘要行
```

**行为描述**：
1. 汇总当日待发完工通知列表
2. 在频道发送 TEXT 清单说明
3. 执行 `ACTION:CARD`，推送完工通知准备 Card，Card 包含：
   - 当日待发完工通知清单明细
   - [发送批量完工通知] 按钮（Karen 点击后触发实际通知发送）
4. 执行 `ACTION:NOTE`，更新 SLA 台账当日摘要行

**注意**：批量完工通知（ACTION:SMS + ACTION:MESSAGE）仍需 Karen 点击 Card 按钮或执行 `/lowes-batch` 命令手动触发，不会全自动发出。

#### Cron（每周五 17:00，lowe's-sla-weekly）

```yaml
trigger:
  type: cron
  schedule: "0 17 * * 5"
  name: lowes-sla-weekly
  actions:
    - ACTION:CARD # Lowe's SLA 周报 Card
    - ACTION:NOTE # 台账追加周汇总行
```

**行为描述**：
1. 执行 `ACTION:CARD`，推送 Lowe's SLA 周报 Card，包含本周 SLA 达成情况
2. 执行 `ACTION:NOTE`，在 SLA 台账追加本周汇总行

---

### 5.5 beth-bot SOUL

**身份**：高管报告机器人，每周一向 Beth 推送结构化周报 Card。

**触发器配置**：

#### Cron（每周一 09:00，exec-weekly）

```yaml
trigger:
  type: cron
  schedule: "0 9 * * 1"
  name: exec-weekly
  actions:
    - ACTION:CARD # 结构化周报 Card，直接推送至 Beth DM
```

**行为描述**：
1. 汇总全国 33 家门店上周运营数据
2. 执行 `ACTION:CARD`，推送结构化周报 Card 到 Beth DM，Card 内容包含：
   - 33 店数据总览
   - 关注门店高亮（异常/超标门店标红）
   - 本周建议询问事项

---

### 5.6 finance-bot SOUL

**身份**：财务自动化机器人，负责分包商付款审批、Lowe's 对账、月度结账流程。

**触发器配置**：

#### Cron（每周四 15:00，subcontractor-payment）

```yaml
trigger:
  type: cron
  schedule: "0 15 * * 4"
  name: subcontractor-payment
  actions:
    - ACTION:CARD # 付款审批 Card → Alex，含付款清单明细 + [批准付款] 按钮
```

**行为描述**：
1. 汇总本周待付分包商款项
2. 执行 `ACTION:CARD`，推送付款审批 Card 给 Alex，Card 包含：
   - 付款清单明细
   - [批准付款] 按钮

#### Cron（每月 5 日，lowe's-payment-reconciliation）

```yaml
trigger:
  type: cron
  schedule: "0 9 5 * *"
  name: lowes-payment-reconciliation
  actions:
    - ACTION:CARD # 逾期应收明细 Card → Alex，含[发催款通知给 Lowe's 协调员] 按钮
    - ACTION:NOTE # 对账台账追加月度行
```

**行为描述**：
1. 执行 `ACTION:CARD`，推送逾期应收明细 Card 给 Alex，Card 包含：
   - 逾期应收款明细
   - [发催款通知给 Lowe's 协调员] 按钮
2. 执行 `ACTION:NOTE`，在对账台账追加本月度行

#### Cron（每月 28 日，month-end-close）

```yaml
trigger:
  type: cron
  schedule: "0 8 28 * *"
  name: month-end-close
  actions:
    - ACTION:NOTE  # Step1: 对账台账汇总追加
    - ACTION:CARD  # Step2: 付款审批 Card → Alex
    - ACTION:NOTE  # Step3: 差旅分摊明细追加台账
    - ACTION:CARD  # Step4: 成本超标预警 Card → Alex
    - ACTION:CARD  # Step5: 月度管理报告 Card → #exec + Beth DM
```

**行为描述（5步全自动流程）**：
1. **Step 1** `ACTION:NOTE`：对账台账汇总行追加 → 自动执行，无需等待
2. **Step 2** `ACTION:CARD`：推送付款审批 Card 给 Alex，含 [批准付款] 按钮 → 推送后继续执行后续步骤
3. **Step 3** `ACTION:NOTE`：差旅分摊明细追加台账 → 自动执行
4. **Step 4** `ACTION:CARD`：推送成本超标预警 Card 给 Alex，超标门店高亮，含 [通知 Karen 启动合同复审] 按钮 → 推送后继续
5. **Step 5** `ACTION:CARD`：推送月度管理报告 Card 至 #exec 频道及 Beth DM → 自动完成

**Alex 只需**：在收到 Card 时点击按钮批准，不需要主动触发任何步骤。整个月结从"3天手工整理"变成"Alex 点 2 次按钮"。

---

### 5.7 hr-bot SOUL

**身份**：人力资源自动化机器人，负责旺季人员扩编通知、季度培训跟踪。

**触发器配置**：

#### Cron（每年 2 月 1 日，seasonal-crew-scaling）

```yaml
trigger:
  type: cron
  schedule: "0 9 1 2 *"
  name: seasonal-crew-scaling
  actions:
    - ACTION:MESSAGE # 向各门店店长发送旺季缺口查询（含模板问题）
    - ACTION:NOTE    # 全国缺口汇总台账初始化
```

**行为描述**：
1. 执行 `ACTION:MESSAGE`，向全国各门店店长发送 MESSAGE，查询旺季人员缺口，消息包含标准化模板问题
2. 执行 `ACTION:NOTE`，初始化全国人员缺口汇总台账

#### Cron（每季度第 1 日，quarterly-training）

```yaml
trigger:
  type: cron
  schedule: "0 9 1 1,4,7,10 *"
  name: quarterly-training
  actions:
    - ACTION:EVENT # 为未完成培训人员创建下次培训场次
    - ACTION:SMS   # 向未完成培训人员发送提醒短信
```

**行为描述**：
1. 查询上季度培训未完成人员名单
2. 执行 `ACTION:EVENT`，为未完成人员创建下次培训场次
3. 执行 `ACTION:SMS`，向未完成培训人员发送培训提醒短信

#### Heartbeat（可选，Linda 配置）

```yaml
trigger:
  type: heartbeat
  schedule: "0 9 1 * *"   # 每月1日，或 Linda 自定义周期
  name: training-progress
  actions:
    - ACTION:NOTE # 月度培训完成率台账更新
    - ACTION:CARD # 未完成人员汇总 Card → Linda DM
```

**行为描述**：
1. 执行 `ACTION:NOTE`，更新月度培训完成率台账
2. 执行 `ACTION:CARD`，推送未完成人员汇总 Card 到 Linda DM

---

## 六、关键场景（Case）

### Case 1：新派单确认流程

**触发**：队长在 #orders 收到新派单通知

**流程**：
1. orders-bot 发送派单详情至 #orders
2. 队长在频道回复确认或使用 `/confirm <order-id>`
3. orders-bot 更新派单状态为 `confirmed`，记录确认时间
4. 若 30 分钟内未确认：30min-confirm Cron 触发，创建 URGENT Task + 发 SMS 给队长
5. 若至次日 08:00 仍未确认：morning-check Cron 触发，再次创建 URGENT Task + SMS 催收

**结果**：所有未确认派单均有 Task 跟踪和 SMS 催收，不会静默丢失。

---

### Case 2：tom-bot 日摘要（Heartbeat 17:30）

**触发**：每日 17:30 Heartbeat 自动触发

**旧行为**：tom-bot 仅在 #atlanta-ops 输出纯文字摘要段落，Tom 看到文字后手动判断是否需要处理超期任务。

**新行为**：
1. tom-bot 在 #atlanta-ops 发送 TEXT 摘要说明
2. 同时推送结构化 **CARD** 至 #atlanta-ops，Card 包含：
   - 今日完成派单数
   - 今日延迟派单数
   - 明日预约数
   - 班组缺口统计
   - 最久未处理 Task 高亮显示
3. 超期 Task **自动升级为 URGENT**，无需等 Tom 手动操作

**Tom 的体验**：打开 #atlanta-ops，看到结构化 Card 一目了然，超期任务已自动升级，Tom 只需确认或进一步跟进。

---

### Case 3：beth-bot 周报（Cron 周一 09:00）

**触发**：每周一 09:00 Cron 自动触发

**旧行为**：beth-bot 向 Beth DM 发送纯文字周报段落，Beth 需阅读全文后自行判断关注点。

**新行为**：
1. Cron 触发后，beth-bot 直接推送结构化 **CARD** 到 Beth DM
2. Card 包含：
   - 全国 33 店数据总览
   - 关注门店高亮（异常/超标门店标红）
   - 本周建议询问事项

**Beth 的体验**：周一早上打开 DM，看到结构化周报 Card，关注门店一眼可见，不需要通读长文段落。

---

### Case 4：karen-bot 完工通知批次（Cron 17:00）

**触发**：每日 17:00 Cron 自动触发

**旧行为**：karen-bot 输出纯文字清单，Karen 需阅读清单后手动输入 `/lowes-batch` 命令触发通知发送。

**新行为**：
1. Cron 触发后，karen-bot 推送 **CARD** 至频道（不再只是文字清单）
2. Card 包含：
   - 当日待发完工通知清单明细
   - [发送批量完工通知] 按钮
3. Karen 点击 Card 上的 [发送批量完工通知] 按钮即可触发实际通知发送

**Karen 的体验**：比输入命令更自然（直接点按钮），比全自动更安全（批量完工通知仍需一次人工确认点击，不会自动发出）。

**注意**：批量完工通知（ACTION:SMS + ACTION:MESSAGE）需 Karen 点击 Card 按钮触发（除手动 `/lowes-batch`）。

---

### Case 5：Finance 月结（5步自动化，月 28 日）

**触发**：每月 28 日 08:00 Cron 自动触发

**旧行为**：finance-bot 发送纯文字提醒，Alex 需手动执行 5 个步骤（整理对账、审批付款、分摊差旅、核查成本、生成报告），通常需要 2-3 天。

**新行为（全自动 5 步流程）**：

| 步骤 | ACTION | 内容 | 需人工？ |
|---|---|---|---|
| Step 1 | ACTION:NOTE | 对账台账汇总行追加 | 否，自动 |
| Step 2 | ACTION:CARD | 付款审批 Card → Alex，含 [批准付款] 按钮 | 是，Alex 点按钮 |
| Step 3 | ACTION:NOTE | 差旅分摊明细追加台账 | 否，自动 |
| Step 4 | ACTION:CARD | 成本超标预警 Card → Alex，超标门店高亮，含 [通知 Karen 启动合同复审] 按钮 | 是，Alex 点按钮 |
| Step 5 | ACTION:CARD | 月度管理报告 Card → #exec 频道 + Beth DM | 否，自动推送 |

**Alex 的体验**：早上来到办公室，发现 #finance 有 2 张 Card 等待批准（付款审批 + 成本超标确认），Beth DM 有管理报告 Card。整个月结从"3天手工整理"变成"Alex 点 2 次按钮"。

---

### Case 6：HR 旺季扩编（每年 2 月 1 日）

**触发**：每年 2 月 1 日 09:00 Cron 自动触发

**旧行为**：hr-bot 向 Linda 发送纯文字通知，Linda 需逐个联系各门店店长了解旺季人员缺口。

**新行为**：
1. Cron 触发后，hr-bot 自动执行 `ACTION:MESSAGE`，向全国各门店店长发送标准化缺口查询消息，消息包含模板问题（如：预计旺季新增需求人数、岗位类型、到岗时间要求）
2. 同时执行 `ACTION:NOTE`，初始化全国人员缺口汇总台账

**Linda 的体验**：2 月 2 日来上班，发现各门店店长已在系统中回复缺口数据，无需逐个打电话询问，直接查看汇总台账即可开始扩编决策。

---

## 七、部署与测试

### 7.1 POC 阶段优先级

| 优先级 | Bot | 场景 |
|---|---|---|
| P0 | orders-bot | 派单确认（morning-check + 30min-confirm） |
| P0 | tom-bot | 日摘要 CARD（Heartbeat 17:30） |
| P1 | karen-bot | 完工通知批次 CARD（Cron 17:00） |
| P1 | beth-bot | 周报 CARD（Cron 周一 09:00） |
| P2 | finance-bot | 月结 5 步自动化（Cron 月 28 日） |
| P2 | hr-bot | 旺季扩编 MESSAGE（Cron 2/1） |

### 7.2 验收标准

- Cron 触发后，对应 ACTION 在 60 秒内完成推送
- CARD 按钮点击后，后续 ACTION 在 30 秒内响应
- SMS 发送成功率 ≥ 99%
- NOTE 写入台账无重复行

---

## 八、附录

### 8.1 ACTION 类型说明

| ACTION | 说明 | 默认允许 |
|---|---|---|
| MESSAGE | 发送消息到指定频道或 DM | Cron / Heartbeat |
| NOTE | 写入结构化记录到台账 | Cron / Heartbeat |
| TASK | 创建或更新任务项 | Cron / Heartbeat |
| CARD | 推送结构化富文本卡片 | Cron / Heartbeat |
| SMS | 发送短信 | Cron（不含 Heartbeat） |
| EVENT | 创建日历事件/培训场次 | Cron |
| VIDEO | 视频通话 | 需白名单 |
| PHONE_CALL | 电话呼出 | 需白名单 |

### 8.2 变更历史

| 版本 | 日期 | 变更说明 |
|---|---|---|
| v1.0 | 初始版本 | 基础 Bot 配置，Cron/Heartbeat 仅纯文本 |
| v2.0 | 当前版本 | 平台升级：Cron/Heartbeat 支持 ACTION 块；各 Bot SOUL 更新；新增月结自动化场景 |
