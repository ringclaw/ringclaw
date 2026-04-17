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

# 交互式配置（会提示输入 Bot Token、Chat ID 等）
ringclaw setup

# 启动
ringclaw start
```

> 所有配置都存放在 `~/.ringclaw/config.json` 中，不再读取 `RC_BOT_TOKEN`
> 等环境变量。运行 `ringclaw setup` 或直接编辑该文件，详见[配置](./configuration.md)。

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

   <a href="/images/rc-login.png" target="_blank"><img src="/images/rc-login.png" width="600" alt="RingCentral 开发者控制台登录" /></a>

2. 点击 **Register App** → 选择 **Bot Add-in**

   <a href="/images/rc-register-app.png" target="_blank"><img src="/images/rc-register-app.png" width="600" alt="注册应用" /></a>

   <a href="/images/rc-bot-addin.png" target="_blank"><img src="/images/rc-bot-addin.png" width="600" alt="选择 Bot Add-in" /></a>

3. 配置应用：
   - **Security** → **Application Scopes**：勾选 **Read Accounts**、**Read Messages**、**TeamMessaging**、**WebSockets Subscription**、**WebSockets**
   - **Access**：Private（仅限自己的账号）

   <a href="/images/rc-scopes.png" target="_blank"><img src="/images/rc-scopes.png" width="600" alt="应用权限配置" /></a>

4. 点击 **Create**
5. 进入 **Bot** 标签 → 点击 **Install** 将 Bot 安装到你的账号
6. 复制 Bot 标签页上显示的 **Bot Token**

   <a href="/images/rc-bot-token.png" target="_blank"><img src="/images/rc-bot-token.png" width="600" alt="复制 Bot Token" /></a>

### 第二步：获取 Chat ID

1. 在 RingCentral 中打开你和 Bot 的对话
2. 点击 **More** → **Copy conversation link**
3. 链接中 `/messages/` 后面的数字即为 Chat ID（如 `https://app.ringcentral.com/l/messages/1234567890` 中的 `1234567890`）

   <a href="/images/rc-chat-id.png" target="_blank"><img src="/images/rc-chat-id.png" width="600" alt="复制对话链接获取 Chat ID" /></a>

### 第三步：创建 Private App（可选）

Private App（REST API + JWT）可以启用以下高级功能：
- **Summarize** 其他聊天的对话
- **跨聊天操作**（读取其他聊天消息、在其他聊天创建任务等）

1. 在开发者控制台，点击 **Register App** → 选择 **REST API App (most common)**

   <a href="/images/rc-rest-api-app.png" target="_blank"><img src="/images/rc-rest-api-app.png" width="600" alt="选择 REST API App" /></a>

2. 配置应用：
   - **Auth**：JWT auth flow
   - **Security** → **Application Scopes**：勾选 **Read Accounts**、**Read Messages**、**TeamMessaging**、**WebSockets Subscription**、**WebSockets**
   - **Access**：Private
3. 点击 **Create** — 获取 **Client ID** 和 **Client Secret**
4. 进入 **Credentials** 标签 → **JWT Credentials** → 点击 **Create JWT Token**

   <a href="/images/rc-jwt-create.png" target="_blank"><img src="/images/rc-jwt-create.png" width="600" alt="创建 JWT Token" /></a>

5. 复制 JWT Token

   <a href="/images/rc-jwt-copy.png" target="_blank"><img src="/images/rc-jwt-copy.png" width="600" alt="复制 JWT Token" /></a>

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

# 通过 Docker（挂载 ~/.ringclaw 与宿主机共享 config.json）
docker run -it -v ~/.ringclaw:/root/.ringclaw \
  ghcr.io/ringclaw/ringclaw start
```
