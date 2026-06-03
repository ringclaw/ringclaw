# Skills 使用指南

12 个 Skill，2 个 Bot（hr-bot · finance-bot），完整的激活机制与应用说明。

---

## 一、Skill 是什么，怎么运作

### Skill 在系统里的位置

```
每条消息到达时，System Prompt 的组装顺序：

  ┌─────────────────────────────────────────┐
  │ Slot 1: SOUL.md（身份，≤80 行）          │  ← 固定不变
  │ Slot 2: Skills Index（所有 skill 的      │  ← 固定不变
  │          名称 + 1 行描述，compact）       │
  │ Slot 3: DOMAIN.md（领域知识，冻结）      │  ← 固定不变
  │ Slot 4: OWNER.md（个人偏好，冻结）       │  ← 固定不变
  │ Slot 5: Chat Memory（当前 chat 状态）    │  ← 每条消息读取
  │ Slot 6: Active Skill（当前激活的 Skill） │  ← 按需注入
  │          = SKILL.md 全文                 │
  │ Slot 7: Entity Memory（当前业务对象）    │  ← 有 entity_id 才注入
  └─────────────────────────────────────────┘
```

**关键机制**：
- Slots 1-4 在 Session 开始时**冻结一次**（Frozen Snapshot），之后不变 → prefix cache 稳定
- Slot 2（Skills Index）只有名称和描述，不占空间
- Slot 6（Active Skill）只在**需要时**展开 SKILL.md 全文
- 这样 SOUL 保持 ≤80 行，不会因为 Skill 内容增加而截断

### Skill 激活的三种方式

```
方式 1：意图检测（自动）
  用户消息包含 Skill 的触发关键词
  Agent 读到 SOUL 里的 "skills:" 声明 + Skills Index
  → 判断应激活哪个 Skill → 注入该 Skill 的 SKILL.md 全文

方式 2：Agent 路由（自动）
  另一个 Bot 发来包含路由标签的消息
  [AGENT_ROUTE:HIRING_REQUEST] → hiring-jd-generator
  [AGENT_ROUTE:COMPLAINT]      → complaint-investigation

方式 3：定时触发（Cron）
  Cron 的 prompt 直接指向某个 Skill
  "触发 month-end-close skill，开始月结流程"
```

### Skill 与 SOUL 的分工

```
SOUL.md 说：我是谁，我有哪些 Skill，我的硬规则是什么
  ↓
SKILL.md 说：这件事具体怎么一步步做
  ↓
DOMAIN.md 说：做这件事需要的业务知识（联系人、规则、模板）
  ↓
Agent 说：理解了，按步骤执行
```

---

## 二、HR Bot 的 7 个 Skills

**Bot**：hr-bot（`~/.ringclaw/hr-bot/`）
**适用对象**：全体 Keller 员工（员工 DM）+ HR 团队（#hr-private）

---

### Skill 1：pto-routing

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/pto-routing/SKILL.md` |
| 触发 | 员工 DM 包含"请假" / "PTO" / "vacation" + 日期 |
| 激活方式 | 意图检测（自动）|
| Entity 类型 | `pto-request` |
| 涉及 ACTION | ACTION:EVENT（日历）· ACTION:MESSAGE（跨 Chat，Linda OOB）|
| 核心价值 | 请假原因隔离，队长只看日期，tom-bot 只看班组影响 |

**SOUL 声明（hr-bot SOUL.md 的 skills 字段）**：
```yaml
skills:
  - pto-routing        # 员工请假申请受理 + 审批路由
```

**典型触发消息**：
```
员工 DM hr-bot：
  "请假申请 6/10-6/12，家庭原因"
  "I need PTO next Monday and Tuesday"
  "能帮我申请假期吗，6月10到12号"
```

**完整执行链**：
```
员工 DM → 意图检测：PTO request
→ pto-routing SKILL.md 注入
→ Agent 按步骤：读余额 → 回复员工 → 通知队长（Linda OOB）→ ACTION:EVENT → 广播
→ entity memory 写入：pto-{employee}-{date}.md
```

---

### Skill 2：hiring-jd-generator

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/hiring-jd-generator/SKILL.md` |
| 触发 A | Agent 路由：`[AGENT_ROUTE:HIRING_REQUEST]`（来自 tom-bot）|
| 触发 B | Linda 输入：`生成 JD：{角色} · {门店}` |
| 激活方式 | Agent 路由 + 意图检测 |
| Entity 类型 | `hiring-request` |
| 涉及 ACTION | ACTION:CARD（JD 草稿）· ACTION:NOTE（发布存档）· ACTION:TASK（跟进任务）|
| 核心价值 | 用人部门提需求 → JD 自动生成 → Linda 一键审批 → 全员 SMS 发布 |

**SOUL 声明**：
```yaml
skills:
  - hiring-jd-generator  # 招聘需求沟通 + JD 自动生成
```

**两种触发路径**：
```
路径 A（Agent→Agent）：
  tom-bot 日摘要检测班组缺口 → Tom 确认招募 →
  tom-bot ACTION:MESSAGE → hr-bot 收到 [AGENT_ROUTE:HIRING_REQUEST] →
  hiring-jd-generator 激活 → 生成 JD Card

路径 B（Human→Agent）：
  Linda @hr-bot "生成 JD：CSR · Dallas · 本月内" →
  意图检测 → hiring-jd-generator 激活
```

**JD 生成需要 DOMAIN.md 提供**：
```markdown
# hiring.role_templates（DOMAIN.md 中）
§ CSR: 基础职责 · 薪资范围 · 工作强度 · 必需技能
§ Installer W-2: 体力要求 · 材料专项 · 薪资范围
§ Installer 1099: 1099 条款 · 保险要求 · 费率说明
§ Crew Lead: 领导力要求 · 多材料要求 · 薪资范围

# company_profile（DOMAIN.md 中）
§ 公司介绍：Keller Interiors，Lowe's 27 年合作伙伴，33 门店 15 州
§ 福利：医保（90天后）· PTO · 材料培训 · 工具支持
§ 申请方式：hr@keller.com · SMS +1 404-555-0099
```

---

### Skill 3：new-hire-onboarding

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/new-hire-onboarding/SKILL.md` |
| 触发 A | hiring-jd-generator 录用确认后自动衔接 |
| 触发 B | Linda 输入：`新员工入职 {姓名} {电话} {角色} {门店} {start_date}` |
| 激活方式 | Skill 链（前置 Skill 触发）+ 意图检测 |
| Entity 类型 | `onboarding` |
| 涉及 ACTION | ACTION:EVENT × N（培训日历）· ACTION:MESSAGE × 2（通知店长）|
| 核心价值 | 7 天 5 个接触点全自动，Linda 只处理异常 |

**完整执行时间轴**：
```
录用确认
  D-7：创建 entity + 欢迎 SMS
  D-5：文件清单 SMS
  D-3：排期 ACTION:EVENT × N + 培训提醒 SMS
  D 0：晨间 SMS + 通知店长 ACTION:MESSAGE
  D+1：系统配置 SMS（AgentRun 使用说明）
  D+7：培训确认
  D+14：Check-in SMS
  D+30：试用期提醒（Cron 到 Linda DM）
```

**CSR 专项（入职培训内容）**：
```markdown
# onboarding.csr_training（SKILL.md references 目录）
模块 1：@orders-bot 基础操作（dispatch 格式）
模块 2：查询 Task 状态，改单流程
模块 3：异常处理（ZIP 不匹配、投诉信号）
```

---

### Skill 4：subcontractor-onboarding

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/subcontractor-onboarding/SKILL.md` |
| 触发 | Linda：`新分包商 {姓名} {手机} {专项材料} {所在州}` |
| 激活方式 | 意图检测（"分包商" / "1099" / "subcontractor"）|
| Entity 类型 | `subcontractor-onboard` |
| 涉及 ACTION | ACTION:NOTE（合规台账）· Fax（政府表格）|
| 联动 | 完成后触发 finance-bot 添加费率记录（Agent 路由）|
| 核心价值 | 7 步入网，15 州不同政府表格自动匹配，保险核验 |

**与 finance-bot 的联动**：
```
subcontractor-onboarding 完成
→ [AGENT_ROUTE:CONTRACTOR_ONBOARDED] → finance-bot
→ finance-bot 在 global memory 添加该分包商费率记录
→ 下周起自动参与付款计算
```

---

### Skill 5：workers-comp-report

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/workers-comp-report/SKILL.md` |
| 触发 | 店长或员工：`工伤报告 {姓名} {门店}` / `accident report` |
| 激活方式 | 意图检测（高优先级关键词匹配）|
| Entity 类型 | `injury-report` |
| 涉及 ACTION | Fax（各州劳工局，从 DOMAIN.md state_labor_requirements 读取）|
| 敏感性 | 极高，所有内容仅存 entity memory，仅 Linda 可读 |
| 核心价值 | 15 州不同截止时间和表格，自动匹配，防止漏报罚款 |

**DOMAIN.md 必须预置（state_labor_requirements）**：
```markdown
§ GA: Georgia SBWC +14046563875 · 21天 · Form WC-1
§ TX: Texas DWC 在线申报 · 8天
§ CA: California DIR +19163273878 · 5天 · Form 5020
...（15 个州）
```

---

### Skill 6：training-scheduler

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/training-scheduler/SKILL.md` |
| 触发 A | Cron（季度强制培训提醒）|
| 触发 B | tom-bot 路由 `[MATERIAL_LAUNCHED]`（新材料上线时）|
| 触发 C | Linda：`schedule training LuxCore all-installers` |
| 激活方式 | 三种均可 |
| 涉及 ACTION | ACTION:EVENT（培训场次）|
| 核心价值 | 合规强制培训自动追踪，续证提醒，报告 ≥95% 达标率 |

**与新员工入职的关系**：
- new-hire-onboarding 调用 training-scheduler 来排期专项培训
- training-scheduler 独立于入职也能运行（季度全员合规检查）

---

### Skill 7：seasonal-crew-scaling

| 属性 | 内容 |
|------|------|
| 文件 | `skills/hr/seasonal-crew-scaling/SKILL.md` |
| 触发 | Cron（每年 2 月 1 日自动触发）|
| 激活方式 | 定时触发 |
| Entity 类型 | `seasonal-hiring-round` |
| 涉及 ACTION | ACTION:MESSAGE × N（各店缺口查询）|
| 核心价值 | 春季旺季前 6 周启动，分包商网络批量激活，培训批次安排 |

---

## 三、Finance Bot 的 5 个 Skills

**Bot**：finance-bot（`~/.ringclaw/finance-bot/`）
**适用对象**：Alex Chen（owner）+ Beth / COO（只读查询）

---

### Skill 8：lowe's-payment-reconciliation

| 属性 | 内容 |
|------|------|
| 文件 | `skills/finance/lowe's-payment-reconciliation/SKILL.md` |
| 触发 | Cron（每月 5 日）+ Alex：`reconcile lowe's {month}` |
| 数据来源 | karen-bot #lowes-handover Note 台账（完工单记录）|
| 涉及 ACTION | ACTION:CARD（月度报告）· [触发 karen-bot 催款传真] |
| 核心价值 | Net-30 对账自动化，逾期超 60 天预警，合同争议窗口保护 |

**与 karen-bot 的联动**：
```
发现逾期应收
→ [AGENT_ROUTE:PAYMENT_FOLLOWUP] → karen-bot
→ karen-bot SendFax 催款传真到 Lowe's HQ
```

---

### Skill 9：subcontractor-payment

| 属性 | 内容 |
|------|------|
| 文件 | `skills/finance/subcontractor-payment/SKILL.md` |
| 触发 | Cron（每周四 15:00）+ Alex 手动审批 |
| 数据来源 | orders-bot chat memory（完工订单）· hr-bot entity memory（分包商费率）|
| 涉及 ACTION | ACTION:CARD（付款清单，Alex 审批）|
| 联动 | hr-bot CONTRACTOR_ONBOARDED → finance-bot 添加费率 |
| 核心价值 | 每周自动生成付款清单，Alex 审批一次，SMS 通知分包商 |

**年度 1099 汇总**（1 月 1 日 Cron）：
```
从全年 entity memory 汇总 → 生成 1099-NEC 数据
→ ACTION:CARD 发 #finance（Alex 核查）
→ Linda 系统生成正式表格
截止：1/31（发给分包商）· 2/28（发给 IRS）
```

---

### Skill 10：cross-store-expense-tracking

| 属性 | 内容 |
|------|------|
| 文件 | `skills/finance/cross-store-expense-tracking/SKILL.md` |
| 触发 | Agent 路由：`[TRAVEL_APPROVED]`（来自 regional-coord-bot）|
| 数据来源 | regional-coord-bot 差旅审批事件 |
| 涉及 ACTION | ACTION:CARD（月度差旅报告，按成本中心分摊）|
| 核心价值 | 跨店支援成本追踪到订单级精度，月报自动分摊 |

**触发路径**：
```
区域协调员 Bot 批准跨店支援
→ [AGENT_ROUTE:TRAVEL_APPROVED] from=regional-coord-bot
→ finance-bot 接收（source_user_ids 包含 regional-coord-bot）
→ cross-store-expense-tracking 激活
→ 创建 entity: travel-{from}-{to}-{date}.md
→ 4 天后 SMS 队长提交实际费用
```

---

### Skill 11：material-cost-variance

| 属性 | 内容 |
|------|------|
| 文件 | `skills/finance/material-cost-variance/SKILL.md` |
| 触发 | Agent 路由：`[ORDER_COMPLETED]`（来自 store-mgr bot）· Cron 每月 10 日 |
| 涉及 ACTION | ACTION:CARD（月度成本分析）· [触发合同费率复查信号 → karen-bot] |
| 核心价值 | 8% 超标阈值预警，连续 2 月超标触发 Lowe's 合同复审 |

**与 karen-bot 的联动**：
```
材料成本连续 2 月超合同费率 8%
→ [AGENT_ROUTE:CONTRACT_RATE_REVIEW] → karen-bot
→ karen-bot 生成 Lowe's 合同复审材料，准备年度谈判
```

---

### Skill 12：month-end-close

| 属性 | 内容 |
|------|------|
| 文件 | `skills/finance/month-end-close/SKILL.md` |
| 触发 | Cron（每月 28 日）|
| 依赖 | Skills 8-11 的结果都必须在 entity memory 里 |
| 涉及 ACTION | ACTION:CARD（管理报告，发 #exec + Beth DM）|
| 核心价值 | 五步关账自动化，月末管理报告一键生成 |

**五步执行顺序**：
```
Step 1: lowe's-payment-reconciliation（逾期催款）
Step 2: subcontractor-payment（月末付款结清）
Step 3: cross-store-expense-tracking（差旅分摊）
Step 4: material-cost-variance（成本分析）
Step 5: 汇总 → ACTION:CARD 月度管理报告 → #exec + Beth DM
```

---

## 四、Skills 全景图

### 按触发方式分类

```
定时触发（Cron/Heartbeat）：
  seasonal-crew-scaling    ← 每年 2/1
  training-scheduler       ← 每季度
  subcontractor-payment    ← 每周四 15:00
  lowe's-payment-recon     ← 每月 5 日
  material-cost-variance   ← 每月 10 日
  month-end-close          ← 每月 28 日

人工触发（Human→Agent）：
  pto-routing              ← 员工 DM
  hiring-jd-generator      ← Linda 输入
  new-hire-onboarding      ← Linda 输入
  subcontractor-onboarding ← Linda 输入
  workers-comp-report      ← 店长/员工 DM
  training-scheduler       ← Linda 输入（也可 Cron）

Agent 路由触发（Agent→Agent）：
  hiring-jd-generator      ← [HIRING_REQUEST] from tom-bot
  new-hire-onboarding      ← hiring-jd-generator 完成后自动
  cross-store-expense      ← [TRAVEL_APPROVED] from regional-coord-bot
  material-cost-variance   ← [ORDER_COMPLETED] from store-mgr-bot
  lowe's-payment-recon     ← [FAX_DELIVERED] from karen-bot
```

### 按 Bot 分类

```
hr-bot（7 个 Skills）：
  pto-routing · hiring-jd-generator · new-hire-onboarding
  subcontractor-onboarding · workers-comp-report
  training-scheduler · seasonal-crew-scaling

finance-bot（5 个 Skills）：
  lowe's-payment-reconciliation · subcontractor-payment
  cross-store-expense-tracking · material-cost-variance
  month-end-close
```

### Skills 之间的依赖链

```
hiring-jd-generator
    └→ new-hire-onboarding（录用确认后）
        └→ training-scheduler（排期专项培训）
        └→ [CONTRACTOR_ONBOARDED] → finance-bot（1099）

subcontractor-onboarding
    └→ training-scheduler（材料专项培训）
    └→ [CONTRACTOR_ONBOARDED] → finance-bot

lowe's-payment-reconciliation
    └→ [PAYMENT_FOLLOWUP] → karen-bot（催款传真）

material-cost-variance
    └→ [CONTRACT_RATE_REVIEW] → karen-bot（合同复审信号）

cross-store-expense-tracking
    ← [TRAVEL_APPROVED] from regional-coord-bot

month-end-close
    ← 依赖 Skills 8-11 的 entity memory 已写入
    └→ ACTION:CARD 月度管理报告 → beth-bot → #exec
```

---

## 五、如何配置一个 Bot 使用这些 Skills

### Step 1：在 SOUL.md 声明激活的 Skills

```markdown
# hr-bot SOUL.md

## Skills
skills:
  - pto-routing
  - hiring-jd-generator
  - new-hire-onboarding
  - subcontractor-onboarding
  - workers-comp-report
  - training-scheduler
  - seasonal-crew-scaling
```

### Step 2：Skills 部署到 Pod 的 skills/ 目录

```
~/.ringclaw/
├── SOUL.md
├── memory/
│   ├── global.md          ← DOMAIN.md（业务知识）
│   ├── user/<id>.md       ← 每个员工的记忆
│   ├── chat/<id>.md       ← 每个频道的上下文
│   └── entities/          ← 业务实体状态
│       ├── pto-*.md
│       ├── onboarding-*.md
│       └── hiring-*.md
└── skills/
    ├── pto-routing/SKILL.md
    ├── hiring-jd-generator/SKILL.md
    │   └── references/role_templates.md
    ├── new-hire-onboarding/SKILL.md
    │   └── references/orders-bot-quick-guide.md
    ├── subcontractor-onboarding/SKILL.md
    ├── workers-comp-report/SKILL.md
    ├── training-scheduler/SKILL.md
    └── seasonal-crew-scaling/SKILL.md
```

### Step 3：Skills Index 自动注入 System Prompt

RingClaw（Hermes 模式）启动时扫描 skills/ 目录，构建 compact index：

```
[Skills Available]
pto-routing              · 员工请假申请受理 + 审批路由
hiring-jd-generator      · 招聘需求沟通 + JD 自动生成
new-hire-onboarding      · 新员工入职全流程
subcontractor-onboarding · 1099 分包安装工入网流程
workers-comp-report      · 工地受伤事故报告
training-scheduler       · 员工和分包商培训安排
seasonal-crew-scaling    · 旺季分包商扩编计划
```

这 7 行就是 system prompt 里的全部技能描述，
不占 SOUL 的 2000 chars 预算。

### Step 4：Skill 激活时注入全文

Agent 判断需要 `hiring-jd-generator` 时，system prompt 追加：

```xml
<context type="skill" name="hiring-jd-generator" state="initial">
[SKILL.md 全文 ~200 行]
当前 entity：{若已创建则附带 entity memory}
</context>
```

Agent 按 SKILL.md 的步骤逐步执行。

### Step 5：配置 Cron（一次性，owner 权限）

```bash
# 在 #hr-private（Linda 是 owner）执行：

/cron add "seasonal-hiring" "0 9 1 2 *"
  "触发 seasonal-crew-scaling skill：
   向各门店查询旺季需求，启动分包商招募流程。"

/cron add "quarterly-training" "0 9 1 */3 *"
  "触发 training-scheduler skill：
   检查强制培训完成率，发送提醒 SMS 给未完成人员。"

# 在 #finance（Alex 是 owner）执行：

/cron add "weekly-payroll" "0 15 * * 4"
  "触发 subcontractor-payment skill：
   汇总本周完工订单，生成付款清单，等 Alex 审批。"

/cron add "month-end" "0 9 28 * *"
  "触发 month-end-close skill：按五步完成月结。"
```

---

## 六、一句话总结每个 Skill 的价值

| Skill | 解决的痛点 | 节省的时间 |
|-------|-----------|-----------|
| pto-routing | 请假信息泄露 + 审批等待 | 当天完成 vs 邮件来回 |
| hiring-jd-generator | JD 手写 + 发布分散 | 1 分钟草稿 vs 1-2 小时 |
| new-hire-onboarding | 入职步骤遗漏 + 人工跟进 | 7 天自动 vs 手工核对清单 |
| subcontractor-onboarding | 15 州表格搞混 + 漏报 | 自动匹配 vs 手查法规 |
| workers-comp-report | 各州截止日不同，漏报有罚款 | 自动识别 vs 忘记 |
| training-scheduler | 培训完成率不达标 + 证书过期 | 自动追踪 vs Excel 表格 |
| seasonal-crew-scaling | 旺季前分包商不够用 | 提前 6 周自动启动 |
| lowe's-payment-recon | 逾期应收发现太晚，超争议窗口 | 月度自动对账 vs 季度手查 |
| subcontractor-payment | 每周手工整理付款 Excel | Bot 汇总，Alex 一次审批 |
| cross-store-expense | 差旅费账不清，成本中心乱 | 事件驱动追踪，到订单级精度 |
| material-cost-variance | 不知道哪个门店超支，Lowe's 谈判无依据 | 月度自动分析，信号自动发给 Karen |
| month-end-close | 月结需要 3 天手工整理 | Bot 五步关账，Alex 审批关键节点 |
