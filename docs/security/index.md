---
title: Security
---

# Security

## Local API Authentication

The HTTP API server (default `127.0.0.1:18011`) requires token authentication. A random token is generated on first startup and stored in `~/.ringclaw/api_token`.

All API requests (except `/health`) must include the `X-RingClaw-Token` header:

```bash
curl -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  http://127.0.0.1:18011/api/send -d '{"text":"hello"}'
```

The server also validates the `Host` header to prevent DNS rebinding attacks — only `localhost`, `127.0.0.1`, and `::1` are accepted.

::: danger
Do not bind `RINGCLAW_API_ADDR` to `0.0.0.0`. This would expose an authenticated but unencrypted gateway to your corporate RingCentral account on the local network. The default `127.0.0.1` binding is sufficient for all normal use cases.
:::

## Mandatory Sender Allowlist

When the `start` command boots, the WebSocket monitor and message handler
both switch into **strict sender mode**: only the user IDs on the trusted
allowlist may drive the AI agent. The allowlist is built from two sources:

- The Private App owner's user ID (auto-injected when a Private App is
  configured).
- All entries in `ringcentral.source_user_ids` (resolved to numeric user IDs
  on startup).

If both sources are empty, the bot logs a startup error and **drops every
incoming message** until the operator adds at least one trusted sender. This
prevents the "any user in an allowed chat can run my AI agent" foot-gun
called out as Finding #1 in the Remote Control security review.

```yaml
ringcentral:
  source_user_ids:
    - "+15551234567"       # phone number, resolved at boot
    - alice@example.com    # email address, resolved via Private App directory
    - "987654321"          # bare numeric extensionId / user ID
```

::: tip
Email and phone-number entries require a Private App with the `ReadAccounts`
permission so they can be resolved to numeric IDs. Without the Private App,
list the numeric extensionIds directly.
:::

## ACP Agent File Permissions

By default, ACP agents are granted **read-only** file access. To allow file writes, set `allow_write: true` in the agent config:

```json
"claude-acp": {
  "type": "acp",
  "command": "claude-agent-acp",
  "allow_write": true
}
```

## Workspace Path Restrictions

`/cwd` and the underlying `Agent.SetCwd` are pinned to a **subtree of
`AgentWorkspace`** (or the default `~/.ringclaw/workspace` when the config
key is unset). Any attempt to switch the working directory to a path that
escapes the configured root is denied with an error like
`Denied: path "/etc" escapes workspace root "/home/alice/code"`.

A denylist is kept as a defense-in-depth secondary check: even when the
allowlist would admit a path, `/cwd` still refuses any of the sensitive
directories `.ssh`, `.gnupg`, `.ringclaw`, `.aws`, `.kube`, `.config/gcloud`.

To widen the allowlist, change `agent_workspace` in your config (or set the
`RINGCLAW_AGENT_WORKSPACE` env var) to the parent directory you want to
expose to AI agents.

## Permission Matrix

| Operation | Bot DM | Bot Group (owner) | Bot Group (others) |
|---|---|---|---|
| Chat with agent | Bot replies | Bot replies | Bot replies |
| Summarize | Private App read, Bot reply | **Blocked** (data leak) | **Blocked** (data leak) |
| Summarize (no Private App) | **Disabled** | **Disabled** | **Disabled** |
| `/clear`, `/new` | Allowed | Allowed | **Blocked** (owner only) |
| `/cwd` | Allowed | Allowed | **Blocked** (owner only) |
| Agent switch (`/cc`) | Allowed | Allowed | **Blocked** (owner only) |
| `/info`, `/help` | Allowed | Allowed | Allowed |
| `/task`, `/note`, `/event` | Private App (or Bot) | Private App (or Bot) | Private App (or Bot) |
| ACTION blocks | Private App (or Bot) | Private App (or Bot) | Private App (or Bot) |

## Client Responsibilities

| Role | Client | Why |
|------|--------|-----|
| WebSocket connection | Bot App | Bot token drives WS |
| Send replies & placeholders | Bot App | Bot identity in all chats |
| Read other chats & summarize | Private App (optional) | Bot cannot access private chats |
| `/task`, `/note`, `/event` API | Private App if available, else Bot | Broader access with Private App |
| ACTION block execution | Private App if available, else Bot | Cross-chat access needs Private App |

## Bot App vs Private App Permissions

The two client types have different RingCentral API permissions. Understanding this helps you decide whether to configure a Private App.

**Bot App** receives the `TeamMessaging` permission automatically. **Private App** (REST API with JWT) can be granted `TeamMessaging` + `ReadAccounts`.

| Feature | API Endpoint | Required Permission | Bot App | Private App |
|---------|-------------|---------------------|---------|-------------|
| Send / update / delete posts | `/team-messaging/v1/chats/{chatId}/posts` | TeamMessaging | YES | YES |
| List / manage chats | `/team-messaging/v1/chats` | TeamMessaging | YES | YES |
| Upload files | `/team-messaging/v1/files` | TeamMessaging | YES | YES |
| Tasks CRUD | `/team-messaging/v1/tasks` | TeamMessaging | YES | YES |
| Notes CRUD | `/team-messaging/v1/notes` | TeamMessaging | YES | YES |
| Calendar Events CRUD | `/team-messaging/v1/events` | TeamMessaging | YES | YES |
| Adaptive Cards CRUD | `/team-messaging/v1/adaptive-cards` | TeamMessaging | YES | YES |
| Get person info | `/team-messaging/v1/persons/{id}` | TeamMessaging | YES | YES |
| Create conversation (DM) | `/team-messaging/v1/conversations` | TeamMessaging | YES | YES |
| Get own extension info | `/restapi/v1.0/account/~/extension/~` | (self-info) | YES | YES |
| **Search company directory** | `/restapi/v1.0/account/~/directory/entries/search` | **ReadAccounts** | **NO** | YES |

### Features That Require Private App

| Feature | What happens without Private App |
|---------|--------------------------------|
| Summarize conversations | Disabled — bot cannot read other users' chats |
| Name resolution in ACTION blocks (`chatid=John`, `assignee=Alice`) | Fails — cannot look up person by name |
| Email-based `source_user_ids` (`alice@example.com`) | Ignored — cannot resolve email to user ID |
| Cross-chat actions (create tasks/notes in other chats) | Limited to chats the bot is a member of |

::: tip
If you only need basic messaging and agent interaction, Bot App alone is sufficient. Add a Private App when you need summarization, name resolution, or cross-chat features.
:::
