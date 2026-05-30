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

::: info
RingCentral 没有公开 REST API 可以直接创建 Developer Console 应用。RingClaw 可以生成官方预填的创建应用链接：

```bash
ringclaw app-url
```

Personal AVA Pro 默认需要两个 App。Bot App 链接仍保持 messaging-only；Private JWT App 即使在 message-only 场景下也必填，并且至少需要 `ReadAccounts`。如果选择 Video/Phone，再额外带上 Video、RingOut、ReadCallLog：

```bash
ringclaw app-url --capability video --capability phone
```

打开生成的 Bot App 和 Private JWT App 链接，在 Developer Console 中确认创建后，再继续运行 `ringclaw setup`，或使用 API 辅助的非交互式流程：

```bash
export RC_BOT_TOKEN='<BOT_TOKEN>'
export RC_CLIENT_ID='<PRIVATE_APP_CLIENT_ID>'
export RC_CLIENT_SECRET='<PRIVATE_APP_CLIENT_SECRET>'
export RC_JWT_TOKEN='<OWNER_JWT>'
export RINGCLAW_BOT_ID='personal-ava-summer'
export RINGCLAW_TENANT_ID='fiji'
export RINGCLAW_OWNER_USER_ID='summer.gan'
export RINGCLAW_CAPABILITIES='video,phone'
ringclaw onboard --from-env --capability video --capability phone
```

`--capability video --capability phone` 会把 Bot 需要的 AVA 能力写入配置，并在输出中提示所需 RingCentral scopes。Private JWT App 默认必须具备 **ReadAccounts**。对于 Video/Phone，再额外配置 **Video**、**RingOut**、**ReadCallLog**。如果缺少 `client_id`、`client_secret` 或 `jwt_token`，`ringclaw onboard` 和 `ringclaw start` 会直接失败。
:::

::: tip Kubernetes 多 Bot 长期运行
MVP 阶段建议采用一个 RingClaw Pod 承载一个 Bot。每个 Pod 使用独立 Secret、独立配置路径和稳定 Bot 身份：

```bash
export RINGCLAW_CONFIG=/etc/ringclaw/config/config.json
ringclaw onboard --from-env --config-out "$RINGCLAW_CONFIG"
ringclaw start -f
```

当 AVA Control Plane 负责 onboarding 和 K8S 渲染时，Pod 可以在启动时向控制面领取配置，而不是把长期凭据直接写死在 Deployment 中：

```bash
ringclaw runtime start \
  --control-plane https://ava-control-plane.example \
  --bot-id personal-ava-summer \
  --bootstrap-token "$RINGCLAW_BOOTSTRAP_TOKEN" \
  --pod-name "$HOSTNAME"
```

上线检查时可以加 `--dry-run`：RingClaw 会领取配置、写入 `RINGCLAW_CONFIG`、发送一次 healthy heartbeat，然后退出，不会连接 RingCentral 消息 runtime。这样可以先验证 Control Plane、bootstrap Secret、Pod identity 和 heartbeat 链路，再切到长期运行模式。

为每个 Bot 设置 `RINGCLAW_BOT_ID` 和 `RINGCLAW_TENANT_ID`。RingClaw 会用它们给 AI Agent 会话加 namespace，这样多个 Bot Pod 即使共用同一个 Codex / Dify / OpenAI-compatible gateway，也不会因为相同 chat/user 维度导致上下文串线。更强隔离时，为每个 Bot 在 Secret 中配置独立 Agent token，并通过 agent `env`、`api_key` 或 custom headers 注入。

N 个 Bot 批量上线时，可以用 manifest 一次渲染每个 Bot 的独立配置：

```json
{
  "defaults": {
    "tenant_id": "fiji",
    "server_url": "https://platform.ringcentral.com",
    "default_agent": "codex",
    "agents": {
      "codex": {
        "type": "http",
        "endpoint": "https://agent-gateway.example.com/v1/chat/completions",
        "api_key": "${CODEX_GATEWAY_TOKEN}"
      }
    }
  },
  "bots": [
    {
      "bot_id": "personal-ava-summer",
      "owner_user_id": "summer.gan",
      "bot_token": "${SUMMER_RC_BOT_TOKEN}",
      "client_id": "${SUMMER_RC_CLIENT_ID}",
      "client_secret": "${SUMMER_RC_CLIENT_SECRET}",
      "jwt_token": "${SUMMER_RC_JWT_TOKEN}",
      "capabilities": ["video", "phone"],
      "chat_ids": ["123"]
    },
    {
      "bot_id": "personal-ava-alice",
      "owner_user_id": "alice",
      "bot_token": "${ALICE_RC_BOT_TOKEN}",
      "chat_ids": ["456"],
      "agents": {
        "codex": {
          "type": "http",
          "endpoint": "https://agent-gateway.example.com/v1/chat/completions",
          "api_key": "${ALICE_CODEX_GATEWAY_TOKEN}"
        }
      }
    }
  ]
}
```

```bash
ringclaw onboard --manifest bots.json --output-dir ./rendered-bots --skip-validate \
  --k8s --k8s-namespace personal-ava \
  --k8s-image ghcr.io/ringclaw/ringclaw:latest
```

每个渲染出来的目录都会包含 `config.json`；带上 `--k8s` 时，还会生成一个 `k8s.yaml`，里面包含 Opaque Secret 和单副本 Deployment。每个 Bot 可以独立 apply：

```bash
kubectl apply -f ./rendered-bots/personal-ava-summer/k8s.yaml
```

Secret 保存完整 RingClaw 配置，包括 Bot Token、JWT App 凭据、已选择的 capabilities、chat IDs 和每个 Bot 独立的 agent token。Deployment 会把它挂载到 `/etc/ringclaw/config/config.json` 并执行 `ringclaw start -f`，因此运行路径与本地 MVP 用法保持一致。
:::

### 第一步：创建 Bot App（必须）

1. 前往 [RingCentral 开发者控制台](https://developers.ringcentral.com/console) 并登录

   <a href="/images/rc-login.png" target="_blank"><img src="/images/rc-login.png" width="600" alt="RingCentral 开发者控制台登录" /></a>

2. 点击 **Register App** → 选择 **Bot Add-in**

   <a href="/images/rc-register-app.png" target="_blank"><img src="/images/rc-register-app.png" width="600" alt="注册应用" /></a>

   <a href="/images/rc-bot-addin.png" target="_blank"><img src="/images/rc-bot-addin.png" width="600" alt="选择 Bot Add-in" /></a>

3. 配置应用：
   - **Security** → **Application Scopes**：勾选 **Read Accounts**、**Read Messages**、**TeamMessaging**、**WebSocketsSubscription**
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

### 第三步：创建 Private App

Private App（REST API + JWT）是 RingClaw 注册的必需配置。它提供 owner-scoped client，用于账号校验、总结、跨聊天操作，以及可选的 Video/Phone 操作：
- **Summarize** 其他聊天的对话
- **跨聊天操作**（读取其他聊天消息、在其他聊天创建任务等）
- **Video/Phone 操作**（创建 RingCentral Video bridge、owner 授权 RingOut、读取个人 Call Log）

1. 在开发者控制台，点击 **Register App** → 选择 **REST API App (most common)**

   <a href="/images/rc-rest-api-app.png" target="_blank"><img src="/images/rc-rest-api-app.png" width="600" alt="选择 REST API App" /></a>

2. 配置应用：
   - **Auth**：JWT auth flow
   - **Security** → **Application Scopes**：至少勾选基础必需 scope **Read Accounts**
   - 如需 Personal AVA Pro Video/Phone：添加 **Video**、**RingOut**、**ReadCallLog**
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
- 提示输入 Private App 凭据（Client ID、Secret、JWT Token）
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
