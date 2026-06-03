---
name: lowe's-payment-reconciliation
description: Lowe's 按完工单付款对账——发票匹配、差异标记、逾期提醒、月度对账报告
version: 1.0.0
metadata:
  tags: [finance, lowe's, reconciliation, invoice, payment]
  applicable_souls: [finance-service]
  entity_type: payment-reconciliation
prerequisites:
  capabilities: [fax]
  memory_keys: [lowe's_payment_schedule, completion_form_ledger]
---

# Lowe's Payment Reconciliation

Keller 的核心收入来源：每完成一笔 Lowe's 安装，Lowe's 按
合同费率付款。付款依据是已传真并确认的完工表单。

典型流程：
  安装完成 → CSR 上传完工单 → Karen-bot 传真 Lowe's → Lowe's 付款（Net-30）

## 触发条件
- 每月 5 日 cron 自动触发（上月对账）
- 或财务输入 "reconcile lowe's {month}"

## 步骤

### Phase 1：数据拉取（月初）

1. **已传真完工单清单**（从 karen-bot 的 #lowes-handover Note 台账读取）:
   - 按门店汇总：传真日期 + FAX REF + 订单号 + 金额
   - 读 memory/entities/ 下所有 `lowe's-case-*.md`（状态=delivered）

2. **Lowe's 付款记录**（手动输入或 EDI 文件上传）:
   财务输入："上传本月 Lowe's 付款报表"
   → bot 解析并与完工单清单做匹配

### Phase 2：差异检测

3. **三种差异类型**:

   | 类型 | 说明 | 处理 |
   |------|------|------|
   | **未收款** | 传真已确认但无对应付款 | 标记为逾期，生成催款传真 |
   | **金额差异** | 付款金额与合同费率不符 | 生成差异报告，karen-bot 发 Lowe's |
   | **重复付款** | 同一 FAX REF 付款两次 | 立即标记，通知财务 |

4. **逾期追踪**（Net-30 后仍未收款）:
   按 FAX REF 生成催款传真草稿，等财务确认后发送：
   ```
   [Payment Follow-up · {FAX_REF}]
   Invoice Date: {fax_date}
   Order: #{order_id}
   Amount: ${amount}
   Due Date: {due_date}（{days_overdue} days past due）
   Please confirm payment status.
   ```

### Phase 3：月度报告

5. **月度对账报告**（发到 #finance + DM Beth）:
   ```
   [Lowe's Payment Reconciliation · {month}]

   已开票：{n} 单 · 总额 ${total}
   已收款：{n} 单 · 总额 ${collected}（{pct}%）
   待收款：{n} 单 · 总额 ${pending}
     · 未逾期：{n} 单 · ${amount}
     · 逾期 1-30 天：{n} 单 · ${amount}
     · 逾期 30+ 天：{n} 单 · ${amount} ⚠️

   差异：{n} 单 · ${amount} → 已生成差异报告
   重复付款：{n} 单 → 已标记待处理

   门店收款率排名：
   1. Atlanta 98% ✅
   2. Dallas 96% ✅
   3. Phoenix 87% ⚠️ (3 单逾期)
   ```

6. **Karen-bot 联动**（如需要发催款传真）:
   ```
   [AGENT_ROUTE:PAYMENT_FOLLOWUP]
   to: karen-bot
   orders: [{order_id, fax_ref, amount, days_overdue}, ...]
   ```
   karen-bot 处理传真发送，财务确认后执行

## Entity Memory 写入

```
reconciliation-{year}-{month}.md：
  period: {year}-{month}
  total_invoiced: ${amount}
  total_collected: ${amount}
  outstanding: [{order_id, amount, days_overdue}, ...]
  discrepancies: [{order_id, invoiced, paid, delta}, ...]
  report_generated_at: {timestamp}
```

## 关键合规要求

- Lowe's 合同通常规定争议必须在付款后 90 天内提出
- 超出争议期的差异直接核销，不能再追讨
- 催款传真必须引用原始 FAX REF 编号（karen-bot 台账是唯一来源）
