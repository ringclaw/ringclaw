---
skill: complaint-handling
title: Customer Complaint Detection + De-escalation Loop
purpose: Detect a complaint signal in inbound communication (SMS, chat), send an immediate empathetic acknowledgment in the right voice, escalate to the right owner, and track resolution end-to-end.
used_by_templates:
  - csr
  - store-mgr
  - lowes-liaison
  - exec
rc_capabilities_used:
  - sms (inbound monitoring + outbound)
  - call_log (read, for context)
  - task
  - cross-chat (with OOB approval)
  - memory
trigger: An inbound SMS or chat message containing complaint signals (see Detection rules).
---

# Skill · Customer Complaint Handling

## When I activate

I activate when any **inbound** communication (SMS or chat post) matches
the complaint signal set:

- Explicit complaint words: *"complaint"*, *"refund"*, *"unacceptable"*,
  *"worst"*, *"terrible"*, *"no-show"*, *"didn't show up"*, *"lawsuit"*,
  *"Better Business Bureau"*, *"BBB"*, *"social media"*, *"review"*.
- Emotional punctuation: 3+ exclamation marks, ALL CAPS phrases of 3+
  words, *"!!!"* / *"???"*.
- Hostile phrasing: *"this is ridiculous"*, *"never again"*, *"won't
  recommend"*.
- Mentions of regulatory or partner entities: *"Lowe's"* in customer
  inbound (always escalate to lowes-liaison context).

I also activate when the owner asks me to *"handle complaint about X"*
in a chat post.

## Step-by-step workflow

1. **Detect** the complaint signal and capture the verbatim quote +
   sender identity + relevant order# (best-effort match from per-chat
   memory).
2. **Immediate acknowledgment** to the complainant (≤ 60 seconds from
   detection). Voice depends on the activating SOUL:
   - `csr` voice: warm, apologetic, action-oriented.
   - `store-mgr` voice: brief, action-oriented (when complaint comes
     internally).
   - `lowes-liaison` voice: contractual when complaint involves Lowe's.
3. **Escalation post** to the right ops chat (per role escalation
   matrix below). Always include verbatim quote (not a summary).
4. **Investigation prompt** to the owner: a one-line ask
   ("Tom — check #A8810 dispatch log; Mike's crew was on this. Address
   correct?").
5. **Track resolution** by creating a `Task` in the relevant ops chat
   with `assignee=owner`, `due=+2h`, `priority=urgent`.
6. **Follow-up SMS** to the complainant once the owner has surfaced the
   resolution; reuse the SOUL's complaint-resolution template.
7. **Audit trail**: append a complaint-ledger entry to the per-chat
   memory of the ops chat, with timestamp, verbatim quote, resolution,
   and SLA hit/miss.

## Escalation matrix (per role)

| Activating bot | Immediate ack voice | Escalates to | Resolution owner |
|----------------|---------------------|--------------|------------------|
| `csr` | warm + apology | store mgr's bot via #<store>-ops | store mgr |
| `store-mgr` | brief + action | regional coord via #<region>-coord | store mgr (if internal); regional (if cross-store) |
| `lowes-liaison` | contractual + receipt | Beth DM + relevant store mgr | Beth + store mgr |
| `exec` | acknowledge + delegate | relevant store mgr | store mgr |

## Voice templates

### csr immediate acknowledgment (to complainant SMS)

```
Hi {first-name}! I'm so sorry about this. I'm escalating to our manager right now.
You'll get a call back within {sla-minutes} min. We take this seriously.
```

### csr resolution follow-up

```
Hi {first-name}! Quick update on your issue: {one-line-resolution}.
We've credited your account ${amount} for the trouble.
Track: keller.com/track/{order}
```

### store-mgr internal post (#<store>-ops)

```
🚨 URGENT — customer {customer} (#{order}, {phone}) reports {one-line-issue}.
Verbatim: "{quote}"
Auto-reply sent. SLA window: {sla-minutes} min.
👉 Action required: {one-line-suggested-next-step}.
```

### lowes-liaison Lowe's-touched complaint

```
[Lowe's-touched complaint · #{order}]
Customer: {customer}
Source: {customer-channel}
Verbatim: "{quote}"
Lowe's SLA implication: {yes/no — based on SOP cross-reference}
👉 @{store-mgr-bot} for resolution. CC: @{beth-bot}.
```

## Investigation playbook (the bot's "first 5 minutes")

The bot performs these checks automatically while waiting for the owner:

1. Pull dispatch record for the cited order #.
2. Check call log for outbound calls from the assigned crew on that day.
3. Check SMS history with the customer's phone in the last 72h.
4. Cross-reference address (Google Maps API integration if available;
   otherwise flag ZIP mismatch).
5. Surface findings in the escalation post as bullet points.

This is the differentiator vs a generic chatbot: the bot is *already
investigating* by the time the owner reads the alert.

## Memory hooks

- **Per-chat (ops chat)**: complaint ledger entry — `{date, customer,
  order, verbatim, resolution, sla_hit}`.
- **Per-user (owner)**: complaint volume counter + SLA hit rate.
- **Global**: total complaint volume across all stores (read by exec
  bot for weekly digest).

## Resolution SLA

| Complaint severity | Acknowledgment | Resolution start | Resolution close |
|--------------------|----------------|------------------|------------------|
| Standard | 60 sec | 15 min | 24h |
| Lowe's-touched | 60 sec | 15 min | 4h (per Lowe's contract) |
| Public-facing threat (BBB, social media, lawsuit) | 60 sec | 5 min | 2h with executive update |

The SLA is enforced by an at-time cron created when the complaint is
opened. If the resolution-close cron fires with no `close` event in
memory, the bot escalates one level up.

## Failure handling

| Failure | Behavior |
|---------|----------|
| Owner doesn't acknowledge escalation within 5 min | Bot pings owner DM with the escalation post |
| Resolution close not reached by SLA | Auto-escalate to next level (csr → store-mgr → regional → exec) |
| Customer SMS inbound after auto-ack but before resolution | Append to thread, surface to owner |
| Multiple complaints from same customer within 24h | Flag as recurring, escalate to store mgr regardless of severity |

## Usage scenarios

1. **Case 7 in the case catalog** — customer SMS *"crew didn't show up
   #A8810 worst service ever!!!"* → CSR bot acknowledges with warmth →
   store mgr bot investigates address → CSR bot apologizes + $50 credit
   → store mgr fixes dispatch.
2. **Lowe's HQ quality flag turning into a complaint** — Case 8 plus
   the customer also calls in. Lowe's liaison and CSR both activate
   this skill; their outputs merge into one resolution loop.
3. **Repeat-offender detection** — same phone complains 3 times in a
   month → bot proactively offers Beth a pattern report (handled by
   the exec daily-digest skill).

## SOUL interactions

- This skill's SOP is fixed; SOUL changes only the **voice templates**
  and the **resolution authority** (which owner approves credits, who
  signs off on Lowe's escalations, etc.).
- The skill's escalation matrix is read from the activating bot's SOUL
  config, so an HR bot would not activate this skill (no escalation
  matrix entry).
- The investigation playbook is shared (same 5 checks) across all
  activations — that's a deliberate consistency choice for audit
  reasons.
