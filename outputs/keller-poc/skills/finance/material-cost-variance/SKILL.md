---
name: material-cost-variance
description: 材料实际成本 vs 报价差异监控——超支预警、供应商价格变动追踪、月度成本分析
version: 1.0.0
metadata:
  tags: [finance, materials, cost, variance, lowe's]
  applicable_souls: [finance-service]
  entity_type: cost-variance
prerequisites:
  capabilities: []
  memory_keys: [material_pricing, lowe's_contract_rates]
---

# Material Cost Variance

Keller 从 Lowe's 接单时，材料费按合同费率定价。
但实际施工中，材料成本会因为：
- 供应商价格上涨
- 施工浪费（测量误差、切割损耗）
- 特殊材料替代（断货时）

导致实际成本 vs 报价出现偏差。超出一定比例影响利润。

## 触发条件
- 每月 10 日 cron 自动触发（上月分析）
- 或财务输入 "material variance {store} {month}"
- 或任意材料订单关闭时（store mgr bot 发 `ORDER_COMPLETED` 事件）

## 监控维度

### 维度 1：单笔订单超支

当一笔订单的实际材料成本 > 报价材料成本 × 1.08（8% 缓冲），触发预警：

```
⚠️ 材料成本超支预警
订单：#{order_id}（{store}）
报价材料成本：${quoted}
实际材料成本：${actual}
超支：${delta}（{pct}%）
原因待确认：测量误差 / 供应商涨价 / 材料替代？
```
→ DM 相关店长 + 财务

### 维度 2：供应商价格变动

当同一材料在 3 个月内价格变化 > 5%，触发分析：

从 global memory `material_pricing` 历史数据检测：
```
[价格变动追踪 · {material_type}]
当前价格：${current}/sqft
3 个月前：${prev}/sqft
变化：{pct}%

受影响门店：{n} 个（本季度涉及该材料）
预计利润影响：${impact}

建议：
· 与 Lowe's 协商重新定价（如变化 >10%）
· 寻找替代供应商
```

### 维度 3：月度店级分析

每月汇总各门店材料成本表现：

```
[材料成本月报 · {store} · {month}]

完成订单：{n} 笔
材料成本总计：${total}（报价 ${quoted_total}）
整体差异：{±}{pct}%

按材料类型：
Engineered Oak：实际 ${actual} vs 报价 ${quoted}（{pct}%）
LuxCore：       实际 ${actual} vs 报价 ${quoted}（{pct}%）
Tile：           实际 ${actual} vs 报价 ${quoted}（{pct}%）

损耗率最高订单：#{order}（{pct}% 超标）
建议：{队长} 队施工精确度需关注 or 测量流程优化
```

### 维度 4：Lowe's 合同费率复查信号

当全国平均材料成本持续 2 个月超出合同费率 8% 以上：

→ 生成报告 DM Karen（Lowe's 联络）：
```
[合同费率复查信号]
材料成本已连续 2 个月超合同费率 {pct}%
建议在下季度合同复审时提出重新定价
受影响材料：{list}
预计年度利润影响：${amount}
```
Karen bot 决定是否触发 Lowe's 沟通流程

## Entity Memory 写入

```
cost-variance-{store}-{month}.md：
  total_variance_pct: {pct}
  high_variance_orders: [{order_id, material, pct}, ...]
  price_change_alerts: [{material, change_pct}, ...]
  lowe's_rate_review_triggered: true/false
```

## 与其他 Bot 的联动

```
store-mgr bot 收到单笔超支预警 → 调查队长施工损耗
karen-bot 收到合同费率复查信号 → 准备 Lowe's 谈判材料
regional-coord-bot 收到月报 → 汇总区域成本趋势给 Beth
```
