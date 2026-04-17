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

## Phase 2 Hardening: Out-of-Band Approval

Phase 2 hardens the two remaining high-risk paths that Phase 1 left
to a warn-log: **owner-initiated cross-chat MESSAGE/CARD dispatches**
and **ACP full-access unlocks**. The model is that an attacker with
a foothold inside RingCentral (account compromise, prompt injection
in a bot DM, malicious teammate impersonating the owner) does **not**
simultaneously have shell access to the host running RingClaw — so a
6-digit PIN that is generated locally and never leaves the host's
disk acts as a second factor.

### Phase 2 surfaces — added / changed

Phase 2 introduces **no new `config.json` fields**. The new surfaces
live on the local filesystem and at runtime in the bot DM. Operators
upgrading from Phase 1 should review the table below.

| Surface | Type | Where | Old behavior (Phase 1) | New behavior (Phase 2) | Operator override |
|---|---|---|---|---|---|
| `~/.ringclaw/oob_pin` | bcrypt-hashed 6-digit PIN file | local filesystem (mode `0600`) | _did not exist_ | **new** — auto-generated on first start; plaintext printed once on `stderr`; never re-logged. Required for every cross-chat ACTION and every `/full-access grant`. | Delete the file and restart to rotate. Operators who cannot run the host write `~/` (e.g. read-only home) get a `WARN` and silently fall back to Phase 1 warn-log behavior. |
| Owner cross-chat `MESSAGE` / `CARD` ACTION | runtime behavior | `messaging/actions.go` | Honored unconditionally; emitted only `WARN action: owner cross-chat dispatch` for audit | **PIN required**: Adaptive Card challenge posted to the owner's bot DM; action blocks until owner replies with the PIN (or times out at 5 min, default). Approvals are cached for 5 min per `(requester, intent)` to avoid prompt fatigue. Falls back to the Phase 1 warn-log when OOB cannot be initialized. | None — gating is unconditional when OOB is configured. The 5-min challenge TTL and 5-min approval cache are constants in `messaging/oob/challenge.go` (`DefaultChallengeTTL`, `DefaultApprovalCacheTTL`). |
| `/full-access` slash command | runtime command | bot DM with the owner | _did not exist_ | **new** — `status` / `grant [duration]` / `revoke`. Each `grant` issues a fresh PIN challenge (cache fast-path bypassed), and on approval flips every newly-created ACP session into `set_mode "full-access"` until the grant expires. | Default grant **5 minutes**, hard cap **4 hours** (oversize input is silently clamped). Restricted to the owner DM — group chat invocations are refused. |
| `agents.<name>.full_access` | `bool` | `config.json` (per ACP agent) | `true` honored at startup when `full_access_ack` (config) or `RINGCLAW_FULL_ACCESS_ACK=1` (env) acknowledges it; otherwise downgraded with a `WARN` | **Unchanged** — the static path still works for "always on" deployments. **In addition**, the dynamic `/full-access` grant overlays it without requiring the static toggle. The session log line now carries a `source` field (`config:full_access`, `oob:/full-access`, or `config:full_access+oob`) for audit. | Operators wanting only ad-hoc unlocks can leave `full_access: false` and rely on `/full-access grant` exclusively. |

### Before / after impact for operators

::: warning
**Cross-chat behavior change.** Phase 1 always honored owner cross-chat ACTIONs and only warned. Phase 2 **blocks** them (when OOB is configured) until the owner replies with the PIN in the bot DM. Operators who automate cross-chat workflows from the owner account must keep the bot DM available to acknowledge each new `(requester, intent)` pair. Repeat actions within 5 minutes do not re-prompt thanks to the approval cache.
:::

| Scenario | Phase 1 behavior | Phase 2 behavior |
|---|---|---|
| Fresh install (no `~/.ringclaw/oob_pin`) | n/a | Bot generates a 6-digit PIN, hashes it with bcrypt to `~/.ringclaw/oob_pin` (mode `0600`), prints the plaintext to `stderr` **once**. Operator must record it now. |
| Upgrading an existing install | PIN file did not exist | Same as fresh install on the next restart. Watch `stderr` for the one-time print. |
| Owner asks AI for a cross-chat MESSAGE / CARD | Action runs immediately, audit `WARN` only | Adaptive Card prompt appears in the bot DM. Owner replies `<id> <pin>` (or just `<pin>` when only one challenge is pending). Action proceeds on approval; second identical action within 5 min is auto-approved from the cache. |
| Non-owner asks AI for a cross-chat ACTION | Refused (Phase 1 lock) | Refused (unchanged). |
| Operator wants ACP full-access for one task | Must ship `full_access: true` + `full_access_ack: true` in `config.json` and restart, then remember to revert | Leave `full_access: false`. Type `/full-access grant 30m` in the bot DM, reply with the PIN, work for 30 min. Use `/full-access revoke` to lock immediately. New ACP sessions started during the window are full-access; sessions started after expiry are guarded again automatically. |
| OOB cannot be initialized (read-only `~/`, missing owner DM, etc.) | n/a | Bot logs a `WARN`, leaves OOB disabled, and **falls back to the Phase 1 warn-log** for cross-chat ACTIONs. `/full-access` returns "OOB approval is not configured". |
| Suspected compromise / lost PIN | n/a | `rm ~/.ringclaw/oob_pin` and restart the bot. A fresh PIN is generated and printed once. |

### Phase 2 audit-log additions

| Event | Log line | Purpose |
|---|---|---|
| Fresh PIN generated | `INFO oob: generated new approval PIN` (with `path`) | Operator can correlate the stderr print with the file write. |
| Challenge issued | `INFO oob: challenge issued` (with `challengeID`, `requesterID`, `intent`, `ttl`) | Track every prompt, including ones that timed out unanswered. |
| Approval succeeded | `INFO oob: challenge approved` | Audit who approved what and when. |
| PIN incorrect | `WARN oob: PIN verification failed` | Brute-force signal. |
| Rate limit hit | `WARN oob: PIN verify rate limit exceeded` | 5 attempts / minute sliding window has tripped. |
| `/deny` from non-requester | `WARN oob: deny refused for non-requester` | Defense-in-depth — only the original requester can `/deny`. |
| Full-access granted | `WARN oob: ACP full-access granted` (with `ttl`, `expiresAt`) | Visible separately from the per-session `WARN ACP session granted full-access` line. |
| Full-access revoked | `WARN oob: ACP full-access revoked` | Triggered by `/full-access revoke` or by re-grant. |
| Cross-chat fallback | `WARN action: owner cross-chat dispatch` | Phase 1 path; only emitted when OOB is **not** configured. |

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

For owner-initiated cross-chat dispatches, behavior depends on whether
the Phase 2 OOB approval flow is configured (see below):

- **OOB configured** (`~/.ringclaw/oob_pin` exists, owner DM resolved):
  the dispatch is **gated** — RingClaw posts an Adaptive Card challenge
  to the owner's bot DM and refuses to send the cross-chat MESSAGE/CARD
  until the owner replies with the 6-digit PIN. Approvals are cached
  for 5 minutes per `(requester, intent)` pair so repeat actions in a
  burst do not prompt repeatedly.
- **OOB not configured** (Phase 1 fallback): the dispatch is honored
  but emits a `WARN action: owner cross-chat dispatch` audit log line.

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
dynamic `oob:/full-access` Phase 2 grant described below).

### Phase 2 — `/full-access` PIN-gated TTL grant

Phase 2 layers a dynamic, time-boxed unlock on top of the static
`full_access` toggle. The static config is still honored when set; the
new flow is **additive** and lets operators leave `full_access: false`
in `config.json` and unlock full-access on demand from the bot DM:

```text
/full-access status         # show current grant state
/full-access grant 30m      # request a 30-minute unlock
/full-access revoke         # immediately lock again
```

Constraints:

- Only the bot's DM with the trusted owner accepts `/full-access`.
  Group-chat invocations are refused with an explanatory message so
  the PIN approval round-trip stays on the secured channel.
- `grant` always issues an OOB challenge (PIN required); the cache
  fast-path is **skipped** so every unlock is explicit.
- Default duration is 5 minutes; the maximum is capped at 4 hours.
  Larger durations are silently clamped.
- Once granted, every newly-created ACP session is flipped into
  `session/set_mode "full-access"` until the grant expires or
  `/full-access revoke` is called.

## Phase 2: Out-of-Band PIN Approval

Phase 2 adds a second factor for high-risk actions that even a trusted
owner should not be able to trigger purely from a chat message: cross-
chat MESSAGE/CARD dispatches and ACP full-access unlocks. The trust
assumption is that an attacker with a foothold inside RingCentral
(account compromise, prompt injection in a bot DM, malicious teammate
impersonating the owner) does **not** simultaneously have shell access
to the host running RingClaw — so a 6-digit PIN that is generated
locally and never leaves the host's disk acts as the second factor.

### PIN file (`~/.ringclaw/oob_pin`)

On first start, RingClaw generates a fresh 6-digit decimal PIN, hashes
it with bcrypt, and writes the hash to `~/.ringclaw/oob_pin` (mode
`0600`, JSON document with version + bcrypt hash + creation time).
**The plaintext PIN is printed to the local terminal exactly once on
stderr.** Record it now — it is never logged again. To rotate, delete
the file and restart the bot.

```text
================ RingClaw OOB approval PIN ================
  PIN: 482931
  Hash file: /home/alice/.ringclaw/oob_pin (mode 0600, bcrypt)
  This PIN is shown ONCE. Record it now.
  ...
================ RingClaw OOB approval PIN ================
```

If `~/.ringclaw/oob_pin` already exists at startup, the bot loads the
existing hash silently — no PIN is reprinted.

PIN verification is rate-limited to 5 attempts per minute (sliding
window) to defeat brute force attempts even by a caller with a
foothold inside the bot.

### Approval round-trip

When a high-risk action is requested, the bot:

1. Issues a single-use challenge with an 8-character hex ID, valid for
   5 minutes by default.
2. Posts an Adaptive Card to the owner's bot DM describing the action
   (intent, requester, origin chat, expiry). If the card POST fails,
   it falls back to a plain text message containing the same info.
3. Blocks the requested action until the owner replies in the bot DM
   with one of the recognized syntaxes:

   ```text
   /approve <id> <pin>     # explicit
   <id> <pin>              # explicit, terse
   <pin>                   # bare PIN, only when exactly 1 challenge is pending
   /deny <id>              # explicit denial
   ```

4. Caches successful approvals for 5 minutes per `(requester, intent)`
   pair so a burst of identical actions does not re-prompt.

`/deny` is only honored from the original requester to prevent a
teammate sharing the bot DM from cancelling another user's challenge.
PIN replies are intercepted **before** they reach the AI agent — the
PIN itself is never forwarded to any model or external process.

### Where OOB applies

| Surface | Phase 1 behavior | Phase 2 behavior (when OOB configured) |
|---|---|---|
| Owner cross-chat MESSAGE / CARD | Honored, with audit log | **PIN required** before each new `(requester, intent)` |
| Non-owner cross-chat | Refused unconditionally | Refused unconditionally (unchanged) |
| ACP full-access unlock | Static `full_access: true` + ack at startup | Static path still works; new `/full-access grant` issues a TTL token via PIN |

If `~/.ringclaw/oob_pin` cannot be created, or the bot DM with the
owner cannot be resolved, the OOB layer logs a warning and falls back
to Phase 1 behavior. This keeps the bot operational on hosts where the
home directory is not writable, but operators should treat it as a
configuration error.

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
| `/full-access` | Allowed (PIN required for `grant`) | **Blocked** (DM-only) | **Blocked** (DM-only) |
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
