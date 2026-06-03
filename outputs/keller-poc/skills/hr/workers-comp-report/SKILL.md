---
name: workers-comp-report
description: 工地受伤事故报告——首报、州政府传真、保险公司通知、跟进追踪
version: 1.0.0
metadata:
  tags: [hr, workers-comp, injury, compliance, legal]
  applicable_souls: [hr-service]
  entity_type: injury-report
  sensitivity: high
prerequisites:
  capabilities: [sms, fax]
  memory_keys: [state_workers_comp_agencies, insurance_contacts]
---

# Workers' Comp Report

地板安装是体力劳动，受伤风险真实存在（切割工具、搬运、跪姿施工）。
Keller 在 15 个州运营，每个州的工伤报告要求和截止时间不同。

## 触发条件
店长或员工 DM hr-bot "工伤报告" 或 "accident report {姓名} {门店}"

## 步骤

### Phase 1：首报（≤1 小时内）

1. **收集基本信息**（hr-bot 在 DM 里逐步询问）:
   ```
   受伤员工姓名：
   门店：
   受伤时间：
   受伤经过（简述）：
   受伤部位：
   是否需要急救 / 就医：
   目击者（如有）：
   ```
   > 所有内容仅存 entity memory，不进入其他频道

2. **紧急情况判断**:
   - 需要急救 → 立即 DM Linda + 店长："需要立即处理，请致电 {emergency_contact}"
   - 普通受伤 → 继续正常流程

3. **DM 通知 Linda + 店长**（Linda OOB approve）:
   ```
   [工伤报告] {员工} · {门店} · {时间}
   经过：{简述}
   后续：HR 正在处理州政府报告，请店长留存现场记录
   ```

### Phase 2：合规报告（州政府，24-48h 内）

4. **州政府报告**（读 global memory `state_workers_comp_agencies`）:
   ```
   动作：ACTION:FAX → 该州工伤保险局传真号
   表格：{state}-workers-comp-first-report（PDF 模板）
   截止：各州不同（CA: 5 天, TX: 10 天, FL: 7 天...）
   ```
   设置截止日提醒 cron

5. **保险公司通知**（读 global memory `insurance_contacts`）:
   - ACTION:FAX 或 ACTION:SMS 通知 Keller 工伤保险公司
   - 记录索赔号到 entity memory

### Phase 3：跟进

6. **恢复状态追踪**（每周 cron 询问 Linda）:
   - 员工恢复情况
   - 预计返岗日期
   - 是否需要临时轻劳动岗位

7. **返岗通知**（员工返岗时）:
   - 更新 entity memory: status=returned
   - 匿名通知店长班组恢复（无受伤细节）

## 敏感性处理

**绝对不公开**：受伤细节、就医情况、保险索赔金额
**店长看到**：员工不在岗的日期范围 + 班组影响（无受伤细节）
**HR 看到**：全部
**存入 entity memory**：完整记录（Linda 和合规审计可查）

## 跨州合规一览（global memory 预置）

```
state_workers_comp_agencies:
  GA: Georgia SBWC, fax +14046563875, first report within 21 days
  TX: Texas DWC, online filing preferred, 8 days
  CA: CA DIR, 5 days employer report
  FL: FL DFS, 7 days, Form DWC-1
  ... (15 states)
```
