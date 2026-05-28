---
title: 聊天命令
---

# 聊天命令

在 RingCentral 聊天中发送以下命令：

| 命令 | 说明 |
|------|------|
| `你好` | 发送给默认 Agent |
| `/codex 写一个排序函数` | 发送给指定 Agent |
| `/cc 解释一下这段代码` | 通过别名发送 |
| `/cc /cx 解释一下这段代码` | 同时广播给多个 Agent，并行处理 |
| `/claude` | 切换默认 Agent 为 Claude |
| `/new` 或 `/clear` | 重置当前 Agent 会话 |
| `/cwd /path/to/project` | 切换所有 Agent 的工作目录 |
| `/task list\|create\|get\|update\|delete\|complete` | 管理任务 |
| `/note list\|create\|get\|update\|delete\|lock\|unlock` | 管理笔记 |
| `/event list [chatId]\|create\|get\|update\|delete` | 管理日历事件 |
| `/card get\|delete` | 管理 Adaptive Card |
| `/video create\|get\|delete` | 创建和管理 RingCentral Video bridge |
| `/phone ringout\|status\|cancel\|calllog` | 电话动作和通话记录；RingOut 仅 owner 可执行 |
| `/chatinfo [chatId]` | 查看聊天详情（名称、类型、成员数） |
| `总结我和 John 的聊天` | 总结某个对话 |
| `/cron list\|add\|delete\|enable\|disable` | 管理定时任务 |
| `/mem add [user\|chat\|global] <文本>` | 追加到 persona memory（默认当前 chat）。见 [配置 › persona](./configuration.md#persona)。 |
| `/mem show [user\|chat\|global]` | 查看对应 scope 的 memory |
| `/mem del [scope]` / `/mem del [scope] confirm` | 清空指定 scope memory（需要再发一次带 `confirm` 的命令） |
| `/persona` | 显示当前 SOUL.md 及其编辑路径 |
| `/info` | 查看当前 Agent 信息（别名：`/status`） |
| `/help` | 查看帮助信息 |

未知的 `/命令`（如 `/status`、`/compact`）会被转发给默认 Agent，因此 Agent 自身的斜杠命令可以透明使用。

## 快捷别名

| 别名 | Agent |
|------|-------|
| `/cc` | Claude |
| `/cx` | Codex |
| `/cs` | Cursor |
| `/km` | Kimi |
| `/gm` | Gemini |
| `/ocd` | OpenCode |
| `/oc` | OpenClaw |
| `/pi` | Pi |
| `/cp` | Copilot |
| `/dr` | Droid |
| `/if` | iFlow |
| `/kr` | Kiro |
| `/qw` | Qwen |
| `/ag` | Augment |

切换默认 Agent 会写入配置文件，重启后仍然生效。

## 多 Agent 广播

将同一条消息同时发送给多个 Agent，并行处理：

```
/cc /cx review this function  # 同时广播给 Claude 和 Codex，并行处理
```

每个 Agent 的回复以 `[agent-name]` 前缀分别发送。

## 会话管理

| 命令 | 说明 |
|------|------|
| `/new` | 重置默认 Agent 会话，重新开始 |
| `/clear` | 同 `/new` |

::: info Dify 注意
使用 Dify Agent 时，`/new` 和 `/clear` 会同时调用 Dify 服务端的 `DELETE /v1/conversations/{id}` 彻底清除历史。Endpoint 请使用 HTTPS，避免 nginx 301 重定向导致 POST 变 GET（405 错误）。
:::

## 动态工作目录

```bash
/cwd ~/projects/my-app    # 切换所有 Agent 到此目录
/cwd                       # 显示当前工作目录信息
```

波浪号（`~`）会自动展开为 home 目录。新工作目录立即对所有运行中的 Agent 生效。

## 任务、笔记与日历事件

在聊天中直接对 RingCentral Team Messaging 资源进行完整的增删改查：

```
/task create 修复登录 bug        # 创建任务
/task list                       # 列出当前聊天的任务
/task complete <id>              # 标记任务完成
/note create 会议纪要 | 内容     # 创建笔记（自动发布）
/event list                      # 列出日历事件
/video create 设计评审           # 创建 Video bridge 并返回入会链接
/phone calllog direction=Outbound # 查看最近的个人通话记录（仅 owner）
```

每个命令支持：`list`、`create`、`get`、`update`、`delete`。任务还支持 `complete`。

## Adaptive Card（自适应卡片）

AI Agent 可以生成 [Adaptive Card](https://adaptivecards.io/) 用于富文本结构化展示（进度报告、仪表盘、表单等）。当 Agent 在回复中包含 `ACTION:CARD` 块时，RingClaw 会自动将卡片发送到聊天：

```
ACTION:CARD
{"type":"AdaptiveCard","version":"1.3","body":[{"type":"TextBlock","text":"Sprint 状态","weight":"bolder"},{"type":"FactSet","facts":[{"title":"已完成","value":"12"},{"title":"剩余","value":"3"}]}]}
END_ACTION
```

通过聊天命令管理卡片：

```
/card get <id>       # 查看卡片详情
/card delete <id>    # 删除卡片
```

## CLI 命令索引

RingClaw 提供完整的 CLI，无需启动 Bridge 即可直接操作 RingCentral Team Messaging。所有命令均支持 `--json` 输出机器可读的 JSON 格式。

```
ringclaw
├── start [-f] [--api-addr]           # 启动桥接
├── stop                               # 停止后台进程
├── restart                            # 重启
├── status                             # 检查运行状态
├── setup                              # 交互式凭据配置
├── app-url                            # 生成预填的 Developer Console 应用创建链接
├── onboard                            # API 辅助的非交互式凭据/K8S onboarding
├── update [--channel beta|alpha] [--branch X]  # 自动更新
├── upgrade / version                  # 别名
│
├── message                            # 消息操作
│   ├── send <chatId> <text>           # 发送消息
│   ├── get  <chatId> <postId>        # 获取单条消息
│   ├── list <chatId> [--count N]     # 列出最近消息
│   ├── edit <chatId> <postId> <text> # 编辑消息
│   └── delete <chatId> <postId>      # 删除消息
│
├── chat                               # 聊天操作
│   ├── list [--type X] [--recent]    # 列出聊天
│   └── get <chatId>                  # 获取聊天详情
│
├── task                               # 任务操作
│   ├── list <chatId>                 # 列出任务
│   ├── create <chatId> <subject>     # 创建任务
│   ├── get <taskId>                  # 获取任务详情
│   ├── update <taskId> <key=value>   # 更新任务
│   ├── complete <taskId>             # 标记完成
│   └── delete <taskId>              # 删除任务
│
├── note                               # 笔记操作
│   ├── list <chatId>                 # 列出笔记
│   ├── create <chatId> <title> [body] # 创建并自动发布
│   ├── get <noteId>                  # 获取笔记
│   ├── update <noteId> <key=value>   # 更新笔记
│   ├── lock <noteId>                 # 锁定编辑
│   ├── unlock <noteId>              # 解锁
│   └── delete <noteId>              # 删除
│
├── event                              # 日历事件操作
│   ├── list [chatId]                 # 列出事件
│   ├── create <title> <start> <end>  # 创建事件
│   ├── get <eventId>                 # 获取事件
│   ├── update <eventId> <key=value>  # 更新事件
│   └── delete <eventId>             # 删除事件
│
├── card                               # 自适应卡片操作
│   ├── create <chatId> <json-file>   # 从 JSON 文件创建
│   ├── get <cardId>                  # 获取卡片
│   └── delete <cardId>              # 删除卡片
│
├── video                              # RingCentral Video 操作
│   ├── create <title> [--type X]     # 创建 bridge
│   ├── get <bridgeId>                # 获取 bridge 详情
│   └── delete <bridgeId>             # 删除 bridge
│
├── phone                              # RingCentral Phone 操作
│   ├── ringout <from> <to>           # 发起双腿 RingOut
│   ├── status <ringOutId>            # 查看 RingOut 状态
│   ├── cancel <ringOutId>            # 取消连接中的 RingOut
│   └── calllog [--limit N]           # 查看个人通话记录
│
├── user                               # 用户操作
│   ├── search <query>                # 搜索公司目录
│   └── get <personId>               # 获取用户信息
│
└── file                               # 文件操作
    └── upload <chatId> <file-path>   # 上传本地文件
```

**示例：**

```bash
# 按最近活动排序列出聊天
ringclaw chat list --recent --json

# 发送消息
ringclaw message send 123456 "来自 CLI 的消息"

# 列出聊天中的任务
ringclaw task list 123456

# 创建 RingCentral Video bridge
ringclaw video create "设计评审" --type Scheduled

# onboarding 时记录 Video/Phone 能力要求
ringclaw onboard --from-env --capability video --capability phone

# 从多 Bot manifest 渲染长期运行的 Kubernetes Pod
ringclaw onboard --manifest bots.json --output-dir ./rendered-bots --skip-validate \
  --k8s --k8s-namespace personal-ava
kubectl apply -f ./rendered-bots/personal-ava-summer/k8s.yaml

# 发起 RingOut（需要 RingOut scope 和 owner 权限）
ringclaw phone ringout +14155550100 +14155550199 --caller-id +14155550100

# 搜索公司目录
ringclaw user search "Alice"

# 上传文件
ringclaw file upload 123456 ./report.pdf
```
