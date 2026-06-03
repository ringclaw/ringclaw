---
skill: daily-digest
title: Daily / Weekly / Monthly Digest Generator
purpose: Generate a structured operational summary on a schedule, pulled from RC data (Tasks, Calls, SMS, Calendar) plus chat history, posted as text to the target chat (cron-compatible — no ACTION blocks).
used_by_templates:
  - store-mgr
  - regional-coordinator
  - exec
  - lowes-liaison
rc_capabilities_used:
  - task (read)
  - call_log (read)
  - note (read)
  - memory
  - cron
trigger: Scheduled (cron). Also activatable on-demand by "show me today's digest" or "give me the weekly summary".
---

# Skill · Daily / Weekly / Monthly Digest Generator

## When I activate

- Scheduled via the bot's cron config (per role template, see defaults).
- On-demand when owner types *"@bot today's digest"* / *"this week's
  summary"* / *"May report"*.

## Step-by-step workflow

1. **Determine the time window** from the trigger:
   - Cron: window = since last digest run (typically 24h / 7d / 1mo).
   - On-demand: window = parsed from owner request (default 24h).
2. **Pull data** in parallel:
   - Tasks created / closed / open in this window (via task API).
   - Call log: total, missed, by direction (via call_log API).
   - SMS sent / received counts (via memory + sms API where allowed).
   - Per-chat memory: status flags, watchlist items, manual notes.
   - For weekly/monthly: prior-period numbers for delta calculations.
3. **Apply role-specific aggregation**:
   - `store-mgr`: by store; flag delays, crew gaps, stuck tasks.
   - `regional-coordinator`: by store, then aggregate by region;
     identify cross-store opportunities.
   - `exec`: by region, then total; highlight ranked top/bottom +
     watchlist.
   - `lowes-liaison`: by store, focused on fax SLA, retry queue,
     compliance ledger.
4. **Compose the digest text** using the role's template (see below).
   Lead with the most-actionable line.
5. **Post to the target chat** via `SendPost` (no ACTION blocks — this
   is cron-safe).
6. **Update memory**: write the digest content + archive URL to per-chat
   memory for future delta comparisons.

## Default templates (per role)

### `store-mgr` daily digest

```
[Cron: {store} Daily · {date}]
Today: {n} installs completed; {n} delayed.
  - #{order} — {reason}
Tomorrow: {n} booked; {n} confirmed.
Crew gap: {gap-summary or "none"}.
Top stuck task >24h: #{task} ({summary}).
📎 Archive: {url}
```

### `regional-coordinator` morning digest

```
[Cron: {region} Morning · {date}]
Yesterday: {n} installs across {n} stores ({pct vs target}).

Today's crew gaps:
  ⚠ {store}: -{n} {role} ({material-specialty if relevant})

Today's surplus:
  ✅ {store}: +{n} ({constraints})

Watchlist:
  {store} — {pattern reason}
📎 Archive: {url}
```

### `exec` weekly snapshot

```
[Cron: Weekly Snapshot · {iso-week}]
📊 {n} stores this week
Installs: {n} ({% vs prior})
CSAT: {score}/5 ({pp delta})
Lowe's SLA: {pct}% ({pp delta})
Crew gap incidents: {n} ({delta})

Top 3 by volume:
  1. {store} — {n}
  2. {store} — {n}
  3. {store} — {n}

⚠ Watchlist:
  • {store} — {pattern} (week {n} consecutive)
📎 Drill-down: {url}
```

### `lowes-liaison` Friday SLA digest

```
[Cron: Lowe's Weekly SLA · {iso-week}]
Faxes sent: {n}
On-time delivery: {n} / {pct}% (target: ≥95%)
Failed: {n} (retry queue: {n})

Top performers:
  {store}: {n} ({pct}%)
  {store}: {n} ({pct}%)

Watch:
  {store}: {pct}% (below target)
📎 Ledger: {url}
```

## Memory hooks

- **Per-chat (target chat)**: append the digest text + archive URL each
  run. Used for delta calculation in subsequent runs.
- **Global (per-role bot)**: maintain a rolling 90-day stats summary so
  monthly/quarterly comparisons are fast.

## Cron safety considerations

The bot framework documents that **cron-triggered Agent replies do NOT
execute `ACTION:` blocks** (see `docs/zh/features/cron.md`). This skill
must therefore:

- Output plain text only. Never emit `ACTION:NOTE`, `ACTION:CARD`,
  `ACTION:TASK`, etc. in cron path.
- If a card-style render is desired, the owner can run the on-demand
  variant of this skill from chat — that's a human-triggered path and
  ACTION blocks execute normally.

## Failure handling

| Failure | Behavior |
|---------|----------|
| Task API timeout | Render the digest with available data + a "⚠ Partial: task data unavailable" footer |
| Call log API 429 | Backoff once 30s, then proceed with cached counts |
| Memory write failure | Log + still post (next digest's delta will be a one-time hiccup) |
| No data in window | Post a brief "Nothing to report" + the digest archive URL for completeness |

## Usage scenarios

1. **Atlanta store mgr's 17:30 daily** — Case 4 (existing scenarios).
2. **West region coord's 8:00 morning** — Case 5 (existing).
3. **Beth's Monday 9:00 weekly snapshot** — Case 7 (existing).
4. **Karen's Friday Lowe's SLA** — Case 5b (existing).
5. **On-demand drill-down**: Beth asks "@bot show me Atlanta's last 30
   days of crew gaps" → bot generates a one-off digest covering that
   window only.

## SOUL interactions

Each role's SOUL writes the prompt that this skill's templates pull
from. The skill is the *machine*; the SOUL is the *editorial voice*.
Two different exec assistants with different owners can use the same
skill and produce digests that read very differently.
