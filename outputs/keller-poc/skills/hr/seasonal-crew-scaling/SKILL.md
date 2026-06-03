---
name: seasonal-crew-scaling
description: 旺季（春季装修潮）分包商扩编计划——需求预测、招募 SMS 广播、培训批次安排
version: 1.0.0
metadata:
  tags: [hr, seasonal, hiring, crew, scaling]
  applicable_souls: [hr-service]
  entity_type: seasonal-hiring-round
prerequisites:
  capabilities: [sms]
  memory_keys: [installer_network, training_calendar, store_capacity]
---

# Seasonal Crew Scaling

春季（3-5月）是地板安装旺季，Keller 需要在 2 月底前确认各门店的
扩编计划并激活分包商网络。

## 触发条件
- 每年 2 月 1 日 cron 自动触发
- 或 Linda 输入 "seasonal hiring round {year}"

## 步骤

### Phase 1：需求预测（2月初）

1. **各店容量缺口分析**（读 global memory `store_capacity`）:
   
   向每个门店店长 bot 发查询（ACTION:MESSAGE → 各店 ops chat）:
   ```
   [SEASONAL_CAPACITY_QUERY]
   请在 48h 内回复：
   Q3 旺季（3-5月）需要额外多少分包安装工？
   请说明：数量 · 专项材料 · 最早上岗日期
   ```

2. **汇总需求**（收集各店回复后，Linda 确认）:
   ```
   [季节性招募需求汇总 · {year}]
   Atlanta: +3 人（Engineered Oak）3/15 上岗
   Dallas:  +2 人（Tile+Carpet）3/01 上岗
   Phoenix: +4 人（任意专项）3/10 上岗
   ...
   总计：+{n} 人，{date} 前完成入网
   ```

### Phase 2：激活现有分包商网络（2月中）

3. **批量 SMS 广播**（需 Linda 确认）:
   从 global memory `installer_network` 读取"状态=inactive/available"的分包商，
   发送兴趣探测 SMS：
   ```
   Hi {name}! Spring season is coming up at Keller.
   We have openings in {city} starting {date}.
   Material: {specialty}. Interested?
   Reply YES + your available start date.
   ```
   设置 3 天 inbound SMS 监听（Group B）或手动跟进

4. **汇总回应**（48h 后）:
   - 匹配需求 vs 有意向分包商
   - 生成匹配报告 → DM Linda
   - 缺口部分 → 触发 subcontractor-onboarding 新招募

### Phase 3：培训批次安排（2月底）

5. **LuxCore 批次培训** 安排（如有需要）:
   - 读 training_calendar，找 3 月前的可用日期
   - ACTION:EVENT 创建培训场次
   - ACTION:SMS 群发通知有意向分包商

6. **入网快速通道**（旺季特别流程）:
   标准入网 7 天 → 旺季加急 3 天（需 Linda 手动批准加急标记）

## 学习触发

旺季结束后（6 月），bot 自动生成技能笔记：
```
seasonal-{year}-learnings/SKILL.md
  · 哪些门店预测准确 / 偏差大
  · 哪类分包商回应率最高
  · 哪个培训时间段出席率最好
```
供下年旺季参考。
