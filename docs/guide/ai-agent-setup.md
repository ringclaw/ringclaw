# RingClaw AI Agent Setup Runbook

This document is an executable runbook for AI agents to set up RingClaw. After reading this document, the AI should complete RingClaw installation, RingCentral DPW configuration, local configuration, restart, and verification step by step.

## Usage

Users just need to give the file path or link to the AI:

```text
Set up RingClaw following {ringclaw-repo}/docs/guide/ai-agent-setup.md
```

The AI must read this document first, then execute the setup directly. Do not just provide a plan, and do not ask the user to copy the content again.

## Rules and Goals

The AI must follow these rules during execution:

- Read official docs first: [Getting Started](./getting-started.md), [Configuration](./configuration.md), [DPW Credential Creation](https://developers.ringcentral.com/guide/getting-started/create-credential)
- Use only the official binary (`~/.local/bin/ringclaw` or `ringclaw` in `PATH`), do not modify source code or build from source
- Write all real credentials only to `~/.ringclaw/config.json`, never to shared documents
- When displaying config, only show booleans, chat IDs, user IDs, agent names — never tokens/secrets/JWTs
- If browser login is needed, let the user complete login/MFA/Okta push, then continue

Final verification must confirm:

1. `ringclaw status` shows running
2. Log contains `private app authentication successful`
3. Log contains `bot DM chat resolved`
4. Log contains `subscribed to post events, listening...`
5. At least one successful end-to-end response via DM or group chat

## 1. Pre-check Local Environment

```bash
which ringclaw || true
ringclaw version || true
curl --version
jq --version
```

If `ringclaw` is not found, install via official method:

```bash
curl -sSL https://raw.githubusercontent.com/ringclaw/ringclaw/main/install.sh | sh
```

Confirm version is `ringclaw v0.4.x`.

Note: The official `ringclaw setup` can interactively collect credentials, but AI setup works better by creating credentials in DPW first, then writing `~/.ringclaw/config.json` directly.

## 2. Create Bot App in DPW

Open https://developers.ringcentral.com/my-account.html#/applications — let user log in if needed.

Create or confirm a Bot App:

1. Click `Create App` / `Register App`, select `Bot Add-in`.
2. App Name suggestion: `<Your Name> RingClaw Bot`, Access: `Private`, enable Bot User.
3. Scopes: `Read Accounts`, `Read Messages`, `Team Messaging`, `WebSocket`, `WebSocket Subscriptions`.
4. Create app, go to Bot tab, click `Install` to install to current account.
5. Copy `Bot Token` for `ringcentral.bot_token`.

Checkpoints: App Type is Bot Add-in, Access is Private, Bot User enabled, all 5 Scopes present, installed, Bot Token obtained.

## 3. Create Private REST API App in DPW

This app is used for owner authentication, owner DM resolution, OOB approval. Required for stable DM functionality.

1. Click `Create App`, select `REST API App (most common)`.
2. App Name suggestion: `<Your Name> RingClaw Private App`, Auth: `JWT auth flow`, Access: `Private`.
3. Scopes: same 5 as Bot App.
4. Create app, record `Client ID` and `Client Secret`.

Create JWT:

1. Open `https://developers.ringcentral.com/console/my-credentials/create?client_id=<PRIVATE_APP_CLIENT_ID>`
2. Label: `RingClaw owner private chat`, select `Only specific apps of my choice`, confirm the Private App appears.
3. Click `Create JWT`, copy the JWT.

Checkpoints: Auth is JWT, Access is Private, JWT created by current owner, JWT authorized for Private App.

## 4. Get Chat IDs

You need at least one chat ID (group chat and/or owner-bot DM).

1. Open RingCentral App, enter the target group chat or DM with the bot.
2. Copy the conversation link — the number after `/messages/` in the URL is the chat ID.

Example: `https://app.ringcentral.com/l/messages/1234567890` → chat ID is `1234567890`

Note: Bot must be added to group chats; DM must be between owner and bot; `chat_ids` should include both group and DM.

## 5. Verify Chat IDs via API

Set Bot Token as environment variable (do not print):

```bash
export RC_BOT_TOKEN='<BOT_TOKEN>'
```

Verify chats:

```bash
for id in <GROUP_CHAT_ID> <OWNER_DM_CHAT_ID>; do
  out="/tmp/ringclaw-chat-${id}.json"
  code=$(curl -sS -o "$out" -w "%{http_code}" \
    -H "Authorization: Bearer ${RC_BOT_TOKEN}" \
    "https://platform.ringcentral.com/team-messaging/v1/chats/${id}")
  printf "chat=%s http=%s\n" "$id" "$code"
  jq '{id, type, name, status}' "$out"
done
```

Expected: group chat returns HTTP `200` with `type: Team`; DM returns `200` with `type: Direct`.

To verify DM members (confirm owner and bot are present), use the members endpoint:

```bash
curl -sS -H "Authorization: Bearer ${RC_BOT_TOKEN}" \
  "https://platform.ringcentral.com/team-messaging/v1/chats/<OWNER_DM_CHAT_ID>/members" \
  | jq '.records[] | {id, name}'
```

> Note: Run `unset RC_BOT_TOKEN` after completion to clean up credentials from shell environment.

## 6. Get Owner User ID

Set Private App credentials as environment variables:

```bash
export RC_CLIENT_ID='<PRIVATE_APP_CLIENT_ID>'
export RC_CLIENT_SECRET='<PRIVATE_APP_CLIENT_SECRET>'
export RC_JWT_TOKEN='<OWNER_JWT>'
```

Get access token and owner extension ID:

```bash
token_json=$(curl -sS -u "$RC_CLIENT_ID:$RC_CLIENT_SECRET" \
  -X POST https://platform.ringcentral.com/restapi/oauth/token \
  -d grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer \
  -d assertion="$RC_JWT_TOKEN")

RC_PRIVATE_ACCESS_TOKEN=$(printf "%s" "$token_json" | jq -r '.access_token // empty')

if [ -z "$RC_PRIVATE_ACCESS_TOKEN" ]; then
  printf "%s\n" "$token_json" | jq '{error, error_description, message}'
  exit 1
fi

curl -sS \
  -H "Authorization: Bearer ${RC_PRIVATE_ACCESS_TOKEN}" \
  https://platform.ringcentral.com/restapi/v1.0/account/~/extension/~ \
  | jq '{id, extensionNumber, name, email}'
```

The `id` in the output is the owner user ID — add it to `ringcentral.source_user_ids`.

Common errors: JWT not created by current owner, JWT not authorized for Private App, wrong Client ID/Secret/JWT.

> Note: Run `unset RC_CLIENT_ID RC_CLIENT_SECRET RC_JWT_TOKEN RC_PRIVATE_ACCESS_TOKEN` after completion.

## 7. Write RingClaw Configuration

Minimal config template (`~/.ringclaw/config.json`):

```json
{
  "default_agent": "codex",
  "ringcentral": {
    "server_url": "https://platform.ringcentral.com",
    "bot_token": "<BOT_TOKEN>",
    "client_id": "<PRIVATE_APP_CLIENT_ID>",
    "client_secret": "<PRIVATE_APP_CLIENT_SECRET>",
    "jwt_token": "<OWNER_JWT>",
    "chat_ids": [
      "<GROUP_CHAT_ID>",
      "<OWNER_DM_CHAT_ID>"
    ],
    "source_user_ids": [
      "<OWNER_USER_ID>"
    ],
    "group_mention_only": true
  }
}
```

For automated writing, export all variables first, then use jq to generate config:

```bash
export RC_BOT_TOKEN='<BOT_TOKEN>'
export RC_CLIENT_ID='<PRIVATE_APP_CLIENT_ID>'
export RC_CLIENT_SECRET='<PRIVATE_APP_CLIENT_SECRET>'
export RC_JWT_TOKEN='<OWNER_JWT>'
export RINGCLAW_GROUP_CHAT_ID='<GROUP_CHAT_ID>'
export RINGCLAW_OWNER_DM_CHAT_ID='<OWNER_DM_CHAT_ID>'
export RINGCLAW_OWNER_USER_ID='<OWNER_USER_ID>'
export RINGCLAW_DEFAULT_AGENT='codex'
```

Write config (supports merging with existing config):

```bash
mkdir -p ~/.ringclaw

new_config=$(jq -n \
  --arg bot "$RC_BOT_TOKEN" \
  --arg cid "$RC_CLIENT_ID" \
  --arg sec "$RC_CLIENT_SECRET" \
  --arg jwt "$RC_JWT_TOKEN" \
  --arg gchat "$RINGCLAW_GROUP_CHAT_ID" \
  --arg dmchat "$RINGCLAW_OWNER_DM_CHAT_ID" \
  --arg uid "$RINGCLAW_OWNER_USER_ID" \
  --arg agent "${RINGCLAW_DEFAULT_AGENT:-codex}" \
  '{
    default_agent: $agent,
    ringcentral: {
      server_url: "https://platform.ringcentral.com",
      bot_token: $bot,
      client_id: $cid,
      client_secret: $sec,
      jwt_token: $jwt,
      chat_ids: ([$gchat, $dmchat] | map(select(. != ""))),
      source_user_ids: [$uid],
      group_mention_only: true
    }
  }')

if [ -f ~/.ringclaw/config.json ]; then
  jq -s '.[0] * .[1]' ~/.ringclaw/config.json <(echo "$new_config") > ~/.ringclaw/config.json.tmp
  mv ~/.ringclaw/config.json.tmp ~/.ringclaw/config.json
else
  echo "$new_config" > ~/.ringclaw/config.json
fi

chmod 600 ~/.ringclaw/config.json
```

Verify config (do not print secrets):

```bash
jq '{
  has_bot_token: ((.ringcentral.bot_token // "") != ""),
  has_private_app: ((.ringcentral.client_id // "") != "" and (.ringcentral.client_secret // "") != "" and (.ringcentral.jwt_token // "") != ""),
  chat_ids: .ringcentral.chat_ids,
  source_user_ids: .ringcentral.source_user_ids,
  group_mention_only: .ringcentral.group_mention_only,
  default_agent: .default_agent
}' ~/.ringclaw/config.json
```

> Note: Run `unset RC_BOT_TOKEN RC_CLIENT_ID RC_CLIENT_SECRET RC_JWT_TOKEN RINGCLAW_GROUP_CHAT_ID RINGCLAW_OWNER_DM_CHAT_ID RINGCLAW_OWNER_USER_ID` after completion.

## 8. Start or Restart RingClaw

Stop old instance:

```bash
ringclaw stop || true
```

Check for remaining old processes:

```bash
ps -ef | grep -E 'ringclaw start -f|codex-acp|claude-agent-acp' | grep -v grep || true
```

If old instances exist, `kill <OLD_RINGCLAW_PID>` to clean up.

`ringclaw start` runs as a background daemon and will not block the terminal:

```bash
ringclaw start
ringclaw status
```

Expected: `ringclaw is running (pid=<PID>)`

## 9. Verify Startup Logs

```bash
tail -160 ~/.ringclaw/ringclaw.log
```

Must see these log lines:

```text
initializing bot client...
bot extension ID resolved
initializing private app client...
private app authentication successful
private app owner ID resolved ownerID=<OWNER_USER_ID>
bot DM chat resolved chatID=<OWNER_DM_CHAT_ID>
OOB approval flow active ownerDMChatID=<OWNER_DM_CHAT_ID>
starting message bridge chatIDs="[<GROUP_CHAT_ID> <OWNER_DM_CHAT_ID>]"
source_user_ids resolved
subscribed to post events, listening...
default agent ready
```

If `no private app configured` or `bot DM chat with owner not resolved` appears, setup is not complete. If `address already in use` appears, go back to step 8 to clean up old processes.

## 10. End-to-End Testing

### DM Test

Have the user send `Reply only OK` in the owner-bot DM, then check logs:

```bash
tail -120 ~/.ringclaw/ringclaw.log | grep -E 'received post|received message|sent typing placeholder|dispatching to agent|agent replied|updated post|sent reply|ERR|WRN'
```

Expected flow: `received post` → `received message` → `sent typing placeholder` → `dispatching to agent` → `agent replied` → `updated post`.

### Group Chat Test

If `group_mention_only` is `true`, must `@<Bot Name> Reply only OK` in group chat.

## 11. Delivery Checklist

The AI's final reply should only report: official binary version, DPW App status, config write status (`has_bot_token`/`has_private_app`/`chat_ids`/`source_user_ids`), `ringclaw status` result, key log line confirmations, end-to-end test result. Remind user to rotate any credentials that were sent in chat.

Do not include Bot Token, Client Secret, JWT, OAuth access token, or full config file in the final reply.

## 12. Troubleshooting

| Symptom | Checks |
|---------|--------|
| DM not responding | Check log for `bot DM chat resolved`; verify `client_id`/`client_secret`/`jwt_token` are all set; JWT must be created by owner and authorized for Private App; owner-bot DM must exist; DM chat ID must be in `chat_ids` |
| `no private app configured` | Config missing `client_id`, `client_secret`, or `jwt_token` — fix and restart |
| `bot DM chat with owner not resolved` | Confirm `private app owner ID resolved` in log; verify owner-bot DM exists; use Bot Token API to verify chat type is `Direct` |
| Group chat `Only the bot owner can use this command` | Sender's user ID not in `source_user_ids`; or needs authorize-mention OOB flow |
| Group chat not triggering | `chat_ids` missing this group; bot not in group; `group_mention_only: true` but didn't `@bot`; WebSocket not subscribed |
| Message received but no reply | Check for `dispatching to agent` in log; verify `default agent ready` and agent availability |
| `address already in use` | Old process on port — `ps -ef \| grep ringclaw \| grep -v grep` to find and kill |

## 13. Adding New Group Chats

No need to recreate DPW apps. Add bot to new group, get chat ID, append to `chat_ids`, restart RingClaw, test with `@bot Reply only OK`.

## 14. Security Requirements

- Never write real credentials to shared documents
- If credentials were sent in chat, recommend rotate/recreate after setup
- Keep `~/.ringclaw/config.json` permissions at `600`
- AI output must always be redacted
- Delete temporary files containing API responses if they include access tokens
