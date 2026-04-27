---
title: Cross-Chat Actions
---

# Cross-Chat Actions (Layer 2)

`ACTION` blocks emitted by the AI may carry a `chatid=` parameter
that targets a different chat than the one the message arrived in.
To prevent "summarize chat A in chat B" style data exfiltration, this
is now allowed only under a structured fail-closed gate. This page
covers the owner-initiated audit-notice path and the non-owner OOB
challenge path.

See [Permission Matrix](./index#permission-matrix) for how Layer 2
relates to the other layers, and [Approval CLI](./approval-cli) for
the terminal command that resolves cross-chat OOB challenges.

## Typical triggers

What a user types → what the agent may emit:

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
    END_ACTION    # ← triggers the cross-chat fail-closed path
```

## Gating table

`ACTION: NOTE|TASK|EVENT|CARD|MESSAGE ... END_ACTION` blocks are
parsed by `ParseAgentActions` and executed by `ExecuteAgentActions`
(`messaging/actions.go`). Gating is **independent of full-access**.

| Scenario | Behavior | Gate |
|---|---|---|
| ACTION in the origin chat (any sender) | ✅ always allowed | `actions.go` |
| `chatid=` override; requester is **not** on the trusted-sender allowlist | ⚠️ **OOB challenge issued** when OOB is configured: bot posts a context-rich challenge prompt to the owner DM; owner approves via `ringclaw approval <id>` on the host; on approval the action executes asynchronously in the target chat. Falls back to silent drop (forced to origin chat) when OOB is not configured. | `actions.go` (`crossChatOOBChallenge`) |
| `chatid=` override by owner; target = origin chat | ✅ allowed (same as row 1) | — |
| `chatid=` override by owner; target = owner's own DM | ✅ allowed without audit notice | `actions.go` (guard) |
| `chatid=` override by owner; target ≠ origin AND target ≠ owner DM | 🔒 **fail-closed**: bot first posts `[notice] <TYPE> by <requesterID> at <RFC3339>: origin=<id> target=<id>` to the owner DM; if the notice succeeds within `crossChatNoticeTimeout` (5 s) the action dispatches, otherwise it is **refused** with `Refused cross-chat <TYPE>: …` | `actions.go` (`announceCrossChatOrRefuse`) |
| Owner cross-chat action, but OOB not configured (no owner DM resolved) | ❌ refused with `Refused cross-chat <TYPE>: no owner DM audit channel configured` | `actions.go` |

::: warning Owner cross-chat behavior
Owner-initiated cross-chat ACTIONs are **not** unconditionally
honored. If the audit channel is missing or the notice send fails,
the action is **refused** — nothing lands on the target chat and the
caller sees a `Refused cross-chat <TYPE>` entry. Operators running
without a resolvable bot DM must either resolve it or keep all
owner-driven actions in the origin chat.
:::

## Owner-initiated cross-chat: synchronous audit notice

Before the cross-chat `MESSAGE` / `CARD` / `TASK` / `NOTE` is
dispatched, the bot posts a metadata-only notice to the owner DM
when the target chat differs from the origin AND from the owner's
own DM:

```text
[notice] MESSAGE by 12345 at 2026-04-17T10:15:00Z: origin=chat-7 target=chat-42
```

The notice carries `TYPE`, `requesterID`, an RFC3339 timestamp,
`originChatID`, and `targetChatID` — **no body, title, or content
preview** leaks into the owner DM. The send is capped at
`crossChatNoticeTimeout` (5 s) so a stuck RC endpoint cannot wedge
the prompt pipeline; when that cap triggers, the cross-chat action
is refused rather than dispatched without an audit record.

Refusal paths (the cross-chat action does NOT land on the target
chat):

- `OwnerDMChat` is empty (bot DM with the owner not yet resolved,
  or OOB not wired): the caller sees
  `Refused cross-chat <TYPE>: no owner DM audit channel configured`.
- Notice send returns an error (timeout, 5xx, transport error):
  `Refused cross-chat <TYPE>: audit notice delivery failed: <cause>`.

## Non-owner cross-chat OOB challenge

Non-owner senders who trigger a cross-chat ACTION (with `chatid=`
targeting a different chat) enter an OOB approval flow instead of
being silently dropped:

```text
Non-owner user @bot → AI reply includes ACTION: MESSAGE chatid=<other-chat>
  ↓
Bot posts a context-rich challenge prompt to the owner DM:

  Pending approval (challenge `def67890`).
  Action: Cross-chat MESSAGE
  Requester: Alice Cross <alice@example.com> (id=user-7)
  Origin chat: Engineering (id=origin-1)
  Target chat: Customer Support (id=target-9)
  Body: Highlights for the next quarter ...

  Effect: bot will write a MESSAGE into the target chat on the requester's behalf.

  Run on the host:
    ringclaw approval def67890        (approve)
    ringclaw approval deny def67890   (deny)

  Expires in 5m.
  ↓
Owner runs: ringclaw approval def67890
  ↓
Approved → action executes asynchronously in target chat → origin chat notified
Denied / expired → origin chat notified
```

The prompt body is capped at 200 characters (truncated with `…`
when longer); `Title:` / `Subject:` / `Assignee:` lines appear when
those ACTION params are present. **No content beyond what the
action itself will write is leaked into the owner DM.**

**Fallback**: when OOB is not configured (no Private App, no owner
DM resolved, or `OwnerID` empty), the legacy silent-override
behavior is preserved — `chatid=` is dropped and the action runs in
the origin chat.

**Terminal-only approval**: the challenge ID is delivered via DM so
the owner knows a request is pending, but the approval itself
requires running `ringclaw approval <id>` on the host machine. A
compromised RC account can see the challenge ID but cannot approve
without host access. See [Approval CLI](./approval-cli).

## Audit-log additions

| Event | Log line | Purpose |
|---|---|---|
| Cross-chat OOB challenge issued | `INFO oob: challenge issued` (`challengeID`, `requesterID`, `intent` starts with `cross-chat …`) | Track every approval prompt, including ones that timed out unanswered. |
| Cross-chat OOB approved — action executed | `INFO action: cross-chat OOB approved - <created note/task/message/…>` (`chatID`, details) | Confirms the action ran after terminal approval. |
| Cross-chat notice sent (pre-dispatch) | `INFO action: cross-chat notice sent (pre-dispatch)` (`type`, `from`, `to`, `ownerDMChat`, `requesterID`) | Confirms the heads-up reached the owner DM; the action is then dispatched. |
| Cross-chat action refused | `WARN action: cross-chat ACTION refused (fail-closed on pre-notice)` (with `error`) | Fires when the audit channel is missing or the notice send fails; the action is NOT dispatched. |
