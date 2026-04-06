---
title: 快速开始
---

# 快速开始

## 一键安装

```bash
# 一键安装（macOS/Linux）
curl -sSL https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.sh | sh

# 一键安装（Windows PowerShell）
irm https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.ps1 | iex

# 设置 Bot Token（必需）
export RC_BOT_TOKEN="your_bot_token"

# 启动
ringclaw start
```

就这么简单。启动时，RingClaw 会：

1. 通过 Bot App 的 WebSocket 连接 RingCentral
2. 自动检测已安装的 AI Agent（Claude、Codex、Gemini 等）
3. 保存配置到 `~/.ringclaw/config.json`
4. 开始接收和回复消息

## RingCentral 配置步骤

::: tip
创建好应用后，运行 `ringclaw setup` 可启动交互式向导，自动收集凭据、验证并保存配置文件。
:::

### 第一步：创建 Bot App（必须）

1. 前往 [RingCentral 开发者控制台](https://developers.ringcentral.com/console) 并登录
2. 点击 **Register App** → 选择 **Bot Add-in (No UI)**
3. 配置应用：
   - **Security** → App Scopes：勾选 **ReadAccounts**、**TeamMessaging**、**WebSocketsSubscription**
   - **Access**：Private（仅限自己的账号）
4. 点击 **Create**
5. 进入 **Bot** 标签 → 点击 **Add** 将 Bot 安装到你的账号
6. 复制 Bot 标签页上显示的 **Bot Token**

### 第二步：获取 Chat ID

1. 打开 [API Explorer → List Chats](https://developers.ringcentral.com/api-reference/Chats/listGlipChatsNew)
2. 登录后点击 **Try It Out**
3. 找到要监控的聊天，复制其 `id` 字段

### 第三步：创建 Private App（可选）

Private App（REST API + JWT）可以启用以下高级功能：
- **Summarize** 其他聊天的对话
- **跨聊天操作**（读取其他聊天消息、在其他聊天创建任务等）

1. 在开发者控制台，点击 **Register App** → 选择 **REST API App**
2. 配置应用：
   - **Auth**：JWT auth flow
   - **Security** → App Scopes：勾选 **ReadAccounts**、**TeamMessaging**、**WebSocketsSubscription**
   - **Access**：Private
3. 点击 **Create** — 获取 **Client ID** 和 **Client Secret**
4. 进入 **Credentials** 标签 → **JWT Credentials** → 点击 **Create JWT Token**
5. 复制 JWT Token

### 交互式配置向导

```bash
ringclaw setup
```

向导会：
- 提示输入 Bot Token（必需）
- 提示输入要监控的 Chat ID
- 可选配置 Private App 凭据（Client ID、Secret、JWT Token）
- 通过 RingCentral API 验证凭据有效性
- 将所有配置保存到 `~/.ringclaw/config.json`

## 安装渠道

```bash
curl -sSL .../install.sh | sh                 # stable（最新正式版）
curl -sSL .../install.sh | sh -s -- beta      # beta（最新 main 构建）
curl -sSL .../install.sh | sh -s -- alpha feature/my-branch  # alpha（指定分支构建）
```

**通过 CLI 切换渠道：**

```bash
ringclaw update                                    # 更新到最新正式版
ringclaw update --channel beta                     # 切换到 beta 渠道
ringclaw update --channel alpha --branch feature/foo  # 切换到 alpha 分支
```

::: info macOS 提示
安装脚本和 `ringclaw update` 会自动清除 Gatekeeper 隔离属性（`com.apple.quarantine`、`com.apple.provenance`），下载后的二进制文件不会被系统拦截。
:::

## 其他安装方式

```bash
# 通过 Go 安装
go install github.com/ringclaw/ringclaw@latest

# 通过 Docker
docker run -it -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  ghcr.io/ringclaw/ringclaw start
```
