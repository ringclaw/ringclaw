# RingClaw Sharing — Slide Content for Google Slides

---

## Slide 1: Title

**RingClaw — AI Agents in RingCentral**

John Lin
2026-04-27

---

## Slide 2: Sharing Highlights / You'll see

**What you'll see today:**

1. **Chat with AI agents directly in RC** — message the bot, get real-time replies from Claude, Codex, Gemini, Kimi — without leaving RingCentral
2. **Natural language → RC actions** — "help me create a task", "summarize my chat with Maxwell" → AI auto-creates Tasks, Notes, Events, sends messages
3. **Multi-agent in one chat** — broadcast to Claude + Codex simultaneously, compare answers, switch default agent in one command
4. **Send screenshots for AI analysis** — paste images in chat, AI reads and responds with analysis
5. **Cross-agent shared memory** — `/remember` saves preferences, every agent picks them up next session
6. **Built with coding agents** — how AI agents helped build RingClaw itself, and the lessons learned along the way

All demos run inside the RingCentral chat window you use every day.

---

## Slide 3: The Solution

**RingClaw = RingCentral + AI Agents**

在 RingCentral 聊天中直接访问所有 AI Agent：

- 给 bot 发消息 → AI 实时回复
- 一个聊天窗口，多个 AI agent
- 不需要离开 RingCentral

> Inspired by WeClaw (WeChat AI Agent Bridge)

---

## Slide 4: How It Works

**Architecture**

```
User
  ↓ sends message
RingCentral
  ↓ WebSocket event
RingClaw (Bot App)
  ↓ intent classification (AI)
  ↓ route to agent
  ├── Claude (ACP)
  ├── Codex (ACP)
  ├── Gemini (ACP)
  ├── Kimi (ACP)
  └── More...
  ↓ reply
RingCentral
  ↓ displays reply
User
```

- **Bot App** — 通过 WebSocket 实时接收/发送消息
- **Private App** (可选) — 解锁跨聊天总结等高级功能

---

## Slide 5: Agent Modes

三种接入方式，覆盖所有 AI Agent：

| Mode | 原理 | 特点 |
|------|------|------|
| **ACP** | 长驻子进程，JSON-RPC over stdio | 最快，复用进程和会话 |
| **CLI** | 每条消息启动新进程 | 简单，支持 session resume |
| **HTTP** | OpenAI 兼容 API | 接入 Dify、OpenClaw 等 |

启动时自动检测已安装的 agent，优先使用 ACP 模式。

---

## Slide 6: Feature — Multi-Agent Routing

**一个聊天窗口，所有 AI Agent**

```
/cc 帮我写一个排序函数          → Claude
/cx review this code            → Codex
/gm explain this error          → Gemini
/km 帮我翻译这段话              → Kimi
```

切换默认 agent：

```
/claude    → 之后所有消息默认发给 Claude
/codex     → 切换到 Codex
```

**Multi-Agent Broadcast** — 同时发给多个 agent：

```
/cc /cx review this function    → Claude 和 Codex 并行回复
```

---

## Slide 7: Feature — Chat Summarization

**自然语言总结聊天记录**

```
总结我和 Maxwell 上周的聊天
summarize my chat with John last week
先週のMaxwellとのチャットを要約して
```

- AI intent 分类 → 识别为 summarize 意图
- Agent 提取目标人名（支持多语言）
- 自动查找对应聊天 → 拉取消息 → 生成摘要

支持复合操作：

```
总结 maxwell 并用 note 发给他
→ 总结 + 自动创建 Note + 发送
```

---

## Slide 8: Feature — AI-Driven Actions

**Agent 自动执行 RingCentral 操作**

Agent 回复中包含 ACTION 指令，RingClaw 自动执行：

| ACTION | 效果 |
|--------|------|
| `ACTION:TASK` | 创建 Task |
| `ACTION:NOTE` | 创建 Note |
| `ACTION:EVENT` | 创建 Calendar Event |
| `ACTION:CARD` | 发送 Adaptive Card（富文本卡片） |
| `ACTION:MESSAGE` | 发消息到指定聊天 |

用户只需自然语言表达，AI 自动完成操作。

---

## Slide 9: Feature — Image Analysis

**发图片给 bot → AI 分析图片内容**

- 支持 PNG/JPEG/GIF/WebP，最多 5 张，每张最大 5MB
- ACP agent（如 Claude）支持图片输入
- 用途：截图分析、文档 OCR、UI 审查、图表解读

---

## Slide 10: Feature — Cron & Heartbeat

**定时任务**

```
/cron add "0 9 * * 1-5" 每天早上汇报昨天的进展
/cron list
/cron delete <id>
```

**Heartbeat 健康检查**

- 定期 check-in，确保 agent 正常运行
- 异常时自动通知

---

## Slide 11: Feature — Full CLI

**30+ 子命令，不需要启动 bridge 也能操作 RC**

```
ringclaw message send <chatId> "Hello"
ringclaw task list <chatId> --json
ringclaw chat list --recent
ringclaw user search "Alice"
ringclaw event create "Meeting" 2026-04-28T14:00:00Z 2026-04-28T15:00:00Z
```

所有命令支持 `--json` 输出。

---

## Slide 12: Live Demo

1. 发消息 → AI 实时回复（Thinking... → 结果）
2. 切换 agent：`/claude` → `/codex`
3. Multi-agent broadcast：`/cc /cx 帮我写一个排序函数`
4. 聊天总结："总结我和 Maxwell 这周的聊天"
5. 发截图 → AI 分析
6. CLI：`ringclaw chat list --recent --json`

---

## Slide 13: Built with Coding Agents

**RingClaw 本身就是用 coding agent 协作开发的**

| Metric | Value |
|--------|-------|
| Go 代码 | 11.7K 行 |
| 测试代码 | 5.7K 行 |
| Merged PRs | 62 |
| Commits | 247 |
| Contributors | 9 |
| 文档 | 33 pages (EN + CN) |
| Packages | 8 (agent, api, cmd, config, messaging, ringcentral, ...) |

---

## Slide 14: Inspect-Verify-Fix with Agents

**实例 1: API sortOrder 被静默忽略**

- Agent 写了 `sortOrder=Descending` → 代码看起来没问题
- 运行后发现列表顺序没变
- 贴日志给 agent → 查 API 文档 → 发现 Team Messaging API 不支持此参数
- 修复：`recordCount=250` + 客户端 `sort.Slice`

**实例 2: 中文名字提取失败**

- "帮忙总结下这周我跟John lin的聊天内容"
- Filler word 清洗结果："帮忙john lin内容" ← 干扰词清不完
- 解决：直接让 agent 自己提取名字，天然支持所有语言

**心得：给 agent 日志和测试失败，比描述 bug 更有效**

---

## Slide 15: Getting Started

**一行安装**

```bash
# macOS/Linux
curl -sSL https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.sh | sh

# Windows
irm https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.ps1 | iex
```

**交互式配置**

```bash
ringclaw setup
```

**文档**

https://ringclaw.github.io/ringclaw/

---

## Slide 16: Q&A

**Questions?**

- GitHub: github.com/ringclaw/ringclaw
- Docs: ringclaw.github.io/ringclaw
