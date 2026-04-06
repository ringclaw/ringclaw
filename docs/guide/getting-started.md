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
2. Click **Register App** → select **Bot Add-in (No UI)**
3. Configure the app:
   - **Security** → App Scopes: check **ReadAccounts**, **TeamMessaging**, **WebSocketsSubscription**
   - **Access**: Private (only your own account)
4. Click **Create**
5. Go to the **Bot** tab → click **Add** to install the bot to your account
6. Copy the **Bot Token** shown on the Bot tab

### Step 2: Find Chat IDs

1. Open [API Explorer → List Chats](https://developers.ringcentral.com/api-reference/Chats/listGlipChatsNew)
2. Sign in and click **Try It Out**
3. Find the chat you want to monitor and copy its `id` field

### Step 3: Create a Private App (Optional)

A Private App (REST API with JWT) enables additional features:
- **Summarize** conversations from other chats
- **Cross-chat actions** (read messages, create tasks in other chats)

1. In the Developer Console, click **Register App** → select **REST API App**
2. Configure the app:
   - **Auth**: JWT auth flow
   - **Security** → App Scopes: check **ReadAccounts**, **TeamMessaging**, **WebSocketsSubscription**
   - **Access**: Private
3. Click **Create** — you'll get a **Client ID** and **Client Secret**
4. Go to **Credentials** tab → **JWT Credentials** → click **Create JWT Token**
5. Copy the JWT token

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
