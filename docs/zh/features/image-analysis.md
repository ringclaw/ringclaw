---
title: 图片解析
---

# 图片解析

在聊天中发送图片，AI 智能体会自动分析图片内容。RingClaw 下载消息中的图片附件，通过 [ACP 协议](https://agentclientprotocol.com/protocol/content#image-content) 传递给 AI 智能体。

## 工作原理

1. 用户发送带图片附件的消息（如「这张图里有什么？」）
2. RingClaw 通过认证的 `contentUri` 下载图片（最多 5 张，单张最大 5MB）
3. ACP 智能体：图片以 base64 编码作为 `ContentBlock::Image` 发送到 prompt
4. CLI/HTTP 智能体：追加一条文本提示作为降级处理

支持格式：**PNG**、**JPEG**、**GIF**、**WebP**。

## Agent 兼容性

图片解析需要 Agent 支持 ACP `promptCapabilities.image` 能力。

| Agent | 图片支持 | 说明 |
|-------|:---:|-------|
| Factory Droid | ✅ 已测试 | ACP 模式，`promptCapabilities.image` |
| Claude (ACP) | ✅ 已测试 | 需安装 `claude-agent-acp`（[见下方](#将-claude-切换到-acp-模式)） |
| Gemini (ACP) | 🔷 理论支持 | Gemini 模型原生支持多模态 |
| Cursor (ACP) | 🔷 理论支持 | 取决于底层模型 |
| GitHub Copilot (ACP) | 🔷 理论支持 | GPT-4o 支持多模态 |
| Cline (ACP) | 🔷 理论支持 | Cline 支持图片输入 |
| Claude (CLI) | ❌ 不支持 | CLI 模式无图片参数 |
| Codex (CLI) | ❌ 不支持 | CLI 模式，仅文本回退 |
| HTTP agents | ❌ 不支持 | 仅文本回退 |

**图例：**
- ✅ 已测试 — 已验证可用
- 🔷 理论支持 — 底层模型支持图片，但尚未在 RingClaw 中测试
- ❌ 不支持 — 该 Agent 类型不支持图片输入

不支持图片的 Agent 会收到带提示的文本消息：
```
[Note: 1 image(s) were attached but this agent does not support image input.]
```

## 将 Claude 切换到 ACP 模式

默认情况下，RingClaw 使用 `claude` CLI 二进制文件。要启用图片支持，需安装 ACP 适配器：

```bash
npm install -g @agentclientprotocol/claude-agent-acp
ringclaw restart
```

RingClaw 启动时会自动检测 `claude-agent-acp`，并将 Claude 从 CLI 模式升级为 ACP 模式，无需手动修改配置。

可以在日志中确认升级成功：
```
INF upgrading agent to ACP component=config name=claude from=cli path=/.../claude-agent-acp
INF agent available component=agents name=claude type=acp
```

## 配置

无需额外配置。图片解析在以下条件下自动生效：

1. 消息包含图片附件
2. 当前活跃的 Agent 实现了 `ImageSupporter` 接口（所有 ACP Agent 均已实现）

`config.json` 中 Agent 条目只需 `"type": "acp"`：

```json
{
  "agents": {
    "claude": {
      "type": "acp",
      "command": "/path/to/claude-agent-acp",
      "model": "sonnet"
    }
  }
}
```
