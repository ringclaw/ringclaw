---
name: month-end-close
description: 月结流程——五步关账：Lowe's 对账 → 分包商付款 → 差旅分摊 → 成本汇总 → 管理报告
version: 1.0.0
metadata:
  tags: [finance, month-end, close, reporting]
  applicable_souls: [finance-service]
  entity_type: monthly-close
prerequisites:
  capabilities: [fax]
  memory_keys: [lowe's_contract_rates, subcontractor_rates, store_cost_centers]
---

# Month-End Close

每月 28 日触发，协调五条财务线，在月末前生成完整的管理报告。

## 触发条件
每月 28 日 cron 自动触发，或 Alex 输入 "start month-end close {month}"

---

## 五步关账流程

### Step 1：Lowe's 收款确认（Day 1-2）

**自动检查**：
1. 读 `lowe's-payment-reconciliation` 最新 entity memory
2. 识别本月仍未收款的完工单（Net-30 到期）
3. 生成待处理清单：

```
[月结 Step 1 · Lowe's 收款]
本月应收：{n} 单 · ${total}
已收：{n} 单 · ${collected}（{pct}%）

⚠️ 逾期未收（{n} 单 · ${amount}）：
  A8819 · Dallas · ${amount} · 逾期 {n} 天 · FAX-REF: {ref}
  A8823 · Phoenix · ${amount} · 逾期 {n} 天 · FAX-REF: {ref}

建议：路由催款传真到 karen-bot

待 Alex 确认是否发催款传真 → 输入 "send collection fax {orders}"
```

**Alex 确认后**：
→ `[AGENT_ROUTE:PAYMENT_FOLLOWUP]` 发到 karen-bot，由其执行传真

---

### Step 2：分包商付款结清（Day 2-3）

**自动汇总**：
从本月所有 `dispatch-{order}-{month}.md` entity 中提取
material × sqft × subcontractor_rate：

```
[月结 Step 2 · 分包商付款]
本月完工：{n} 单

应付明细：
Mike Reyes:    ${amount}（{n} 单，已含本周付款）
Carlos Ruiz:   ${amount}
David Park:    ${amount}
...

已付（本月内）：${total_paid}
本月末待付：${remaining}

暂扣（质量标记未解决）：
Mike Reyes A8810：-${amount}（等 Lowe's 复检）

净应付总额：${net}
待 Alex 审批 → "approve month-end payments"
```

---

### Step 3：差旅费分摊（Day 3）

**自动汇总**：
读本月所有 `travel-{from}-{to}-{month}.md` entity：

```
[月结 Step 3 · 差旅分摊]
本月跨店支援：{n} 次

按成本中心分摊：
CC-001 Atlanta（接收 2 次支援）：$1,240
CC-002 Dallas（输出 3 次支援，获报销）：+$1,860
CC-003 Phoenix（接收 1 次）：$620

全国差旅费合计：${total}
均摊到月度成本报告 ✅
```

---

### Step 4：成本汇总与差异（Day 3-4）

**自动运行 material-cost-variance**（本月汇总版本）：

```
[月结 Step 4 · 成本差异]
全国材料成本：${actual} vs 报价 ${quoted}（{±}{pct}%）

超出阈值（>8%）门店：
⚠️ Phoenix：实际 112%（超标 4%）→ 已通知店长
✅ Atlanta：104%（在容忍范围内）
✅ Dallas：98%（低于报价，优秀）

材料价格变动预警：
LuxCore 本月采购价 ↑5.2%（连续 2 个月）
→ 已生成合同费率复查信号，待 Karen 跟进
```

---

### Step 5：管理报告（Day 4-5）

**生成月度管理报告**，TEXT ONLY，投递到 #exec + #finance + DM Beth：

```
[Keller 月度财务报告 · {month}]

━━ 收入 ━━
Lowe's 应收：${invoiced}
Lowe's 实收：${collected}（{pct}%，目标 ≥95%）
逾期应收：${overdue}（{n} 单，{oldest_days} 天最久）

━━ 成本 ━━
分包商工程款：${labor_cost}（利润率 {margin}%）
材料成本：${material}（vs 报价 {±}{pct}%）
跨店差旅：${travel}（{n} 次支援）

━━ 净利润估算 ━━
本月营业利润（估算）：${profit}（{margin}%）
上月对比：{↑↓}{pct}%
年初至今：${ytd}

━━ 关注 ━━
· LuxCore 材料成本连续 2 月超标，合同复审信号已发 Karen
· Phoenix 本月成本差异 +12%，店长已介入
· 逾期应收 ${overdue} 催款传真已发送（{n} 单）

📎 完整明细：#finance 频道
```

---

## 关账完成标志

```
当以下 5 个 entity 都写入 status=closed：
  ✅ lowe's-reconcile-{month}.md
  ✅ subcontractor-payment-{month}.md
  ✅ travel-expense-{month}.md
  ✅ cost-variance-{month}.md
  ✅ mgmt-report-{month}.md

finance-bot 在 #finance 发布：
  ✅ {month} 月结完成 · {date}
  管理报告已发 #exec
```

## 失败处理

| 情况 | 行为 |
|------|------|
| Karen-bot 未回应催款路由 | 3 天后 DM Alex，手动处理 |
| 分包商付款数据缺失（某店未报）| 标记该店为 incomplete，月报中注明 |
| 月末仍有逾期应收超 60 天 | DM Beth 专项提醒（超出正常争议窗口风险）|
| 成本数据与 karen-bot 台账不一致 | 暂停生成月报，DM Alex 核查 |
