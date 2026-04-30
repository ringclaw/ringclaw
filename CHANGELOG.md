# Changelog

All notable changes to this project are documented here.

This project follows [Semantic Versioning](https://semver.org/) on
the bot binary surface (`config.json` schema + CLI flags + log
contract). Doc-only or test-only changes do not bump the version.

## [Unreleased]

Planned for v0.5.0 — OS-level sandboxing for non-owner agent
processes to close the WebFetch / WebSearch / MCP gap left open by
v0.4.3's client-side gate.

---

## v0.4.4 — 2026-04-29 — fix: restore chat_user_allow loading

### Bug

v0.4.3 promised that `chat_user_allow` users would be preserved
across restarts and run under the new non-owner ceiling, but
`cmd/start.go` inadvertently kept the v0.4.2 stop-gap that
force-cleared the map at startup — so configured per-chat
exceptions silently disappeared on every boot, and OOB-approved
users had to be re-approved after every restart.

### Fix

- **`cmd/start.go`** restores the v0.4.1 resolve-and-inject path:
  email / phone / numeric identifiers resolve to numeric user IDs
  via the Private App directory and push into both Monitor and
  Handler. Listed users run under the v0.4.3 non-owner ceiling
  (Layer A read-only ACP mode + Layer B fail-closed deny of
  `fs/*` / `terminal/*` / `session/request_permission`); plain-text
  replies and RC `ACTION:MESSAGE / TASK / NOTE / EVENT` blocks
  remain available.
- **OOB-enabled startup notice** demoted from `WARN` to `INFO`
  with an updated description (the v0.4.3 ceiling makes the old
  "approved users get full agent capability" warning incorrect).
- **New `WARN`** when an entry resolves to zero numeric IDs
  (typical cause: chat ID is a team display name instead of the
  numeric chat ID), with the offending raw identifiers logged.

### Docs

- `docs/security/sender-allowlist.md`: removed the "force-cleared
  at startup" language; added an **Enabling OOB approval for
  group members** section (4 prerequisites, minimal config,
  expected startup logs, approval flow) and a **How to find a
  chat ID** section (3 methods).
- `docs/security/index.md`, `docs/guide/configuration.md`, and
  Chinese mirrors updated to match.
- Audit-log table gained two `v0.4.4:` rows (loaded /
  resolved-to-zero).

### Files

- `cmd/start.go`
- `docs/security/sender-allowlist.md` (+ZH mirror)
- `docs/security/index.md` (+ZH mirror)
- `docs/guide/configuration.md` (+ZH mirror)
- `CHANGELOG.md`

### Compatibility

- Existing v0.4.3 binaries that started with `chat_user_allow`
  populated will see the old force-clear behavior persist until
  the operator upgrades — that is, the on-disk map keeps getting
  emptied on every boot. v0.4.4 stops doing the wipe; pre-existing
  entries (whether OOB-issued or hand-edited) start surviving
  restarts again as soon as the upgrade lands.
- No config schema change.

---

## v0.4.3 — 2026-04-28 — SECURITY: fail-closed two-tier non-owner isolation

### Highlights

v0.4.3 turns the non-owner ceiling promised in v0.4.2 into actual
runtime enforcement, without waiting for v0.5.0's separate-process
sandbox. Two cooperating layers protect every non-owner ACP
session:

- **Layer A — protocol.** ringclaw issues
  `session/set_mode <restricted>` immediately after creating the
  session. The modeID comes from a per-agent map: `droid → spec`,
  `claude → plan`, `gemini → plan`, `qwen → plan`,
  `cursor-agent → plan`; unknown agents fall back to a heuristic
  over `availableModes` (`plan` / `spec` / `read` / `safe`).
  Operators can override per agent via the new
  `agents.<name>.restricted_mode_id` field; the override must
  match a mode the agent advertises, otherwise the built-in
  selection wins.
- **Layer B — client gate (fail-closed).** ringclaw rejects
  `fs/read_text_file`, `fs/write_text_file`, `terminal/create`,
  `terminal/output`, `terminal/wait_for_exit`, `terminal/kill`,
  `terminal/release`, and `session/request_permission` JSON-RPC
  requests from non-owner sessions with
  `code=-32001 "denied for non-owner senders"`, regardless of
  whether the agent honored Layer A. Layer B is the actual
  security boundary for `fs/*` / `terminal/*` — Layer A is
  defense-in-depth.

### Owner / non-owner split

"Owner" is now strictly the resolved Private App owner plus
`source_user_ids`. `chat_user_allow` users and any v0.4.0
OOB-approved users are **non-owners** and run under the v0.4.3
ceiling. DMs to the bot are always treated as owner conversations
regardless of allowlist membership.

### Fail-closed behavior

When the agent advertises no read-only mode and the operator did
not configure an override, ringclaw refuses the non-owner message
outright instead of forwarding it. The user receives a refusal
text; the audit log records
`event=restricted_mode_unsupported_no_mode` (or
`restricted_mode_unsupported` when `set_mode` itself was rejected).
The (`agentCmd`, `modeID`) pair is cached so subsequent attempts
skip the failed RPC.

### Known limitations (still requiring v0.5.0)

`WebFetch`, `WebSearch`, the agent's built-in HTTP tools, and MCP
custom tools are dispatched directly inside the agent process.
ringclaw does not see those JSON-RPC requests, so Layer B cannot
apply — those remain best-effort under Layer A only. v0.5.0 will
close that gap with OS-level sandboxing of the non-owner agent
subprocess.

### Audit log additions

| Event | Log line |
|---|---|
| Restricted mode applied | `WARN acp restricted-mode event` (`event=restricted_mode_applied`, `mode_id`, `mode_source`, `conversation`, `sender_id`) |
| Restricted mode unsupported (no candidate) | `WARN acp restricted-mode event` (`event=restricted_mode_unsupported_no_mode`, `available_modes`) |
| Restricted mode unsupported (`set_mode` rejected) | `WARN acp restricted-mode event` (`event=restricted_mode_unsupported`, `error`) |
| Layer-B tool-call denied | `WARN acp non-owner tool call denied` (`event=tool_call_denied`, `method`, `session`, `reason`) |

Each warning is deduped per `(session, method)` or
`(agentCmd, modeID)` pair so a misbehaving agent does not flood
the operator log.

### Config schema

```jsonc
{
  "agents": {
    "droid": {
      "type": "acp",
      "command": "droid",
      "restricted_mode_id": "spec"     // optional override
    },
    "claude": {
      "type": "acp",
      "command": "claude",
      "restricted_mode_id": "plan"
    }
  }
}
```

### Files

- `agent/origin.go`, `agent/restricted_modes.go`, `agent/acp_gate.go`
- `agent/acp_agent.go` — `sessionRoles` / `sessionModes` /
  `restrictedSetModeUnsupported` / `restrictedSetModeWarned` /
  `deniedToolWarned` maps; `getOrCreateSession` takes an `Origin`;
  fail-closed branch in `chatWithEntries`; `applyRestrictedMode`
  helper.
- `agent/acp_terminal.go` — Layer-B gate at all seven
  `fs/*` / `terminal/*` handler entries.
- `agent/acp_rpc.go` — `handlePermissionRequest` denies non-owner
  sessions (using either the agent-offered `kind=deny` option or a
  `cancelled` outcome).
- `messaging/sender_role.go` — `originForPost`: DM = owner,
  `source_user_ids` = owner, `chat_user_allow` = non-owner with
  `Reason="chat_user_allow"`, anyone else = non-owner.
- `messaging/handler.go` / `messaging/handler_summarize.go` —
  Origin attached to the dispatch context.
- `config/config.go` — `AgentConfig.RestrictedModeID` field with
  godoc SECURITY NOTE.
- Docs: `docs/security/sender-allowlist.md`,
  `docs/security/index.md`, `docs/guide/configuration.md`, and
  Chinese mirrors.
- Tests: `restricted_modes_test.go`, `gate_test.go`,
  `origin_test.go`, `acp_restricted_mode_test.go`,
  `messaging/sender_role_test.go`. Existing `acp_agent_test.go`,
  `acp_set_mode_unsupported_test.go`, `acp_terminal_test.go`,
  `acp_rpc_test.go` updated for the new `Origin` parameter.

### Compatibility

- Owner-side behavior is unchanged. Existing tests pass without
  configuration changes.
- `OriginFromContext` defaults to "owner" when no Origin is
  attached, so non-messaging callers (cron, `/api/send`) continue
  to operate as owner.
- Operators with read-only-mode-incapable agents need to either
  set `restricted_mode_id` or accept that non-owner messages will
  be refused with the v0.4.3 fail-closed text.

---

## v0.4.2 — 2026-04-28 — SECURITY: revert v0.4.1 default-on; force-clear `chat_user_allow`

### :rotating_light: Security advisory (retracts v0.4.1)

A capability-isolation gap was discovered in v0.4.1 (and earlier):
once a non-owner sender becomes "trusted" — whether through
`source_user_ids`, `chat_user_allow`, or the v0.4.1 OOB-approval
flow — they drive the same agent backend the bot operator uses.
Through the agent tool-call channel they can request:

- Filesystem reads / writes (`List`, `Read`, `Write`)
- Terminal commands (`Bash`)
- External HTTP calls (`WebFetch`, `WebSearch` and similar
  agent-side tools)

ringclaw has no per-sender capability gating before v0.5.0, so this
amounts to handing the trusted user host-shell-equivalent power in
their authorized chats. v0.4.1 made this worse by flipping the OOB
approval default to **on**, which combined with operator click-fatigue
to expand the blast radius.

### What v0.4.2 does

- `ringcentral.allow_group_mention_authorize` default reverted to
  **OFF**. The v0.4.1 default-on flip is withdrawn. Set the flag to
  `true` explicitly to opt in.
- `ringcentral.chat_user_allow` is **force-cleared at every
  startup** with a loud `ERROR` log (per-chat detail + global
  summary) and persisted via `config.Save()`. Pre-existing v0.4.1
  entries are wiped so the operator must re-add by hand after
  re-evaluating the trade-off.
- Startup `WARN` whenever `allow_group_mention_authorize` is
  explicitly enabled, reminding operators that approved users
  currently get full agent capability.
- Docs: `docs/security/sender-allowlist.md` (and Chinese mirror)
  carry a `:::danger SECURITY ADVISORY` block + a `:::warning`
  Layer 2 callout. Configuration / index / approval-cli /
  api-and-clients pages updated to reflect the flipped default and
  the force-clear behavior.

### What v0.4.2 does **not** do

- It does **not** sandbox `source_user_ids` users. They retain the
  same capability surface as the owner. Review your list before
  upgrading.
- It does **not** introduce the restricted backend. That ships in
  v0.5.0.

### Upgrade notes

- If you relied on v0.4.1 defaults to auto-prompt non-trusted
  senders, ringclaw will now silently drop them again. Set
  `allow_group_mention_authorize: true` in `config.json` if you
  want the behavior back, after reading the advisory.
- If you had `chat_user_allow` entries (hand-rolled or written by
  v0.4.1 OOB), they will be wiped on first boot. Watch for the
  `SECURITY: chat_user_allow ... cleared on startup` ERROR lines
  and decide which entries to re-add.

### Files

- `config/config.go` — godoc advisories on
  `AllowGroupMentionAuthorize` and `ChatUserAllow`;
  `IsAuthorizeMentionEnabled` returns `false` on nil again.
- `cmd/start.go` — startup force-clear of `ChatUserAllow` with
  `ERROR` logs + `Save()`; startup `WARN` when OOB is explicitly
  enabled; `ERROR` (no INFO branch) when the Private App is
  missing.
- `config/authorize_mention_test.go` — assertion flipped back to
  default-disabled.
- `docs/security/sender-allowlist.md`, `docs/security/index.md`,
  `docs/security/approval-cli.md`,
  `docs/security/api-and-clients.md`,
  `docs/guide/configuration.md` — flipped semantics + advisory.
- Chinese mirror: `docs/zh/security/sender-allowlist.md`,
  `docs/zh/security/index.md`, `docs/zh/security/approval-cli.md`,
  `docs/zh/security/api-and-clients.md`,
  `docs/zh/guide/configuration.md`.

---

## v0.4.1 — 2026-04 — `:warning: RETRACTED by v0.4.2`

> **Retracted.** The default-on flip introduced in this release
> expanded the v0.4.0 capability surface in a way that could let
> OOB-approved users reach filesystem / terminal / HTTP tools via
> the agent. v0.4.2 reverts the default and force-clears
> `chat_user_allow`. The 24-hour cooldown is retained.

- `feat(security)`: default `allow_group_mention_authorize` to
  **on** (unset = enabled).
- `feat(security)`: 24-hour per-`(chat, user)` cooldown after deny
  / expire so a noisy non-trusted user can't spam the owner DM.
- Docs + ZH mirror updates for the flipped default.

PR #132. Tag `v0.4.1`.

---

## v0.4.0 — 2026-04 — Authorize-mention OOB flow

- `feat(security)`: introduce the authorize-mention OOB flow.
  Non-trusted `@bot` in allowed group chats can be approved per
  chat without restarting or hand-editing `config.json`.
- `feat(security)`: persist approvals into
  `chat_user_allow[<chatID>]` with email preferred over numeric
  extension ID.
- `feat(security)`: same `ringclaw approval <id>` CLI handles
  authorize-mention, `/full-access`, and cross-chat ACTION
  challenges.
- Docs: new `docs/security/sender-allowlist.md` chapter, Chinese
  mirror, mermaid sequence diagrams.

Default: opt-in (`allow_group_mention_authorize: true` to enable).

Tag `v0.4.0`.

---

## v0.3.x — Earlier

- Sonar coverage lifted past 80 % via UTs + conservative
  exclusions (PR #131).
- Docs restructure under `docs/security/` (PR #130).
- Earlier history: see `git log` for the v0.3.x line.
