# Changelog

All notable changes to this project are documented here.

This project follows [Semantic Versioning](https://semver.org/) on
the bot binary surface (`config.json` schema + CLI flags + log
contract). Doc-only or test-only changes do not bump the version.

## [Unreleased]

Planned for v0.5.0 — restricted agent backend for non-owner senders
(text replies + RingCentral `ACTION:MESSAGE / TASK / NOTE / EVENT`
only; no filesystem, terminal, or external HTTP).

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
