---
name: training-scheduler
description: 员工和分包商培训安排——强制培训提醒、报名确认、完成追踪、季度合规报告
version: 1.0.0
metadata:
  tags: [hr, training, compliance, scheduling, certification]
  applicable_souls: [hr-service]
  entity_type: training-record
prerequisites:
  capabilities: [sms]
  memory_keys: [training_calendar, employee_directory]
---

# Training Scheduler

Keller 有两类培训：
1. **合规性强制培训**（安全、材料认证）— 有截止日，未完成影响上岗资格
2. **技能培训**（新材料系列、工具操作）— 按材料更新触发

## 触发条件

- **季度强制培训提醒**：每季度第 1 个月 1 日 cron
- **新材料上线**：store-mgr bot 或 CSR 触发 `MATERIAL_LAUNCHED` 事件
- **员工入职**：hr-bot subcontractor-onboarding 或 onboarding flow 完成后
- **手动**：Linda 输入 "schedule training {type} {audience}"

---

## 步骤

### Phase 1：需求识别

1. **强制培训到期检测**（cron 触发）:
   扫描 per-user memory，找 `training_status.{skill}` = pending 或过期（>1 年）:
   ```
   待完成强制培训人员：
   · OSHA 10 安全：{n} 人（已逾期 {n} 天）
   · LuxCore 材料认证：{n} 人（未入职培训）
   · 防滑安全：{n} 人（上次完成 >12 个月）
   ```

2. **新材料培训触发**（`MATERIAL_LAUNCHED` 事件）:
   从 global memory 找涉及该材料的队长和分包商，生成受训名单

### Phase 2：批量 SMS 通知

3. **发送培训提醒**（需 Linda 确认，批量 ACTION:SMS）:

   **强制合规培训**（语气正式）:
   ```
   Hi {name}! This is a reminder from Keller HR.
   You have {n} days left to complete Q{n} {training_name}.
   Take it here: {url_or_location}
   Reply DONE when complete, or HELP if you need support.
   ```

   **技能培训**（语气友好）:
   ```
   Hi {name}! Keller is rolling out the new {material} series.
   Training: {date}, {time}, {location}
   Materials covered: {material_list}
   Reply YES to confirm, or text another date if you can't make it.
   ```

4. **inbound SMS 监听**（Group B，待 wire）:
   - "DONE" 或 "YES" → 更新 per-user memory `training_status`
   - "HELP" → DM Linda 处理
   - 其他回复 → 标记为待处理，Linda 查看

### Phase 3：完成追踪

5. **培训当天签到确认**（如有现场培训）:
   Linda 在培训后输入 "mark training complete {training_name} {date} {attendees}"
   → hr-bot 批量更新 per-user memory

6. **截止日提醒升级**（cron，截止前 7 天）:
   未完成强制培训的人员 → 升级提醒 + DM 其店长（匿名：该员工本周需完成 {training_name}）

### Phase 4：合规报告

7. **季度合规报告**（每季度末 cron，TEXT ONLY，发 #hr-private + DM Linda）:
   ```
   [培训合规报告 · Q{n} {year}]

   强制培训完成率：{pct}%（目标 ≥95%）

   OSHA 10 安全：{n}/{total} 完成
     · 未完成：{n} 人（DM Linda 查看名单）

   LuxCore 认证：{n}/{total} 完成
     · 新增本季度：{n} 人

   防滑安全更新：{n}/{total} 完成

   ⚠️ 以下人员逾期超 30 天，影响上岗资格：{n} 人
   建议：Linda 在本周内处理
   ```

---

## Entity Memory 写入

```
training-{type}-Q{n}-{year}.md：
  type: {training_name}
  deadline: {date}
  required_count: {n}
  completed_count: {n}
  pending: [{employee_id}, ...]（不含姓名，保护隐私）
  completion_rate: {pct}%
```

## 失败处理

| 情况 | 行为 |
|------|------|
| SMS 未收到回复（7 天内）| 二次提醒，语气升级 |
| 员工反映无法参加 | DM Linda，安排补训 |
| 截止日过后仍未完成 | DM Linda + 匿名通知店长（影响上岗）|
| 外部培训链接失效 | DM Linda 更新链接，暂停提醒 SMS |
