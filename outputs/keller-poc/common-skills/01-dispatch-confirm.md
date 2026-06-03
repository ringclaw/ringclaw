---
skill: dispatch-confirm
title: Dispatch & Confirmation Loop
purpose: Turn "assign X to person Y at time Z" into a tracked Task + outbound SMS + auto-escalation if the assignee doesn't confirm.
used_by_templates:
  - csr
  - store-mgr
  - lowes-liaison
  - hr
rc_capabilities_used:
  - task
  - sms
  - directory
  - memory
trigger: User says any variant of "dispatch / assign / schedule / send / route X to Y for Z"
---

# Skill · Dispatch & Confirmation Loop

## When I activate

The owner gives me an assignment that has:
- a unit of work (order #, install, repair, errand)
- an assignee (name or group, e.g. *"Mike"*, *"Carlos's crew"*)
- a time window (*"tomorrow 10am"*, *"by Friday EOD"*)

I activate this skill whenever a message contains an assignment + an
assignee. I do NOT activate if the message lacks an assignee (then I ask
*"who?"* instead).

## Step-by-step workflow

1. **Parse** the assignment. Extract: `work_unit`, `assignee_hint`,
   `due_time`, `optional_notes`.
2. **Resolve assignee** via the directory: if `assignee_hint` matches
   exactly one contact, use it; if multiple, ask owner to pick; if none,
   refuse and ask for a phone number explicitly.
3. **Pre-flight checks** (skill-specific guards from SOUL):
   - Address sanity (ZIP matches city, owner confirms typo if mismatch).
   - Crew lead availability (if a calendar event exists for that crew on
     that day, surface the conflict).
4. **Create the Task** via `ACTION:TASK` with assignee, due, work-unit
   reference, and any notes.
5. **Send the assignment SMS** via `ACTION:SMS to=<resolved phone>` with
   the standard dispatch template (see below). Always include
   *"Reply CONFIRM"* as the last instruction.
6. **Schedule a follow-up cron** (one-shot `at:` timer set 30 min from
   now): *if no CONFIRM reply has arrived, post an escalation message to
   the relevant ops chat tagging the role's escalation target* (CSR
   escalates to store mgr; store mgr escalates to regional coord;
   Lowe's liaison escalates to Beth DM).
7. **Confirm to owner** in the original chat: one line with Task ID, SMS
   recipient phone, and escalation target.

## SMS template (default — overridable per template)

```
Install #{work_unit} {date} {time_window}.
Address: {address}
Material: {material}, {sqft} sqft
Customer: {customer_name}, {customer_phone}
Reply CONFIRM to acknowledge.
```

For non-install assignments (e.g. follow-up call, paperwork):

```
{role-verb} #{work_unit} due {due_time}.
Detail: {notes}
Reply CONFIRM to acknowledge.
```

## Confirmation handling

When an inbound SMS arrives with body matching `^CONFIRM` (case-insensitive)
from a known assignee phone:

1. Match it to the open dispatch in per-chat memory (most recent open
   assignment to that phone).
2. Mark the Task as `acknowledged=true` via `PATCH /api/tasks/{id}`.
3. Cancel the 30-min escalation cron via `/cron disable {auto-id}`.
4. Post a one-line "✅ {assignee} confirmed #{work_unit}" to the chat.

When 30 min passes with no CONFIRM:

1. The cron fires and the escalation message goes to the chat with the
   escalation target tagged.
2. The skill stays in "awaiting confirmation" state for up to 2 hours
   total before the dispatch is marked stale and the owner is asked
   what to do (`reassign` / `manual-call` / `give-up`).

## Memory hooks

- **Per-chat**: record `{work_unit_id, assignee_phone, dispatched_at,
  status}`. This is what the confirmation matcher reads.
- **Per-user (owner)**: increment dispatch counter for the day. Surface
  in the daily digest.
- **Per-user (assignee)**: track typical confirm-time. Surface in monthly
  review.

## Failure handling

| Failure | Skill behavior |
|---------|----------------|
| Assignee not in directory | Refuse, ask for explicit phone |
| Multiple matches | List candidates, ask owner to pick |
| Address ZIP mismatch | Refuse, surface both candidate addresses |
| SMS API error (4xx) | Surface error, escalate to owner DM with full payload |
| SMS API error (5xx) | Retry once after 30s, then surface |
| Owner-only resource (no permission) | Refuse and explain |

## Usage scenarios (where this skill earns its keep)

1. **CSR dispatching install jobs** — Case 1. ~20-30/day per CSR. Skill
   compresses 5-min manual flow to 30 sec.
2. **CSR rescheduling install** — Case 2. Same skill, with the extra
   side-effect of cascading the update to the crew lead's SMS.
3. **Store mgr assigning store-level errands** ("Tom, please pick up
   the supply order at Lowe's #4421") — same skill, no install context.
4. **HR routing PTO approval** — Case 6. HR uses dispatch-confirm to
   send the approval request to the crew lead and track the decision.
5. **Lowe's liaison forwarding a quality re-inspection** — Case 8. Uses
   dispatch-confirm against the relevant store mgr's bot to track the
   re-inspection assignment.

## SOUL interactions

Each SOUL refines the skill's behavior:

- **csr SOUL** sets the customer-facing tone for the dispatch SMS (no
  internal jargon).
- **store-mgr SOUL** sets escalation target = regional coord.
- **lowes-liaison SOUL** sets escalation target = Beth DM and adds a
  contract-language preamble to the SMS.
- **hr SOUL** anonymizes the assignment in any chat broadcast (the
  crew lead DM has the name; the chat broadcast does not).
