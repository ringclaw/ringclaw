---
title: Heartbeat
---

# Heartbeat

Periodic agent check-in driven by a user-authored checklist. Create `~/.ringclaw/HEARTBEAT.md`:

```markdown
# Heartbeat checklist
- Check if any urgent emails arrived
- Scan for open PRs needing review
- Check CI pipeline status
```

## How It Works

RingClaw reads this file on each heartbeat interval, sends it to the default agent, and:
- Agent replies `HEARTBEAT_OK` → suppressed (nothing to report)
- Agent replies with content → delivered to the default chat with `[Heartbeat]` prefix
- Duplicate replies within 24h are suppressed to avoid noise

## Configuration

```json
{
  "heartbeat": {
    "enabled": true,
    "interval": "30m",
    "active_hours": "09:00-18:00",
    "timezone": "Asia/Shanghai"
  }
}
```

## Config Options

| Option | Default | Description |
|--------|---------|-------------|
| `enabled` | `false` | Enable heartbeat runner |
| `interval` | `30m` | Time between heartbeat checks |
| `active_hours` | — | Only run during these hours (e.g. `09:00-18:00`) |
| `timezone` | local | IANA timezone for active hours |
