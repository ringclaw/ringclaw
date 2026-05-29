---
title: Getting Started
---

# Getting Started

## Quick Start

```bash
# One-line install (macOS/Linux)
curl -sSL https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.sh | sh

# One-line install (Windows PowerShell)
irm https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.ps1 | iex

# Interactive setup (prompts for bot token, chat IDs, etc.)
ringclaw setup

# Start
ringclaw start
```

> All configuration lives in `~/.ringclaw/config.json`. Environment
> variables such as `RC_BOT_TOKEN` are no longer consulted — run
> `ringclaw setup` or edit the file directly. See
> [Configuration](./configuration.md).

That's it. On first start, RingClaw will:
1. Connect to RingCentral via the Bot App's WebSocket
2. Auto-detect installed AI agents (Claude, Codex, Gemini, etc.)
3. Save config to `~/.ringclaw/config.json`
4. Start receiving and replying to messages

## RingCentral Setup

::: tip
After creating your apps, run `ringclaw setup` for an interactive wizard that collects credentials, validates them, and saves the config file.
:::

::: info
RingCentral does not expose a public REST endpoint for creating Developer Console apps. RingClaw can generate official pre-filled create-app URLs:

```bash
ringclaw app-url
```

Personal AVA Pro requires both apps. The Bot App link stays messaging-only. The Private JWT App is required even for message-only bots and must include `ReadAccounts`; when Video/Phone is selected, include the extra Video, RingOut, and ReadCallLog scopes:

```bash
ringclaw app-url --capability video --capability phone
```

Open the generated Bot App and Private JWT App links, confirm creation in the Developer Console, then continue with `ringclaw setup` or the API-assisted non-interactive flow:

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

`--capability video --capability phone` records the bot's requested AVA capabilities in config and prints the required RingCentral scopes. The Private JWT App always needs **ReadAccounts**. For Video/Phone, add **Video**, **RingOut**, and **ReadCallLog**. `ringclaw onboard` and `ringclaw start` fail fast if `client_id`, `client_secret`, or `jwt_token` is missing.
:::

::: tip Kubernetes multi-bot runtime
For long-lived K8S deployments, run one RingClaw pod per bot in the MVP phase. Give every pod its own Secret, config path, and bot identity:

```bash
export RINGCLAW_CONFIG=/etc/ringclaw/config/config.json
ringclaw onboard --from-env --config-out "$RINGCLAW_CONFIG"
ringclaw start -f
```

Set `RINGCLAW_BOT_ID` and `RINGCLAW_TENANT_ID` for every bot. RingClaw uses them to namespace AI agent conversations, so multiple bot pods can share the same Codex/Dify/OpenAI-compatible gateway without cross-bot context collisions. For stronger isolation, configure per-bot agent tokens in each pod's Secret and inject them through the agent `env`, `api_key`, or custom headers.

For N-bot rollout, render one config per bot from a manifest:

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

Each rendered folder contains a `config.json` and, when `--k8s` is set, a `k8s.yaml` with an Opaque Secret and one-replica Deployment. Apply each bot manifest independently:

```bash
kubectl apply -f ./rendered-bots/personal-ava-summer/k8s.yaml
```

The Secret stores the full RingClaw config, including Bot Token, JWT App credentials, selected capabilities, chat IDs, and per-bot agent tokens. The Deployment mounts it at `/etc/ringclaw/config/config.json` and runs `ringclaw start -f`, so the runtime path is the same as local MVP usage.
:::

### Step 1: Create a Bot App (Required)

1. Go to [RingCentral Developer Console](https://developers.ringcentral.com/console) and sign in

   <a href="/images/rc-login.png" target="_blank"><img src="/images/rc-login.png" width="600" alt="RingCentral Developer Console login" /></a>

2. Click **Register App** → select **Bot Add-in**

   <a href="/images/rc-register-app.png" target="_blank"><img src="/images/rc-register-app.png" width="600" alt="Register App" /></a>

   <a href="/images/rc-bot-addin.png" target="_blank"><img src="/images/rc-bot-addin.png" width="600" alt="Select Bot Add-in" /></a>

3. Configure the app:
   - **Security** → **Application Scopes**: check **Read Accounts**, **Read Messages**, **TeamMessaging**, **WebSocketsSubscription**
   - **Access**: Private (only your own account)

   <a href="/images/rc-scopes.png" target="_blank"><img src="/images/rc-scopes.png" width="600" alt="Application Scopes" /></a>

4. Click **Create**
5. Go to the **Bot** tab → click **Install** to install the bot to your account
6. Copy the **Bot Token** shown on the Bot tab

   <a href="/images/rc-bot-token.png" target="_blank"><img src="/images/rc-bot-token.png" width="600" alt="Copy Bot Token" /></a>

### Step 2: Find Chat IDs

1. Open the conversation between you and your Bot in RingCentral
2. Click **More** → **Copy conversation link**
3. The number after `/messages/` is the Chat ID (e.g. `1234567890` from `https://app.ringcentral.com/l/messages/1234567890`)

   <a href="/images/rc-chat-id.png" target="_blank"><img src="/images/rc-chat-id.png" width="600" alt="Copy conversation link to get Chat ID" /></a>

### Step 3: Create a Private App

A Private App (REST API with JWT) is required by RingClaw registration. It provides the owner-scoped client used for account checks, summaries, cross-chat actions, and optional Video/Phone actions:
- **Summarize** conversations from other chats
- **Cross-chat actions** (read messages, create tasks in other chats)
- **Video/Phone actions** (create RingCentral Video bridges, start owner-approved RingOut, read extension Call Log)

1. In the Developer Console, click **Register App** → select **REST API App (most common)**

   <a href="/images/rc-rest-api-app.png" target="_blank"><img src="/images/rc-rest-api-app.png" width="600" alt="Select REST API App" /></a>

2. Configure the app:
   - **Auth**: JWT auth flow
   - **Security** → **Application Scopes**: check **Read Accounts** as the base required scope
   - For Personal AVA Pro Video/Phone: add **Video**, **RingOut**, and **ReadCallLog**
   - **Access**: Private
3. Click **Create** — you'll get a **Client ID** and **Client Secret**
4. Go to **Credentials** tab → **JWT Credentials** → click **Create JWT Token**

   <a href="/images/rc-jwt-create.png" target="_blank"><img src="/images/rc-jwt-create.png" width="600" alt="Create JWT Token" /></a>

5. Copy the JWT token

   <a href="/images/rc-jwt-copy.png" target="_blank"><img src="/images/rc-jwt-copy.png" width="600" alt="Copy JWT Token" /></a>

### Interactive Setup

```bash
ringclaw setup
```

The wizard will:
- Prompt for Bot Token (required)
- Prompt for chat IDs to monitor
- Prompt for Private App credentials (Client ID, Secret, JWT Token)
- Validate credentials against the RingCentral API
- Save everything to `~/.ringclaw/config.json`

## Install Channels

```bash
curl -sSL .../install.sh | sh                 # stable (latest tag)
curl -sSL .../install.sh | sh -s -- beta      # beta (latest main build)
curl -sSL .../install.sh | sh -s -- alpha feature/my-branch  # alpha (specific branch)
```

**Switch channels via CLI:**

```bash
ringclaw update                                    # update to latest stable
ringclaw update --channel beta                     # switch to beta channel
ringclaw update --channel alpha --branch feature/foo  # switch to alpha branch
```

::: info macOS Note
The installer and `ringclaw update` automatically clear Gatekeeper quarantine attributes (`com.apple.quarantine`, `com.apple.provenance`), so the binary won't be killed after download.
:::

## Other Install Methods

```bash
# Via Go
go install github.com/ringclaw/ringclaw@latest

# Via Docker (mount ~/.ringclaw to share config.json with the container)
docker run -it -v ~/.ringclaw:/root/.ringclaw \
  ghcr.io/ringclaw/ringclaw start
```
