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

## Phase 2b Hardening: `/approval` + Cross-Chat Notifications

Phase 2b replaces the short-lived Phase 2 design (bcrypt-hashed
6-digit PIN, Adaptive-Card-driven approvals, blocking cross-chat
gate) with a lighter model better matched to RingCentral's delivery
semantics. RingCentral's WebSocket API only delivers `TextMessage`
events — `Action.Submit` from Adaptive Cards is never pushed, so the
old PIN UI was always going to be typed into the DM as text anyway.
Phase 2b keeps the human-in-the-loop confirmation for the only path
that truly needs it (ACP full-access) and downgrades the cross-chat
gate to a non-blocking notification, improving usability without
weakening the practical security model.

Trust assumption (unchanged): an attacker with a foothold inside
RingCentral (account compromise, prompt injection in a bot DM,
malicious teammate impersonating the owner) does **not**
simultaneously have shell access to the host running RingClaw. The
owner's DM with the bot — reachable only by the trusted sender
allowlist wired up in Phase 1 — is treated as the secured channel.

### Phase 2b surfaces — added / changed

Phase 2b introduces **no new `config.json` fields** and removes the
`~/.ringclaw/oob_pin` file that Phase 2 wrote. Operators upgrading
from Phase 2 should review the table below.

| Surface | Type | Where | Phase 2 behavior | Phase 2b behavior | Operator override |
|---|---|---|---|---|---|
| `~/.ringclaw/oob_pin` | bcrypt-hashed PIN file | local filesystem (mode `0600`) | Auto-generated on first start; plaintext printed once on `stderr` | **Removed.** The file is no longer created or read. Operators upgrading can safely `rm ~/.ringclaw/oob_pin`. No on-disk state is kept; all OOB state is in-memory and reset on restart. | n/a |
| `golang.org/x/crypto/bcrypt` | Go dependency | `go.mod` | Direct dependency introduced in Phase 2 | **Removed.** `go mod tidy` drops it and its transitive deps on upgrade. | n/a |
| Owner cross-chat `MESSAGE` / `CARD` / `TASK` / `NOTE` | runtime behavior | `messaging/actions.go` | PIN-gated: Adaptive Card challenge posted to owner DM, action blocks until approved or 5-min TTL expires | **Executes immediately**; afterwards a metadata-only heads-up is posted asynchronously to the owner DM when the target chat differs from the origin AND from the owner's own DM. The notice carries `TYPE`, `requesterID`, RFC3339 timestamp, `originChatID`, `targetChatID` — no body, title, or content preview. Best-effort: a failed notice is logged but does not roll back the action. | Tune `crossChatNoticeTimeout` in `messaging/actions.go` (currently 5s). The notice target (`OwnerDMChat`) is derived from the bot DM; it cannot be pointed elsewhere. |
| `/full-access grant [duration]` | runtime command | bot DM with the owner | PIN-gated: each grant issued a fresh Adaptive Card challenge that the owner approved with `<id> <PIN>` in the DM | **Two-step `/approval`**. `grant` issues a short-lived challenge ID and posts a plain-text prompt to the owner DM; the bot replies to the command with `Full-access grant requested. Confirm via /approval in owner DM.`. The owner then replies `/approval <id>` (or `/approval deny <id>`) within 5 min to activate or reject the grant. No PIN, no card. | **Default grant 1 day**, hard cap 30 days (oversize input is silently clamped). Duration is parsed with `time.ParseDuration` (e.g. `30m`, `2h`, `7d` via `168h`). Restricted to the owner DM; group-chat invocations are refused. |
| `/approval <id>` / `/approval deny <id>` | slash command | bot DM with the owner | n/a (was `<id> <PIN>` text reply, accepted bare PIN when exactly 1 was pending) | **New canonical shape.** Parsed from the first whitespace-delimited token; `<id>` must be 8 hex characters and `/approval deny <id>` takes precedence over `/approval <id>` when `deny` appears as the first subword. Handled in `messaging/oob.HandleApprovalReply` and routed via `Handler.routeOOBApprovalReply`. Non-requester replies are refused with a plain-text rejection. | n/a |
| `agents.<name>.full_access` | `bool` | `config.json` (per ACP agent) | Static path still honored via `full_access_ack`; dynamic `oob:/full-access` grant layered on top | **Unchanged from Phase 2.** The static config is still honored; the dynamic grant now activates via `/approval` instead of a PIN. Per-session log lines continue to carry `source` (`config:full_access`, `oob:/full-access`, or `config:full_access+oob`). | Operators wanting only ad-hoc unlocks can leave `full_access: false` and rely on `/full-access grant` exclusively. |

### Before / after impact for operators

::: warning
**Cross-chat behavior change from Phase 2.** Phase 2 blocked owner
cross-chat ACTIONs until the PIN was typed in. Phase 2b executes
them immediately and posts a metadata-only heads-up to the owner DM
**after** the dispatch. Operators who relied on the PIN prompt as a
moment to review the action should instead tail the owner DM or the
action audit log.
:::

| Scenario | Phase 2 behavior | Phase 2b behavior |
|---|---|---|
| Fresh install | Bot generates a 6-digit PIN, bcrypt-hashes to `~/.ringclaw/oob_pin`, prints plaintext to `stderr` once | **No PIN file, no stderr print, no bcrypt dep.** Bot starts clean; all OOB state is in-memory. |
| Upgrading from Phase 2 | n/a | `rm ~/.ringclaw/oob_pin` (optional — left-over file is ignored). No config edits required. |
| Owner asks AI for a cross-chat MESSAGE / CARD / TASK / NOTE | Adaptive Card challenge in bot DM; action blocks until owner replies `<id> <pin>` | Action runs immediately. An asynchronous notice `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>` appears in the owner DM when target ≠ origin and target ≠ owner DM. |
| Non-owner asks AI for a cross-chat ACTION | Refused (Phase 1 lock) | Refused (unchanged). |
| Operator wants ACP full-access for one task | `/full-access grant 30m` in bot DM, PIN prompt appears, reply `<id> <pin>`, grant activates | `/full-access grant 30m` in bot DM — bot replies `Full-access grant requested. Confirm via /approval in owner DM.` and posts `Pending approval: reply /approval <id> to confirm or /approval deny <id> to reject. Expires in 5 min. Requested TTL: 30m.`. Owner replies `/approval <id>`. Grant activates; bot replies `Full-access granted until <RFC3339 expiry>.`. |
| Operator wants default-length full-access | `/full-access grant` → 5 min | `/full-access grant` → **24 h** (1 day). Cap is 30 days. |
| Operator wants to revoke | `/full-access revoke` | Unchanged. |
| OOB not configured (missing owner DM) | PIN file still created; cross-chat falls back to warn-log; `/full-access` refuses | `/full-access` refuses; cross-chat actions still execute but **no notice is attempted** (logged as `action: cross-chat notice skipped`). |
| Suspected compromise | `rm ~/.ringclaw/oob_pin` + restart rotated the PIN | Restart the bot — all in-memory state (pending challenges, active `/full-access` grant) is cleared. |

### Phase 2b audit-log additions

| Event | Log line | Purpose |
|---|---|---|
| Challenge issued (e.g. `/full-access grant`) | `INFO oob: challenge issued` (`challengeID`, `requesterID`, `intent`, `ttl`) | Track every `/approval` prompt, including ones that timed out unanswered. |
| Challenge approved | `INFO oob: challenge approved` | Audit who approved what and when. |
| Challenge denied | `INFO oob: challenge denied` | Counterpart to approval log line. |
| Approval refused (non-requester) | `WARN oob: approval refused for non-requester` | Defense-in-depth — only the original requester can confirm their own challenge. |
| `/approval deny` refused (non-requester) | `WARN oob: deny refused for non-requester` | Same invariant for denials. |
| Full-access granted | `WARN oob: ACP full-access granted` (`ttl`, `expiresAt`) | Separate from the per-session `WARN ACP session granted full-access` line. |
| Full-access revoked | `WARN oob: ACP full-access revoked` | Triggered by `/full-access revoke` or by re-grant. |
| Cross-chat notice sent | `INFO action: cross-chat notice sent` (`type`, `from`, `to`, `ownerDMChat`, `requesterID`) | Confirms the heads-up reached the owner DM. |
| Cross-chat notice failed | `WARN action: cross-chat notice delivery failed` (with `error`) | Best-effort failure; action already executed. |
| Cross-chat notice skipped | `WARN action: cross-chat notice skipped; no reply client` | Fired when OOB is not wired (no bot DM resolved). |

## Phase 1 Hardening: Configuration Changes

Phase 1 of the Remote Control hardening review introduces **two new
top-level `config.json` fields** (`agent_allow_workspace_list` and
`full_access_ack`) and otherwise reuses fields that already existed in
the schema, changing how they are interpreted at startup. It also adds
two new environment variables as fallbacks. Operators upgrading from a
previous release should review the table below — defaults marked
"**new**" may change behavior even when `config.json` is left untouched.

| Setting | Type | Where | Old default | New default | Operator override |
|---------|------|-------|-------------|-------------|-------------------|
| `ringcentral.source_user_ids` | `[]string` | `config.json` (also `RC_SOURCE_USER_IDS` env, comma-separated) | Empty list = **allow every sender** in any allowed chat | Empty list + Private App = **owner-only** (auto-injected). Empty list + no Private App = **deny all** with startup error | List numeric IDs / emails / phone numbers to add additional trusted senders. Email and phone require Private App with `ReadAccounts`. |
| `agent_workspace` | `string` | `config.json` (also `RINGCLAW_AGENT_WORKSPACE` env) | Default cwd for agents (no allowlist enforcement) | **Unchanged behavior** as the default cwd, AND implicitly added to the cwd allowlist so the agent can chdir into it | Continues to control the initial cwd. To widen the allowlist, prefer the dedicated `agent_allow_workspace_list` field below. |
| `agent_allow_workspace_list` | `[]string` | `config.json` (also `RINGCLAW_AGENT_ALLOW_WORKSPACE_LIST` env, comma-separated) | _did not exist_ | **new** — explicit list of directories that `/cwd` and `Agent.SetCwd` may target. Always merged with `~/.ringclaw/workspace` and (if set) `agent_workspace`; duplicates are dropped | List every subtree the AI agents are allowed to enter. Anything outside every entry is rejected at runtime. |
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
the action runs in the origin chat.

For owner-initiated cross-chat dispatches, the Phase 2b behavior is
a **non-blocking heads-up** rather than a pre-dispatch gate:

- **Owner DM resolved**: the cross-chat `MESSAGE` / `CARD` / `TASK` /
  `NOTE` executes immediately in the target chat. If the target chat
  differs from both the origin chat and the owner's own DM, the bot
  asynchronously posts a metadata-only notice to the owner DM:
  `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>`.
  No body, title, or content preview is leaked into the owner DM.
  Delivery is best-effort (capped at `crossChatNoticeTimeout`, 5 s).
- **Owner DM not resolved** (OOB not configured): the dispatch still
  executes but no notice is attempted; the event is recorded via
  `WARN action: cross-chat notice skipped; no reply client` plus the
  Phase 1 `WARN action: owner cross-chat dispatch` audit log line.

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
`WARN ACP session granted full-access` log line for audit (the `source`
field distinguishes the static `config:full_access` path from the
dynamic `oob:/full-access` Phase 2b grant described below).

### Phase 2b — `/full-access` two-step `/approval` grant

Phase 2b layers a dynamic, time-boxed unlock on top of the static
`full_access` toggle. The static config is still honored when set; the
new flow is **additive** and lets operators leave `full_access: false`
in `config.json` and unlock full-access on demand from the bot DM:

```text
/full-access status         # show current grant state
/full-access grant           # request a 24h unlock (default)
/full-access grant 30m      # request a 30-minute unlock
/full-access revoke         # immediately lock again
```

The grant flow is a two-step confirmation in the owner DM:

1. Owner sends `/full-access grant [duration]`. Bot replies
   immediately with `Full-access grant requested. Confirm via /approval in owner DM.`
   and posts a prompt with a short-lived challenge ID:
   `Pending approval: reply /approval <id> to confirm or /approval deny <id> to reject. Expires in 5 min. Requested TTL: <duration>.`.
2. Owner replies `/approval <id>` to activate or `/approval deny <id>`
   to reject. On approval the bot responds
   `Full-access granted until <RFC3339 expiry>.`; on denial or expiry
   the grant does not take effect.

Constraints:

- Only the bot's DM with the trusted owner accepts `/full-access` and
  `/approval`. Group-chat invocations are refused with an explanatory
  message so the round-trip stays on the secured channel.
- Default grant duration is **24 hours**; the maximum is capped at
  **30 days**. Oversized inputs are silently clamped. Durations are
  parsed with `time.ParseDuration` (e.g. `30m`, `2h`, `168h`).
- Non-requester approvals are refused — a teammate who also sees the
  DM cannot poke at another user's pending challenge.
- Once granted, every newly-created ACP session is flipped into
  `session/set_mode "full-access"` until the grant expires or
  `/full-access revoke` is called. All OOB state is in-memory and is
  cleared on restart, so a crash-restart re-locks the bot until the
  operator explicitly re-grants.

### Phase 2b — Cross-chat heads-up notices

Owner-initiated cross-chat `MESSAGE` / `CARD` / `TASK` / `NOTE` actions
execute immediately (no pre-dispatch approval). When the target chat
differs from both the origin chat and the owner's own DM, the bot
posts a **metadata-only** heads-up to the owner DM after the action
lands:

```text
[notice] MESSAGE by 12345 at 2026-04-17T10:15:00Z: origin=chat-7 target=chat-42
```

The notice carries `TYPE`, `requesterID`, an RFC3339 timestamp,
`originChatID`, and `targetChatID` — no body, title, or content
preview leaks into the owner DM. Delivery is best-effort and runs
on a detached goroutine capped at `crossChatNoticeTimeout` (5s) so
a stuck RC endpoint cannot block the caller or leak goroutines.

Non-owner cross-chat actions remain **unconditionally refused** by
the Phase 1 trusted-sender lock — the notification path only applies
to owner-initiated dispatches.

## Workspace Path Restrictions

`/cwd` and the underlying `Agent.SetCwd` are pinned to an **allowlist of
directory roots**. Any attempt to switch the working directory to a path
outside every configured root is denied with an error like
`Denied: path "/etc" escapes configured workspace allowlist [/home/alice/code /home/alice/.ringclaw/workspace]`.

The effective allowlist is the union of (deduplicated, symlink-resolved):

1. Every entry in `agent_allow_workspace_list` (or the comma-separated
   `RINGCLAW_AGENT_ALLOW_WORKSPACE_LIST` env var).
2. The legacy `agent_workspace` (continues to be the default cwd).
3. `~/.ringclaw/workspace` — always implicitly trusted so the built-in
   default cwd is never rejected.

A denylist is kept as a defense-in-depth secondary check: even when the
allowlist would admit a path, `/cwd` still refuses any of the sensitive
directories `.ssh`, `.gnupg`, `.ringclaw`, `.aws`, `.kube`, `.config/gcloud`.

```jsonc
{
  // Default cwd (initial directory the agent starts in).
  "agent_workspace": "/home/alice/projects/main",

  // Additional directories the agent may chdir into via /cwd.
  "agent_allow_workspace_list": [
    "/home/alice/projects/secondary",
    "/home/alice/scratch"
  ]
}
```

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
| `/full-access` | Allowed (`/approval` required for `grant`) | **Blocked** (DM-only) | **Blocked** (DM-only) |
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
