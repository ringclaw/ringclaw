# RingClaw Architecture — V2 (Keller POC)

This document captures the architectural design decisions, platform capability model, and correct usage patterns for the Keller POC deployment. It reflects the updated Cron/Heartbeat ACTION execution policy introduced in V2.

---

## System Overview

RingClaw acts as a bridge between RingCentral Team Messaging and AI agent backends. For the Keller deployment, multiple specialized bots (orders-bot, tom-bot, karen-bot, beth-bot, finance-bot, hr-bot) share the same infrastructure but have distinct personas, Cron schedules, and channel assignments.

```
RingCentral Team Messaging
         │
         │  WebSocket (inbound messages)
         ▼
     RingClaw Bridge
         │
         ├── orders-bot  (dispatch + field ops)
         ├── tom-bot     (store operations Heartbeat)
         ├── karen-bot   (Lowe's account + fax)
         ├── beth-bot    (executive reporting)
         ├── finance-bot (payments + reconciliation)
         └── hr-bot      (crew scaling + training)
         │
         │  ACTION blocks → RC API calls
         ▼
  RingCentral API (Tasks, Notes, Cards, SMS, Messages, Events)
```

---

## Four Entry Points

RingClaw can act on RingCentral via four distinct entry points. The permission model varies per path:

| Entry point | Sender layer | Command layer | ACTION layer | What gates it |
|-------------|-------------|--------------|--------------|---------------|
| WebSocket message | YES | YES | YES (cross-chat gated) | Chat allowlist + sender allowlist + handler |
| HTTP API (`/api/send`, `/api/tasks`, etc.) | NO | NO | NO | API token + loopback Host only |
| Cron job | NO | Job created via `/cron add` (privileged) | **YES — see Cron allowlist below** | Job config in `~/.ringclaw/cron/jobs.json` |
| Heartbeat | NO | Config-driven | **YES — see Heartbeat allowlist below** | `heartbeat.enabled` + `HEARTBEAT.md` |

---

## Cron / Heartbeat Graduated ACTION Capability

### V2 Design Principle

Cron and Heartbeat triggers now execute ACTION blocks by default, within a defined capability tier. The previous blanket restriction ("Cron/Heartbeat replies are posted verbatim; ACTION blocks are not executed") has been replaced by a graduated allowlist model.

The rationale for graduation rather than full open execution:

- **Low-blast-radius actions** (NOTE, TASK, CARD, MESSAGE) are idempotent or reversible and produce audit trails automatically. These are safe to run without a human sender in the loop.
- **SMS** carries a small cost and reaches external recipients; allowed for Cron (where a human configured the job and schedule) but excluded from Heartbeat (which runs continuously and has no operator confirmation step per run).
- **FAX** has external transmission cost, regulatory considerations, and is used in the Keller deployment only for the Lowe's account workflow. It is excluded from both default allowlists — a human must trigger it (via Card button or `/lowes-batch`).
- **VIDEO / PHONE_CALL** initiate real-time communication sessions. These require explicit whitelist opt-in and are not in any default capability tier.

### Cron Default Allowlist

Actions that execute automatically when a Cron-triggered agent reply contains the corresponding ACTION block:

| ACTION type | Default allowed | Notes |
|-------------|----------------|-------|
| MESSAGE | Yes | Can target cross-chat (owner audit notice still applies for cross-chat) |
| NOTE | Yes | Creates/appends to notes in target chat |
| TASK | Yes | Creates tasks; `priority=URGENT` is honored |
| CARD | Yes | Posts structured Adaptive Card to target chat |
| SMS | Yes | Sends SMS to specified recipients |
| FAX | **No** | Not in default allowlist; requires manual trigger |
| VIDEO | **No** | Requires whitelist opt-in |
| PHONE_CALL | **No** | Requires whitelist opt-in |

### Heartbeat Default Allowlist

Actions that execute automatically when a Heartbeat-triggered agent reply contains the corresponding ACTION block:

| ACTION type | Default allowed | Notes |
|-------------|----------------|-------|
| MESSAGE | Yes | Can target cross-chat |
| NOTE | Yes | Creates/appends to notes |
| TASK | Yes | Creates tasks; priority escalation honored |
| CARD | Yes | Posts structured Adaptive Card |
| SMS | **No** | Excluded from Heartbeat default; Heartbeat runs continuously without per-run operator confirmation |
| FAX | **No** | Not in default allowlist |
| VIDEO | **No** | Requires whitelist opt-in |
| PHONE_CALL | **No** | Requires whitelist opt-in |

### Summary Matrix

| ACTION type | WebSocket message | Cron | Heartbeat |
|-------------|------------------|------|-----------|
| MESSAGE | Yes (cross-chat gated) | Yes | Yes |
| NOTE | Yes | Yes | Yes |
| TASK | Yes | Yes | Yes |
| CARD | Yes | Yes | Yes |
| SMS | Yes | Yes | No |
| FAX | Yes (if configured) | No | No |
| VIDEO | Yes (if whitelisted) | No (whitelist opt-in) | No (whitelist opt-in) |
| PHONE_CALL | Yes (if whitelisted) | No (whitelist opt-in) | No (whitelist opt-in) |

---

## Cross-Chat ACTION Behavior from Cron/Heartbeat

When a Cron or Heartbeat job emits an ACTION block targeting a different chat than the job's origin chat, the cross-chat rules still apply:

- For Cron jobs created by the owner, the **synchronous audit notice** path fires: the bot posts a metadata-only notice to the owner DM before dispatching the cross-chat action.
- The notice contains only `TYPE`, `requesterID`, timestamp, `originChatID`, and `targetChatID` — no body content.
- If the audit channel (owner DM) is not resolvable, the cross-chat action is refused with `Refused cross-chat <TYPE>: no owner DM audit channel configured`.

This means Cron jobs that send SMS to crew leaders or push Cards to executive DMs operate through the same audit trail as operator-initiated cross-chat actions, providing a complete record of automated activity.

---

## Bot Cron/Heartbeat Schedule Reference

### orders-bot

| Job name | Schedule | Trigger type | Actions executed |
|----------|----------|-------------|-----------------|
| morning-check | 08:00 daily | Cron | TEXT list + TASK (URGENT per order) + SMS to crew leaders |
| 30min-confirm | 30 min after dispatch | Cron (one-shot per dispatch) | TASK (URGENT, unconfirmed) + SMS to crew leader (second request) |

### tom-bot

| Job name | Schedule | Trigger type | Actions executed |
|----------|----------|-------------|-----------------|
| daily-summary | 17:30 daily | Heartbeat | TEXT summary + CARD (#atlanta-ops) + TASK escalation (overdue → URGENT) |

Note: Heartbeat does not include SMS. tom-bot cannot send SMS from the Heartbeat path.

### karen-bot

| Job name | Schedule | Trigger type | Actions executed |
|----------|----------|-------------|-----------------|
| batch-prep | 17:00 daily | Cron | TEXT list + CARD (fax prep with button) + NOTE (SLA ledger daily row) |
| lowe's-sla-weekly | Friday 17:00 | Cron | CARD (SLA weekly report) + NOTE (ledger weekly summary) |

Note: The actual FAX transmission is NOT executed by the Cron. The CARD contains an "Execute Batch Fax" button; Karen clicks the button (or types `/lowes-batch`) to trigger the actual fax send.

### beth-bot

| Job name | Schedule | Trigger type | Actions executed |
|----------|----------|-------------|-----------------|
| exec-weekly | Monday 09:00 | Cron | CARD (weekly report → Beth DM, store highlights, suggested inquiries) |

### finance-bot

| Job name | Schedule | Trigger type | Actions executed |
|----------|----------|-------------|-----------------|
| subcontractor-payment | Thursday 15:00 | Cron | CARD (payment approval → Alex, [Approve Payment] button) |
| lowe's-payment-reconciliation | 5th of month | Cron | CARD (overdue receivables → Alex, [Send Collection Fax] button) + NOTE (reconciliation ledger monthly row) |
| month-end-close | 28th of month | Cron | Step 1: NOTE (ledger summary) → Step 2: CARD (payment approval → Alex) → Step 3: NOTE (travel expense allocation) → Step 4: CARD (cost overrun alert → Alex) → Step 5: CARD (management report → #exec + Beth DM) |

### hr-bot

| Job name | Schedule | Trigger type | Actions executed |
|----------|----------|-------------|-----------------|
| seasonal-crew-scaling | February 1 (annual) | Cron | MESSAGE to all store managers (gap inquiry) + NOTE (national gap ledger init) |
| quarterly-training | Quarterly (1st) | Cron | EVENT (training session for non-compliant employees) + SMS to non-compliant employees |
| training-heartbeat | Linda-configured | Heartbeat | NOTE (monthly completion rate update) + CARD (non-compliant summary → Linda DM) |

---

## Design Principles

### 1. Graduated automation with retained human checkpoints

Not all operations should be fully automated. The Keller deployment distinguishes three tiers:

- **Fully automatic:** Low-blast-radius, reversible, or audit-logged (NOTE, TASK creation, informational CARD). The Cron or Heartbeat executes these without waiting for a human.
- **One-tap approval:** High-stakes or irreversible operations (payment authorization, fax transmission) are delivered as Cards with action buttons. The human must click once, but does not need to draft or type anything.
- **Manual-only:** Operations with external regulatory, cost, or real-time implications (VIDEO, PHONE_CALL) require explicit capability whitelist configuration and cannot be triggered from scheduled jobs by default.

### 2. Cards as the default output format for scheduled reports

Scheduled jobs (Cron and Heartbeat) should prefer `ACTION:CARD` over plain TEXT for structured reporting. Cards:

- Are filterable and scannable in the RingCentral interface.
- Can embed action buttons for one-tap approvals.
- Are addressable (can be updated or deleted by card ID).
- Provide a clear visual boundary between automated output and human messages.

Plain TEXT output from scheduled jobs is appropriate for operational alerts (the list before the Card) but should not be the sole output format for reports that require action.

### 3. NOTE as the audit ledger primitive

Recurring Cron jobs that accumulate data over time (SLA tracking, reconciliation, training completion rates) should write each run's output as an `ACTION:NOTE` append to a persistent ledger note. This provides:

- A running history without flooding the chat with individual messages.
- A single note URL that stakeholders can bookmark.
- Automatic locking/unlocking (handled by the NOTE action executor).

### 4. FAX remains manually gated

Despite Cron's default SMS and MESSAGE capabilities, FAX is excluded from the default allowlist. The design rationale:

- FAX transmission has external delivery confirmation requirements (the Lowe's contract references physical fax as the binding delivery method for certain notices).
- An accidental or duplicate FAX transmission cannot be recalled.
- The Card-with-button pattern provides the same convenience as automation (one click, pre-filled queue) while preserving the one-human-confirmation requirement.

If a future deployment needs fully automated FAX, add `fax: true` to the Cron capability config for the specific bot. This is not recommended for the Keller Lowe's workflow.

### 5. Heartbeat SMS exclusion

Heartbeat runs on a continuous interval without a per-run operator confirmation step. Sending SMS from a continuous interval loop creates risk of recipient spam if:

- The agent incorrectly assesses a condition as requiring notification on multiple consecutive runs.
- The deduplication window (24h suppression for identical Heartbeat replies) does not catch variations in wording.

Cron jobs have a fixed, operator-configured schedule and a specific prompt; the operator consciously chose "run this every day at 08:00 and send SMS if conditions match." Heartbeat's implicit "check continuously" model does not carry the same deliberate per-notification authorization. SMS is therefore excluded from the Heartbeat default allowlist.

---

## Diagnosing Cron/Heartbeat Issues

### Symptom: ACTION block appears as plain text in chat

This was the correct behavior under the V1 platform rule. Under V2, if ACTION blocks are still appearing as plain text from Cron or Heartbeat jobs, check:

1. **RingClaw version** — V2 ACTION execution from Cron/Heartbeat requires the updated `messaging/cron.go` and `messaging/heartbeat` execution paths. Run `ringclaw version` to confirm.
2. **ACTION type** — Verify the ACTION type is in the default allowlist for the trigger type (Cron vs Heartbeat). FAX, VIDEO, and PHONE_CALL are not in the default list and will still be posted as text.
3. **Cross-chat audit channel** — If the ACTION targets a different chat and the owner DM is not resolved, the action is refused (not posted as text — it is refused silently). Check logs for `Refused cross-chat`.
4. **Cron job config** — Inspect `~/.ringclaw/cron/jobs.json` to confirm the job is targeting the correct chat ID and agent.

### Symptom: SMS not sent from Heartbeat

Expected. SMS is not in the Heartbeat default allowlist. If the bot persona requires SMS-on-condition behavior, convert it to a Cron job with a schedule that matches the intended check interval.

### Symptom: FAX button on Card does nothing

The Card button triggers the `/lowes-batch` command or equivalent. Confirm:

1. The karen-bot instance has the Lowe's fax credentials configured.
2. The user clicking the button is in the authorized sender list for the chat.
3. Check logs for `fax: not in default cron allowlist` — if this appears, the fax was attempted from an ACTION block (not a button click) and was correctly blocked.

### Symptom: finance-bot month-end-close Cron only partially completes

The month-end-close Cron executes 5 steps sequentially. If it stops partway:

1. Steps 2 and 4 push Cards and continue without waiting for Alex's button click. They do not block.
2. Step failure is logged as `ERROR cron: month-end-close step N failed`. Check the specific step.
3. NOTE actions (Steps 1 and 3) can fail if the target note is locked by another edit session. The NOTE action retries once; if it fails, it logs and continues to the next step.
4. CARD actions (Steps 2, 4, 5) fail if the target chat ID is not in the bot's allowed chat list. Verify `chat_ids` in config.

---

## Security Posture for Scheduled Operations

Scheduled Cron and Heartbeat jobs operate without a human sender, which has implications for the permission model:

- **Layer 0 (sender allowlist):** Not applicable — Cron/Heartbeat have no inbound sender. The job was created via `/cron add` (Layer 1 privileged command) by an authorized operator.
- **Layer 1 (command authorization):** Not applicable at execution time. Authorization was verified at job creation.
- **Layer 2 (cross-chat ACTION gate):** Applies. Cross-chat actions from Cron/Heartbeat jobs require the owner DM audit channel to be configured. If it is not, cross-chat actions are refused.
- **Layer 3 (ACP session mode):** Applies normally. The ACP session mode for the Cron/Heartbeat agent is determined by the bot's static config.

Operators should treat the Cron job store (`~/.ringclaw/cron/jobs.json`) with the same sensitivity as the API token — anyone who can write to it can schedule arbitrary agent prompts that execute ACTION blocks against the RC API on a timer.
