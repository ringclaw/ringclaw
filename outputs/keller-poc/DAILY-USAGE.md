# Keller POC — Daily Usage Guide

This guide describes how each team member at Keller interacts with the bot system day-to-day, reflecting the updated Cron/Heartbeat capabilities where scheduled agents can now execute ACTION blocks automatically.

---

## Role Profiles

### Sarah — CSR (Customer Service Representative)

Sarah manages dispatch confirmations and field crew coordination. The bot system now reduces a large portion of her manual follow-up work.

**Morning (08:00) — orders-bot morning-check Cron**

Sarah no longer needs to manually scan for unconfirmed dispatch orders at the start of her day. The `morning-check` Cron fires automatically:

- Posts a TEXT list in the dispatch channel of all orders that have been unconfirmed for more than 18 hours.
- Automatically creates an `ACTION:TASK` (URGENT priority) for each unconfirmed order — these appear immediately in the task list without Sarah touching anything.
- Sends `ACTION:SMS` alerts to the crew leader responsible for each unconfirmed order.

**Post-dispatch (30 minutes after dispatch) — orders-bot 30min-confirm Cron (new)**

After a dispatch is issued, the system fires once at the 30-minute mark:

- Creates another `ACTION:TASK` (URGENT) flagging the order as still unconfirmed if no confirmation has come in.
- Sends a second `ACTION:SMS` to the crew leader requesting confirmation.

Sarah's role shifts from "chase people all morning" to "review what the system already escalated and act on exceptions." She still handles inbound calls and edge cases, but routine reminders are no longer on her plate.

---

### Tom — Store Operations Manager (#atlanta-ops)

Tom oversees daily field operations across his region. He checks the `#atlanta-ops` channel for the end-of-day wrap-up.

**End of day (17:30) — tom-bot Heartbeat**

Previously Tom would receive a plain-text summary paragraph. The Heartbeat now outputs:

- A **TEXT summary** of the day's activity (still readable inline).
- An `ACTION:CARD` — a structured daily report Card posted to `#atlanta-ops`. The Card contains:
  - Today's completed jobs
  - Delayed orders
  - Tomorrow's scheduled appointments
  - Crew staffing gaps
  - The longest-outstanding open Task
- `ACTION:TASK` upgrades for any overdue-unresolved Tasks — they are automatically escalated to URGENT status. Tom does not need to manually bump priority.

Note: The Heartbeat does not send SMS. If Tom needs to alert a crew leader directly, he can do so from the Card's context or via a manual message.

Tom's daily check-in becomes a Card review rather than reading through a text block. Overdue items are already escalated before he reads the Card.

---

### Karen — Lowe's Account Manager

Karen handles the Lowe's account, including weekly SLA reporting and batch SMS dispatch for work order completion notices.

**Afternoon (17:00) — karen-bot batch-prep Cron**

Previously Karen had to read a plain-text notification list and then type `/lowes-batch` to trigger transmission. The new flow:

- Posts a TEXT notification list (still readable for audit).
- Pushes an `ACTION:CARD` — a "Completion Notice Preparation Card" that contains the full SMS notification queue and a **"Send Batch Completion Notices"** button.
- Creates an `ACTION:NOTE` updating the SLA ledger note with a daily summary row.

Karen clicks the button on the Card to trigger the actual SMS notification dispatch. This replaces typing a slash command — more discoverable, same one-step human confirmation. SMS batch dispatch is not in the default Cron allowlist, so the SMS notification dispatch still requires Karen's button press; the system will not send SMS notifications autonomously.

**Friday (17:00) — karen-bot lowe's-sla-weekly Cron**

- Pushes an `ACTION:CARD` with the Lowe's SLA weekly report (structured, not a text block).
- Appends an `ACTION:NOTE` to the SLA ledger with the week's summary row.

Karen reviews the Card in the channel and can forward or screenshot it directly for the Lowe's account record.

---

### Beth — Executive (District/Regional Oversight)

Beth receives high-level weekly performance reporting across all 33 stores.

**Monday (09:00) — beth-bot exec-weekly Cron**

Previously Beth received a text paragraph in her DM. The new flow:

- Pushes an `ACTION:CARD` directly to Beth's DM — a structured weekly report Card.
- The Card highlights stores requiring attention (performance flags, SLA misses, cost overruns).
- Includes this week's suggested inquiry points.

Beth's Monday morning starts with a Card in her DM rather than a block of text. The Card format makes it easy to forward to other executives or reference in calls without reformatting.

---

### Alex — Finance Manager

Alex approves payments and manages month-end close. The bot system now automates the multi-step process.

**Thursday (15:00) — finance-bot subcontractor-payment Cron**

Previously Alex received a text list and had to manually process each item. The new flow:

- Pushes an `ACTION:CARD` to Alex — a payment approval Card containing the full payment list breakdown and an **"[Approve Payment]"** button.
- Alex clicks the button to authorize the batch. No manual entry required.

**5th of each month — finance-bot lowe's-payment-reconciliation Cron**

- Pushes an `ACTION:CARD` to Alex — an overdue receivables detail Card with a **"[Send Collection SMS to Lowe's coordinator]"** button.
- Appends an `ACTION:NOTE` to the reconciliation ledger with the monthly row.

**28th of each month — finance-bot month-end-close Cron**

The Cron executes all five month-end steps automatically. Alex previously ran these manually over multiple days:

| Step | What happens automatically | Alex's role |
|------|---------------------------|-------------|
| 1 | `ACTION:NOTE` — reconciliation ledger summary appended | None (automatic) |
| 2 | `ACTION:CARD` — payment approval Card pushed to Alex, with [Approve Payment] button | Click to approve |
| 3 | `ACTION:NOTE` — travel expense allocation detail appended to ledger | None (automatic) |
| 4 | `ACTION:CARD` — cost overrun alert Card pushed to Alex; flagged stores highlighted, [Notify Karen to Start Contract Review] button | Click to escalate if needed |
| 5 | `ACTION:CARD` — monthly management report Card pushed to `#exec` channel and Beth's DM | None (automatic) |

Alex's month-end workflow changes from "3 days of manual assembly" to "arrive Monday, see 2 Cards in #finance, click 2 buttons."

---

### Linda — HR Manager

Linda manages crew scaling and recurring training compliance.

**February 1 (annual) — hr-bot seasonal-crew-scaling Cron**

Previously Linda received a text notification and had to call each store manager individually. The new flow:

- Sends `ACTION:MESSAGE` to each store manager — a structured inquiry with template questions about peak-season staffing gaps.
- Initializes an `ACTION:NOTE` — a national staffing gap tracking ledger for the season.

Linda collects responses passively the next day as store managers reply to the MESSAGE. No outbound phone calls needed.

**Quarterly (1st of each quarter) — hr-bot quarterly-training Cron**

Previously Linda received a text reminder. The new flow:

- Creates `ACTION:EVENT` — a training session scheduled for each employee who has not completed the current cycle's requirement.
- Sends `ACTION:SMS` to each non-compliant employee with the training reminder.

Linda's quarterly training cycle opens with Events already on people's calendars and SMS reminders already sent.

**Optional Heartbeat (Linda-configured) — hr-bot Heartbeat**

If Linda enables this Heartbeat:

- Appends an `ACTION:NOTE` updating the monthly training completion rate in the ledger.
- Pushes an `ACTION:CARD` to Linda's DM summarizing non-compliant employees.

---

## Automation Reference Table

This table summarizes which actions happen automatically, which require user trigger, and which are cross-agent automatic operations.

| Bot | Trigger | Time | Auto (no human needed) | User trigger required | Cross-agent / cross-chat |
|-----|---------|------|------------------------|----------------------|--------------------------|
| orders-bot | morning-check Cron | 08:00 daily | TEXT list, TASK creation, SMS to crew leaders | — | SMS to crew leaders (cross-chat) |
| orders-bot | 30min-confirm Cron | 30 min post-dispatch | TASK creation, SMS to crew leaders | — | SMS to crew leaders (cross-chat) |
| tom-bot | Heartbeat | 17:30 daily | TEXT summary, CARD to #atlanta-ops, overdue TASK escalation to URGENT | — | CARD posted to #atlanta-ops |
| karen-bot | batch-prep Cron | 17:00 daily | TEXT notification list, CARD with button, NOTE to SLA ledger | **Click "Send Batch Completion Notices" button on Card** | NOTE update to ledger |
| karen-bot | sla-weekly Cron | Friday 17:00 | CARD (SLA weekly report), NOTE to ledger | — | NOTE to ledger |
| beth-bot | exec-weekly Cron | Monday 09:00 | CARD to Beth DM | — | CARD direct to Beth DM |
| finance-bot | subcontractor-payment Cron | Thursday 15:00 | CARD to Alex | **Click "[Approve Payment]" on Card** | CARD to Alex DM |
| finance-bot | lowe's-payment-reconciliation Cron | 5th of month | CARD to Alex, NOTE to ledger | **Click "[Send Collection SMS to Lowe's coordinator]" on Card (if needed)** | CARD to Alex DM, NOTE to ledger |
| finance-bot | month-end-close Cron | 28th of month | Steps 1, 3, 5 fully automatic; Steps 2, 4 push Cards | **Click approval buttons on 2 Cards** | CARD to #exec, CARD to Beth DM |
| hr-bot | seasonal-crew-scaling Cron | Feb 1 annually | MESSAGE to all store managers, NOTE ledger init | — | MESSAGE to each store (cross-chat) |
| hr-bot | quarterly-training Cron | Quarterly (1st) | EVENT creation, SMS to non-compliant employees | — | SMS to employees (cross-chat) |
| hr-bot | Heartbeat (optional) | Linda-configured | NOTE to training ledger, CARD to Linda DM | — | CARD to Linda DM |

### Key distinctions

**Auto (no human needed):** The Cron or Heartbeat fires, the agent executes the ACTION blocks, and the result appears in the platform without anyone clicking anything.

**User trigger required:** A human must click a button on a Card (or issue a manual command) before the critical final action (e.g., SMS notification dispatch, payment approval) executes. This one-step confirmation is intentional for irreversible or high-stakes operations.

**Cross-chat:** The ACTION targets a chat or DM other than the one the Cron/Heartbeat is running in. This is now permitted by default for Cron (MESSAGE, NOTE, TASK, CARD, SMS) and Heartbeat (MESSAGE, NOTE, TASK, CARD — no SMS).

---

## What Changed From the Previous Version

| Behavior | Before | After |
|----------|--------|-------|
| Cron-triggered ACTION blocks | Not executed — posted as plain text | Executed: MESSAGE, NOTE, TASK, CARD, SMS run against the RC API |
| Heartbeat-triggered ACTION blocks | Not executed — posted as plain text | Executed: MESSAGE, NOTE, TASK, CARD run against the RC API |
| SMS from Heartbeat | N/A (blocked) | Still not allowed from Heartbeat |
| SMS batch dispatch from Cron | N/A (blocked) | Still not in default allowlist — requires button press or /lowes-batch |
| VIDEO / PHONE_CALL from Cron or Heartbeat | N/A (blocked) | Requires whitelist opt-in; not default |
| Tom's 17:30 summary | Plain text in #atlanta-ops | TEXT + CARD + auto TASK escalation |
| Karen's 17:00 batch prep | Plain text notification list | TEXT + CARD with button (SMS notification dispatch still needs one tap) |
| Beth's Monday report | Plain text paragraph in DM | Structured CARD direct to Beth DM |
| Alex's month-end close | Manual 5-step process over 3 days | Cron runs all 5 steps; Alex clicks 2 approval buttons |
| Linda's quarterly training | Text reminder, manual scheduling | Auto EVENT creation + SMS to non-compliant employees |
