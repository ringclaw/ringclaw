---
title: 工作目录白名单
---

# 工作目录白名单

`/cwd` 与底层的 `Agent.SetCwd` 都被锁在一份**目录根白名单**之
内。任何尝试切换到不在配置 root 之下的路径都会被拒绝并报错：

```text
Denied: path "/etc" escapes configured workspace allowlist [/home/alice/code /home/alice/.ringclaw/workspace]
```

白名单只约束 agent **起始工作目录** 在哪里被设定。它**不是**
ACP session 内部文件访问的沙箱——更宽的图景见
[ACP Full-Access › 第三层重要安全边界](./full-access#第三层重要安全边界)。

## 有效白名单

有效白名单是以下三者的并集（去重并解析符号链接）：

1. `config.json` 中 `agent_allow_workspace_list` 的所有条目。
2. 历史字段 `agent_workspace`（仍然是默认 cwd）。
3. `~/.ringclaw/workspace` —— 始终被隐式信任，避免内置默认
   cwd 被拒绝。

**denylist** 作为纵深防御副 check 保留：即使 allowlist 已经
允许某条路径，`/cwd` 仍会拒绝以下敏感目录：`.ssh`、`.gnupg`、
`.ringclaw`、`.aws`、`.kube`、`.config/gcloud`。两项检查与
full-access 状态无关。

## 示例配置

```jsonc
{
  // 默认 cwd（agent 启动时所在的目录）。
  "agent_workspace": "/home/alice/projects/main",

  // /cwd 可以切换到的额外目录。
  "agent_allow_workspace_list": [
    "/home/alice/projects/secondary",
    "/home/alice/scratch"
  ]
}
```

## 它**不**做什么

- 它**不**沙箱 ACP session 内部的文件读写。default 模式的 ACP
  agent 可以 `fs/read_text_file` 任何它有 OS 权限触及的路径；
  当 `allow_write: true`，也可以 `fs/write_text_file` 同样的路
  径。
- 它**不**阻止 `terminal/create` 创建的 shell 命令对白名单之
  外的路径执行操作。allowlist 只约束起始 cwd。
- 它**不**自动限制 ACP agent 内部使用的工具（例如 Claude 的
  grep / read 工具）。agent 选择使用什么由 OS 权限决定，而非
  由 RingClaw 决定。

更宽的文件权限图景见 [ACP Full-Access](./full-access)；`/cwd`
命令本身的第一层门控（群聊仅 owner）见
[命令授权](./command-authorization)。
