---
title: ACP Full-Access
---

# ACP Full-Access (Layer 3)

Layer 3 controls what an ACP agent can read, write, or execute
inside a session. Full-access mode disables RingClaw's per-call MCP
tool-call approval and asks the agent to flip into
`session/set_mode "full-access"`. There are two ways to activate
it: **static** (config) and **dynamic** (runtime
`/full-access grant`).

This page applies **only to ACP agents** (`agent/acp_agent.go`).
HTTP and CLI agents do not expose a `session/set_mode` equivalent.

See [Permission Matrix](./index#permission-matrix) for how Layer 3
relates to the other layers, and [Approval CLI](./approval-cli) for
the terminal command that resolves `/full-access` challenges.

## ACP agent file permissions (default)

By default, ACP agents are granted **read-only** file access. To
allow file writes, set `allow_write: true` in the agent config:

```json
"claude-acp": {
  "type": "acp",
  "command": "claude-agent-acp",
  "allow_write": true
}
```

`allow_write` only gates the ACP `fs/write_text_file` protocol path.
See the [invariants](#layer-3-invariants-worth-highlighting) below
for why this is not a complete write sandbox.

## Layer 3 capability matrix

The `full_access` toggle flips newly-created ACP sessions between
`session/set_mode "default"` and `session/set_mode "full-access"`
(`acp_agent.go`). The effective RingClaw-side gates are:

| Capability | Default mode | Full-access mode | Gate |
|---|---|---|---|
| `session/set_mode` parameter | `"default"` | `"full-access"` | `acp_agent.go` |
| `session/request_permission` callbacks from the agent | **Auto-allowed by RingClaw** (the client always replies with the first `"allow"` option) — RingClaw itself does not interactively gate MCP tool calls | Agent generally stops issuing `request_permission` under full-access mode | `acp_rpc.go` |
| `fs/read_text_file` (ACP protocol) | ✅ allowed; no path check, no sandbox | ✅ allowed (unchanged) | `acp_terminal.go` |
| `fs/write_text_file` (ACP protocol) | ✅ iff agent config `allow_write: true`; otherwise `write permission denied: allowWrite is false` | ✅ iff `allow_write: true` — **full-access does NOT override `allow_write`** | `acp_terminal.go` |
| `terminal/create` (shell subprocess) | ✅ arbitrary command, arbitrary `cwd`, **no `allow_write` check, no path allowlist check** | ✅ same (unchanged) | `acp_terminal.go` |
| Agent-visible tool catalog | Whatever the ACP agent exposes in `default` mode (per-agent policy) | Whatever the ACP agent exposes in `full-access` mode | ACP-agent-specific |
| Top-level `/cwd` allowlist + denylist | Applies to `/cwd` command and `Agent.SetCwd` initial cwd only | **Unchanged** — the allowlist never applies to agent-chosen paths inside tool calls | `handler_commands.go` |

### Layer 3 invariants worth highlighting

::: warning
- **`allow_write: false` is not airtight.** It blocks the ACP
  protocol path (`fs/write_text_file`). It does **not** block the
  agent from shelling out via `terminal/create` to run
  `echo … > file`, `sed -i`, `git commit`, etc. Treat `allow_write`
  as a hint, not a sandbox.
- **No per-call approval in RingClaw.** `handlePermissionRequest`
  auto-selects the first `allow` option. A stricter gate lives in
  the ACP agent itself (for example, Claude's own tool-approval
  logic), not in RingClaw. Moving from default → full-access does
  not flip a RingClaw-side gate on or off; it just changes which
  `session/set_mode` RingClaw asks the agent to adopt.
- **`/cwd` allowlist ≠ file-access sandbox.** The allowlist
  constrains where the `/cwd` command may chdir the agent's
  starting working directory. An ACP agent can still read/write any
  file it has OS permission to touch, and can open terminals in any
  cwd it picks. See [Workspace Allowlist](./workspace-allowlist).
- **Full-access is additive on TWO axes.** Either a static
  `full_access: true` + top-level `full_access_ack: true` in
  `config.json`, or a runtime `/full-access grant` →
  `ringclaw approval <id>` handshake, will flip new sessions into
  full-access mode. Revoke / TTL expiry also **demotes every live
  session** via `DemoteAllACPFullAccess`; sessions whose demote
  call fails are dropped from the session map and rebuilt fresh on
  the next prompt.
:::

## Static activation: `full_access` + `full_access_ack`

Setting `full_access: true` on an ACP agent calls
`session/set_mode "full-access"` and disables RingClaw's per-call
MCP tool-call approval. This is dangerous: a prompt-injected agent
could read or destroy any file the process can reach.

To prevent silent activation through a stolen or copy-pasted
config, RingClaw requires an explicit acknowledgement in
`config.json`:

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

The legacy `RINGCLAW_FULL_ACCESS_ACK` environment variable is
silently ignored — a stray shell export cannot re-enable full
access.

When the request is downgraded, the session keeps the default
guarded mode. When honored, every freshly created ACP session emits
an additional `WARN ACP session granted full-access` log line for
audit (the `source` field distinguishes the static
`config:full_access` path from the dynamic `oob:/full-access` path
described below).

## Dynamic activation: `/full-access` two-step approval

A dynamic, time-boxed unlock layered on top of the static toggle.
The static config is still honored when set; the dynamic flow is
**additive** and lets operators leave `full_access: false` in
`config.json` and unlock full-access on demand from the bot DM:

```text
/full-access status         # show current grant state
/full-access grant           # request a 24h unlock (default)
/full-access grant 30m      # request a 30-minute unlock
/full-access revoke         # immediately lock again
```

The grant flow is a two-step confirmation requiring host machine
access:

1. Owner sends `/full-access grant [duration]`. Bot replies
   immediately with
   `Full-access grant requested. Confirm via terminal.`
   and posts a context-rich prompt to the owner DM:

   ```text
   Pending approval (challenge `abc12345`).
   Action: Grant ACP full-access for 30m
   Requester: Owen Owner <owen@example.com> (id=user-owner)
   Effect: agents with `full_access:true` will run MCP tool calls without per-call approval until the grant expires or `/full-access revoke` is used.

   Run on the host:
     ringclaw approval abc12345        (approve)
     ringclaw approval deny abc12345   (deny)

   Expires in 5m. Grant TTL: 30m.
   ```

   The requester label is a best-effort directory lookup
   (`<displayName> <email> (id=<numeric>)`); on lookup failure the
   prompt falls back to the bare numeric ID and still ships.

2. Owner runs `ringclaw approval <id>` on the host machine (or
   `ringclaw approval deny <id>` to reject). On approval the bot
   responds `Full-access granted until <RFC3339 expiry>.`; on
   denial or expiry the grant does not take effect.

**Chat-based `/approval` is disabled.** Any `/approval ...` message
in the bot DM is consumed and redirected to the terminal CLI. This
decouples approval authority from the RC account — a compromised
account cannot approve without host machine access. See
[Approval CLI](./approval-cli).

### Constraints

- Only the bot's DM with the trusted owner accepts `/full-access`.
  Group-chat invocations are refused with an explanatory message
  so the round-trip stays on the secured channel.
- Default grant duration is **24 hours**; the maximum is capped at
  **30 days**. Oversized inputs are silently clamped. Durations
  are parsed with `time.ParseDuration` (e.g. `30m`, `2h`, `168h`).
- **Approval requires host machine access.**
  `ringclaw approval <id>` calls the local API server
  (`127.0.0.1:18011`, loopback-only, token-authenticated). A
  compromised RC account can see the challenge ID in the DM but
  cannot approve without SSH or physical access to the host.
- Once granted, every newly-created ACP session is flipped into
  `session/set_mode "full-access"` until the grant expires or
  `/full-access revoke` is called. All OOB state is in-memory and
  is cleared on restart, so a crash-restart re-locks the bot until
  the operator explicitly re-grants.

### Live sessions are demoted on revoke / TTL expiry

::: warning
`/full-access revoke` (and TTL expiry) not only prevent NEW
sessions from entering full-access but also proactively send
`session/set_mode "default"` to every live ACP session that was
unlocked during the grant window. Sessions whose demote call fails
are dropped from the session map; the next prompt in that
conversation rebuilds a fresh session in the locked-down mode (so
a small amount of in-memory conversation context may be lost).
:::

When a grant ends (explicit `/full-access revoke` OR the TTL
elapses), the manager fires a revoke hook wired to
`agent.DemoteAllACPFullAccess`. That walker iterates every live
ACP session created during the grant window and sends
`session/set_mode "default"` to each. A narrow race between
grant-and-revoke landing during session creation is also closed by
a double-read in `getOrCreateSession`; if revoke lands while the
initial `set_mode "full-access"` call is in flight, the agent
immediately compensates with `set_mode "default"`.

## Audit-log additions

| Event | Log line | Purpose |
|---|---|---|
| Static full-access activated for a session | `WARN ACP session granted full-access` (`source: config:full_access` or `oob:/full-access` or `config:full_access+oob`) | One line per ACP session created in full-access mode. |
| Full-access challenge issued | `INFO oob: challenge issued` (`intent: grant ACP full-access for <duration>`) | Track every `/full-access grant` prompt. |
| Full-access granted | `WARN oob: ACP full-access granted` (`ttl`, `expiresAt`) | Fires after `ringclaw approval <id>` resolves the challenge. |
| Full-access revoked | `WARN oob: ACP full-access revoked` | Triggered by `/full-access revoke` or by re-grant. |
| Full-access expired (TTL) | `WARN oob: ACP full-access expired (TTL reached)` | Fires proactively when the grant's `expiresAt` is reached. |
| Live session demoted | `INFO acp demote: session returned to default mode` (`session`, `conversation`) | Confirms `session/set_mode "default"` landed on a live session after revoke / expiry. |
| Live session demotion failed | `WARN acp demote: set_mode default failed, session dropped from map` (with `error`) | The session is removed from the session map; the next prompt on that conversation creates a fresh (default-mode) session. |
