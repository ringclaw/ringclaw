---
title: Sender Allowlist
---

# Sender Allowlist (Layer 0)

Every WebSocket message is gated on the trusted-sender allowlist
before any other layer applies. This page covers the three sources
that build the allowlist, the mandatory-strict-mode startup
behavior, and the **authorize-mention OOB flow** that lets operators
admit a non-trusted user into one specific chat without restarting.

See [Permission Matrix](./index#permission-matrix) for how Layer 0
relates to the other layers.

## How the allowlist is built

When `ringclaw start` boots, the WebSocket monitor and message
handler both switch into **strict sender mode**: only the user IDs
on the trusted allowlist may drive the AI agent. The allowlist is
the **union** of three sources:

1. The Private App owner's user ID (auto-injected when a Private App
   is configured).
2. All entries in `ringcentral.source_user_ids` (resolved to numeric
   user IDs on startup).
3. All entries in `ringcentral.chat_user_allow[<chatID>]` for the
   destination chat (resolved on startup the same way as
   `source_user_ids`). This is a **per-chat layered exception**, not
   a global widening — `chat_user_allow` only admits the listed
   users in the listed chats.

If all three sources are empty, the bot logs a startup error and
**drops every incoming message** until the operator adds at least
one trusted sender. This prevents the "any user in an allowed chat
can run my AI agent" foot-gun.

```yaml
ringcentral:
  source_user_ids:
    - "+15551234567"       # phone number, resolved at boot
    - alice@example.com    # email address, resolved via Private App directory
    - "987654321"          # bare numeric extensionId / user ID
```

::: tip
Email and phone-number entries require a Private App with the
`ReadAccounts` permission so they can be resolved to numeric IDs.
Without the Private App, list the numeric extensionIds directly.
:::

## `chat_user_allow` — per-chat exception layer

`ringcentral.chat_user_allow` is a per-chat allowlist layered on top
of `source_user_ids`:

```jsonc
{
  "ringcentral": {
    "chat_user_allow": {
      "chat-engineering-7": ["alice@example.com", "3061708020"],
      "chat-design-9":      ["bob@example.com"]
    }
  }
}
```

Identifiers may be numeric extension IDs, emails, or E.164 phone
numbers (resolved to numeric IDs at startup via the Private App
directory). Operators may pre-seed by hand, or let the
[authorize-mention OOB flow](#authorize-mention-oob-flow) populate
the map on operator approval.

Key invariant: `chat_user_allow` **only widens the sender allowlist
for the listed chat**. It does **not** unlock privileged Layer 1
commands (`/cwd`, `/cron`, `/new`, `/reload`, `/full-access`,
summarize NL triggers) — those still require Private-App-owner
identity. See [Command Authorization](./command-authorization).

## Authorize-mention OOB flow

::: danger SECURITY ADVISORY (v0.4.2 → v0.4.3)
Before v0.4.3, any user that became "trusted" — whether through
`source_user_ids`, `chat_user_allow`, or the v0.4.1 OOB-approval
flow — drove the same agent backend the bot operator uses, and
could request filesystem (`List`, `Read`, `Write`), terminal
(`Bash`), and external HTTP tool calls through the agent tool-call
channel.

**v0.4.3 (this release) — fail-closed two-tier non-owner isolation
for `fs/*` + `terminal/*`:**

- "Owner" is now strictly the **`source_user_ids`** set (plus the
  resolved Private App owner). `chat_user_allow` users and any
  v0.4.0 OOB-approved users are **non-owners** and run with the
  reduced ceiling described below.
- **Layer A (protocol):** ringclaw issues
  `session/set_mode <restricted>` immediately after creating an
  ACP session for a non-owner. The modeID is picked from a
  per-agent map: `droid → spec`, `claude → plan`,
  `gemini → plan`, `qwen → plan`, `cursor-agent → plan`.
  Unknown agents fall back to a heuristic that scans
  `availableModes` for `plan` / `spec` / `read` / `safe`.
- **Layer B (client gate, fail-closed):** ringclaw rejects every
  `fs/read_text_file` / `fs/write_text_file` /
  `terminal/create` / `terminal/output` / `terminal/wait_for_exit` /
  `terminal/kill` / `terminal/release` JSON-RPC request issued
  from a non-owner session, regardless of the agent's mode
  behavior. `session/request_permission` is also denied (the
  agent gets either the offered `kind=deny` option or a
  `cancelled` outcome).
- **Fail-closed if no restricted mode exists.** When the agent
  does not advertise any read-only mode and the operator did not
  configure an override, the non-owner message is **not**
  forwarded to the agent. The user receives a refusal text;
  audit log emits `restricted_mode_unsupported_no_mode`.
- **`chat_user_allow` is still force-cleared at every startup**
  (v0.4.2 stop-gap). Operators must re-add entries by hand.

**Layer-A known limitations (best-effort only):** upstream
agents do not always enforce the read-only mode by themselves
(qwen-code#1806 returns success without enforcement;
gemini-cli#22191 has known plan-mode bugs over ACP). Layer B is
the actual security boundary for `fs/*` + `terminal/*`. **WebFetch
/ WebSearch / built-in HTTP tools / MCP custom tools** are
dispatched directly by the agent process and ringclaw does not
see those JSON-RPC requests, so Layer B cannot apply — those
remain best-effort under Layer A only. v0.5.0 (planned) will
close this gap with OS-level sandboxing.

**Operator-facing override:** the per-agent default modeID can be
overridden via `agents.<name>.restricted_mode_id` in
`config.json`. The override must match a mode the agent
advertises in its `availableModes` list, otherwise the built-in
selection still wins.
:::

A separate OOB surface, layered on the same challenge /
terminal-approval infrastructure as `/full-access` and the
non-owner cross-chat ACTION challenge, lets operators authorize
**per-chat** non-trusted senders without restarting or hand-editing
`config.json`. The feature is controlled by
`ringcentral.allow_group_mention_authorize`:

- **Unset** (default since v0.4.2): feature **off**. Non-trusted
  group `@bot` mentions are silently dropped — same as the v0.4.0
  baseline.
- **`true`**: feature on. Non-trusted group `@bot` mentions
  surface as a `/approval` prompt in the owner DM. Requires a
  Private App + resolved owner DM at runtime; otherwise the
  feature is disabled at startup with an ERROR log. ringclaw also
  emits a startup WARN reminding operators that approved users
  currently get full agent capability.
- **`false`**: feature off, explicitly. Same behavior as unset.

The trigger is narrow: a user who is **not** on the global
`source_user_ids` allowlist and **not** in the destination chat's
`chat_user_allow` entry sends a message with `@bot` (a true
mention) in an allowed group chat. Plain text from the same user,
or `@bot` in a non-allowed chat, still drops as before.

```mermaid
sequenceDiagram
    participant U as Non-trusted user
    participant G as Group chat
    participant M as Monitor (ringcentral)
    participant H as Handler (messaging)
    participant O as Owner DM
    participant CLI as ringclaw approval (host)

    U->>G: @bot help me
    G->>M: PostAdded event
    M->>M: trusted? (source_user_ids ∪ chat_user_allow[G])
    Note over M: NO → group + @bot + opt-in flag set
    M->>H: AuthorizeMention(post)
    H->>H: dedupe (G,U) — already pending? then drop
    H->>H: OOB.Issue(challenge, ttl=5m)
    H->>O: Pending authorization (challenge `<id>`)\nChat / User / Mention preview\nhost commands
    Note over H,G: Original message dropped (not replayed)
    O->>CLI: ringclaw approval <id>
    CLI->>H: Approve(id)
    H->>H: chat_user_allow[G] += email (or numeric ID)
    H->>H: Monitor.AddChatUserAllow(G, U) — live allowlist push
    H->>H: persist callback → config.json Save()
    H->>O: Authorized `<email>` in chat `<G>`. Saved to chat_user_allow.
    U->>G: @bot help me (second time, manual re-mention)
    G->>M: PostAdded event
    M->>M: trusted now? YES (chat_user_allow[G] ∋ U)
    M->>H: dispatch normally
    H->>G: AI reply
```

### Key invariants

- **Original message dropped.** The first `@bot` that triggered the
  challenge is **not** replayed on approval. The user must `@bot`
  again to actually drive the AI. This avoids accidental
  side-effects from prompts that were authored before the operator
  approved them.
- **Per-chat scope only.** The grant is recorded under
  `chat_user_allow[<chatID>]`, never in the global
  `source_user_ids`. Approving a user in chat A does not authorize
  them in chat B.
- **Privileged Layer 1 commands NOT unlocked.** `chat_user_allow`
  only widens Layer 0. Non-owner privileged Layer 1 commands still
  require Private-App-owner identity. See
  [Command Authorization](./command-authorization).

::: warning Layer 2 (agent tool calls) — non-owner ceiling (v0.4.3+)
Listed users (chat_user_allow / OOB-approved) can drive the AI
agent like any trusted sender, **but** v0.4.3 enforces a
fail-closed ceiling on their session:

- `fs/read_text_file`, `fs/write_text_file`,
  `terminal/create` / `output` / `wait_for_exit` / `kill` /
  `release`, and `session/request_permission` are denied at the
  ringclaw client. The agent is told the call failed with
  `code=-32001` ("denied for non-owner senders").
- The session is also asked to switch to a read-only mode
  (`spec` for droid, `plan` for the others) as a defense-in-depth
  layer. When the agent does not support a suitable mode, the
  message is refused outright rather than forwarded.
- Plain text replies and RingCentral
  `ACTION:MESSAGE / TASK / NOTE / EVENT` blocks remain available
  (the latter go through the RingCentral REST API, not ACP).

What is **not** covered by Layer B: `WebFetch`, `WebSearch`, and
MCP custom tools. The agent dispatches those internally; ringclaw
cannot see them. v0.5.0 will close that gap with OS-level
sandboxing. Until then, treat a `chat_user_allow` user as
"can read the public web through the agent and make the agent
talk", but no longer "can shell into your host".
:::
- **Pending dedupe.** A pending challenge for a `(chatID, userID)`
  pair blocks new challenges from the same pair for the challenge
  TTL. The pending lock is released on approve / deny / expire /
  prompt-post failure.
- **24h cooldown after deny / expire (v0.4.1+).** When a challenge
  resolves to deny or expire, the same `(chatID, userID)` pair is
  silenced for 24 hours: subsequent `@bot` mentions are dropped
  without re-prompting the owner. This keeps a noisy or hostile
  non-trusted user from spamming the owner DM by repeatedly
  re-mentioning the bot. The window does **not** apply on approve
  (the user becomes trusted via `chat_user_allow` instead) or on
  transient errors (e.g. owner DM post failed) so the operator
  always gets another chance once the underlying issue clears.
  Cooldown state is in-memory only — restarting the bot resets it,
  which is acceptable because restarts are rare and operator-driven.
- **Persistence is best-effort.** The in-memory monitor + handler
  allowlists are updated synchronously on approval; the
  `config.json` Save is fired afterwards. A persist failure is
  logged as `ERROR authorize-mention: persist failed` but the user
  remains authorized for the current process lifetime — operators
  relying on durable persistence should monitor that log line.
- **Email preferred for persistence.** The persisted identifier is
  the resolved email when the directory lookup succeeds; the numeric
  extension ID otherwise (with a
  `WARN authorize-mention: no email available` line). Hand-edited
  entries may use any of the three
  forms `source_user_ids` accepts.
- **No new approval verb.** The same `ringclaw approval <id>` /
  `ringclaw approval deny <id>` CLI handles authorize-mention,
  `/full-access`, and cross-chat OOB challenges uniformly. The
  challenge `intent` field disambiguates them in audit logs. See
  [Approval CLI](./approval-cli).
- **Owner self-challenge guarded.** The handler refuses to issue an
  authorize-mention challenge when `post.CreatorID` equals the
  Private App owner's ID. Monitor's Layer 0 already admits the
  owner, so reaching this path with that condition would only
  happen on a bug or hostile direct caller — failing closed
  prevents the owner from being routed a "user X requesting
  authorization" prompt for themselves.

### Failure modes

| Condition | Behavior |
|---|---|
| `allow_group_mention_authorize` unset (default since v0.4.2) | Feature off. Non-trusted `@bot` is silently dropped (v0.4.0-style baseline). |
| `allow_group_mention_authorize: false` | Feature off, explicitly. Same as unset. |
| `allow_group_mention_authorize: true` | Feature on. ringclaw emits a startup WARN reminding the operator that approved users gain full agent capability. |
| Private App not configured (no owner DM resolvable) | Feature disabled at startup with ERROR log; falls back to silent drop. |
| Existing `chat_user_allow` entries on disk (v0.4.1 leftover) | Force-cleared at startup with ERROR log; operator must re-add by hand after re-evaluating. |
| Non-owner sender hits an agent with no read-only mode | v0.4.3 fail-closed: the message is **not** forwarded; the user receives a refusal reply; audit log emits `restricted_mode_unsupported_no_mode`. |
| Non-owner sender + agent rejects `session/set_mode` | v0.4.3 fail-closed: same as above; the (`agentCmd`, `modeID`) pair is cached so subsequent attempts skip the RPC. |
| Owner DM not yet resolved at runtime | The single message that hits this race is dropped with `WARN authorize-mention: OOB or owner DM unconfigured; dropping`. Subsequent messages succeed once the DM resolves. |
| Owner denies the challenge | Pending lock released; owner DM notified. The `(chat, user)` pair enters a 24h cooldown — re-mentions drop silently until the window elapses. |
| Challenge expires (5 min TTL) | Pending lock released; owner DM notified. Same 24h cooldown as deny. |
| Owner DM post fails (transient RC error) | Challenge auto-denied; pending lock released. **No** cooldown is recorded so the user's next `@bot` retries the post. |
| Persist callback fails (e.g. config write error) | In-memory grant survives the current process; `ERROR authorize-mention: persist failed` is logged. Restart re-locks the user. |

### Pre-seeding without OOB

Operators who want to authorize a known user without enabling the
OOB flow at all can hand-edit `chat_user_allow` directly:

```jsonc
{
  "ringcentral": {
    "allow_group_mention_authorize": false,  // OOB flow off
    "chat_user_allow": {
      "chat-engineering-7": ["alice@example.com"]
    }
  }
}
```

This admits Alice in `chat-engineering-7` and only there; no
challenge is ever issued. The two fields are independent.

## Audit-log additions

| Event | Log line | Purpose |
|---|---|---|
| Sender allowlist empty at startup | `sender allowlist is empty: ...` | Canonical signal that strict mode dropped to deny-all because no source_user_ids and no Private App owner are configured. |
| Authorize-mention routing (monitor) | `INFO authorize-mention: routing non-trusted group mention` (`chatID`, `userID`) | Confirms the WebSocket monitor handed a non-trusted group `@bot` to the OOB flow instead of dropping it. |
| Authorize-mention challenge issued | `INFO oob: challenge issued` (`intent` starts with `authorize user … in chat …`) | Same `INFO oob: challenge issued` line as `/full-access` and cross-chat OOB; the `intent` field disambiguates. |
| Authorize-mention granted | `INFO authorize-mention: granted` (`challengeID`, `chatID`, `userID`, `identifier`) | Fired after `applyAuthorize` updates the in-memory monitor + handler allowlists and (best-effort) persists the identifier to `chat_user_allow`. |
| Authorize-mention denied / expired | `INFO authorize-mention: denied` or `INFO authorize-mention: challenge expired` (with `cooldown=24h`) | Counterparts to the granted line. The pending `(chat, user)` dedupe is released and a 24h silence window is recorded. |
| Authorize-mention dropped by cooldown | `DEBUG authorize-mention: in cooldown after recent deny/expire, dropping` | A re-mention from a `(chat, user)` pair within their 24h silence window is dropped without re-prompting the owner. |
| Authorize-mention persist failure | `ERROR authorize-mention: persist failed` (with `error`) | The in-memory grant succeeded but writing `config.json` did not — the grant survives the current process but is lost on restart. |
| Authorize-mention prompt failure | `ERROR authorize-mention: post prompt failed` (with `error`) | The owner DM post failed; the challenge is auto-denied and the `(chat, user)` pending lock released so the user can retry. |
| Authorize-mention email unavailable | `WARN authorize-mention: no email available, persisting numeric ID` | Directory lookup did not yield an email; the numeric extension ID is persisted instead (still chat-scoped). |
| v0.4.3: restricted mode applied to non-owner | `WARN acp restricted-mode event` (`event=restricted_mode_applied`, `mode_id`, `mode_source`, `conversation`, `sender_id`) | Layer-A success: agent accepted the read-only mode for a non-owner conversation. |
| v0.4.3: restricted mode unsupported (no candidate) | `WARN acp restricted-mode event` (`event=restricted_mode_unsupported_no_mode`, `available_modes`) | Layer-A fail-closed: no built-in / heuristic match. Non-owner message refused. |
| v0.4.3: restricted mode unsupported (`set_mode` rejected) | `WARN acp restricted-mode event` (`event=restricted_mode_unsupported`, `error="Method not found"`) | Layer-A fail-closed: agent rejected the call. Cached so the next attempt skips. |
| v0.4.3: Layer-B tool call denied | `WARN acp non-owner tool call denied` (`event=tool_call_denied`, `method`, `session`, `reason`) | Client-side fail-closed deny of a `fs/*` / `terminal/*` / `session/request_permission` request. Deduped per (session, method). |
