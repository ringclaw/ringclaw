---
name: cross-store-expense-tracking
description: 跨店支援的差旅费追踪——酒店预订确认、日津贴计算、费用报告、月度汇总
version: 1.0.0
metadata:
  tags: [finance, expense, travel, cross-store]
  applicable_souls: [finance-service]
  entity_type: travel-expense
prerequisites:
  capabilities: [sms]
  memory_keys: [per_diem_rates, hotel_policy, store_locations]
---

# Cross-Store Expense Tracking

当一个门店的施工队支援另一个门店时，产生差旅费（酒店 + 日津贴 + 里程）。
这些费用需要：准确追踪 → 分摊到发起请求的门店 → 月度汇总上报。

## 触发条件
区域协调员 bot 批准跨店支援后，发送路由事件：
```
[AGENT_ROUTE:TRAVEL_APPROVED]
from_store: Dallas
to_store: Atlanta
dates: {start} to {end}
crew_count: 2
```

## 步骤

### Phase 1：费用预估（支援批准时即触发）

1. **读取差旅政策**（global memory `hotel_policy`）:
   - 酒店上限：$180/晚（或当地市价，取低）
   - 日津贴：$60/天（含餐饮）
   - 里程：$0.67/英里（IRS 标准）

2. **自动计算预估费用**:
   ```
   Atlanta 跨店支援预算（Dallas → Atlanta，2 人，{n} 天）
   酒店：$180 × {n} 天 × 2 人 = ${hotel}
   日津贴：$60 × {n} 天 × 2 人 = ${per_diem}
   里程：{miles} 英里 × $0.67 = ${mileage}
   预估总计：${total}
   ```
   这个预估已在区域协调员的审批消息里显示过，无需重复发送。

3. **创建 expense entity**:
   ```
   travel-{from}-{to}-{date}.md：
     status: approved
     crew_count: 2
     dates: {start} to {end}
     cost_center: {requesting_store}（Atlanta 承担）
   ```

### Phase 2：实际费用收集（支援结束后 48h）

4. **提醒队长提交费用**（ACTION:SMS → 队长手机）:
   ```
   Hi {crew_lead}! Please submit your travel receipts for the
   Atlanta trip ({start}–{end}).
   Reply: hotel total ${amount}, meals ${amount}, miles {n}
   or email finance@keller.com with receipts.
   ```

5. **录入实际费用**（队长回复或财务输入）:
   更新 entity memory：actual_hotel / actual_per_diem / actual_mileage

6. **差异检查**:
   | 情况 | 行为 |
   |------|------|
   | 实际 < 预估 | 正常，更新记录 |
   | 实际超预估 < 10% | 自动批准，记录 |
   | 实际超预估 > 10% | 标记，DM 财务审核 |
   | 缺少收据（酒店 > $100）| 暂停报销，SMS 提醒补交 |

### Phase 3：费用报告与分摊

7. **生成费用报告**（支援结束后 3 天内）:
   ```
   [差旅费报告 · Dallas→Atlanta · {date}]
   施工队：{crew_lead} + 1 名
   出行天数：{n} 天
   酒店：${hotel}（收据 #{receipt_id}）
   日津贴：${per_diem}
   里程：{miles} 英里 × $0.67 = ${mileage}
   实际总计：${total}（预算 ${budget}，{±}${delta}）
   费用中心：Atlanta 门店
   ```

8. **月度汇总**（每月 1 日 cron）:
   ```
   [跨店差旅费月报 · {month}]
   总次数：{n} 次跨店支援
   总费用：${total}
   
   按发起门店分摊：
   Atlanta： ${amount}（{n} 次）
   Phoenix:  ${amount}（{n} 次）
   ...
   
   费用最高的单次支援：{store} → {store}，${amount}
   ⚠️ 需审核（超预算 >10%）：{n} 笔
   ```
   发到 #finance + DM Beth

## 与区域协调员 Bot 的联动

区域协调员在提出跨店方案时必须附出行成本估算（SOUL 里的硬规则）。
Finance bot 收到 `TRAVEL_APPROVED` 事件后自动启动追踪——
两个 bot 协作确保"说了多少钱 → 花了多少钱"的闭环。
