---
title: Background Mode
---

# Background Mode

```bash
# Start (runs in background by default)
ringclaw start

# Check if running
ringclaw status

# Stop
ringclaw stop

# Run in foreground (for debugging)
ringclaw start -f
```

Logs are written to `~/.ringclaw/ringclaw.log`.

## System Service (Auto-start on Boot)

### macOS (launchd)

```bash
cp service/com.ringclaw.ringclaw.plist ~/Library/LaunchAgents/
launchctl load ~/Library/LaunchAgents/com.ringclaw.ringclaw.plist
```

### Linux (systemd)

```bash
sudo cp service/ringclaw.service /etc/systemd/system/
sudo systemctl enable --now ringclaw
```
