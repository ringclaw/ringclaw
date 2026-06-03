---
name: hiring-jd-generator
description: 招聘需求沟通 + JD 自动生成——从用人部门提需求，到生成结构化 JD，到 Linda 审核发布
version: 1.0.0
metadata:
  tags: [hr, hiring, jd, recruitment, job-description]
  applicable_souls: [hr-service]
  entity_type: hiring-request
prerequisites:
  capabilities: [sms]
  memory_keys: [role_templates, company_profile, state_labor_requirements]
---

# Hiring JD Generator

## 触发条件

三种触发方式：

1. **店长路由**（Agent→Agent）
   tom-bot 在 #atlanta-ops 检测到班组缺口 + 决定招人，发送：
   `[AGENT_ROUTE:HIRING_REQUEST]`

2. **Linda 直接触发**（Human→Agent）
   Linda 在 hr-bot DM 或 #hr-private 说：
   "生成 JD：{角色} · {门店} · {特殊要求}"

3. **定期触发**（高流失率岗位）
   Cron 每月检查 per-user memory 中 employment_type=W2 且
   tenure < 3 months 的记录，若本月离职率 > 阈值 → 触发招募预警

---

## 步骤

### Phase 1：需求收集

1. **解析需求**（从路由消息或 Linda 输入）：
   - 岗位类型：CSR / Installer / Crew Lead / Warehouse
   - 所在门店 + 州（影响薪资范围和法律条款）
   - 专项材料（如需要：Engineered Oak / LuxCore / Tile）
   - 急迫程度（立即 / 本月内 / 季度内）
   - W-2 或 1099

2. **读 DOMAIN.md 角色模板**（role_templates 字段）：
   - 基础职责描述
   - 必需技能
   - 工作强度说明
   - Keller 提供的福利列表

3. **如信息不完整**，向 Linda 追问（在 #hr-private）：
   "需要确认：{缺失字段}。请补充后我生成 JD。"

### Phase 2：JD 生成

4. **生成结构化 JD**，ACTION:CARD 发到 #hr-private（Linda 审核）：

```
JD 草稿 · {角色} · {门店}

━━ 职位信息 ━━
职位：{title}
门店：Keller Interiors · {store}，{state}
雇佣类型：{W-2 / 1099}
薪资范围：{range}（基于{state}市场水平）

━━ 职位描述 ━━
{2-3 段，描述 Keller 公司背景 + 该岗位在团队中的位置}

━━ 主要职责 ━━
· {从 role_template 提取，结合具体需求定制}

━━ 任职要求 ━━
· {硬性要求：体力/工具/认证}
· {加分项：材料专项经验}

━━ Keller 提供 ━━
· {福利列表：医保/PTO/培训/工具}

━━ 申请方式 ━━
发简历至：hr@keller.com 或 SMS 至：{hr_phone}
主题注明：{role} · {store}
```

Card 按钮：[批准发布] [修改] [暂不发布]

### Phase 3：审核与发布

5. **Linda 审核**：
   - 点击「批准发布」→ 触发 Phase 4
   - 点击「修改」→ Linda 输入修改意见，重新生成（最多 3 轮）
   - 点击「暂不发布」→ 存入 entity memory draft 状态

6. **JD 发布**（Linda 批准后）：

   a. ACTION:NOTE title="JD · {角色} · {门店} · {日期}"（存 #hr-private，持久化）
   
   b. 发布到候选人渠道（Linda 配置的渠道）：
      - 内部推荐 SMS 广播（现有员工）：
        "Keller 正在 {门店} 招募 {角色}！认识合适的人？
         回复此消息推荐，成功入职奖励 $200。"
      - 外部分包商网络（如果是 1099 安装工）：
        直接用 subcontractor-onboarding skill 的 SMS 模板

   c. ACTION:TASK subject="招募跟进 · {角色} · {门店}"
      assignee=Linda · due={急迫程度对应截止日}

   d. 写入 entity memory：
      `hiring-{role}-{store}-{date}.md`：status=open

### Phase 4：候选人跟踪

7. **inbound SMS 候选人回复**（Group B，待 wire）：
   候选人回复内推 SMS → hr-bot 在 #hr-private 汇总
   
   **Group A 替代方案**：
   Linda 在 hr-bot DM 输入："新候选人 {名字} {电话} for {JD}"
   → hr-bot 发欢迎 SMS + 安排初步沟通 + 更新 entity memory

8. **JD 关闭**（招到人后）：
   Linda 说"JD 关闭，{名字} 录用"
   → entity memory status=hired
   → 触发 new-hire-onboarding skill（见下一个 skill）

---

## 角色 JD 模板（DOMAIN.md 预置，hiring.role_templates）

```markdown
## CSR (Customer Service Rep)
基础职责: 接受 Lowe's 客户预约，安排施工队，跟踪订单状态，处理客户沟通
关键技能: 多任务处理、客户沟通、RC Team Messaging 使用
薪资参考: $18-22/小时（W-2）
工作强度: 每天 20-30 张派单，移动端为主
特别说明: 与 AI 助手协作（AgentRun），需要接受系统使用培训

## Installer（安装工 W-2）
基础职责: 地板安装施工，Lowe's 项目
关键技能: 体力劳动、工具操作，{material_specialty} 经验
薪资参考: $20-28/小时（视材料专项和经验）
工作强度: 日均 1-3 个安装单，工地环境，需要有效驾照

## Installer（安装工 1099）
同上，但按完工面积付款（sqft 费率）
无固定时薪，需要自带工具，需要自有责任险

## Crew Lead
基础职责: 带领 3-4 人安装队，现场管理，客户沟通
关键技能: 领导力，多种材料施工经验，调度管理
薪资参考: $25-35/小时（W-2）
```

---

## 失败处理

| 情况 | 行为 |
|------|------|
| 需求信息不完整 | 追问 Linda，不生成不完整 JD |
| Linda 3 轮修改后仍不满意 | 存草稿，标记"需 Linda 手动完成"，DM 提醒 |
| 内推 SMS 发送失败 | DM Linda，手动处理 |
| JD 超过 30 天未关闭 | 每周一 cron 提醒 Linda："{角色} JD 仍 open，{n} 天了" |
