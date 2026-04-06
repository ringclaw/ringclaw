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

# Set bot token (required)
export RC_BOT_TOKEN="your_bot_token"

# Start
ringclaw start
```

That's it. On first start, RingClaw will:
1. Connect to RingCentral via the Bot App's WebSocket
2. Auto-detect installed AI agents (Claude, Codex, Gemini, etc.)
3. Save config to `~/.ringclaw/config.json`
4. Start receiving and replying to messages

## RingCentral Setup

::: tip
After creating your apps, run `ringclaw setup` for an interactive wizard that collects credentials, validates them, and saves the config file.
:::

### Step 1: Create a Bot App (Required)

1. Go to [RingCentral Developer Console](https://developers.ringcentral.com/console) and sign in

   <a href="/images/rc-login.png" target="_blank"><img src="/images/rc-login.png" width="600" alt="RingCentral Developer Console login" /></a>

2. Click **Register App** → select **Bot Add-in**

   <a href="/images/rc-register-app.png" target="_blank"><img src="/images/rc-register-app.png" width="600" alt="Register App" /></a>

   <a href="/images/rc-bot-addin.png" target="_blank"><img src="/images/rc-bot-addin.png" width="600" alt="Select Bot Add-in" /></a>

3. Configure the app:
   - **Security** → **Application Scopes**: check **Read Accounts**, **Read Messages**, **TeamMessaging**, **WebSockets Subscription**, **WebSockets**
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

### Step 3: Create a Private App (Optional)

A Private App (REST API with JWT) enables additional features:
- **Summarize** conversations from other chats
- **Cross-chat actions** (read messages, create tasks in other chats)

1. In the Developer Console, click **Register App** → select **REST API App (most common)**

   <a href="/images/rc-rest-api-app.png" target="_blank"><img src="/images/rc-rest-api-app.png" width="600" alt="Select REST API App" /></a>

2. Configure the app:
   - **Auth**: JWT auth flow
   - **Security** → **Application Scopes**: check **Read Accounts**, **Read Messages**, **TeamMessaging**, **WebSockets Subscription**, **WebSockets**
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
- Optionally configure Private App credentials (Client ID, Secret, JWT Token)
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

# Via Docker
docker run -it -v ~/.ringclaw:/root/.ringclaw \
  -e RC_BOT_TOKEN=xxx \
  ghcr.io/ringclaw/ringclaw start
```
