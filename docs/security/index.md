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

## Phase 1 Hardening: Configuration Changes

Phase 1 of the Remote Control hardening review introduces **one new
top-level `config.json` field** (`full_access_ack`) and otherwise reuses
fields that already existed in the schema, changing how they are
interpreted at startup. It also adds one new environment variable as a
fallback. Operators upgrading from a previous release should review the
table below — defaults marked "**new**" may change behavior even when
`config.json` is left untouched.

| Setting | Type | Where | Old default | New default | Operator override |
|---------|------|-------|-------------|-------------|-------------------|
| `ringcentral.source_user_ids` | `[]string` | `config.json` (also `RC_SOURCE_USER_IDS` env, comma-separated) | Empty list = **allow every sender** in any allowed chat | Empty list + Private App = **owner-only** (auto-injected). Empty list + no Private App = **deny all** with startup error | List numeric IDs / emails / phone numbers to add additional trusted senders. Email and phone require Private App with `ReadAccounts`. |
| `agent_workspace` | `string` | `config.json` (also `RINGCLAW_AGENT_WORKSPACE` env) | Used only as the agent's initial cwd; `/cwd` could escape it | **Hard root** for `/cwd` and `Agent.SetCwd`. Falls back to `~/.ringclaw/workspace` when unset | Set to the directory subtree you want to expose to AI agents. Anything outside is rejected at runtime. |
| `agents.<name>.full_access` | `bool` | `config.json` (per ACP agent) | `true` immediately enabled `session/set_mode "full-access"` on every new ACP session | `true` is **ignored** unless `full_access_ack` (config) or `RINGCLAW_FULL_ACCESS_ACK=1` (env) acknowledges it; otherwise downgraded with a `WARN` log | Set `full_access_ack: true` in `config.json` (preferred), or export `RINGCLAW_FULL_ACCESS_ACK=1`. |
| `full_access_ack` | `*bool` | `config.json` (top-level) | _did not exist_ | **new** — `true` honors `full_access`, `false` explicitly refuses (and **suppresses any env-var override**), unset = fall back to env var | Preferred over the env var; lives under version control alongside the agent that needs it. |
| `RINGCLAW_FULL_ACCESS_ACK` | env var | process environment | _did not exist_ | **new** — must equal `1` to honor any agent's `full_access: true` when `full_access_ack` is unset in `config.json` | Only consulted when `full_access_ack` is omitted from config. |

Resolution order for `full_access_ack`: **config wins over env**. If
`full_access_ack` is set in `config.json` (either `true` or `false`), the
env var is ignored. This means an explicit `"full_access_ack": false`
neutralizes a misplaced shell export.

Behaviors implied by Phase 1 that have **no config knob** (intentionally
not exposed yet):

- The cross-chat `ACTION` lock is unconditional for non-owner senders.
  There is no opt-out.
- `cli_agent.Chat` always rejects empty `conversationID`.
- The `/cwd` denylist (`.ssh`, `.gnupg`, `.ringclaw`, `.aws`, `.kube`,
  `.config/gcloud`) is hard-coded as a secondary check, even when the
  `agent_workspace` allowlist would otherwise admit the path.

::: warning
After upgrading, operators who relied on the legacy "empty
`source_user_ids` = allow everyone" behavior will see the bot drop
**every** incoming message until they either (a) configure a Private App
(the owner is auto-trusted) or (b) populate
`ringcentral.source_user_ids`. The startup log line
`sender allowlist is empty: ...` is the canonical signal for this case.
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

## Cross-Chat Action Lock

`ACTION` blocks emitted by the AI may carry a `chatid=` parameter that
targets a different chat than the one the message arrived in. To prevent
"summarize chat A in chat B" style data exfiltration, this is now allowed
only when the originating sender is on the trusted allowlist (the machine
owner). For any other sender, `chatid=` is ignored with a warning log and
the action runs in the origin chat. Owner cross-chat dispatches are still
honored, but each one emits a `WARN action: owner cross-chat dispatch`
log line for audit.

## ACP Agent File Permissions

By default, ACP agents are granted **read-only** file access. To allow file writes, set `allow_write: true` in the agent config:

```json
"claude-acp": {
  "type": "acp",
  "command": "claude-agent-acp",
  "allow_write": true
}
```

## ACP Full-Access Mode

Setting `full_access: true` on an ACP agent calls `session/set_mode
"full-access"` and disables RingClaw's per-call MCP tool-call approval. This
is dangerous: a prompt-injected agent could read or destroy any file the
process can reach.

To prevent silent activation through a stolen or copy-pasted config,
RingClaw now requires an explicit acknowledgement at startup. There are
two ways to grant it; **`config.json` wins over the env var**:

```jsonc
{
  // Preferred: explicit, version-controlled acknowledgement.
  "full_access_ack": true
}
```

```bash
# Fallback when full_access_ack is not set in config.json.
RINGCLAW_FULL_ACCESS_ACK=1 ringclaw start --foreground
```

Resolution order:

1. If `full_access_ack` is set in `config.json` (`true` or `false`), use
   that value. Setting it to `false` explicitly **suppresses any env-var
   override** so a misplaced shell export cannot re-enable full access.
2. Otherwise, fall back to `RINGCLAW_FULL_ACCESS_ACK=1`.
3. Otherwise, refuse `full_access` with a loud warning.

When the request is downgraded, the session keeps the default guarded
mode. When honored, every freshly created ACP session emits an additional
`WARN ACP session granted full-access` log line for audit.

Phase 2 will replace this static acknowledgement with a PIN-gated
`/full-access` slash command that issues a TTL-bounded session token.

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
