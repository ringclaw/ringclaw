---
title: API and Clients
---

# API and Clients

This page covers the local HTTP API server (the path that bypasses
Layers 0–2 entirely) and the dual-client model that determines what
the bot can do on the RingCentral side.

## Local API authentication

The HTTP API server (default `127.0.0.1:18011`) requires token
authentication. A random token is generated on first startup and
stored in `~/.ringclaw/api_token`.

All API requests (except `/health`) must include the
`X-RingClaw-Token` header:

```bash
curl -H "X-RingClaw-Token: $(cat ~/.ringclaw/api_token)" \
  http://127.0.0.1:18011/api/send -d '{"text":"hello"}'
```

The server also validates the `Host` header to prevent DNS
rebinding attacks — only `localhost`, `127.0.0.1`, and `::1` are
accepted.

::: danger
Do not bind `api_addr` in `config.json` to `0.0.0.0`. This would
expose an authenticated but unencrypted gateway to your corporate
RingCentral account on the local network. The default `127.0.0.1`
binding is sufficient for all normal use cases.
:::

::: danger API token equals machine operator
Anyone with read access to `~/.ringclaw/api_token` **bypasses
Layers 0–2 entirely** — they can send arbitrary text/media to any
chat and create/delete any task, note, event, or card through
`/api/...`. They can also approve any pending OOB challenge by
calling `/api/oob/approve`. Treat the token file like an SSH key.
The default loopback-only bind (`api_addr: 127.0.0.1:18011`) limits
the blast radius to local processes on the same host.
:::

## Approval CLI calls the local API

`ringclaw approval <id>` reads `~/.ringclaw/api_token` and calls
the local API server (loopback-only, token-authenticated):

- `POST /api/oob/approve`
- `POST /api/oob/deny`
- `GET /api/oob/list`

Approval requires access to the host machine running `ringclaw`.
This is the property that decouples approval authority from the
RingCentral account — see [Approval CLI](./approval-cli) for the
full rationale.

## Client Responsibilities

RingClaw can use **two distinct RingCentral clients** in parallel:
the Bot App (always required) and the Private App (optional).
Different roles are routed to different clients based on the API
permissions each client has.

| Role | Client | Why |
|------|--------|-----|
| WebSocket connection | Bot App | Bot token drives WS |
| Send replies & placeholders | Bot App | Bot identity in all chats |
| Read other chats & summarize | Private App (optional) | Bot cannot access private chats |
| `/task`, `/note`, `/event` API | Private App if available, else Bot | Broader access with Private App |
| ACTION block execution | Private App if available, else Bot | Cross-chat access needs Private App |

## Bot App vs Private App permissions

The two client types have different RingCentral API permissions.
Understanding this helps you decide whether to configure a Private
App.

**Bot App** receives the `TeamMessaging` permission automatically.
**Private App** (REST API with JWT) can be granted `TeamMessaging`
+ `ReadAccounts`.

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

## Features that require Private App

| Feature | What happens without Private App |
|---------|--------------------------------|
| Summarize conversations | Disabled — bot cannot read other users' chats |
| Name resolution in ACTION blocks (`chatid=John`, `assignee=Alice`) | Fails — cannot look up person by name |
| Email-based `source_user_ids` (`alice@example.com`) | Ignored — cannot resolve email to user ID |
| Phone-number-based `source_user_ids` (`+15551234567`) | Ignored — cannot resolve phone to user ID |
| Cross-chat actions (create tasks/notes in other chats) | Limited to chats the bot is a member of |
| Authorize-mention OOB flow (`allow_group_mention_authorize`) | Disabled at startup. Logs `ERROR ...` when explicitly opted in (`allow_group_mention_authorize: true`); logs `INFO ...` when defaulted on (v0.4.1+). Either way, non-trusted `@bot` falls back to silent drop. |
| Owner-only Layer 1 gate in bot DMs | Falls back to "any trusted sender" — see [Command Authorization](./command-authorization) |

::: tip
If you only need basic messaging and agent interaction, Bot App
alone is sufficient. Add a Private App when you need
summarization, name resolution, cross-chat features, or any of the
OOB hardening surfaces (`/full-access`, authorize-mention).
:::
