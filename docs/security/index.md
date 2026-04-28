---
title: Security
---

# Security

RingClaw is a remote-control bridge from RingCentral Team Messaging
into a host machine that runs AI agents with broad filesystem and
shell access. The threat model and defense layers below describe how
the project keeps that power scoped to the right people through
config, command authorization, OOB approvals, and process-local
state.

## Quick navigation

| If you want to … | Read |
|---|---|
| Decide who can drive the bot at all | [Sender Allowlist](./sender-allowlist) |
| See which slash commands each user may run | [Command Authorization](./command-authorization) |
| Understand the cross-chat ACTION gate | [Cross-Chat Actions](./cross-chat-actions) |
| Use `full_access` or `/full-access grant` | [ACP Full-Access](./full-access) |
| Approve an OOB challenge from the host | [Approval CLI](./approval-cli) |
| Pin the agent's working directory | [Workspace Allowlist](./workspace-allowlist) |
| Lock down the local HTTP API or compare Bot vs Private App | [API & Clients](./api-and-clients) |

## Threat model

The trust assumption underpinning every defense layer is that an
attacker with a foothold inside RingCentral — account compromise,
prompt injection in the bot DM, or a malicious teammate
impersonating the owner — does **not** simultaneously have shell
access to the host running RingClaw.

That single assumption is what makes the OOB approval design
defensible:

- All approvals (`/full-access grant`, non-owner cross-chat ACTIONs,
  authorize-mention) require running `ringclaw approval <id>` on the
  host.
- A compromised RC account can read the challenge ID from the owner
  DM but cannot execute the approval without SSH or physical access
  to the box.
- All OOB state is in-memory and cleared on restart, so a
  crash-restart naturally re-locks the bot until the operator
  explicitly re-grants.

If the host machine is compromised at the OS level, RingClaw cannot
help — the API token, the config file, and any agent secrets are
already accessible. The relevant question is whether RingClaw adds
new attack surface beyond what an attacker would already have on a
compromised box; the layered model below answers "no" by keeping
every privileged surface either config-only or gated on host
access.

## Four entry points (not one)

RingClaw can act on RingCentral via **four distinct entry points**.
The three-layer permission model in the next section only applies to
the WebSocket message path; each of the other entries has its own
gate and is listed here so operators do not mistake "non-owner
cannot use `/cwd` in a group" for "non-owner cannot make the bot do
anything":

| Entry point | Layer 0 (sender) | Layer 1 (commands) | Layer 2 (ACTION fan-out) | Layer 3 (ACP mode) | What actually gates it |
|---|---|---|---|---|---|
| WebSocket message | YES | YES | YES | YES | Chat allowlist + sender allowlist + handler checks |
| HTTP API (`/api/send`, `/api/tasks`, `/api/notes`, `/api/events`, `/api/cards`) | NO | NO | NO | n/a | **API token + loopback Host only** (`api/auth.go`); see [API & Clients](./api-and-clients) |
| Cron job | NO | job is created via `/cron add` (Layer 1); execution has no human sender | NO — **ACTION blocks are NOT executed**; reply is posted verbatim | YES | Job config in `~/.ringclaw/cron/jobs.json` |
| Heartbeat | NO | n/a (config-driven) | NO — **ACTION blocks are NOT executed** | YES | `heartbeat.enabled` + `HEARTBEAT.md` |

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
(`ringcentral/monitor.go`). If a message silently disappears,
check the chat allowlist first — the log line reads
`ignoring message from non-allowed chat`.
:::

## Permission Matrix

The WebSocket message path is gated through three orthogonal layers
that compose bottom-up. A message must clear every applicable layer
before it takes effect.

| Layer | Question it answers | Detail page |
|---|---|---|
| 0 | Can this sender drive the bot at all? | [Sender Allowlist](./sender-allowlist) |
| 1 | Can this sender run this slash command? | [Command Authorization](./command-authorization) |
| 2 | Can this AI-emitted ACTION fan out (especially cross-chat)? | [Cross-Chat Actions](./cross-chat-actions) |
| 3 | What can the ACP session read, write, or execute? | [ACP Full-Access](./full-access) |

::: tip Full access only affects Layer 3
The `/full-access` grant (and the static `full_access: true` config)
only changes ACP **session mode**. It does not unlock any chat
command — a non-owner still cannot use `/cwd` in a group, and
`/full-access` itself is still DM-only. It also does not relax the
Layer 2 cross-chat fail-closed notice.
:::

::: warning DM is the trust boundary, not "owner only"
Layer 1's owner-only gate for privileged commands (`/cwd`, `/cron`,
`/new`, `/reload`, summarize NL triggers) fires in **group chats**.
In **bot DMs**, the gate applies only when a Private App is
configured — in that case privileged commands are restricted to the
Private App owner even in DM. Without a Private App, RingClaw has no
way to tell "owner" from "another trusted sender in their own DM",
so every trusted sender gets full privileged-command power in their
own DM with the bot. If you list multiple people in
`ringcentral.source_user_ids` without a Private App, you are
trusting all of them equally — including with `/cron add`, which can
keep running arbitrary prompts after the sender walks away.
:::

### Layer 0 reminder

Every message first passes through the trusted-sender allowlist,
enforced **twice**
([`ringcentral/monitor.go`](https://github.com/ringclaw/ringclaw/blob/main/ringcentral/monitor.go)
on the socket and
[`messaging/handler.go`](https://github.com/ringclaw/ringclaw/blob/main/messaging/handler.go)
on the handler). Senders outside the allowlist are dropped before
any layer below applies.

The Layer 0 set is the **union** of `ringcentral.source_user_ids`,
the auto-injected Private App owner ID, and the destination chat's
`ringcentral.chat_user_allow[<chatID>]` entry (when present).
Authorize-mention approvals land in `chat_user_allow` — they widen
Layer 0 only, never any of the layers below. See
[Sender Allowlist](./sender-allowlist) for the full picture.

## Configuration changes worth flagging at upgrade time

Operators upgrading from an old release should confirm the following
before restarting:

| Setting | Where | Behavior to confirm |
|---|---|---|
| `ringcentral.source_user_ids` | `config.json` | Empty list + Private App = **owner-only** (auto-injected). Empty list + no Private App = **deny all** with startup error. See [Sender Allowlist](./sender-allowlist). |
| `agent_workspace` | `config.json` | Continues to be the default cwd, AND is implicitly added to the cwd allowlist. See [Workspace Allowlist](./workspace-allowlist). |
| `agent_allow_workspace_list` | `config.json` | Explicit list of directories that `/cwd` and `Agent.SetCwd` may target. Always merged with `~/.ringclaw/workspace` and (if set) `agent_workspace`. |
| `agents.<name>.full_access` | `config.json` (per ACP agent) | `true` is **ignored** unless the top-level `full_access_ack: true` also appears; otherwise downgraded with a `WARN` log. See [ACP Full-Access](./full-access). |
| `full_access_ack` | `config.json` (top-level) | `true` honors `full_access`, `false` or unset refuses. |
| `ringcentral.allow_group_mention_authorize` | `config.json` (under `ringcentral`) | **Off by default since v0.4.2** (the v0.4.1 default-on flip was reverted as a security stop-gap). Set to `true` explicitly to enable. After deny / expire, the same `(chat, user)` pair is silenced for 24 h. Requires Private App + resolvable owner DM. See [Sender Allowlist › SECURITY ADVISORY](./sender-allowlist#authorize-mention-oob-flow). |
| `ringcentral.chat_user_allow` | `config.json` (under `ringcentral`) | Per-chat trusted-sender exception. **v0.4.2 still force-clears this map at startup** with an ERROR log; listed users now run under the v0.4.3 non-owner ceiling (read-only ACP mode + fail-closed deny of `fs/*` / `terminal/*` / `session/request_permission`). Operators must still re-add by hand on every restart while v0.4.2's stop-gap stays in effect. |
| `agents.<name>.restricted_mode_id` | `config.json` (under each agent) | v0.4.3+: ACP modeID applied to non-owner sessions. Defaults: `droid → spec`, `claude / gemini / qwen / cursor-agent → plan`, others fall back to a heuristic over `availableModes` (`plan`, `spec`, `read`, `safe`). When neither path yields a modeID and no override is set, non-owner messages are refused outright. The override must match a mode the agent advertises; otherwise the built-in selection still wins. See [Sender Allowlist › SECURITY ADVISORY (v0.4.2 → v0.4.3)](./sender-allowlist#authorize-mention-oob-flow). |

The previously supported `RC_*` / `RINGCLAW_*` /
`OPENCLAW_GATEWAY_*` env-var fallbacks have been **removed and are
silently ignored**. All configuration lives in
`~/.ringclaw/config.json`.

::: warning
After upgrading, operators who relied on the legacy "empty
`source_user_ids` = allow everyone" behavior will see the bot drop
**every** incoming message until they either (a) configure a Private
App (the owner is auto-trusted) or (b) populate
`ringcentral.source_user_ids`. The startup log line
`sender allowlist is empty: ...` is the canonical signal for this
case.
:::
