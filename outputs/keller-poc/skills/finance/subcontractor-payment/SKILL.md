---
name: subcontractor-payment
description: 1099 分包商工程款支付——完工确认、付款计算、支付通知、1099 年度汇总
version: 1.0.0
metadata:
  tags: [finance, subcontractor, 1099, payment, payroll]
  applicable_souls: [finance-service]
  entity_type: subcontractor-payment
prerequisites:
  capabilities: [sms, fax]
  memory_keys: [subcontractor_rates, payment_schedule]
---

# Subcontractor Payment

Keller 的安装工多为 1099 分包商，按完工订单付款（不是按小时）。
付款流程：完工单确认 → 计算应付款 → 财务审批 → 付款 → SMS 通知。

## 触发条件
- 每周五 cron（本周完工订单汇总 → 应付款计算）
- 或财务输入 "pay subcontractor {name} orders {order_ids}"

## 步骤

### Phase 1：完工确认

1. **本周完工订单提取**（从 orders-bot chat memory 读取状态=completed 的订单）:
   按分包商汇总：
   ```
   Mike Reyes 本周完工：
   A8819 · Engineered Oak 850sqft · rate $2.10/sqft · $1,785
   A8821 · Engineered Oak 900sqft · rate $2.10/sqft · $1,890
   小计：$3,675
   ```

2. **比对质检状态**（从 karen-bot 台账 + store-mgr bot 确认）:
   - 已传真完工单 ✅ → 可付款
   - Lowe's 质量标记 ⚠️ → 暂扣款，等复检通过
   - 投诉未关闭 ⚠️ → 暂扣款，等投诉解决

### Phase 2：付款计算

3. **按合同费率计算**（读 global memory `subcontractor_rates`）:

   费率类型：
   | 材料 | 基础费率 |
   |------|---------|
   | Engineered Oak | $2.10/sqft |
   | LuxCore | $2.40/sqft（新材料溢价）|
   | Tile | $3.20/sqft |
   | Carpet | $1.80/sqft |
   | Hardwood | $2.80/sqft |

   加成项：
   - 超大面积（>1500sqft）：+10%
   - 跨店支援（差旅额外付）：+cross-store-expense 实际费用

4. **生成付款清单**（DM 财务审批）:
   ```
   [本周分包商付款清单 · {date}]

   Mike Reyes     $3,675  （A8819, A8821）
   Carlos Ruiz    $2,890  （A8823, A8826）
   David Park     $4,210  （A8824, A8825, A8827）

   暂扣（质量标记）：
   Mike Reyes     -$1,890（A8821，Lowe's 复检待确认）

   本周应付总额：$8,885
   下周待确认：$1,890

   [批准] → 财务输入 "approve payments {date}"
   ```

### Phase 3：付款与通知

5. **付款执行**（财务批准后，外部系统处理，bot 记录）:
   付款通过公司财务系统（ACH）执行，bot 不直接接触银行 API
   
6. **SMS 通知分包商**:
   ```
   Hi {name}! Payment of ${amount} for orders {order_list}
   has been processed. ETA 1-2 business days via ACH.
   Questions? Reply here or email finance@keller.com
   ```

### Phase 4：1099 年度汇总（每年 1 月）

7. **年度付款汇总**（1 月 1 日 cron）:
   从 entity memory 汇总全年所有分包商付款记录：
   ```
   1099 汇总 · {year}
   
   Mike Reyes    $52,300（> $600 阈值，需报税）
   Carlos Ruiz   $41,800（需报税）
   临时安装工 #7  $320（< $600，无需 1099）
   ...
   
   需要生成 1099-NEC 的分包商：{n} 人
   截止：1 月 31 日（发给分包商）+ 2 月 28 日（报 IRS）
   ```

8. **1099-NEC 表格处理**（Linda + 财务协作）:
   - bot 生成汇总数据
   - 财务人员在会计系统里生成正式表格
   - karen-bot（如适用）传真到 IRS（或使用电子申报）

## 关键合规要求

- 向 IRS 报告 $600+ 的分包商付款（1099-NEC）
- 付款记录保留 7 年
- 跨州分包商可能涉及州税务申报（15 个州各不同）

## 与 HR Bot 的联动

```
新分包商入网（subcontractor-onboarding 完成）
  → hr-bot 发 AGENT_ROUTE:NEW_CONTRACTOR 给 finance-bot
  → finance-bot 在 global memory 里添加该分包商的费率记录
  → 下周付款计算时自动包含

分包商退网
  → hr-bot 发 AGENT_ROUTE:CONTRACTOR_OFFBOARDED
  → finance-bot 确认最后一笔付款已处理，标记为 inactive
```
