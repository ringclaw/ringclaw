---
name: subcontractor-onboarding
description: 1099 分包安装工入网流程——资质核验、合同传真、工具培训确认、门店分配
version: 1.0.0
metadata:
  tags: [hr, subcontractor, 1099, compliance, onboarding]
  applicable_souls: [hr-service]
  entity_type: subcontractor-onboard
prerequisites:
  capabilities: [sms, fax]
  memory_keys: [state_labor_requirements, training_calendar]
---

# Subcontractor Onboarding

Keller 的安装工大量使用 1099 分包商。每位新分包商入网需要：
资质核验 → 合同签署 → 政府表格传真 → 专项材料培训 → 门店分配。

## 触发条件
HR 说"新增分包商 {姓名} {手机} {专项材料} {所在州}"
或店长通过 ops chat 发起分包商引荐

## 步骤

### Phase 1：入网核验（HR 主导）

1. **资质清单 SMS** → 分包商手机：
   ```
   Hi {name}! Welcome to the Keller installer network.
   To get started, please provide:
   1. Proof of liability insurance (min $500K)
   2. Valid driver's license
   3. W-9 form
   Reply here or email hr@keller.com
   ```

2. **背景调查触发**（记录在 entity memory，Linda 手动处理）
   - 在 #hr-private post 清单
   - 设置 3 天跟进 cron

3. **专项认证核验**（按 entity.material_specialty）:
   | 材料 | 要求 |
   |------|------|
   | Engineered Oak | Keller 内部认证 OR 同等施工经验证明 |
   | LuxCore | 必须完成 06/15 或 06/22 培训 |
   | Tile | NTCA 认证优先 |
   | Carpet | 无强制认证，经验说明即可 |

### Phase 2：合同与政府表格

4. **合同传真**（ACTION:FAX，Linda 确认后执行）:
   - 发合同 PDF 到分包商传真号（或电子签）
   - 合规台账写入：`{name} | W-9 received | {date}`

5. **州政府表格**（按 entity.state）:
   读 global memory `state_labor_requirements`，发对应表格到该州劳工局传真号：
   ```
   [AGENT_ROUTE:GOVERNMENT_FAX]
   form_type: {state}-new-contractor
   recipient_fax: {state_fax_number}
   ```
   > 注：每个州的要求不同（15 个州），表格类型和传真号在 global memory 里

### Phase 3：培训与上线

6. **培训日历推送** → 分包商 SMS:
   ```
   Hi {name}! Your next step: LuxCore installation training.
   Date: {training_date}, 9am, {store_location}.
   Please confirm: reply YES to this message.
   ```
   设置 at-time cron：培训前 24h 发提醒

7. **培训完成确认**（inbound SMS 检测 "YES" 或培训当天签到）:
   - 更新 per-user memory：`training_status.luxcore = completed`
   - 通知店长："新分包商 {name} 培训完成，可分配施工。"

8. **门店分配**（店长确认）:
   ACTION:MESSAGE → 相关店长 DM：
   ```
   新分包商入网：{name}，专项 {specialty}，可用日期 {start_date}。
   需要分配到你的门店吗？
   ```

## Entity Memory 写入

```
subcontractor-{name}-{date}.md：
  phase: verification / contract / training / active
  state: {state}
  material_specialty: [...]
  insurance_verified: true/false
  w9_received: true/false
  training_completed: {date 或 pending}
  assigned_stores: [...]
  government_fax_sent: {date}
```

## 失败处理

| 情况 | 行为 |
|------|------|
| 3 天未回复资质清单 | HR DM Linda 提醒，不自动催促 |
| 保险额度不足 | 停止流程，SMS 说明最低要求 |
| 培训缺席 | 记录，重新发下一场次通知 |
| 政府传真失败 | 同 fax-batch 重试逻辑，3 次后 DM Linda |
