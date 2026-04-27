---
title: Cron Jobs
---

# Cron (Scheduled Tasks)

Schedule recurring or one-shot tasks that send messages to AI agents on a timer:

```
/cron add "standup" every:24h "summarize yesterday's work"
/cron add "check" */30 * * * * "check for new PRs"
/cron add "reminder" at:2026-04-01T09:00:00 "sprint review today"
/cron list
/cron enable <id>
/cron disable <id>
/cron delete <id>
```

## Schedule Formats

| Format | Example | Description |
|--------|---------|-------------|
| `every:<duration>` | `every:5m`, `every:24h` | Fixed interval |
| Cron expression | `*/30 * * * *`, `0 9 * * 1-5` | Standard 5-field cron |
| `at:<time>` | `at:2026-04-01T09:00:00` | One-shot (auto-disables after run) |

## Commands

| Command | Description |
|---------|-------------|
| `/cron add "<name>" <schedule> "<message>"` | Create a new scheduled task |
| `/cron list` | List all cron jobs |
| `/cron enable <id>` | Enable a disabled job |
| `/cron disable <id>` | Disable a job without deleting |
| `/cron delete <id>` | Delete a job permanently |

## Persistence

Jobs are persisted to `~/.ringclaw/cron/jobs.json` and survive restarts. Each job can optionally target a specific agent or chat.

## Security Notes

- **`/cron add` is a privileged command.** It is subject to the same
  owner gate as `/cwd` and `/new`. See
  [Security › Command Authorization](../security/command-authorization.md)
  for where it is allowed.
- **Cron-triggered agent replies do NOT execute `ACTION:` blocks.**
  The scheduler posts the reply text verbatim — if the agent's output
  contains an `ACTION: NOTE/TASK/EVENT/CARD/MESSAGE` block, it lands
  in the chat as plain text and is not dispatched against the RC API.
  This is intentional: scheduled jobs have no human sender, so the
  Layer 2 cross-chat gate cannot distinguish owner from non-owner.
  If you want a cron job to actually create a task/note, have the job
  prompt tell the agent to call `/task create ...` in its output, and
  run the job in a chat whose members are comfortable with that.
