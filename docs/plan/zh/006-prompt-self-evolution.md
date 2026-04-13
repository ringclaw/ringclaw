# 计划 006: Prompt 自进化

**日期:** 2026-04-13
**优先级:** P3
**状态:** Draft
**参考:** [hermes-agent-self-evolution](https://github.com/NousResearch/hermes-agent-self-evolution) (DSPy + GEPA)

## 问题描述

RingClaw 在 `messaging/prompts.go` 中有 5 个集中管理的 prompt（ActionPrompt、IntentPrompt、NameExtractPrompt、SummaryPrompt、HeartbeatPrompt）。目前全靠手动调优，没有系统化的评估方式。问题都是通过用户反馈被动发现的：

- IntentPrompt："总结 John 的代码"被错误分类为"summarize"而非"chat"
- NameExtractPrompt："总结 maxwell 上周的"提取出"maxwell 上周"（包含了时间词）
- ActionPrompt：agent 将 person ID 作为 chatid 使用，尽管有明确规则禁止
- ActionPrompt：agent 为请求选择了错误的 ACTION 类型

目前无法：
1. 在修改前后量化 prompt 质量
2. 修改 prompt 时发现回归问题
3. 利用真实失败数据系统性地改进 prompt

## 目标

1. 构建评估工具，用 golden test cases 评分 prompt 质量
2. 为 ActionPrompt 和 IntentPrompt 提供基线分数
3. 支持数据驱动的 prompt 迭代（修改 → 测量 → 对比）
4. （Phase 2）用 LLM 自动提出 prompt 改进方案 + 约束检查

## 非目标

- 引入 Python/DSPy 依赖（保持纯 Go）
- 挖掘外部工具的会话历史（RingClaw 不存储本地历史）
- 进化代码（仅限 prompt 文本）
- 自动提交进化后的 prompt（始终通过 PR 人工审查）
- 运行时/生产环境的 prompt 进化（仅离线工具）

## 背景：Hermes GEPA 方法

[hermes-agent-self-evolution](https://github.com/NousResearch/hermes-agent-self-evolution) 项目使用以下方式进化 prompt：

1. **DSPy + GEPA 优化器** — 将 prompt 文本包装为可参数化模块，通过遗传-Pareto 进化进行变异
2. **LLM 裁判评分** — 在 3 个维度评估：正确性（0.5）、流程遵循（0.3）、简洁性（0.2）
3. **约束检查** — 大小 ≤15KB、增长 ≤20%、测试套件必须 100% 通过
4. **执行轨迹反思** — GEPA 读取失败*原因*（而非仅知道失败了），提出针对性变异
5. **多来源评估数据** — 合成（LLM 生成）、sessiondb（从 Claude/Copilot 历史挖掘）、golden（手动策划）

核心洞察：昂贵的部分（LLM 裁判 + GEPA）通过 API 调用离线运行，无需 GPU，每次运行约 $2-10。

## 架构

### Phase 1：评估工具 + Golden 数据集

```
golden.jsonl ──► eval_prompt.go ──► Agent (ACP) ──► LLM 裁判 ──► 评分报告
                      │                                              │
                      └── 从 prompts.go 加载 prompt ─────────────────┘
```

**组件：**

| 组件 | 路径 | 描述 |
|------|------|------|
| 评估运行器 | `scripts/eval_prompt.go` | CLI 工具：加载 prompt → 运行测试 → 评分 → 报告 |
| Golden 数据集 | `datasets/prompts/<name>/golden.jsonl` | 手动策划（task_input, expected_behavior, difficulty） |
| 评分报告 | stdout + `output/prompt-eval/<name>/report.json` | 单例分数 + 汇总 |

**Golden 数据集格式** (`golden.jsonl`)：
```json
{"task_input": "总结 John 的代码", "expected_behavior": "分类为 'chat'（非 'summarize'），因为这是问代码而非聊天消息", "difficulty": "hard", "category": "boundary"}
{"task_input": "总结一下最近的消息", "expected_behavior": "分类为 'summarize'，因为这是请求聊天消息摘要", "difficulty": "easy", "category": "basic"}
```

**评分维度**（改编自 Hermes `FitnessScore`）：
- 正确性（权重 0.5）：agent 是否产生了预期输出？
- 流程遵循（权重 0.3）：是否遵循了 prompt 的指令？
- 简洁性（权重 0.2）：响应是否适当简洁？
- 长度惩罚：从 90% 大小时的 0 线性增加到 100%+ 时的 0.3

**约束检查：**
- Prompt 大小 ≤ 15,000 字符
- 增长 ≤ 20%（相对基线）
- 所有现有测试通过（`go test ./messaging/...`）

**使用方式：**
```bash
go run scripts/eval_prompt.go --prompt intent --agent claude --iterations 1
go run scripts/eval_prompt.go --prompt action --agent claude --compare prompts/action_v2.md
```

### Phase 2：自动变异（未来）

```
失败轨迹 ──► 变异 LLM ──► 候选 prompt ──► 评估工具 ──► 更优则保留
                 │                              │
                 └──── 约束检查 ◄───────────────┘
```

1. 从 Phase 1 收集失败测试用例 + 执行轨迹
2. 发送给 LLM："这是当前 prompt，这些是失败案例。请提出改进版本。"
3. 用评估工具运行改进后的 prompt
4. 如果分数提升且通过约束检查 → 保存为候选
5. 重复 N 次迭代，保留最佳变体
6. 输出 diff 供人工审查

## Prompt 进化优先级

| Prompt | 失败模式 | 进化价值 | 阶段 |
|--------|----------|----------|------|
| **ActionPrompt** | 选错 action 类型、person ID 作 chatid、缺少字段 | 高 | 1 |
| **IntentPrompt** | 边界情况（代码摘要 vs 聊天摘要） | 高 | 1 |
| **NameExtractPrompt** | 包含时间词、部分名称 | 中 | 1 |
| **SummaryPrompt** | 运行良好，投诉少 | 低 | 2 |
| **HeartbeatPrompt** | 运行良好 | 低 | 2 |

## 实施计划

### Phase 1（约 200 行 Go + JSONL 文件）

| 步骤 | 文件 | 描述 |
|------|------|------|
| 1 | `datasets/prompts/intent/golden.jsonl` | 15-20 个手动策划的意图分类测试用例 |
| 2 | `datasets/prompts/action/golden.jsonl` | 15-20 个手动策划的 ACTION 生成测试用例 |
| 3 | `scripts/eval_prompt.go` | 评估运行器：加载数据集 → 运行 agent → LLM 裁判 → 报告 |
| 4 | docs | 更新本计划状态 |

### Phase 2（约 400 行 Go，未来）

| 步骤 | 文件 | 描述 |
|------|------|------|
| 1 | `scripts/mutate_prompt.go` | 变异引擎：读取轨迹 → 提出修改 → 重新评估 |
| 2 | `scripts/eval_prompt.go` | 添加 `--evolve` 标志支持自动迭代 |
| 3 | 约束验证 | 大小/增长检查 + `go test` 门控 |

## 待定问题

1. 裁判 LLM 和被测 agent 是否应该使用同一个模型？（Hermes 使用不同模型）
2. 需要多少 golden 样例才能得到有意义的信号？（Hermes 使用 20 个，按 50/25/25 划分）
3. 评估结果是否应该纳入 git 以检测回归？
