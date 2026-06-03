---
name: pto-routing
description: 员工请假申请受理 + 审批路由 + 日历更新 + 匿名班组缺口广播
version: 1.0.0
metadata:
  tags: [hr, pto, approval, privacy]
  applicable_souls: [hr-service]
  entity_type: pto-request
prerequisites:
  capabilities: [sms]
  memory_keys: [employee_directory, approval_chain]
---

# PTO Routing

## 触发条件
员工 DM hr-bot 提交请假请求（含日期 + 任意理由描述）

## 步骤

1. **接收** — 读取请假日期（必填）和理由（仅存 HR，不外传）
2. **余额查询** — 读 per-user memory `{employee-id}.md` 的 `pto_balance` 字段
3. **回复员工**（在 DM 原 chat，无跨 chat）:
   ```
   收到，{名字}。
   请假：{start} — {end}（{n} 天）
   余额：{current} → {after} 天
   通知 {队长} 审批，理由保密。结果在这里告诉你。
   ```
4. **路由审批**（跨 chat → 队长 DM，Linda OOB approve）:
   ```
   请假审批请求
   日期：{start} — {end}（{n} 天）
   班组影响：这 {n} 天少 {impact} 名协助
   （理由保密）
   批准：回 hr-bot "approve {date}"  拒绝：回 "deny {date} [原因]"
   ```
5. **批准后**（队长回复）:
   - ACTION:EVENT title="PTO" start={start} end={end}（员工日历）
   - 更新 per-user memory：`pto_balance -= n`
   - 回复员工 ✅
6. **匿名广播**（跨 chat → #<store>-ops，Linda OOB approve）:
   ```
   班组缺口：{队长} 队 {start}—{end} -{n} 名协助。（来源：HR 保密。）
   ```

## 特殊路径

| 场景 | 处理 |
|------|------|
| 队长自己申请请假 | 升级到店长审批 |
| 余额不足 | 告知员工余额，询问是否继续（无薪假） |
| 紧急请假（< 24h 前）| 标记为紧急，同时 DM 店长 |
| 连续超 5 天 | 要求附病假证明，Linda 手动处理 |

## 信息隔离规则

**永远不传递**：请假原因、医疗内容、家庭细节
**队长看到**：日期 + 班组影响
**店长看到**：班组缺口广播（无姓名无原因）
**HR 看到**：全部

## Entity Memory 写入

```
pto-{employee-id}-{date}.md：
  status: pending / approved / denied
  dates: {start} to {end}
  balance_before: {n}  balance_after: {n}
  approver: {队长 ID}
  approved_at: {timestamp}
  -- 理由字段留空（永不写入）--
```
