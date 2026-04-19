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
Do not bind `api_addr` in `config.json` to `0.0.0.0`. This would expose an authenticated but unencrypted gateway to your corporate RingCentral account on the local network. The default `127.0.0.1` binding is sufficient for all normal use cases.
:::

## Phase 2 Hardening: `/approval` + Cross-Chat Notices

Phase 2 layers two owner-DM-scoped surfaces on top of the Phase 1
trusted-sender allowlist: a two-step `/approval` confirmation for
ACP full-access grants, and a **fail-closed** metadata-only heads-up
notice that must land in the owner DM **before** an owner-initiated
ACTION dispatches into a chat other than the origin or the owner's
own DM. If that pre-dispatch notice cannot be delivered (no owner DM
resolved, or RC transport failure), the cross-chat action is refused
— a silent cross-chat write with no audit record is not an
acceptable failure mode.

Trust assumption: an attacker with a foothold inside RingCentral
(account compromise, prompt injection in a bot DM, malicious
teammate impersonating the owner) does **not** simultaneously have
shell access to the host running RingClaw. The owner's DM with the
bot — reachable only by the Phase 1 trusted-sender allowlist — is
treated as the secured channel for confirmations and audit.

### Phase 2 surfaces — added

Phase 2 introduces **no new `config.json` fields** and adds no
on-disk state. All OOB state is in-memory and cleared on restart,
so a crash-restart naturally re-locks the bot until the operator
explicitly re-grants.

| Surface | Type | Where | Phase 1 behavior | Phase 2 behavior | Operator override |
|---|---|---|---|---|---|
| Owner cross-chat `MESSAGE` / `CARD` / `TASK` / `NOTE` | runtime behavior | `messaging/actions.go` | Honored unconditionally; `WARN action: owner cross-chat dispatch` audit log only | **Synchronous fail-closed pre-dispatch notice.** Before the action is dispatched, a metadata-only heads-up is posted to the owner DM when the target chat differs from the origin AND from the owner's own DM. The notice carries `TYPE`, `requesterID`, RFC3339 timestamp, `originChatID`, `targetChatID` — no body, title, or content preview. If the owner DM is not configured or the notice send fails, **the cross-chat action is refused** (caller receives `Refused cross-chat <TYPE>: …`). | Tune `crossChatNoticeTimeout` in `messaging/actions.go` (currently 5 s). The notice target (`OwnerDMChat`) is derived from the bot DM; it cannot be pointed elsewhere. |
| `/full-access` slash command | runtime command | bot DM with the owner | _did not exist_ | **New.** `status` / `grant [duration]` / `revoke`. `grant` is a two-step confirmation: the bot issues a short-lived challenge ID, posts a plain-text `/approval` prompt to the owner DM, and only activates the grant after the owner replies `/approval <id>`. While a grant is active, each newly-created ACP session is flipped into `session/set_mode "full-access"`. When the grant is revoked (explicitly or by TTL expiry), **every live ACP session is also demoted back to `set_mode "default"`** via a revoke hook — sessions whose demote call fails are dropped from the session map so the next prompt rebuilds them in the locked-down mode. | **Default grant 1 day**, hard cap **30 days**. Oversized inputs are silently clamped. Durations are parsed with `time.ParseDuration` (e.g. `30m`, `2h`, `168h`). Restricted to the owner DM; group-chat invocations are refused. |
| `/approval <id>` / `/approval deny <id>` | slash command | bot DM with the owner | _did not exist_ | **New.** Canonical reply shape for pending challenges. 8-char hex `<id>`; `/approval deny <id>` explicitly rejects. Non-requester replies are refused with a plain-text message so a teammate sharing the DM cannot cancel another user's pending challenge. Replies never reach the AI agent. | n/a |
| `agents.<name>.full_access` | `bool` | `config.json` (per ACP agent) | `true` honored at startup when the top-level `full_access_ack: true` is also set in `config.json`; otherwise downgraded with a `WARN` | **Unchanged** for the static path. In addition, an active `/full-access` grant overlays it dynamically — new ACP sessions read the TTL state on every creation, so a grant takes effect without restarting and naturally drops back to guarded mode when it expires. The per-session log line now carries a `source` field (`config:full_access`, `oob:/full-access`, or `config:full_access+oob`) for audit. | Operators wanting only ad-hoc unlocks can leave `full_access: false` and rely on `/full-access grant` exclusively. |

### Before / after impact for operators

::: warning
**Owner cross-chat behavior change.** Phase 1 always honored owner
cross-chat ACTIONs and only warned. Phase 2 gates them on a
synchronous pre-dispatch audit notice to the owner DM. If the
owner DM is not configured, or the notice send fails, **the
cross-chat action is refused** — nothing lands on the target chat
and the caller sees a `Refused cross-chat <TYPE>` entry. Operators
running without a resolvable bot DM must either resolve it or keep
all owner-driven actions in the origin chat.
:::

::: warning
**`/full-access` revoke demotes live sessions.** In Phase 2
`/full-access revoke` (and TTL expiry) not only prevent NEW sessions
from entering full-access but also proactively send
`session/set_mode "default"` to every live ACP session that was
unlocked during the grant window. Sessions whose demote call fails
are dropped from the session map; the next prompt in that
conversation rebuilds a fresh session in the locked-down mode (so a
small amount of in-memory conversation context may be lost).
:::

| Scenario | Phase 1 behavior | Phase 2 behavior |
|---|---|---|
| Owner asks AI for a cross-chat MESSAGE / CARD / TASK / NOTE | Action runs immediately; `WARN action: owner cross-chat dispatch` audit log only | Bot first posts `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>` to the owner DM (when target ≠ origin and target ≠ owner DM). Only if that pre-notice succeeds does the action run on the target chat. |
| Non-owner asks AI for a cross-chat ACTION | Refused (Phase 1 lock) | Refused (unchanged). |
| Operator wants ACP full-access for one task | Must ship `full_access: true` + `full_access_ack: true` in `config.json` and restart, then remember to revert | Leave `full_access: false`. In the bot DM, run `/full-access grant 30m` — bot replies `Full-access grant requested. Confirm via /approval in owner DM.` and posts `Pending approval: reply /approval <id> to confirm or /approval deny <id> to reject. Expires in 5 min. Requested TTL: 30m.`. Owner replies `/approval <id>`. Bot confirms `Full-access granted until <RFC3339 expiry>.` and the ACP agent runs unlocked for 30 min. |
| Operator wants default-length full-access | n/a | `/full-access grant` → **24 h** (1 day). Cap is 30 days. |
| Operator wants to revoke | n/a | `/full-access revoke` — clears the grant AND immediately demotes every live session back to `set_mode "default"`. |
| OOB not configured (owner DM cannot be resolved) | n/a | `/full-access` returns "OOB approval is not configured". **Owner cross-chat actions are refused** with `Refused cross-chat <TYPE>: no owner DM audit channel configured` (logged as `WARN action: cross-chat ACTION refused (fail-closed on pre-notice)`). |
| Suspected compromise / want to drop state | n/a | Restart the bot — all in-memory state (pending challenges, active `/full-access` grant) is cleared. |

### Phase 2 audit-log additions

| Event | Log line | Purpose |
|---|---|---|
| Challenge issued (e.g. `/full-access grant`) | `INFO oob: challenge issued` (`challengeID`, `requesterID`, `intent`, `ttl`) | Track every `/approval` prompt, including ones that timed out unanswered. |
| Challenge approved | `INFO oob: challenge approved` | Audit who approved what and when. |
| Challenge denied | `INFO oob: challenge denied` | Counterpart to approval log line. |
| Approval refused (non-requester) | `WARN oob: approval refused for non-requester` | Defense-in-depth — only the original requester can confirm their own challenge. |
| `/approval deny` refused (non-requester) | `WARN oob: deny refused for non-requester` | Same invariant for denials. |
| Full-access granted | `WARN oob: ACP full-access granted` (`ttl`, `expiresAt`) | Separate from the per-session `WARN ACP session granted full-access` line. |
| Full-access revoked | `WARN oob: ACP full-access revoked` | Triggered by `/full-access revoke` or by re-grant. |
| Full-access expired (TTL) | `WARN oob: ACP full-access expired (TTL reached)` | Fires proactively when the grant's `expiresAt` is reached, before any caller polls `FullAccessActive`. |
| Live session demoted | `INFO acp demote: session returned to default mode` (`session`, `conversation`) | Confirms `session/set_mode "default"` landed on a live session after revoke / expiry. |
| Live session demotion failed | `WARN acp demote: set_mode default failed, session dropped from map` (with `error`) | The session is removed from the session map; the next prompt on that conversation creates a fresh (default-mode) session. |
| Cross-chat notice sent (pre-dispatch) | `INFO action: cross-chat notice sent (pre-dispatch)` (`type`, `from`, `to`, `ownerDMChat`, `requesterID`) | Confirms the heads-up reached the owner DM; the action is then dispatched. |
| Cross-chat action refused | `WARN action: cross-chat ACTION refused (fail-closed on pre-notice)` (with `error`) | Fires when the audit channel is missing or the notice send fails; the action is NOT dispatched. |

## Phase 1 Hardening: Configuration Changes

Phase 1 of the Remote Control hardening review introduces **two new
top-level `config.json` fields** (`agent_allow_workspace_list` and
`full_access_ack`) and otherwise reuses fields that already existed in
the schema, changing how they are interpreted at startup. Operators
upgrading from a previous release should review the table below —
defaults marked "**new**" may change behavior even when `config.json`
is left untouched.

All configuration lives in `~/.ringclaw/config.json`; the previously
supported `RC_*` / `RINGCLAW_*` / `OPENCLAW_GATEWAY_*` env-var
fallbacks have been removed and are silently ignored.

| Setting | Type | Where | Old default | New default | Operator override |
|---------|------|-------|-------------|-------------|-------------------|
| `ringcentral.source_user_ids` | `[]string` | `config.json` | Empty list = **allow every sender** in any allowed chat | Empty list + Private App = **owner-only** (auto-injected). Empty list + no Private App = **deny all** with startup error | List numeric IDs / emails / phone numbers to add additional trusted senders. Email and phone require Private App with `ReadAccounts`. |
| `agent_workspace` | `string` | `config.json` | Default cwd for agents (no allowlist enforcement) | **Unchanged behavior** as the default cwd, AND implicitly added to the cwd allowlist so the agent can chdir into it | Continues to control the initial cwd. To widen the allowlist, prefer the dedicated `agent_allow_workspace_list` field below. |
| `agent_allow_workspace_list` | `[]string` | `config.json` | _did not exist_ | **new** — explicit list of directories that `/cwd` and `Agent.SetCwd` may target. Always merged with `~/.ringclaw/workspace` and (if set) `agent_workspace`; duplicates are dropped | List every subtree the AI agents are allowed to enter. Anything outside every entry is rejected at runtime. |
| `agents.<name>.full_access` | `bool` | `config.json` (per ACP agent) | `true` immediately enabled `session/set_mode "full-access"` on every new ACP session | `true` is **ignored** unless the top-level `full_access_ack: true` also appears in `config.json`; otherwise downgraded with a `WARN` log | Set `full_access_ack: true` in `config.json`. |
| `full_access_ack` | `*bool` | `config.json` (top-level) | _did not exist_ | **new** — `true` honors `full_access`, `false` or unset refuses | Version-controlled alongside the agent that needs it. |

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

For owner-initiated cross-chat dispatches, Phase 2 adds a
**synchronous fail-closed pre-dispatch gate**:

- **Owner DM resolved**: before the cross-chat `MESSAGE` / `CARD` /
  `TASK` / `NOTE` is dispatched, the bot posts a metadata-only notice
  to the owner DM (when target ≠ origin and target ≠ owner DM):
  `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>`.
  No body, title, or content preview is leaked into the owner DM. The
  notice send is capped at `crossChatNoticeTimeout` (5 s). Only after
  the notice succeeds does the action run on the target chat.
- **Notice delivery fails** (transport error, 5xx, timeout): the
  cross-chat action is **refused**. Caller sees
  `Refused cross-chat <TYPE>: audit notice delivery failed: …` and
  nothing lands on the target chat.
- **Owner DM not resolved** (OOB not configured): the cross-chat
  action is **refused** with
  `Refused cross-chat <TYPE>: no owner DM audit channel configured`.
  Operators who run without a resolvable bot DM must either resolve
  it or keep all owner-driven actions in the origin chat.

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
RingClaw now requires an explicit acknowledgement in `config.json`:

```jsonc
{
  // Explicit, version-controlled acknowledgement.
  "full_access_ack": true
}
```

Resolution:

1. `full_access_ack: true` in `config.json` → honor `full_access`.
2. Anything else (omitted or `false`) → refuse `full_access` with a
   loud warning.

The legacy `RINGCLAW_FULL_ACCESS_ACK` environment variable is silently
ignored — a stray shell export cannot re-enable full access.

When the request is downgraded, the session keeps the default guarded
mode. When honored, every freshly created ACP session emits an additional
`WARN ACP session granted full-access` log line for audit (the `source`
field distinguishes the static `config:full_access` path from the
dynamic `oob:/full-access` Phase 2 grant described below).

### Phase 2 — `/full-access` two-step `/approval` grant

Phase 2 layers a dynamic, time-boxed unlock on top of the static
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
- **Live sessions are also demoted on revoke / TTL expiry.** When a
  grant ends (explicit `/full-access revoke` OR the TTL elapses),
  the manager fires a revoke hook wired to
  `agent.DemoteAllACPFullAccess`. That walker iterates every live
  ACP session created during the grant window and sends
  `session/set_mode "default"` to each. Sessions whose demote call
  fails are dropped from the session map so the next prompt rebuilds
  them fresh in default mode (a small amount of in-memory
  conversation context may be lost, but the session cannot linger
  in full-access). A narrow race between grant-and-revoke landing
  during session creation is also closed by a double-read in
  `getOrCreateSession`; if revoke lands while the initial
  `set_mode "full-access"` call is in flight, the agent immediately
  compensates with `set_mode "default"`.

### Phase 2 — Cross-chat heads-up notices (fail-closed)

Owner-initiated cross-chat `MESSAGE` / `CARD` / `TASK` / `NOTE` actions
are gated on a **synchronous pre-dispatch notice**. When the target
chat differs from both the origin chat and the owner's own DM, the
bot posts a **metadata-only** heads-up to the owner DM **before** the
action runs, and refuses the action if that notice cannot be
delivered:

```text
[notice] MESSAGE by 12345 at 2026-04-17T10:15:00Z: origin=chat-7 target=chat-42
```

The notice carries `TYPE`, `requesterID`, an RFC3339 timestamp,
`originChatID`, and `targetChatID` — no body, title, or content
preview leaks into the owner DM. The send is capped at
`crossChatNoticeTimeout` (5 s) so a stuck RC endpoint cannot wedge
the prompt pipeline; when that cap triggers, the cross-chat action
is refused rather than dispatched without an audit record.

Refusal paths (the cross-chat action does NOT land on the target
chat):

- `OwnerDMChat` is empty (bot DM with the owner not yet resolved, or
  OOB not wired): the caller sees `Refused cross-chat <TYPE>: no
  owner DM audit channel configured`.
- Notice send returns an error (timeout, 5xx, transport error):
  `Refused cross-chat <TYPE>: audit notice delivery failed: <cause>`.

Non-owner cross-chat actions remain **unconditionally refused** by
the Phase 1 trusted-sender lock — the fail-closed notice path only
applies to owner-initiated dispatches (non-owner `chatid=` overrides
are ignored earlier in the dispatch loop).

## Workspace Path Restrictions

`/cwd` and the underlying `Agent.SetCwd` are pinned to an **allowlist of
directory roots**. Any attempt to switch the working directory to a path
outside every configured root is denied with an error like
`Denied: path "/etc" escapes configured workspace allowlist [/home/alice/code /home/alice/.ringclaw/workspace]`.

The effective allowlist is the union of (deduplicated, symlink-resolved):

1. Every entry in `agent_allow_workspace_list` from `config.json`.
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

### Before you read — four entry points, not one

RingClaw can act on RingCentral via **four distinct entry points**. The
three-layer model below only applies to the WebSocket message path;
each of the other entries has its own gate and is listed here so
operators do not mistake "non-owner cannot use `/cwd` in a group" for
"non-owner cannot make the bot do anything":

| Entry point | Layer 0 (sender) | Layer 1 (commands) | Layer 2 (ACTION fan-out) | Layer 3 (ACP mode) | What actually gates it |
|---|---|---|---|---|---|
| WebSocket message | ✅ | ✅ | ✅ | ✅ | Chat allowlist + Phase 1 sender allowlist + handler checks |
| HTTP API (`/api/send`, `/api/tasks`, `/api/notes`, `/api/events`, `/api/cards`) | ❌ | ❌ | ❌ | n/a | **API token + loopback Host only** (`api/auth.go`) |
| Cron job | ❌ | job is created via `/cron add` (Layer 1); execution has no human sender | ❌ **ACTION blocks are NOT executed** — reply is posted verbatim | ✅ | Job config in `~/.ringclaw/cron/jobs.json` |
| Heartbeat | ❌ | n/a (config-driven) | ❌ **ACTION blocks are NOT executed** | ✅ | `heartbeat.enabled` + `HEARTBEAT.md` |

::: danger API token equals machine operator
Anyone with read access to `~/.ringclaw/api_token` **bypasses Layers
0–2 entirely** — they can send arbitrary text/media to any chat and
create/delete any task, note, event, or card through `/api/...`.
Treat the token file like an SSH key. The default loopback-only bind
(`api_addr: 127.0.0.1:18011`) limits the blast radius to local
processes on the same host.
:::

::: tip Chat allowlist is the outermost ring
**Layer -1**: messages from chats **not** in `ringcentral.chat_ids`
are dropped by the WebSocket monitor before Layer 0 even applies
(`ringcentral/monitor.go:380-383`). If a message silently disappears,
check the chat allowlist first — the log line reads
`ignoring message from non-allowed chat`.
:::

### The three WebSocket-path layers

RingClaw gates incoming WebSocket messages through three orthogonal
layers. They compose bottom-up: a message must clear every applicable
layer before it takes effect.

1. **Layer 1 — Chat command authorization**: who may trigger each slash command in each chat shape.
2. **Layer 2 — AI-driven ACTION dispatch**: whether an AI-emitted `ACTION:` block may fan out (especially cross-chat).
3. **Layer 3 — ACP session capabilities**: what an ACP agent can read, write, or execute inside a session.

::: tip Full access only affects Layer 3
The `/full-access` grant (and the static `full_access: true` config) only
changes ACP **session mode**. It does not unlock any chat command — a
non-owner still cannot use `/cwd` in a group, and `/full-access` itself
is still DM-only. It also does not relax the Layer 2 cross-chat
fail-closed notice.
:::

**Layer 0 reminder** — every message first passes through the Phase 1
trusted-sender allowlist, enforced **twice**
([`ringcentral/monitor.go:395-403`](https://github.com/ringclaw/ringclaw/blob/main/ringcentral/monitor.go#L395)
on the socket and
[`messaging/handler.go:444-448`](https://github.com/ringclaw/ringclaw/blob/main/messaging/handler.go#L444)
on the handler). Senders outside the allowlist are dropped before any
layer below applies.

::: warning DM is the trust boundary, not "owner only"
Layer 1's owner-only gate for privileged commands (`/cwd`, `/cron`,
`/new`, `/reload`, summarize NL triggers) fires in **group chats**.
In **bot DMs**, the gate applies only when a Private App is configured
— in that case privileged commands are restricted to the Private App
owner even in DM. Without a Private App, RingClaw has no way to tell
"owner" from "another trusted sender in their own DM", so every
trusted sender gets full privileged-command power in their own DM
with the bot. If you list multiple people in
`ringcentral.source_user_ids` without a Private App, you are trusting
all of them equally — including with `/cron add`, which can keep
running arbitrary prompts after the sender walks away.
:::

### Layer 1 — Chat command authorization

Column legend: ✅ allowed; ❌ blocked (bot replies with an explicit
refusal or silently drops); ⚠️ allowed with an extra check.

"Owner" means the Private App owner when one is configured (the true
machine operator). The "Bot DM (non-owner trusted sender)" column only
applies when `ringcentral.source_user_ids` lists more than one person;
see the "DM is the trust boundary" warning above.

| Command / Message Shape | Bot DM (owner) | Bot DM (other trusted sender, Private App configured) | Bot Group (owner) | Bot Group (others) | Gate |
|---|---|---|---|---|---|
| Plain text with no `/` prefix (→ default agent) | ✅ | ✅ | ✅ | ✅ | `handler.go:528-530` |
| `/help` | ✅ | ✅ | ✅ | ✅ | `handler.go:491` |
| `/info` / `/status` | ✅ | ✅ | ✅ | ✅ | `handler.go:475` |
| `/chatinfo [id]` | ✅ | ✅ | ✅ | ✅ | `handler.go:505` |
| `/task` / `/note` / `/event` / `/card` | ✅ | ✅ | ✅ | ✅ | `actions_commands.go:30` |
| `/<agent> <msg>` (send / broadcast) | ✅ | ✅ | ✅ | ✅ | `handler.go:562` |
| `/<agent>` (switch default agent) | ✅ | ✅ | ✅ | ❌ | `handler.go:537-539` |
| `/new` / `/clear` | ✅ | ❌ | ✅ | ❌ | `handler.go:462-496` + `handler_commands.go:248` |
| `/cwd [path]` | ✅ ⚠️ | ❌ | ✅ ⚠️ | ❌ | `handler_commands.go:19` (allowlist + denylist) |
| `/cron add\|list\|delete` | ✅ | ❌ | ✅ | ❌ | `handler.go:462-496` + `handler_commands.go:254` |
| `/reload` | ✅ | ❌ | ✅ | ❌ | `handler.go:462-496` + `handler_commands.go:257` |
| Summarize (NL trigger, e.g. "总结", "summarize") | ✅ (needs Private App) | ❌ | ⚠️ configured group only | ❌ | `handler_summarize.go:57-82` + `handler_commands.go:245` |
| Summarize without Private App | ❌ disabled | n/a | ❌ disabled | ❌ disabled | `handler_summarize.go:76` |
| `/full-access status\|grant\|revoke` | ✅ ⚠️ | ❌ (owner-only, DM-only) | ❌ (DM-only) | ❌ (DM-only) | `handler_fullaccess.go:62-67` |
| `/approval <id>` / `/approval deny <id>` | ✅ ⚠️ (requester only) | ⚠️ only consumed if the sender is the original requester | ❌ refused with explanatory message | ❌ refused with explanatory message | `handler.go:603-628` + `oob/authorize.go:126` |
| `/remember [user\|chat\|global] <text>` | ✅ | ❌ | ✅ | ❌ | `handler_persona.go` + `handler_commands.go` privileged gate |
| `/forget [scope] [confirm]` | ✅ | ❌ | ✅ | ❌ | same as `/remember`; two-phase confirmation |
| `/recall [scope]` | ✅ | ✅ | ✅ | ✅ | read-only, unprivileged |
| `/persona` | ✅ | ✅ | ✅ | ✅ | read-only, unprivileged |

Extra checks:

- `/cwd` — the absolute path must land inside `agent_allow_workspace_list ∪ agent_workspace ∪ ~/.ringclaw/workspace` AND must not contain any of the denylisted directories (`.ssh`, `.gnupg`, `.ringclaw`, `.aws`, `.kube`, `.config/gcloud`). Both checks run regardless of full-access state.
- `/full-access grant [duration]` — only activates after the owner replies `/approval <id>`. Challenge TTL 5 min, default grant 24 h, max 30 d.
- `/approval` — only the original requester may resolve their own challenge (`oob/authorize.go:146-154`). A `/approval ...` shape posted outside the bot DM is intercepted with an explicit refusal (`` `/approval` is only recognized in the bot DM with the owner.``) so the syntax never leaks into a default-agent prompt.
- Summarize in group — only the group whose ID matches `ringcentral.group_summary_group_id`; cross-group / cross-person summarize refused (`handler_summarize.go:84-115`).
- `/remember` and `/forget` — Layer 1 privileged (same gate as `/cron`). All memory file writes land strictly under `persona.memory_dir`; hostile chat/user IDs cannot escape the tree because IDs go through `SanitizeID` before being used as filenames. See [Configuration › persona](../guide/configuration.md#persona) for the scope layout.
- `/forget` without the trailing `confirm` token never actually clears memory — it only prints a confirmation prompt. This prevents accidental clears in shared scrollbacks.
- **Cron / Heartbeat / HTTP API do NOT inject the persona banner.** These non-interactive entry points have no real chat or user context; the banner is only prepended to WebSocket user messages (`dispatchToAgent` and `broadcastToAgents`).

### Layer 2 — AI-driven ACTION dispatch

An `ACTION: NOTE|TASK|EVENT|CARD|MESSAGE ... END_ACTION` block embedded
in the agent's reply is parsed by `ParseAgentActions` and executed by
`ExecuteAgentActions` (`messaging/actions.go:166`). Gating is
**independent of full-access**.

**Typical triggers** (what a user types → what the agent may emit):

```text
User: "记个笔记：本周优先级 A/B/C"
→ Agent reply includes:
    ACTION: NOTE title=本周优先级
    A. …
    B. …
    END_ACTION

User: "创建一个任务交给 Alice：跟进 PR #42"
→ ACTION: TASK subject=跟进 PR #42 assignee=Alice END_ACTION

User: "把刚才的会议要点发消息告诉 David"
→ ACTION: MESSAGE chatid=David
    会议要点:
    …
    END_ACTION

User: "这个讨论的摘要发到 #engineering 频道"
→ ACTION: MESSAGE chatid=engineering
    …
    END_ACTION    # ← triggers the Layer 2 cross-chat fail-closed path
```

| Scenario | Behavior | Gate |
|---|---|---|
| ACTION in the origin chat (any sender) | ✅ always allowed | `actions.go:166-310` |
| `chatid=` override; requester is **not** on the trusted-sender allowlist | ⚠️ `chatid` silently dropped, forced back to origin chat (`WARN action: ignoring chatid override from non-owner sender`) | `actions.go:172-184` |
| `chatid=` override by owner; target = origin chat | ✅ allowed (same as row 1) | — |
| `chatid=` override by owner; target = owner's own DM | ✅ allowed without audit notice | `actions.go:195` (guard) |
| `chatid=` override by owner; target ≠ origin AND target ≠ owner DM | 🔒 **fail-closed**: bot first posts `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>` to the owner DM; if the notice succeeds within `crossChatNoticeTimeout` (5 s) the action dispatches, otherwise it is **refused** with `Refused cross-chat <TYPE>: …` | `actions.go:195-203`; `announceCrossChatOrRefuse` at `actions.go:329-357` |
| Owner cross-chat action, but OOB not configured (no owner DM resolved) | ❌ refused with `Refused cross-chat <TYPE>: no owner DM audit channel configured` | `actions.go:330-332` |

### Layer 3 — ACP session capabilities (full-access toggle)

Applies **only to ACP agents** (`agent/acp_agent.go`). HTTP and CLI
agents do not expose a `session/set_mode` equivalent.

The `full_access` toggle flips newly-created ACP sessions between
`session/set_mode "default"` and `session/set_mode "full-access"`
(`acp_agent.go:570-620`). The effective RingClaw-side gates are:

| Capability | Default mode | Full-access mode | Gate |
|---|---|---|---|
| `session/set_mode` parameter | `"default"` | `"full-access"` | `acp_agent.go:570-620` |
| `session/request_permission` callbacks from the agent | **Auto-allowed by RingClaw** (the client always replies with the first `"allow"` option) — RingClaw itself does not interactively gate MCP tool calls | Agent generally stops issuing `request_permission` under full-access mode | `acp_rpc.go:230-265` |
| `fs/read_text_file` (ACP protocol) | ✅ allowed; no path check, no sandbox | ✅ allowed (unchanged) | `acp_terminal.go:420-463` |
| `fs/write_text_file` (ACP protocol) | ✅ iff agent config `allow_write: true`; otherwise `write permission denied: allowWrite is false` | ✅ iff `allow_write: true` — **full-access does NOT override `allow_write`** | `acp_terminal.go:472-475` |
| `terminal/create` (shell subprocess) | ✅ arbitrary command, arbitrary `cwd`, **no `allow_write` check, no path allowlist check** | ✅ same (unchanged) | `acp_terminal.go:295-315`, `acp_terminal.go:128-187` |
| Agent-visible tool catalog | Whatever the ACP agent exposes in `default` mode (per-agent policy) | Whatever the ACP agent exposes in `full-access` mode | ACP-agent-specific |
| Top-level `/cwd` allowlist + denylist | Applies to `/cwd` command and `Agent.SetCwd` initial cwd only | **Unchanged** — the allowlist never applies to agent-chosen paths inside tool calls | `handler_commands.go:19-98` |

::: warning ACP Layer 3 invariants worth highlighting
- **`allow_write: false` is not airtight.** It blocks the ACP protocol path (`fs/write_text_file`). It does **not** block the agent from shelling out via `terminal/create` to run `echo … > file`, `sed -i`, `git commit`, etc. Treat `allow_write` as a hint, not a sandbox.
- **No per-call approval in RingClaw.** `handlePermissionRequest` auto-selects the first `allow` option. A stricter gate lives in the ACP agent itself (for example, Claude's own tool-approval logic), not in RingClaw. Moving from default → full-access does not flip a RingClaw-side gate on or off; it just changes which `session/set_mode` RingClaw asks the agent to adopt.
- **`/cwd` allowlist ≠ file-access sandbox.** The allowlist constrains where the `/cwd` command may chdir the agent's starting working directory. An ACP agent can still read/write any file it has OS permission to touch, and can open terminals in any cwd it picks.
- **Full-access is additive on TWO axes.** Either a static `full_access: true` + top-level `full_access_ack: true` in `config.json` (`acp_agent.go:246-252`), or a runtime `/full-access grant` → `/approval <id>` handshake in the owner DM (`handler_fullaccess.go`), will flip new sessions into full-access mode. Revoke / TTL expiry also **demotes every live session** via `DemoteAllACPFullAccess` (`acp_agent.go:703-728`); sessions whose demote call fails are dropped from the session map and rebuilt fresh on the next prompt.
:::

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
