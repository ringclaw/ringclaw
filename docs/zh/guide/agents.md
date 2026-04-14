---
title: Agent 配置
---

# Agent 配置

RingClaw 支持三种 Agent 类型：**ACP**、**CLI** 和 **HTTP**。每个 Agent 在 `config.json` 的 `agents` 部分配置。

## 推荐：ACP 模式

ACP（Agent Communication Protocol）是所有 Agent 的**推荐**运行模式：

- **会话持久化** — 多轮对话保持完整上下文
- **更快** — 复用长驻进程，无需每条消息启动新进程
- **自动权限** — 无需 `--dangerously-skip-permissions` 等参数

CLI 模式每条消息启动新进程，**没有多轮上下文**——每条消息都是独立的。

### ACP 兼容性列表

| Agent | ACP 命令 | 安装方式 |
|-------|---------|---------|
| Claude | `claude-agent-acp` | `npm i -g @agentclientprotocol/claude-agent-acp` |
| Codex | `codex-acp` | `npm i -g @zed-industries/codex-acp` |
| Cursor | `agent acp` | Cursor CLI 内置 |
| Gemini | `gemini --acp` | Gemini CLI 内置 |
| Kimi | `kimi acp` | Kimi CLI 内置 |
| Copilot | `copilot --acp --stdio` | Copilot CLI 内置 |
| Droid | `droid exec --output-format acp` | Factory Droid 内置 |
| iFlow | `iflow --experimental-acp` | iFlow CLI 内置 |
| Kiro | `kiro-cli acp` | Kiro CLI 内置 |
| Qwen | `qwen --acp` | Qwen CLI 内置 |
| Augment | `auggie --acp` | Auggie CLI 内置 |
| OpenCode | `opencode acp` | OpenCode 内置 |
| Pi | `pi-acp` | `npm i -g pi-acp` |

### 从 CLI 升级到 ACP

1. 安装 ACP 适配器（如 `npm install -g @zed-industries/codex-acp`）
2. 在聊天中使用 `/reload` — RingClaw 会自动重新检测并升级
3. 或重启 RingClaw — 启动时会自动检测

::: tip
RingClaw 启动时自动检测已安装的 Agent。如果同时存在 CLI 和 ACP 两种方式，优先使用 ACP。也支持 npx 方式 — 如果没安装独立 binary，RingClaw 会尝试通过 `npx @zed-industries/codex-acp` 运行。
:::

## Agent 类型

### ACP（Agent Communication Protocol）

长驻子进程，通过 stdio JSON-RPC 通信。速度最快，复用进程和会话。

```json
{
  "agents": {
    "claude": {
      "type": "acp",
      "command": "/usr/local/bin/claude-agent-acp",
      "model": "sonnet",
      "aliases": ["ai"],
      "env": {
        "ANTHROPIC_API_KEY": "sk-xxx"
      }
    }
  }
}
```

### CLI

每条消息启动一个新进程，支持通过 `--resume` 恢复会话。

```json
{
  "agents": {
    "claude": {
      "type": "cli",
      "command": "/usr/local/bin/claude",
      "cwd": "/home/user/my-project",
      "args": ["--dangerously-skip-permissions"]
    }
  }
}
```

### HTTP

OpenAI 兼容的 Chat Completions API。支持 `openai`（默认）、`nanoclaw`、`dify` 三种格式。

```json
{
  "agents": {
    "openclaw": {
      "type": "http",
      "endpoint": "https://api.example.com/v1/chat/completions",
      "api_key": "sk-xxx",
      "model": "openclaw:main"
    },
    "dify": {
      "type": "http",
      "format": "dify",
      "endpoint": "https://api.dify.ai/v1/chat-messages",
      "api_key": "app-xxx",
      "aliases": ["df"]
    }
  }
}
```

## 自定义别名

可以在 `config.json` 中为每个 Agent 定义自定义触发别名：

```json
{
  "claude": {
    "type": "acp",
    "command": "/usr/local/bin/claude-agent-acp",
    "aliases": ["gpt", "ai"]
  }
}
```

之后 `/gpt 你好` 会路由到 Claude。如果自定义别名与内置别名或其他 Agent 冲突，RingClaw 启动时会发出警告。

## 权限配置

部分 Agent 默认需要交互式权限确认，在消息机器人场景下无法操作。可通过 `args` 配置跳过：

| Agent | 参数 | 说明 |
|-------|------|------|
| Claude (CLI) | `--dangerously-skip-permissions` | 跳过所有工具权限确认 |
| Codex (CLI) | `--skip-git-repo-check` | 允许在非 git 仓库目录运行 |

配置示例：

```json
{
  "agent_workspace": "/home/user/my-project",
  "claude": {
    "type": "cli",
    "command": "/usr/local/bin/claude",
    "cwd": "/home/user/my-project",
    "args": ["--dangerously-skip-permissions"]
  },
  "codex": {
    "type": "cli",
    "command": "/usr/local/bin/codex",
    "cwd": "/home/user/my-project",
    "args": ["--skip-git-repo-check"]
  }
}
```

::: warning
这些参数会跳过安全检查，请了解风险后再启用。ACP 模式的 Agent 会自动处理权限，无需配置。
:::

## 工作目录配置

- **`agent_workspace`**（顶层）：指定所有 Agent 的默认工作目录。RingClaw 会先进入这个 workspace，再调用对应 Agent。
- **`cwd`**（Agent 级别）：覆盖该 Agent 的 `agent_workspace` 配置。
- 如果两者都未设置，则默认使用 `~/.ringclaw/workspace`。

```json
{
  "agent_workspace": "/home/user/my-project",
  "agents": {
    "claude": {
      "type": "acp",
      "command": "claude-agent-acp",
      "cwd": "/home/user/special-project"
    }
  }
}
```
