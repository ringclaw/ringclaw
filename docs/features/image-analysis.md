---
title: Image Analysis
---

# Image Analysis

Send images in chat and the AI agent will analyze them. RingClaw downloads image attachments from incoming messages and passes them to the AI agent via the [ACP protocol](https://agentclientprotocol.com/protocol/content#image-content).

## How It Works

1. User sends a message with image attachments (e.g. "What's in this image?")
2. RingClaw downloads images via authenticated `contentUri` (max 5 images, max 5MB each)
3. For ACP agents: images are base64-encoded and sent as `ContentBlock::Image` in the prompt
4. For CLI/HTTP agents: a text-only fallback note is appended

Supported formats: **PNG**, **JPEG**, **GIF**, **WebP**.

## Agent Compatibility

Image analysis requires the agent to support the ACP `promptCapabilities.image` capability.

| Agent | Image Support | Notes |
|-------|:---:|-------|
| Factory Droid | ✅ Tested | ACP with `promptCapabilities.image` |
| Claude (ACP) | ✅ Tested | Requires `claude-agent-acp` ([see below](#switching-claude-to-acp-mode)) |
| Gemini (ACP) | 🔷 Expected | Gemini models are multi-modal |
| Cursor (ACP) | 🔷 Expected | Depends on underlying model |
| GitHub Copilot (ACP) | 🔷 Expected | GPT-4o is multi-modal |
| Cline (ACP) | 🔷 Expected | Cline supports image input |
| Claude (CLI) | ❌ No | CLI mode has no image flag |
| Codex (CLI) | ❌ No | CLI mode, text-only fallback |
| HTTP agents | ❌ No | Text-only fallback |

**Legend:**
- ✅ Tested — verified working
- 🔷 Expected — the underlying model supports images, but not yet tested with RingClaw
- ❌ No — the agent type does not support image input

Agents that don't support images will receive the text message with a note like:
```
[Note: 1 image(s) were attached but this agent does not support image input.]
```

## Switching Claude to ACP Mode

By default, RingClaw uses the `claude` CLI binary. To enable image support, install the ACP wrapper:

```bash
npm install -g @agentclientprotocol/claude-agent-acp
ringclaw restart
```

RingClaw auto-detects `claude-agent-acp` on startup and upgrades from CLI to ACP automatically. No manual config editing needed.

You can verify the upgrade in the logs:
```
INF upgrading agent to ACP component=config name=claude from=cli path=/.../claude-agent-acp
INF agent available component=agents name=claude type=acp
```

## Configuration

No additional configuration is required. Image analysis works automatically when:

1. The message contains image attachments
2. The active agent implements the `ImageSupporter` interface (all ACP agents do)

The `config.json` agent entry just needs `"type": "acp"`:

```json
{
  "agents": {
    "claude": {
      "type": "acp",
      "command": "/path/to/claude-agent-acp",
      "model": "sonnet"
    }
  }
}
```
